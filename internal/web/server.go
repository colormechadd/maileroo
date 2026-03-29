package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/colormechadd/maileroo/internal/config"
	"github.com/colormechadd/maileroo/internal/db"
	"github.com/colormechadd/maileroo/internal/mail"
	"github.com/colormechadd/maileroo/internal/outbound"
	"github.com/colormechadd/maileroo/internal/storage"
	"github.com/colormechadd/maileroo/pkg/auth"
	"github.com/colormechadd/maileroo/pkg/models"
	"github.com/colormechadd/maileroo/templates"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

type Server struct {
	cfg     config.Config
	db      db.WebDB
	storage storage.Storage
	hub     *Hub
	sender  outbound.Sender
	mail    *mail.Service
}

func NewServer(cfg config.Config, webDB db.WebDB, storage storage.Storage, hub *Hub, sender outbound.Sender, mailSvc *mail.Service) *Server {
	return &Server{
		cfg:     cfg,
		db:      webDB,
		storage: storage,
		hub:     hub,
		sender:  sender,
		mail:    mailSvc,
	}
}

func (s *Server) Routes() chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	fs := http.FileServer(http.Dir("static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fs))

	r.Get("/login", templ.Handler(templates.LoginPage("")).ServeHTTP)
	r.Post("/login", s.handleLoginPost)
	r.Post("/logout", s.handleLogout)

	r.Group(func(r chi.Router) {
		r.Use(s.AuthMiddleware)
		r.Get("/", s.handleDashboard)
		r.Get("/events", s.handleEvents)
		r.Get("/mailbox/{mailboxID}", s.handleMailboxView)
		r.Get("/email/{emailID}", s.handleEmailView)
		r.Get("/email/{emailID}/headers", s.handleEmailHeaders)
		r.Get("/email/{emailID}/pipeline", s.handleEmailPipeline)
		r.Post("/email/{emailID}/star", s.handleEmailStar)
		r.Post("/email/{emailID}/delete", s.handleEmailDelete)
		r.Post("/email/{emailID}/release", s.handleEmailRelease)
		r.Get("/attachment/{attachmentID}", s.handleAttachmentDownload)

		r.Get("/compose", s.handleCompose)
		r.Post("/send", s.handleEmailSend)
	})

	return r
}

func (s *Server) handleCompose(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	addresses, err := s.db.GetActiveSendingAddresses(r.Context(), user.ID)
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

	replyToIDRaw := r.URL.Query().Get("replyTo")
	if replyToIDRaw != "" {
		replyToID, err := uuid.Parse(replyToIDRaw)
		if err == nil {
			orig, err := s.db.GetEmailByIDForUser(r.Context(), replyToID, user.ID)
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

				ccList, _ := s.mail.GetCcAddresses(r.Context(), orig)
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
			orig, err := s.db.GetEmailByIDForUser(r.Context(), forwardID, user.ID)
			if err == nil {
				title = "Forward"
				subject = orig.Subject
				if !strings.HasPrefix(strings.ToLower(subject), "fwd:") {
					subject = "Fwd: " + subject
				}

				dateStr := orig.ReceiveDatetime.Format("Jan 02, 2006, 15:04")
				origBody, origIsHTML, _ := s.mail.FetchBody(r.Context(), orig)

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

	mailboxes, _ := s.db.GetMailboxesByUserID(r.Context(), user.ID)
	s.render(w, r, user, mailboxes, uuid.Nil, "all", templates.Compose(addresses, fromID, to, cc, bcc, subject, inReplyTo, references, title, body, bodyHTML))
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

	sa, err := s.db.GetSendingAddressByID(r.Context(), fromID, user.ID)
	if err != nil {
		slog.Warn("unauthorized sending attempt", "user_id", user.ID, "from_id", fromID, "error", err)
		http.Error(w, "Unauthorized from address", http.StatusForbidden)
		return
	}

	to := parseAddresses(toRaw)
	cc := parseAddresses(ccRaw)
	bcc := parseAddresses(bccRaw)

	outMsg := outbound.Message{
		From:       sa.Address,
		To:         to,
		Cc:         cc,
		Bcc:        bcc,
		Subject:    subject,
		TextBody:   body,
		HTMLBody:   bodyHTML,
		InReplyTo:  inReplyTo,
		References: references,
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

	rawBytes, from, recipients, err := s.sender.BuildMessage(outMsg)
	if err != nil {
		slog.Error("failed to build outbound message", "user_id", user.ID, "error", err)
		http.Error(w, "Failed to build email", http.StatusInternalServerError)
		return
	}

	email, err := s.mail.Persist(r.Context(), mail.PersistOptions{
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

	if _, err := s.db.InsertOutboundJob(r.Context(), &email.ID, from, recipients, rawBytes); err != nil {
		slog.Error("failed to enqueue outbound job", "user_id", user.ID, "email_id", email.ID, "error", err)
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Location", "/mailbox/"+sa.MailboxID.String()+"?filter=sent")
		return
	}
	http.Redirect(w, r, "/mailbox/"+sa.MailboxID.String()+"?filter=sent", http.StatusSeeOther)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	events := s.hub.Subscribe(user.ID)
	defer s.hub.Unsubscribe(user.ID, events)

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

func (s *Server) render(w http.ResponseWriter, r *http.Request, user *models.User, mailboxes []models.Mailbox, currentMailboxID uuid.UUID, filter string, content templ.Component) {
	if r.Header.Get("HX-Request") == "true" {
		content.Render(r.Context(), w)
		return
	}
	templates.Dashboard(user, mailboxes, currentMailboxID, filter, content).Render(r.Context(), w)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	mailboxes, err := s.db.GetMailboxesByUserID(r.Context(), user.ID)
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

	s.render(w, r, user, mailboxes, uuid.Nil, "all", templates.MailboxContent(uuid.Nil, "all", nil))
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

	mailboxes, err := s.db.GetMailboxesByUserID(r.Context(), user.ID)
	if err != nil {
		slog.Error("failed to fetch mailboxes", "user_id", user.ID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	emails, err := s.db.GetEmailsByMailboxID(r.Context(), mailboxID, filter, 50, 0)
	if err != nil {
		slog.Error("failed to fetch emails", "mailbox_id", mailboxID, "filter", filter, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	s.render(w, r, user, mailboxes, mailboxID, filter, templates.MailboxContent(mailboxID, filter, emails))
}

func (s *Server) handleEmailView(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	emailID, err := uuid.Parse(chi.URLParam(r, "emailID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	email, err := s.db.GetEmailByIDForUser(r.Context(), emailID, user.ID)
	if err != nil {
		slog.Error("failed to fetch email", "email_id", emailID, "user_id", user.ID, "error", err)
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	if !email.IsRead {
		if err := s.db.MarkEmailRead(r.Context(), emailID, user.ID, true); err != nil {
			slog.Error("failed to mark email read", "email_id", emailID, "error", err)
		}
		email.IsRead = true
	}

	attachments, err := s.db.GetAttachmentsByEmailID(r.Context(), emailID)
	if err != nil {
		slog.Error("failed to fetch attachments", "email_id", emailID, "error", err)
	}

	content, isHTML, err := s.mail.FetchBody(r.Context(), email)
	if err != nil {
		slog.Error("failed to fetch body", "key", email.StorageKey, "error", err)
		content = "Failed to load content"
	}

	mailboxes, err := s.db.GetMailboxesByUserID(r.Context(), user.ID)
	if err != nil {
		slog.Error("failed to fetch mailboxes", "user_id", user.ID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	s.render(w, r, user, mailboxes, email.MailboxID, "all", templates.EmailDetail(email, attachments, content, isHTML))
}

func (s *Server) handleEmailStar(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	emailID, err := uuid.Parse(chi.URLParam(r, "emailID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	email, err := s.db.GetEmailByIDForUser(r.Context(), emailID, user.ID)
	if err != nil {
		slog.Error("failed to fetch email for star", "email_id", emailID, "user_id", user.ID, "error", err)
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	starred := !email.IsStar
	if err := s.db.MarkEmailStarred(r.Context(), emailID, user.ID, starred); err != nil {
		slog.Error("failed to toggle star", "email_id", emailID, "error", err)
		http.Error(w, "Error", http.StatusInternalServerError)
		return
	}

	attachments, err := s.db.GetAttachmentsByEmailID(r.Context(), emailID)
	if err != nil {
		slog.Error("failed to fetch attachments", "email_id", emailID, "error", err)
	}

	content, isHTML, err := s.mail.FetchBody(r.Context(), email)
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

	email, err := s.db.GetEmailByIDForUser(r.Context(), emailID, user.ID)
	if err != nil {
		slog.Error("failed to fetch email for delete", "email_id", emailID, "user_id", user.ID, "error", err)
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	targetStatus := models.StatusDeleted
	if email.Status == models.StatusDeleted {
		targetStatus = models.StatusInbox
	}

	if err := s.db.UpdateEmailStatus(r.Context(), emailID, user.ID, targetStatus); err != nil {
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

	email, err := s.db.GetEmailByIDForUser(r.Context(), emailID, user.ID)
	if err != nil {
		slog.Error("failed to fetch email for release", "email_id", emailID, "user_id", user.ID, "error", err)
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	if err := s.db.UpdateEmailStatus(r.Context(), emailID, user.ID, models.StatusInbox); err != nil {
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

func (s *Server) handleEmailHeaders(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	emailID, err := uuid.Parse(chi.URLParam(r, "emailID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	email, err := s.db.GetEmailByIDForUser(r.Context(), emailID, user.ID)
	if err != nil {
		slog.Error("failed to fetch email for headers", "email_id", emailID, "error", err)
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	headers, err := s.mail.FetchHeaders(r.Context(), email)
	if err != nil {
		slog.Error("failed to fetch headers", "key", email.StorageKey, "error", err)
	}

	mailboxes, err := s.db.GetMailboxesByUserID(r.Context(), user.ID)
	if err != nil {
		slog.Error("failed to fetch mailboxes", "user_id", user.ID, "error", err)
	}

	s.render(w, r, user, mailboxes, email.MailboxID, "all", templates.EmailHeaders(email, headers))
}

func (s *Server) handleEmailPipeline(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	emailID, err := uuid.Parse(chi.URLParam(r, "emailID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	email, err := s.db.GetEmailByIDForUser(r.Context(), emailID, user.ID)
	if err != nil {
		slog.Error("failed to fetch email for pipeline", "email_id", emailID, "error", err)
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	steps, err := s.db.GetIngestionStepsByEmailID(r.Context(), emailID, user.ID)
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

	mailboxes, err := s.db.GetMailboxesByUserID(r.Context(), user.ID)
	if err != nil {
		slog.Error("failed to fetch mailboxes", "user_id", user.ID, "error", err)
	}

	s.render(w, r, user, mailboxes, email.MailboxID, "all", templates.EmailPipeline(email, steps))
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

		session, err := s.db.GetWebmailSession(r.Context(), cookie.Value)
		if err != nil || session.ExpiresDatetime.Before(time.Now()) {
			slog.Warn("invalid or expired session", "error", err)
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/login")
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		user, err := s.db.GetUserByID(r.Context(), session.UserID)
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
	username := r.FormValue("username")
	password := r.FormValue("password")

	user, err := s.db.GetUserByUsername(r.Context(), username)
	if err != nil || !user.IsActive {
		slog.Warn("login failed: user not found or inactive", "username", username)
		templates.LoginPage("Invalid credentials").Render(r.Context(), w)
		return
	}

	match, err := auth.ComparePassword(password, user.PasswordHash)
	if err != nil || !match {
		slog.Warn("login failed: incorrect password", "username", username)
		templates.LoginPage("Invalid credentials").Render(r.Context(), w)
		return
	}

	token := generateToken()
	expires := time.Now().Add(24 * time.Hour)
	if err := s.db.CreateWebmailSession(r.Context(), user.ID, token, r.RemoteAddr, r.UserAgent(), expires); err != nil {
		slog.Error("failed to create session", "user_id", user.ID, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "maileroo_session",
		Value:    token,
		Expires:  expires,
		HttpOnly: true,
		Path:     "/",
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("maileroo_session")
	if err == nil {
		if err := s.db.ExpireWebmailSession(r.Context(), cookie.Value); err != nil {
			slog.Error("failed to expire session on logout", "error", err)
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "maileroo_session",
		Value:    "",
		Expires:  time.Now().Add(-1 * time.Hour),
		HttpOnly: true,
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

	att, err := s.db.GetAttachmentByIDForUser(r.Context(), attID, user.ID)
	if err != nil {
		slog.Error("attachment not found", "att_id", attID, "user_id", user.ID, "error", err)
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	rc, err := s.storage.Get(r.Context(), att.StorageKey)
	if err != nil {
		slog.Error("failed to fetch attachment", "key", att.StorageKey, "error", err)
		http.Error(w, "Failed to load", http.StatusInternalServerError)
		return
	}
	defer rc.Close()

	bodyReader, err := s.mail.DecompressReader(rc, att.StorageKey)
	if err != nil {
		slog.Error("failed to decompress attachment", "key", att.StorageKey, "error", err)
		http.Error(w, "Failed to load", http.StatusInternalServerError)
		return
	}
	if closer, ok := bodyReader.(io.Closer); ok {
		defer closer.Close()
	}

	w.Header().Set("Content-Type", att.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", att.Filename))
	io.Copy(w, bodyReader)
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
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
