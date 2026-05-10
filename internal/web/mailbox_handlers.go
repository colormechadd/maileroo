package web

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/colormechadd/mailaroo/templates"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

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

	s.render(w, r, user, mailboxes, uuid.Nil, "all", nil, templates.MailboxContent(uuid.Nil, "all", nil, "", false), "MAILAROO")
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
		filter = "mail"
		canonical := "/mailbox/" + mailboxID.String() + "?filter=mail"
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Replace-Url", canonical)
		} else {
			http.Redirect(w, r, canonical, http.StatusSeeOther)
			return
		}
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
		s.render(w, r, user, mailboxes, mailboxID, filter, counts, templates.DraftsContent(mailboxID, drafts), "Drafts")
		return
	}

	const pageSize = 50
	emails, err := s.DB.GetEmailsByMailboxID(r.Context(), mailboxID, filter, pageSize, nil, nil)
	if err != nil {
		slog.Error("failed to fetch emails", "mailbox_id", mailboxID, "filter", filter, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	hasMore := len(emails) == pageSize
	s.render(w, r, user, mailboxes, mailboxID, filter, counts, templates.MailboxContent(mailboxID, filter, emails, "", hasMore), filterTitle(filter))
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

	const pageSize = 50
	emails, err := s.DB.SearchEmailsByMailboxID(r.Context(), mailboxID, user.ID, query, pageSize, nil, nil)
	if err != nil {
		slog.Error("search failed", "mailbox_id", mailboxID, "query", query, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	counts := s.getCounts(r.Context(), mailboxID, user.ID)
	hasMore := len(emails) == pageSize
	s.render(w, r, user, mailboxes, mailboxID, "search", counts, templates.SearchContent(mailboxID, query, emails, hasMore), "Search: "+query)
}

func (s *Server) handleMailboxMore(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	mailboxID, err := uuid.Parse(chi.URLParam(r, "mailboxID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	mailboxes, err := s.DB.GetMailboxesByUserID(r.Context(), user.ID)
	if err != nil {
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

	filter := r.URL.Query().Get("filter")
	if filter == "" {
		filter = "all"
	}

	cursorTime, cursorID, err := decodeCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		http.Error(w, "Invalid cursor", http.StatusBadRequest)
		return
	}

	const pageSize = 50
	emails, err := s.DB.GetEmailsByMailboxID(r.Context(), mailboxID, filter, pageSize, cursorTime, cursorID)
	if err != nil {
		slog.Error("failed to fetch emails", "mailbox_id", mailboxID, "filter", filter, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	hasMore := len(emails) == pageSize
	var loadMoreURL string
	if hasMore {
		last := emails[len(emails)-1]
		loadMoreURL = "/mailbox/" + mailboxID.String() + "/more?filter=" + filter + "&cursor=" + encodeCursor(last.ReceiveDatetime, last.ID)
	}
	templates.EmailListRows(emails, filter, loadMoreURL, hasMore).Render(r.Context(), w)
}

func (s *Server) handleSearchMore(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	mailboxID, err := uuid.Parse(chi.URLParam(r, "mailboxID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	mailboxes, err := s.DB.GetMailboxesByUserID(r.Context(), user.ID)
	if err != nil {
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
		return
	}

	cursorTime, cursorID, err := decodeCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		http.Error(w, "Invalid cursor", http.StatusBadRequest)
		return
	}

	const pageSize = 50
	emails, err := s.DB.SearchEmailsByMailboxID(r.Context(), mailboxID, user.ID, query, pageSize, cursorTime, cursorID)
	if err != nil {
		slog.Error("search more failed", "mailbox_id", mailboxID, "query", query, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	hasMore := len(emails) == pageSize
	var loadMoreURL string
	if hasMore {
		last := emails[len(emails)-1]
		loadMoreURL = "/mailbox/" + mailboxID.String() + "/search/more?q=" + url.QueryEscape(query) + "&cursor=" + encodeCursor(last.ReceiveDatetime, last.ID)
	}
	templates.EmailListRows(emails, "search", loadMoreURL, hasMore).Render(r.Context(), w)
}

func encodeCursor(t time.Time, id uuid.UUID) string {
	raw := t.UTC().Format(time.RFC3339Nano) + "|" + id.String()
	return base64.URLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(cursor string) (*time.Time, *uuid.UUID, error) {
	data, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, nil, err
	}
	parts := strings.SplitN(string(data), "|", 2)
	if len(parts) != 2 {
		return nil, nil, fmt.Errorf("invalid cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, nil, err
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return nil, nil, err
	}
	return &t, &id, nil
}
