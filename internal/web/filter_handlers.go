package web

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/colormechadd/mailaroo/templates"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/csrf"
)

func (s *Server) handleFilterRulesList(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	mailboxID, mailboxes, err := s.getMailboxForUser(r, chi.URLParam(r, "mailboxID"), user.ID)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	rules, err := s.DB.ListFilterRules(r.Context(), mailboxID)
	if err != nil {
		slog.Error("failed to list filter rules", "mailbox_id", mailboxID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	mbUsers, err := s.DB.GetMailboxUsers(r.Context(), mailboxID)
	if err != nil {
		slog.Error("failed to list mailbox users", "mailbox_id", mailboxID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	sendingAddresses, err := s.DB.GetSendingAddressesByMailboxID(r.Context(), mailboxID)
	if err != nil {
		slog.Error("failed to list sending addresses", "mailbox_id", mailboxID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	blockRules, err := s.DB.ListBlockRules(r.Context(), mailboxID)
	if err != nil {
		slog.Error("failed to list block rules", "mailbox_id", mailboxID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	var mbName string
	for _, mb := range mailboxes {
		if mb.ID == mailboxID {
			mbName = mb.Name
			break
		}
	}

	counts := s.getCounts(r.Context(), mailboxID, user.ID)
	s.render(w, r, user, mailboxes, mailboxID, "filters", counts, templates.MailboxConfig(mailboxID, mbName, mbUsers, sendingAddresses, rules, blockRules, csrf.Token(r)), "Configure Mailbox")
}

func (s *Server) handleFilterRuleNew(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	mailboxID, mailboxes, err := s.getMailboxForUser(r, chi.URLParam(r, "mailboxID"), user.ID)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	counts := s.getCounts(r.Context(), mailboxID, user.ID)
	s.render(w, r, user, mailboxes, mailboxID, "filters", counts, templates.FilterRuleForm(mailboxID, nil, csrf.Token(r)), "New Filter")
}

func (s *Server) handleFilterRuleCreate(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	mailboxID, _, err := s.getMailboxForUser(r, chi.URLParam(r, "mailboxID"), user.ID)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	rule, err := parseFilterRuleForm(r, mailboxID)
	if err != nil {
		http.Error(w, "Bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.DB.CreateFilterRule(r.Context(), rule); err != nil {
		slog.Error("failed to create filter rule", "mailbox_id", mailboxID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/mailbox/"+mailboxID.String()+"/filters", http.StatusSeeOther)
}

func (s *Server) handleFilterRuleEdit(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	mailboxID, mailboxes, err := s.getMailboxForUser(r, chi.URLParam(r, "mailboxID"), user.ID)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	ruleID, err := uuid.Parse(chi.URLParam(r, "ruleID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	rule, err := s.DB.GetFilterRuleByID(r.Context(), ruleID, mailboxID)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	counts := s.getCounts(r.Context(), mailboxID, user.ID)
	s.render(w, r, user, mailboxes, mailboxID, "filters", counts, templates.FilterRuleForm(mailboxID, rule, csrf.Token(r)), "Edit Filter")
}

func (s *Server) handleFilterRuleUpdate(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	mailboxID, _, err := s.getMailboxForUser(r, chi.URLParam(r, "mailboxID"), user.ID)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	ruleID, err := uuid.Parse(chi.URLParam(r, "ruleID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	rule, err := parseFilterRuleForm(r, mailboxID)
	if err != nil {
		http.Error(w, "Bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	rule.ID = ruleID

	if err := s.DB.UpdateFilterRule(r.Context(), rule); err != nil {
		slog.Error("failed to update filter rule", "rule_id", ruleID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/mailbox/"+mailboxID.String()+"/filters", http.StatusSeeOther)
}

func (s *Server) handleFilterRuleDelete(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	mailboxID, _, err := s.getMailboxForUser(r, chi.URLParam(r, "mailboxID"), user.ID)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	ruleID, err := uuid.Parse(chi.URLParam(r, "ruleID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	if err := s.DB.DeleteFilterRule(r.Context(), ruleID, mailboxID); err != nil {
		slog.Error("failed to delete filter rule", "rule_id", ruleID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	_ = user

	w.Header().Set("HX-Redirect", "/mailbox/"+mailboxID.String()+"/filters")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleFilterRuleReorder(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	mailboxID, _, err := s.getMailboxForUser(r, chi.URLParam(r, "mailboxID"), user.ID)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	_ = user

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	rawIDs := r.Form["rule_id"]
	orderedIDs := make([]uuid.UUID, 0, len(rawIDs))
	for _, raw := range rawIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			http.Error(w, "Invalid ID", http.StatusBadRequest)
			return
		}
		orderedIDs = append(orderedIDs, id)
	}

	if err := s.DB.ReorderFilterRules(r.Context(), mailboxID, orderedIDs); err != nil {
		slog.Error("failed to reorder filter rules", "mailbox_id", mailboxID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func parseFilterRuleForm(r *http.Request, mailboxID uuid.UUID) (*models.FilterRule, error) {
	if err := r.ParseForm(); err != nil {
		return nil, err
	}

	action := r.FormValue("action")
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = "Filter rule"
	}

	rule := &models.FilterRule{
		ID:             uuid.New(),
		MailboxID:      mailboxID,
		Name:           name,
		IsActive:       r.FormValue("is_active") == "on" || r.FormValue("is_active") == "true",
		MatchAll:       r.FormValue("match_all") != "false",
		Action:         action,
		StopProcessing: r.FormValue("stop_processing") != "false",
	}

	fields := r.Form["condition_field"]
	operators := r.Form["condition_operator"]
	values := r.Form["condition_value"]

	for i := range fields {
		if i >= len(operators) {
			break
		}
		var val *string
		if i < len(values) && values[i] != "" {
			v := values[i]
			val = &v
		}
		rule.Conditions = append(rule.Conditions, models.FilterCondition{
			ID:       uuid.New(),
			Field:    fields[i],
			Operator: operators[i],
			Value:    val,
		})
	}

	return rule, nil
}
