package web

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	netmail "net/mail"
	"regexp"
	"strings"

	"github.com/colormechadd/mailaroo/internal/db"
	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/colormechadd/mailaroo/templates"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

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

	unsubInfo, err := s.Mail.FetchUnsubscribeInfo(r.Context(), email)
	if err != nil {
		slog.Error("failed to fetch unsubscribe info", "key", email.StorageKey, "error", err)
	}

	mailboxes, err := s.DB.GetMailboxesByUserID(r.Context(), user.ID)
	if err != nil {
		slog.Error("failed to fetch mailboxes", "user_id", user.ID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	senderAddr := email.FromAddress
	if parsed, err := netmail.ParseAddress(email.FromAddress); err == nil {
		senderAddr = parsed.Address
	}
	senderContact, err := s.DB.GetContactByEmail(r.Context(), email.MailboxID, senderAddr)
	if err != nil {
		senderContact = nil
	}

	senderBlockRule, err := s.DB.IsBlockedByMailboxRules(r.Context(), email.MailboxID, senderAddr)
	if err != nil {
		slog.Error("failed to check block rules", "mailbox_id", email.MailboxID, "error", err)
	}
	senderBlocked := senderBlockRule != nil

	counts := s.getCounts(r.Context(), email.MailboxID, user.ID)
	s.render(w, r, user, mailboxes, email.MailboxID, "all", counts, templates.EmailDetail(email, attachments, content, isHTML, unsubInfo, senderContact, senderBlocked), truncateTitle(email.Subject, 60))
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

	email.IsStar = starred

	if r.URL.Query().Get("list") == "1" {
		templates.EmailListStarButton(email.ID, starred).Render(r.Context(), w)
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

	unsubInfo, err := s.Mail.FetchUnsubscribeInfo(r.Context(), email)
	if err != nil {
		slog.Error("failed to fetch unsubscribe info", "key", email.StorageKey, "error", err)
	}

	starSenderAddr := email.FromAddress
	if parsed, err := netmail.ParseAddress(email.FromAddress); err == nil {
		starSenderAddr = parsed.Address
	}
	senderContact, _ := s.DB.GetContactByEmail(r.Context(), email.MailboxID, starSenderAddr)
	starBlockRule, _ := s.DB.IsBlockedByMailboxRules(r.Context(), email.MailboxID, starSenderAddr)
	templates.EmailDetail(email, attachments, content, isHTML, unsubInfo, senderContact, starBlockRule != nil).Render(r.Context(), w)
}

func (s *Server) handleBulkEmailAction(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	mailboxID, err := uuid.Parse(chi.URLParam(r, "mailboxID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	rawIDs := strings.Split(r.FormValue("ids"), ",")
	var ids []uuid.UUID
	for _, raw := range rawIDs {
		raw = strings.TrimSpace(raw)
		if id, err := uuid.Parse(raw); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		http.Error(w, "No valid IDs", http.StatusBadRequest)
		return
	}

	action := r.FormValue("action")
	switch action {
	case "mark-read":
		if err := s.DB.BulkMarkEmailRead(r.Context(), ids, user.ID, true); err != nil {
			slog.Error("bulk mark read failed", "error", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
	case "mark-unread":
		if err := s.DB.BulkMarkEmailRead(r.Context(), ids, user.ID, false); err != nil {
			slog.Error("bulk mark unread failed", "error", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
	case "delete":
		if err := s.DB.BulkUpdateEmailStatus(r.Context(), ids, user.ID, models.StatusDeleted); err != nil {
			slog.Error("bulk delete failed", "error", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, "Unknown action", http.StatusBadRequest)
		return
	}

	filter := r.URL.Query().Get("filter")
	if filter == "" {
		filter = "unread"
	}
	mailboxes, _ := s.DB.GetMailboxesByUserID(r.Context(), user.ID)
	counts := s.getCounts(r.Context(), mailboxID, user.ID)
	emails, err := s.DB.SearchEmails(r.Context(), mailboxID, user.ID, db.EmailFilter{View: filter}, 50, nil, nil)

	if err != nil {
		slog.Error("failed to fetch emails after bulk action", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	hasMore := len(emails) == 50
	var contacts map[string]*models.Contact
	if cs, err := s.DB.ListContacts(r.Context(), mailboxID); err == nil {
		contacts = buildContactsMap(cs)
	}
	s.render(w, r, user, mailboxes, mailboxID, filter, counts, templates.MailboxContent(mailboxID, filter, emails, "", hasMore, contacts), "")
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

	dest := "/mailbox/" + email.MailboxID.String()
	if email.Direction == models.DirectionOutbound {
		dest += "?filter=sent"
	}
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", dest)
		return
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

func (s *Server) handleEmailDeleteAndBlock(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	emailID, err := uuid.Parse(chi.URLParam(r, "emailID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	email, err := s.DB.GetEmailByIDForUser(r.Context(), emailID, user.ID)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	addr := email.FromAddress
	if parsed, err := netmail.ParseAddress(email.FromAddress); err == nil {
		addr = parsed.Address
	}
	if err := s.DB.CreateBlockRule(r.Context(), email.MailboxID, user.ID, regexp.QuoteMeta(addr)); err != nil {
		slog.Error("failed to create block rule", "mailbox_id", email.MailboxID, "error", err)
		http.Error(w, "Error", http.StatusInternalServerError)
		return
	}

	if err := s.DB.UpdateEmailStatus(r.Context(), emailID, user.ID, models.StatusDeleted); err != nil {
		slog.Error("failed to delete email", "email_id", emailID, "error", err)
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

func (s *Server) handleEmailUnsubscribe(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	emailID, err := uuid.Parse(chi.URLParam(r, "emailID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	email, err := s.DB.GetEmailByIDForUser(r.Context(), emailID, user.ID)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	info, err := s.Mail.FetchUnsubscribeInfo(r.Context(), email)
	if err != nil || info == nil || info.URL == "" {
		http.Error(w, "No unsubscribe URL available", http.StatusBadRequest)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, info.URL, strings.NewReader("List-Unsubscribe=One-Click"))
	if err != nil {
		slog.Error("failed to build unsubscribe request", "url", info.URL, "error", err)
		http.Error(w, "Failed to unsubscribe", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("unsubscribe request failed", "url", info.URL, "error", err)
		http.Error(w, "Failed to unsubscribe", http.StatusInternalServerError)
		return
	}
	resp.Body.Close()

	w.Header().Set("HX-Reswap", "none")
	w.Header().Set("HX-Trigger", `{"showToast":"Unsubscribed successfully"}`)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleEmailBlockSender(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	emailID, err := uuid.Parse(chi.URLParam(r, "emailID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	email, err := s.DB.GetEmailByIDForUser(r.Context(), emailID, user.ID)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	addr := email.FromAddress
	if parsed, err := netmail.ParseAddress(email.FromAddress); err == nil {
		addr = parsed.Address
	}
	pattern := regexp.QuoteMeta(addr)
	if err := s.DB.CreateBlockRule(r.Context(), email.MailboxID, user.ID, pattern); err != nil {
		slog.Error("failed to create block rule", "mailbox_id", email.MailboxID, "error", err)
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
		content = "Failed to load content"
	}
	unsubInfo, _ := s.Mail.FetchUnsubscribeInfo(r.Context(), email)
	senderContact, _ := s.DB.GetContactByEmail(r.Context(), email.MailboxID, addr)

	w.Header().Set("HX-Trigger", `{"showToast":"Sender blocked"}`)
	templates.EmailDetail(email, attachments, content, isHTML, unsubInfo, senderContact, true).Render(r.Context(), w)
}

func (s *Server) handleManualBlockSender(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	mailboxID, _, err := s.getMailboxForUser(r, chi.URLParam(r, "mailboxID"), user.ID)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	addr := strings.TrimSpace(r.FormValue("address"))
	if addr == "" {
		http.Error(w, "Address required", http.StatusBadRequest)
		return
	}
	if parsed, err := netmail.ParseAddress(addr); err == nil {
		addr = parsed.Address
	}

	pattern := regexp.QuoteMeta(addr)
	if err := s.DB.CreateBlockRule(r.Context(), mailboxID, user.ID, pattern); err != nil {
		slog.Error("failed to create block rule", "mailbox_id", mailboxID, "error", err)
		http.Error(w, "Error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/mailbox/"+mailboxID.String()+"/filters", http.StatusSeeOther)
}

func (s *Server) handleUnblockSender(w http.ResponseWriter, r *http.Request) {
	mailboxID, _, err := s.getMailboxForUser(r, chi.URLParam(r, "mailboxID"), r.Context().Value("user").(*models.User).ID)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	blockRuleID, err := uuid.Parse(chi.URLParam(r, "blockRuleID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	if err := s.DB.DeleteBlockRule(r.Context(), blockRuleID, mailboxID); err != nil {
		slog.Error("failed to delete block rule", "block_rule_id", blockRuleID, "error", err)
		http.Error(w, "Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Trigger", `{"showToast":"Sender unblocked"}`)
	w.WriteHeader(http.StatusOK)
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
	s.render(w, r, user, mailboxes, email.MailboxID, "all", counts, templates.EmailHeaders(email, headers), truncateTitle(email.Subject, 60))
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
	s.render(w, r, user, mailboxes, email.MailboxID, "all", counts, templates.EmailPipeline(email, steps), truncateTitle(email.Subject, 60))
}
