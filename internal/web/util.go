package web

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/a-h/templ"
	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/colormechadd/mailaroo/templates"
	"github.com/google/uuid"
	"github.com/gorilla/csrf"
)

func (s *Server) render(w http.ResponseWriter, r *http.Request, user *models.User, mailboxes []models.Mailbox, currentMailboxID uuid.UUID, filter string, counts map[string]int, content templ.Component, title string) {
	if r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-History-Restore-Request") != "true" {
		content.Render(r.Context(), w)
		fmt.Fprintf(w, "<title>%s - MAILAROO</title>", html.EscapeString(title))
		return
	}
	templates.Dashboard(user, mailboxes, currentMailboxID, filter, counts, content, csrf.Token(r), title).Render(r.Context(), w)
}

func filterTitle(filter string) string {
	switch filter {
	case "sent":
		return "Sent"
	case "mail":
		return "Mail"
	case "starred":
		return "Starred"
	case "quarantined":
		return "Quarantined"
	case "deleted":
		return "Deleted"
	default:
		return "Mail"
	}
}

func truncateTitle(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

func (s *Server) draftCount(ctx context.Context, mailboxID uuid.UUID, userID uuid.UUID) int {
	if mailboxID == uuid.Nil {
		return 0
	}
	count, _ := s.DB.CountDraftsByMailboxID(ctx, mailboxID, userID)
	return count
}

func defaultMailboxID(mailboxes []models.Mailbox) uuid.UUID {
	for _, mb := range mailboxes {
		if strings.ToLower(mb.Name) == "inbox" {
			return mb.ID
		}
	}
	if len(mailboxes) > 0 {
		return mailboxes[0].ID
	}
	return uuid.Nil
}

func (s *Server) getCounts(ctx context.Context, mailboxID, userID uuid.UUID) map[string]int {
	counts := make(map[string]int)
	if mailboxID == uuid.Nil {
		return counts
	}
	unread, _ := s.DB.CountEmailsByMailboxID(ctx, mailboxID, "unread")
	counts["unread"] = unread
	drafts, _ := s.DB.CountDraftsByMailboxID(ctx, mailboxID, userID)
	counts["drafts"] = drafts
	return counts
}

func (s *Server) getMailboxForUser(r *http.Request, mailboxIDStr string, userID uuid.UUID) (uuid.UUID, []models.Mailbox, error) {
	mailboxID, err := uuid.Parse(mailboxIDStr)
	if err != nil {
		return uuid.Nil, nil, err
	}
	mailboxes, err := s.DB.GetMailboxesByUserID(r.Context(), userID)
	if err != nil {
		return uuid.Nil, nil, err
	}
	for _, mb := range mailboxes {
		if mb.ID == mailboxID {
			return mailboxID, mailboxes, nil
		}
	}
	return uuid.Nil, nil, fmt.Errorf("mailbox not found")
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
