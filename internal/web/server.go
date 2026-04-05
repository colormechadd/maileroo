package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/a-h/templ"
	"github.com/colormechadd/maileroo/internal/config"
	"github.com/colormechadd/maileroo/internal/db"
	"github.com/colormechadd/maileroo/internal/mail"
	"github.com/colormechadd/maileroo/internal/outbound"
	"github.com/colormechadd/maileroo/internal/rspamd"
	"github.com/colormechadd/maileroo/internal/storage"
	"github.com/colormechadd/maileroo/pkg/auth"
	"github.com/colormechadd/maileroo/pkg/models"
	"github.com/colormechadd/maileroo/templates"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/gorilla/csrf"
	"golang.org/x/time/rate"
)

type Server struct {
	ServerConfig

	loginMu       sync.Mutex
	loginLimiters map[string]*loginEntry
}

type loginEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func (s *Server) loginLimiter(ip string) *rate.Limiter {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	if e, ok := s.loginLimiters[ip]; ok {
		e.lastSeen = time.Now()
		return e.limiter
	}
	e := &loginEntry{
		limiter:  rate.NewLimiter(rate.Every(time.Minute), 5),
		lastSeen: time.Now(),
	}
	s.loginLimiters[ip] = e
	return e.limiter
}

func (s *Server) cleanupLoginLimiters() {
	for {
		time.Sleep(5 * time.Minute)
		s.loginMu.Lock()
		for ip, e := range s.loginLimiters {
			if time.Since(e.lastSeen) > time.Hour {
				delete(s.loginLimiters, ip)
			}
		}
		s.loginMu.Unlock()
	}
}

type ServerConfig struct {
	Config      config.Config
	DB          db.WebDB
	RateLimitDB db.RateLimitDB
	Storage     storage.Storage
	Hub         *Hub
	Sender      outbound.Sender
	Mail        *mail.Service
	Rspamd      *rspamd.Client
}

func NewServer(cfg ServerConfig) *Server {
	s := &Server{
		ServerConfig:  cfg,
		loginLimiters: make(map[string]*loginEntry),
	}
	go s.cleanupLoginLimiters()
	return s
}

