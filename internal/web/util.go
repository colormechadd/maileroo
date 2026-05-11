package web

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"

	"github.com/a-h/templ"
	"github.com/colormechadd/mailaroo/internal/db"
	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/colormechadd/mailaroo/templates"
	"github.com/google/uuid"
	"github.com/gorilla/csrf"
)

var searchFieldRe = regexp.MustCompile(`(?i)\b(from|to|subject):(\S+)`)

// buildEmailFilterFromQuery parses a user search query into an EmailFilter.
// Recognises from:, to:, subject: prefixes; remaining text becomes a full-text search.
func buildEmailFilterFromQuery(query string) db.EmailFilter {
	var f db.EmailFilter
	remaining := searchFieldRe.ReplaceAllStringFunc(query, func(match string) string {
		parts := searchFieldRe.FindStringSubmatch(match)
		switch strings.ToLower(parts[1]) {
		case "from":
			f.From = parts[2]
		case "to":
			f.To = parts[2]
		case "subject":
			f.Subject = parts[2]
		}
		return ""
	})
	text := strings.TrimSpace(remaining)
	if text != "" {
		f.Text = text
	} else if f.From == "" && f.To == "" && f.Subject == "" {
		f.Text = query
	}
	return f
}

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

func buildContactsMap(contacts []models.Contact) map[string]*models.Contact {
	m := make(map[string]*models.Contact, len(contacts))
	for i := range contacts {
		m[strings.ToLower(contacts[i].Email)] = &contacts[i]
	}
	return m
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
