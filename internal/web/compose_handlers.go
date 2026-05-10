package web

import (
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/colormechadd/mailaroo/internal/mail"
	"github.com/colormechadd/mailaroo/internal/outbound"
	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/colormechadd/mailaroo/templates"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

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

				dateStr := orig.ReceiveDatetime.Format("Jan 02, 2006, 15:04")
				origBody, origIsHTML, _ := s.Mail.FetchBody(r.Context(), orig)
				if origIsHTML {
					headerHTML := fmt.Sprintf(
						"On %s, %s wrote:<br>",
						html.EscapeString(dateStr),
						html.EscapeString(orig.FromAddress),
					)
					bodyHTML = fmt.Sprintf(
						`<br><br><blockquote style="border-left:2px solid #ccc;margin:0;padding:0 0 0 0.75em">%s%s</blockquote>`,
						headerHTML, origBody,
					)
				} else {
					body = fmt.Sprintf(
						"\n\nOn %s, %s wrote:\n\n%s",
						dateStr, orig.FromAddress,
						strings.ReplaceAll(origBody, "\n", "\n> "),
					)
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
	counts := s.getCounts(r.Context(), uuid.Nil, user.ID)
	s.render(w, r, user, mailboxes, uuid.Nil, "all", counts, templates.Compose(addresses, fromID, to, cc, bcc, subject, inReplyTo, references, draftID, title, body, bodyHTML), "Compose")
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