func (s *Server) Routes() http.Handler {
	csrfKey, err := base64.StdEncoding.DecodeString(s.Config.Web.CSRFAuthKey)
	if err != nil || len(csrfKey) != 32 {
		panic("WEB_CSRF_AUTH_KEY must be a base64-encoded 32-byte key")
	}
	csrfMiddleware := csrf.Protect(
		csrfKey,
		csrf.Secure(true),
		csrf.RequestHeader("X-CSRF-Token"),
		csrf.FieldName("gorilla.csrf.Token"),
	)

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	if s.Config.Web.TrustProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)

	fs := http.FileServer(http.Dir("static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fs))

	r.Get("/login", s.handleLoginGet)
	r.Post("/login", s.handleLoginPost)
	r.Post("/logout", s.handleLogout)

	r.Group(func(r chi.Router) {
		r.Use(s.AuthMiddleware)
		r.Get("/", s.handleDashboard)
		r.Get("/events", s.handleEvents)
		r.Get("/mailbox/{mailboxID}", s.handleMailboxView)
		r.Get("/mailbox/{mailboxID}/search", s.handleMailboxSearch)
		r.Group(func(r chi.Router) {
			r.Use(s.validateUserAccessToEmailID)

			r.Get("/email/{emailID}", s.handleEmailView)
			r.Get("/email/{emailID}/headers", s.handleEmailHeaders)
			r.Get("/email/{emailID}/pipeline", s.handleEmailPipeline)
			r.Post("/email/{emailID}/star", s.handleEmailStar)
			r.Post("/email/{emailID}/delete", s.handleEmailDelete)
			r.Post("/email/{emailID}/release", s.handleEmailRelease)
			r.Post("/email/{emailID}/mark-spam", s.handleEmailMarkSpam)
			r.Post("/email/{emailID}/mark-ham", s.handleEmailMarkHam)
		})
		r.Get("/attachment/{attachmentID}", s.handleAttachmentDownload)

		r.Get("/compose", s.handleCompose)
		r.Post("/send", s.handleEmailSend)

		r.Get("/user-info", s.handleUserInfo)
		r.Post("/user/sending-address/{saID}/display-name", s.handleUpdateDisplayName)

		r.Post("/draft", s.handleDraftSave)
		r.Delete("/draft/{draftID}", s.handleDraftDelete)

		r.Get("/contacts", s.handleContactsPage)
		r.Get("/contacts/search", s.handleContactSearch)
		r.Post("/contacts", s.handleContactCreate)
		r.Put("/contacts/{contactID}", s.handleContactUpdate)
		r.Delete("/contacts/{contactID}", s.handleContactDelete)
		r.Post("/contacts/{contactID}/favorite", s.handleContactToggleFavorite)
		r.Post("/email/{emailID}/add-contact", s.handleAddContactFromEmail)
	})

	return csrfMiddleware(r)
}

func (s *Server) handleCompose(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	addresses, err := s.DB.GetActiveSendingAddresses(r.Context(), user.ID)
	if err != nil {
		slog.Error("failed to fetch sending addresses", "user_id", user.ID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	fromID := ""
	to := ""
	cc := ""
	bcc := ""
	subject := ""
	inReplyTo := ""
	references := ""
	title := "New Message"
	body := ""
	bodyHTML := ""
	draftID := ""

	// Pre-fill To from query param (e.g. from contacts page)
	if toParam := r.URL.Query().Get("to"); toParam != "" {
		to = toParam
	}

	// Resume a saved draft
	draftIDRaw := r.URL.Query().Get("draft")
	if draftIDRaw != "" {
		draftUUID, err := uuid.Parse(draftIDRaw)
		if err == nil {
			draft, err := s.DB.GetDraftByIDForUser(r.Context(), draftUUID, user.ID)
			if err == nil {
				draftID = draft.ID.String()
				title = "Draft"
				to = draft.ToAddress
				cc = draft.CcAddress
				bcc = draft.BccAddress
				subject = draft.Subject
				body = draft.Body
				bodyHTML = draft.BodyHTML
				if draft.InReplyTo != nil {
					inReplyTo = *draft.InReplyTo
				}
				if draft.References != nil {
					references = *draft.References
				}
				if draft.SendingAddressID != nil {
					fromID = draft.SendingAddressID.String()
				}
			}
		}
	}

	replyToIDRaw := r.URL.Query().Get("replyTo")
	if replyToIDRaw != "" {
		replyToID, err := uuid.Parse(replyToIDRaw)
		if err == nil {
			orig, err := s.DB.GetEmailByIDForUser(r.Context(), replyToID, user.ID)
			if err == nil {
				title = "Reply"
				to = orig.FromAddress
				subject = orig.Subject
				if !strings.HasPrefix(strings.ToLower(subject), "re:") {
					subject = "Re: " + subject
				}
				inReplyTo = orig.MessageID

				origRefs := ""
				if orig.References != nil {
					origRefs = *orig.References
				}
				references = strings.TrimSpace(origRefs + " " + orig.MessageID)

				ccList, _ := s.Mail.GetCcAddresses(r.Context(), orig)
				if len(ccList) > 0 {
					cc = strings.Join(ccList, ", ")
				}

				for _, addr := range addresses {
					if strings.EqualFold(addr.Address, orig.ToAddress) {
						fromID = addr.ID.String()
						break
					}
				}
			}
		}
	}

	forwardOfIDRaw := r.URL.Query().Get("forwardOf")
	if forwardOfIDRaw != "" {
		forwardID, err := uuid.Parse(forwardOfIDRaw)
		if err == nil {
			orig, err := s.DB.GetEmailByIDForUser(r.Context(), forwardID, user.ID)
			if err == nil {
				title = "Forward"
				subject = orig.Subject
				if !strings.HasPrefix(strings.ToLower(subject), "fwd:") {
					subject = "Fwd: " + subject
				}

				dateStr := orig.ReceiveDatetime.Format("Jan 02, 2006, 15:04")
				origBody, origIsHTML, _ := s.Mail.FetchBody(r.Context(), orig)

				if origIsHTML {
					headerHTML := fmt.Sprintf(
						"<strong>---------- Forwarded message ----------</strong><br>From: %s<br>Date: %s<br>Subject: %s<br>To: %s<br><br>",
						html.EscapeString(orig.FromAddress),
						html.EscapeString(dateStr),
						html.EscapeString(orig.Subject),
						html.EscapeString(orig.ToAddress),
					)
					bodyHTML = fmt.Sprintf(
						`<br><br><blockquote style="border-left:2px solid #ccc;margin:0;padding:0 0 0 0.75em">%s%s</blockquote>`,
						headerHTML, origBody,
					)
				} else {
					body = fmt.Sprintf(
						"\n\n---------- Forwarded message ----------\nFrom: %s\nDate: %s\nSubject: %s\nTo: %s\n\n%s",
						orig.FromAddress, dateStr, orig.Subject, orig.ToAddress, origBody,
					)
				}

				for _, addr := range addresses {
					if strings.EqualFold(addr.Address, orig.ToAddress) {
						fromID = addr.ID.String()
						break
					}
				}
			}
		}
	}

	mailboxes, _ := s.DB.GetMailboxesByUserID(r.Context(), user.ID)
	counts := s.getCounts(r.Context(), uuid.Nil, user.ID) // No specific mailbox context here
	s.render(w, r, user, mailboxes, uuid.Nil, "all", counts, templates.Compose(addresses, fromID, to, cc, bcc, subject, inReplyTo, references, draftID, title, body, bodyHTML))
}

func (s *Server) validateUserAccessToEmailID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := r.Context().Value("user").(*models.User)
		emailID, err := uuid.Parse(chi.URLParam(r, "emailID"))
		if err != nil {
			http.Error(w, "Invalid ID", http.StatusBadRequest)
			return
		}
		if _, err := s.DB.GetEmailByIDForUser(r.Context(), emailID, user.ID); err != nil {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleEmailSend(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)

	if err := r.ParseMultipartForm(50 * 1024 * 1024); err != nil {
		slog.Error("failed to parse multipart form", "error", err)
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	fromIDRaw := r.FormValue("from_id")
	toRaw := r.FormValue("to")
	ccRaw := r.FormValue("cc")
	bccRaw := r.FormValue("bcc")
	subject := r.FormValue("subject")
	body := r.FormValue("body")
	bodyHTML := r.FormValue("body_html")
	inReplyTo := r.FormValue("in_reply_to")
	references := r.FormValue("references")

	fromID, err := uuid.Parse(fromIDRaw)
	if err != nil {
		http.Error(w, "Invalid From ID", http.StatusBadRequest)
		return
	}

	sa, err := s.DB.GetSendingAddressByID(r.Context(), fromID, user.ID)
	if err != nil {
		slog.Warn("unauthorized sending attempt", "user_id", user.ID, "from_id", fromID, "error", err)
		http.Error(w, "Unauthorized from address", http.StatusForbidden)
		return
	}

	to := parseAddresses(toRaw)
	cc := parseAddresses(ccRaw)
	bcc := parseAddresses(bccRaw)

	fromDisplayName := ""
	if sa.DisplayName != nil {
		fromDisplayName = *sa.DisplayName
	}

	outMsg := outbound.Message{
		From:            sa.Address,
		FromDisplayName: fromDisplayName,
		To:              to,
		Cc:              cc,
		Bcc:             bcc,
		Subject:         subject,
		TextBody:        body,
		HTMLBody:        bodyHTML,
		InReplyTo:       inReplyTo,
		References:      references,
	}

	files := r.MultipartForm.File["attachments"]
	for _, fileHeader := range files {
		f, err := fileHeader.Open()
		if err != nil {
			slog.Error("failed to open attachment", "filename", fileHeader.Filename, "error", err)
			continue
		}
		outMsg.Attachments = append(outMsg.Attachments, outbound.Attachment{
			Filename:    fileHeader.Filename,
			ContentType: fileHeader.Header.Get("Content-Type"),
			Content:     f,
		})
	}

	if s.Config.RateLimit.OutboundPerUserHour > 0 {
		count, err := s.RateLimitDB.CountOutboundByUserHour(r.Context(), user.ID)
		if err != nil {
			slog.Error("failed to check outbound rate limit", "user_id", user.ID, "error", err)
			http.Error(w, "Failed to check rate limit", http.StatusInternalServerError)
			return
		}
		if count >= s.Config.RateLimit.OutboundPerUserHour {
			http.Error(w, "Hourly sending limit reached, please try again later", http.StatusTooManyRequests)
			return
		}
	}

	rawBytes, from, recipients, err := s.Sender.BuildMessage(outMsg)
	if err != nil {
		slog.Error("failed to build outbound message", "user_id", user.ID, "error", err)
		http.Error(w, "Failed to build email", http.StatusInternalServerError)
		return
	}

	email, err := s.Mail.Persist(r.Context(), mail.PersistOptions{
		MailboxID:        sa.MailboxID,
		RawMessage:       rawBytes,
		IsOutbound:       true,
		UserID:           user.ID,
		SendingAddressID: &sa.ID,
		InReplyTo:        inReplyTo,
		References:       references,
	})
	if err != nil {
		slog.Error("failed to persist outbound email", "user_id", user.ID, "error", err)
		http.Error(w, "Failed to save email", http.StatusInternalServerError)
		return
	}

	if _, err := s.DB.InsertOutboundJob(r.Context(), &email.ID, from, recipients, rawBytes); err != nil {
		slog.Error("failed to enqueue outbound job", "user_id", user.ID, "email_id", email.ID, "error", err)
	}

	if draftIDRaw := r.FormValue("draft_id"); draftIDRaw != "" {
		if draftID, err := uuid.Parse(draftIDRaw); err == nil {
			if err := s.DB.DeleteDraft(r.Context(), draftID, user.ID); err != nil {
				slog.Error("failed to delete draft after send", "draft_id", draftID, "error", err)
			}
		}
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Location", "/mailbox/"+sa.MailboxID.String()+"?filter=sent")
		return
	}
	http.Redirect(w, r, "/mailbox/"+sa.MailboxID.String()+"?filter=sent", http.StatusSeeOther)
}

func (s *Server) handleDraftSave(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	fromIDRaw := r.FormValue("from_id")
	fromID, err := uuid.Parse(fromIDRaw)
	if err != nil {
		http.Error(w, "Invalid from_id", http.StatusBadRequest)
		return
	}

	sa, err := s.DB.GetSendingAddressByID(r.Context(), fromID, user.ID)
	if err != nil {
		http.Error(w, "Unauthorized from address", http.StatusForbidden)
		return
	}

	var inReplyTo *string
	var references *string
	if v := r.FormValue("in_reply_to"); v != "" {
		inReplyTo = &v
	}
	if v := r.FormValue("references"); v != "" {
		references = &v
	}

	draftIDRaw := r.FormValue("draft_id")
	if draftIDRaw != "" {
		draftID, err := uuid.Parse(draftIDRaw)
		if err == nil {
			draft := models.Draft{
				ID:               draftID,
				UserID:           user.ID,
				SendingAddressID: &sa.ID,
				ToAddress:        r.FormValue("to"),
				CcAddress:        r.FormValue("cc"),
				BccAddress:       r.FormValue("bcc"),
				Subject:          r.FormValue("subject"),
				Body:             r.FormValue("body"),
				BodyHTML:         r.FormValue("body_html"),
				InReplyTo:        inReplyTo,
				References:       references,
			}
			if err := s.DB.UpdateDraft(r.Context(), draft); err != nil {
				slog.Error("failed to update draft", "draft_id", draftID, "error", err)
				http.Error(w, "Internal error", http.StatusInternalServerError)
				return
			}
			fmt.Fprintf(w, `Saved<input type="hidden" id="draft-id-input" name="draft_id" value="%s" hx-swap-oob="true">`, draftID)
			return
		}
	}

	draft := models.Draft{
		MailboxID:        sa.MailboxID,
		UserID:           user.ID,
		SendingAddressID: &sa.ID,
		ToAddress:        r.FormValue("to"),
		CcAddress:        r.FormValue("cc"),
		BccAddress:       r.FormValue("bcc"),
		Subject:          r.FormValue("subject"),
		Body:             r.FormValue("body"),
		BodyHTML:         r.FormValue("body_html"),
		InReplyTo:        inReplyTo,
		References:       references,
	}
	created, err := s.DB.CreateDraft(r.Context(), draft)
	if err != nil {
		slog.Error("failed to create draft", "user_id", user.ID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, `Saved<input type="hidden" id="draft-id-input" name="draft_id" value="%s" hx-swap-oob="true">`, created.ID)
}

func (s *Server) handleDraftDelete(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	draftID, err := uuid.Parse(chi.URLParam(r, "draftID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	draft, err := s.DB.GetDraftByIDForUser(r.Context(), draftID, user.ID)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	if err := s.DB.DeleteDraft(r.Context(), draftID, user.ID); err != nil {
		slog.Error("failed to delete draft", "draft_id", draftID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/mailbox/"+draft.MailboxID.String()+"?filter=drafts")
		return
	}
	http.Redirect(w, r, "/mailbox/"+draft.MailboxID.String()+"?filter=drafts", http.StatusSeeOther)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	events := s.Hub.Subscribe(user.ID)
	defer s.Hub.Unsubscribe(user.ID, events)

	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	for {
		select {
		case event := <-events:
			data, _ := json.Marshal(map[string]string{
				"type":       event.Type,
				"mailbox_id": event.MailboxID.String(),
			})
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, string(data))
			flusher.Flush()
		case <-r.Context().Done():
			return
		case <-time.After(30 * time.Second):
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) handleLoginGet(w http.ResponseWriter, r *http.Request) {
	templates.LoginPage("", csrf.Token(r)).Render(r.Context(), w)
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, user *models.User, mailboxes []models.Mailbox, currentMailboxID uuid.UUID, filter string, counts map[string]int, content templ.Component) {
	if r.Header.Get("HX-Request") == "true" {
		content.Render(r.Context(), w)
		return
	}
	templates.Dashboard(user, mailboxes, currentMailboxID, filter, counts, content, csrf.Token(r)).Render(r.Context(), w)
}

func (s *Server) draftCount(ctx context.Context, mailboxID uuid.UUID, userID uuid.UUID) int {
	if mailboxID == uuid.Nil {
		return 0
	}
	count, _ := s.DB.CountDraftsByMailboxID(ctx, mailboxID, userID)
	return count
}

func (s *Server) getCounts(ctx context.Context, mailboxID, userID uuid.UUID) map[string]int {
	counts := make(map[string]int)
	if mailboxID == uuid.Nil {
		return counts
	}

	// For now, let's just get the ones we need for the UI
	unread, _ := s.DB.CountEmailsByMailboxID(ctx, mailboxID, "unread")
	counts["unread"] = unread

	drafts, _ := s.DB.CountDraftsByMailboxID(ctx, mailboxID, userID)
	counts["drafts"] = drafts

	// Also count Inbox specifically for the top link if current mailbox is Inbox
	// But actually the Inbox link at the top should probably always show the Inbox count.
	// For simplicity, let's just use what's available.
	return counts
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	mailboxes, err := s.DB.GetMailboxesByUserID(r.Context(), user.ID)
	if err != nil {
		slog.Error("failed to fetch mailboxes", "user_id", user.ID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if len(mailboxes) > 0 {
		targetID := mailboxes[0].ID.String()
		for _, mb := range mailboxes {
			if strings.ToLower(mb.Name) == "inbox" {
				targetID = mb.ID.String()
				break
			}
		}
		http.Redirect(w, r, "/mailbox/"+targetID, http.StatusSeeOther)
		return
	}

	s.render(w, r, user, mailboxes, uuid.Nil, "all", nil, templates.MailboxContent(uuid.Nil, "all", nil))
}

func (s *Server) handleMailboxView(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	mailboxID, err := uuid.Parse(chi.URLParam(r, "mailboxID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	filter := r.URL.Query().Get("filter")
	if filter == "" {
		filter = "all"
	}

	mailboxes, err := s.DB.GetMailboxesByUserID(r.Context(), user.ID)
	if err != nil {
		slog.Error("failed to fetch mailboxes", "user_id", user.ID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	owned := false
	for _, mb := range mailboxes {
		if mb.ID == mailboxID {
			owned = true
			break
		}
	}
	if !owned {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	counts := s.getCounts(r.Context(), mailboxID, user.ID)

	if filter == "drafts" {
		drafts, err := s.DB.GetDraftsByMailboxID(r.Context(), mailboxID, user.ID)
		if err != nil {
			slog.Error("failed to fetch drafts", "mailbox_id", mailboxID, "error", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		s.render(w, r, user, mailboxes, mailboxID, filter, counts, templates.DraftsContent(mailboxID, drafts))
		return
	}

	emails, err := s.DB.GetEmailsByMailboxID(r.Context(), mailboxID, filter, 50, 0)
	if err != nil {
		slog.Error("failed to fetch emails", "mailbox_id", mailboxID, "filter", filter, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	s.render(w, r, user, mailboxes, mailboxID, filter, counts, templates.MailboxContent(mailboxID, filter, emails))
}

func (s *Server) handleMailboxSearch(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	mailboxID, err := uuid.Parse(chi.URLParam(r, "mailboxID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	mailboxes, err := s.DB.GetMailboxesByUserID(r.Context(), user.ID)
	if err != nil {
		slog.Error("failed to fetch mailboxes", "user_id", user.ID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	owned := false
	for _, mb := range mailboxes {
		if mb.ID == mailboxID {
			owned = true
			break
		}
	}
	if !owned {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		http.Redirect(w, r, "/mailbox/"+mailboxID.String(), http.StatusSeeOther)
		return
	}

	emails, err := s.DB.SearchEmailsByMailboxID(r.Context(), mailboxID, user.ID, query, 50, 0)
	if err != nil {
		slog.Error("search failed", "mailbox_id", mailboxID, "query", query, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	counts := s.getCounts(r.Context(), mailboxID, user.ID)
	s.render(w, r, user, mailboxes, mailboxID, "search", counts, templates.SearchContent(mailboxID, query, emails))
}

func (s *Server) handleEmailView(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	emailID, err := uuid.Parse(chi.URLParam(r, "emailID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	email, err := s.DB.GetEmailByIDForUser(r.Context(), emailID, user.ID)
	if err != nil {
		slog.Error("failed to fetch email", "email_id", emailID, "user_id", user.ID, "error", err)
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	if !email.IsRead {
		if err := s.DB.MarkEmailRead(r.Context(), emailID, user.ID, true); err != nil {
			slog.Error("failed to mark email read", "email_id", emailID, "error", err)
		}
		email.IsRead = true
	}

	attachments, err := s.DB.GetAttachmentsByEmailID(r.Context(), emailID)
	if err != nil {
		slog.Error("failed to fetch attachments", "email_id", emailID, "error", err)
	}

	content, isHTML, err := s.Mail.FetchBody(r.Context(), email)
	if err != nil {
		slog.Error("failed to fetch body", "key", email.StorageKey, "error", err)
		content = "Failed to load content"
	}

	mailboxes, err := s.DB.GetMailboxesByUserID(r.Context(), user.ID)
	if err != nil {
		slog.Error("failed to fetch mailboxes", "user_id", user.ID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	counts := s.getCounts(r.Context(), email.MailboxID, user.ID)
	s.render(w, r, user, mailboxes, email.MailboxID, "all", counts, templates.EmailDetail(email, attachments, content, isHTML))
}

func (s *Server) handleEmailStar(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	emailID, err := uuid.Parse(chi.URLParam(r, "emailID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	email, err := s.DB.GetEmailByIDForUser(r.Context(), emailID, user.ID)
	if err != nil {
		slog.Error("failed to fetch email for star", "email_id", emailID, "user_id", user.ID, "error", err)
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	starred := !email.IsStar
	if err := s.DB.MarkEmailStarred(r.Context(), emailID, user.ID, starred); err != nil {
		slog.Error("failed to toggle star", "email_id", emailID, "error", err)
		http.Error(w, "Error", http.StatusInternalServerError)
		return
	}

	attachments, err := s.DB.GetAttachmentsByEmailID(r.Context(), emailID)
	if err != nil {
		slog.Error("failed to fetch attachments", "email_id", emailID, "error", err)
	}

	content, isHTML, err := s.Mail.FetchBody(r.Context(), email)
	if err != nil {
		slog.Error("failed to fetch body", "key", email.StorageKey, "error", err)
	}
	email.IsStar = starred

	templates.EmailDetail(email, attachments, content, isHTML).Render(r.Context(), w)
}

func (s *Server) handleEmailDelete(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	emailID, err := uuid.Parse(chi.URLParam(r, "emailID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	email, err := s.DB.GetEmailByIDForUser(r.Context(), emailID, user.ID)
	if err != nil {
		slog.Error("failed to fetch email for delete", "email_id", emailID, "user_id", user.ID, "error", err)
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	targetStatus := models.StatusDeleted
	if email.Status == models.StatusDeleted {
		targetStatus = models.StatusInbox
	}

	if err := s.DB.UpdateEmailStatus(r.Context(), emailID, user.ID, targetStatus); err != nil {
		slog.Error("failed to toggle delete", "email_id", emailID, "error", err)
		http.Error(w, "Error", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/mailbox/"+email.MailboxID.String())
		return
	}
	http.Redirect(w, r, "/mailbox/"+email.MailboxID.String(), http.StatusSeeOther)
}

func (s *Server) handleEmailRelease(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	emailID, err := uuid.Parse(chi.URLParam(r, "emailID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	email, err := s.DB.GetEmailByIDForUser(r.Context(), emailID, user.ID)
	if err != nil {
		slog.Error("failed to fetch email for release", "email_id", emailID, "user_id", user.ID, "error", err)
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	if err := s.DB.UpdateEmailStatus(r.Context(), emailID, user.ID, models.StatusInbox); err != nil {
		slog.Error("failed to release email", "email_id", emailID, "error", err)
		http.Error(w, "Error", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/mailbox/"+email.MailboxID.String()+"?filter=quarantined")
		return
	}
	http.Redirect(w, r, "/mailbox/"+email.MailboxID.String()+"?filter=quarantined", http.StatusSeeOther)
}

func (s *Server) handleEmailMarkSpam(w http.ResponseWriter, r *http.Request) {
	s.handleEmailLearn(w, r, true, models.StatusQuarantined)
}

func (s *Server) handleEmailMarkHam(w http.ResponseWriter, r *http.Request) {
	s.handleEmailLearn(w, r, false, models.StatusInbox)
}

func (s *Server) handleEmailLearn(w http.ResponseWriter, r *http.Request, spam bool, targetStatus models.EmailStatus) {
	user := r.Context().Value("user").(*models.User)
	emailID, err := uuid.Parse(chi.URLParam(r, "emailID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	email, err := s.DB.GetEmailByIDForUser(r.Context(), emailID, user.ID)
	if err != nil {
		slog.Error("failed to fetch email for spam learning", "email_id", emailID, "error", err)
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	if err := s.DB.UpdateEmailStatus(r.Context(), emailID, user.ID, targetStatus); err != nil {
		slog.Error("failed to update email status for spam learning", "email_id", emailID, "error", err)
		http.Error(w, "Error", http.StatusInternalServerError)
		return
	}

	if s.Rspamd != nil {
		raw, err := s.Mail.FetchRaw(r.Context(), email)
		if err != nil {
			slog.Error("failed to fetch raw email for rspamd learning", "email_id", emailID, "error", err)
		} else {
			var learnErr error
			if spam {
				learnErr = s.Rspamd.LearnSpam(r.Context(), raw)
			} else {
				learnErr = s.Rspamd.LearnHam(r.Context(), raw)
			}
			if learnErr != nil {
				slog.Error("rspamd learn failed", "email_id", emailID, "spam", spam, "error", learnErr)
			}
		}
	}

	redirectURL := "/email/" + emailID.String()
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", redirectURL)
		return
	}
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

func (s *Server) handleEmailHeaders(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	emailID, err := uuid.Parse(chi.URLParam(r, "emailID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	email, err := s.DB.GetEmailByIDForUser(r.Context(), emailID, user.ID)
	if err != nil {
		slog.Error("failed to fetch email for headers", "email_id", emailID, "error", err)
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	headers, err := s.Mail.FetchHeaders(r.Context(), email)
	if err != nil {
		slog.Error("failed to fetch headers", "key", email.StorageKey, "error", err)
	}

	mailboxes, err := s.DB.GetMailboxesByUserID(r.Context(), user.ID)
	if err != nil {
		slog.Error("failed to fetch mailboxes", "user_id", user.ID, "error", err)
	}

	counts := s.getCounts(r.Context(), email.MailboxID, user.ID)
	s.render(w, r, user, mailboxes, email.MailboxID, "all", counts, templates.EmailHeaders(email, headers))
}

func (s *Server) handleEmailPipeline(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	emailID, err := uuid.Parse(chi.URLParam(r, "emailID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	email, err := s.DB.GetEmailByIDForUser(r.Context(), emailID, user.ID)
	if err != nil {
		slog.Error("failed to fetch email for pipeline", "email_id", emailID, "error", err)
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	steps, err := s.DB.GetIngestionStepsByEmailID(r.Context(), emailID, user.ID)
	if err != nil {
		slog.Error("failed to fetch ingestion steps", "email_id", emailID, "error", err)
	}

	for i := range steps {
		if len(steps[i].Details) > 0 && string(steps[i].Details) != "null" {
			var pretty bytes.Buffer
			if err := json.Indent(&pretty, steps[i].Details, "", "  "); err == nil {
				steps[i].Details = pretty.Bytes()
			}
		}
	}

	mailboxes, err := s.DB.GetMailboxesByUserID(r.Context(), user.ID)
	if err != nil {
		slog.Error("failed to fetch mailboxes", "user_id", user.ID, "error", err)
	}

	counts := s.getCounts(r.Context(), email.MailboxID, user.ID)
	s.render(w, r, user, mailboxes, email.MailboxID, "all", counts, templates.EmailPipeline(email, steps))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		//w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("maileroo_session")
		if err != nil {
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/login")
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		session, err := s.DB.GetWebmailSession(r.Context(), cookie.Value)
		if err != nil || session.ExpiresDatetime.Before(time.Now()) {
			slog.Warn("invalid or expired session", "error", err)
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/login")
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		user, err := s.DB.GetUserByID(r.Context(), session.UserID)
		if err != nil || !user.IsActive {
			slog.Warn("user not found or inactive", "user_id", session.UserID, "error", err)
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/login")
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		ctx := context.WithValue(r.Context(), "user", user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	remoteIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	if remoteIP == "" {
		remoteIP = r.RemoteAddr
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	// recordFailure consumes a token from the per-IP limiter and, if exhausted,
	// returns true to signal that the caller should reject with 429.
	recordFailure := func() bool {
		limiter := s.loginLimiter(remoteIP)
		if !limiter.Allow() {
			slog.Warn("login rate limited", "ip", remoteIP)
			http.Error(w, "Too many failed login attempts, please try again later", http.StatusTooManyRequests)
			return true
		}
		return false
	}

	user, err := s.DB.GetUserByUsername(r.Context(), username)
	if err != nil || !user.IsActive {
		slog.Warn("login failed: user not found or inactive", "username", username)
		if recordFailure() {
			return
		}
		templates.LoginPage("Invalid credentials", csrf.Token(r)).Render(r.Context(), w)
		return
	}

	match, err := auth.ComparePassword(password, user.PasswordHash)
	if err != nil || !match {
		slog.Warn("login failed: incorrect password", "username", username)
		if recordFailure() {
			return
		}
		templates.LoginPage("Invalid credentials", csrf.Token(r)).Render(r.Context(), w)
		return
	}

	token := generateToken()
	expires := time.Now().Add(24 * time.Hour)
	if err := s.DB.CreateWebmailSession(r.Context(), user.ID, token, r.RemoteAddr, r.UserAgent(), expires); err != nil {
		slog.Error("failed to create session", "user_id", user.ID, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "maileroo_session",
		Value:    token,
		Expires:  expires,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("maileroo_session")
	if err == nil {
		if err := s.DB.ExpireWebmailSession(r.Context(), cookie.Value); err != nil {
			slog.Error("failed to expire session on logout", "error", err)
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "maileroo_session",
		Value:    "",
		Expires:  time.Now().Add(-1 * time.Hour),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
	})

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleAttachmentDownload(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	attID, err := uuid.Parse(chi.URLParam(r, "attachmentID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	att, err := s.DB.GetAttachmentByIDForUser(r.Context(), attID, user.ID)
	if err != nil {
		slog.Error("attachment not found or forbidden", "att_id", attID, "user_id", user.ID, "error", err)
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	rc, err := s.Storage.Get(r.Context(), att.StorageKey)
	if err != nil {
		slog.Error("failed to fetch attachment", "key", att.StorageKey, "error", err)
		http.Error(w, "Failed to load", http.StatusInternalServerError)
		return
	}
	defer rc.Close()

	bodyReader, err := s.Mail.DecompressReader(rc, att.StorageKey)
	if err != nil {
		slog.Error("failed to decompress attachment", "key", att.StorageKey, "error", err)
		http.Error(w, "Failed to load", http.StatusInternalServerError)
		return
	}
	if closer, ok := bodyReader.(io.Closer); ok {
		defer closer.Close()
	}

	w.Header().Set("Content-Type", att.ContentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": att.Filename}))
	io.Copy(w, bodyReader)
}

func (s *Server) handleUserInfo(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)

	mailboxes, err := s.DB.GetMailboxesByUserID(r.Context(), user.ID)
	if err != nil {
		slog.Error("failed to fetch mailboxes", "user_id", user.ID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	sendingAddresses, err := s.DB.GetActiveSendingAddresses(r.Context(), user.ID)
	if err != nil {
		slog.Error("failed to fetch sending addresses", "user_id", user.ID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	component := templates.UserInfo(user, mailboxes, sendingAddresses)
	component.Render(r.Context(), w)
}

func (s *Server) handleUpdateDisplayName(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	saIDRaw := chi.URLParam(r, "saID")
	saID, err := uuid.Parse(saIDRaw)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	displayName := r.FormValue("display_name")

	err = s.DB.UpdateSendingAddressDisplayName(r.Context(), saID, user.ID, displayName)
	if err != nil {
		slog.Error("failed to update display name", "user_id", user.ID, "sa_id", saID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func generateToken() string {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic("failed to generate random token: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func parseAddresses(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var res []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			res = append(res, trimmed)
		}
	}
	return res
}
