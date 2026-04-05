package web

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"

	"github.com/colormechadd/maileroo/pkg/models"
	"github.com/colormechadd/maileroo/templates"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (s *Server) handleContactsPage(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	contacts, err := s.DB.ListContacts(r.Context(), user.ID)
	if err != nil {
		slog.Error("failed to list contacts", "user_id", user.ID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	selectedIDRaw := r.URL.Query().Get("id")
	var selected *models.Contact
	if selectedIDRaw != "" {
		if id, err := uuid.Parse(selectedIDRaw); err == nil {
			for i := range contacts {
				if contacts[i].ID == id {
					selected = &contacts[i]
					break
				}
			}
		}
	}

	mailboxes, _ := s.DB.GetMailboxesByUserID(r.Context(), user.ID)
	s.render(w, r, user, mailboxes, uuid.Nil, "contacts", nil, templates.ContactsPage(contacts, selected))
}

func (s *Server) handleContactSearch(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
		return
	}

	contacts, err := s.DB.SearchContacts(r.Context(), user.ID, q)
	if err != nil {
		slog.Error("failed to search contacts", "user_id", user.ID, "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
		return
	}

	type result struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Email    string `json:"email"`
		Initials string `json:"initials"`
		Display  string `json:"display"`
	}
	out := make([]result, 0, len(contacts))
	for _, c := range contacts {
		display := c.Email
		name := strings.TrimSpace(c.FirstName + " " + c.LastName)
		if name != "" {
			display = name + " <" + c.Email + ">"
		}
		out = append(out, result{
			ID:       c.ID.String(),
			Name:     name,
			Email:    c.Email,
			Initials: c.Initials(),
			Display:  display,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func (s *Server) handleContactCreate(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	c := models.Contact{
		UserID:     user.ID,
		FirstName:  strings.TrimSpace(r.FormValue("first_name")),
		LastName:   strings.TrimSpace(r.FormValue("last_name")),
		Email:      strings.TrimSpace(r.FormValue("email")),
		Phone:      strings.TrimSpace(r.FormValue("phone")),
		Street:     strings.TrimSpace(r.FormValue("street")),
		City:       strings.TrimSpace(r.FormValue("city")),
		State:      strings.TrimSpace(r.FormValue("state")),
		PostalCode: strings.TrimSpace(r.FormValue("postal_code")),
		Country:    strings.TrimSpace(r.FormValue("country")),
		Notes:      strings.TrimSpace(r.FormValue("notes")),
		IsFavorite: r.FormValue("is_favorite") == "true" || r.FormValue("is_favorite") == "on",
	}

	if c.Email == "" {
		http.Error(w, "Email is required", http.StatusBadRequest)
		return
	}

	created, err := s.DB.CreateContact(r.Context(), c)
	if err != nil {
		slog.Error("failed to create contact", "user_id", user.ID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	contacts, _ := s.DB.ListContacts(r.Context(), user.ID)
	templates.ContactsPage(contacts, created).Render(r.Context(), w)
}

func (s *Server) handleContactUpdate(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	contactID, err := uuid.Parse(chi.URLParam(r, "contactID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	c := models.Contact{
		ID:         contactID,
		UserID:     user.ID,
		FirstName:  strings.TrimSpace(r.FormValue("first_name")),
		LastName:   strings.TrimSpace(r.FormValue("last_name")),
		Email:      strings.TrimSpace(r.FormValue("email")),
		Phone:      strings.TrimSpace(r.FormValue("phone")),
		Street:     strings.TrimSpace(r.FormValue("street")),
		City:       strings.TrimSpace(r.FormValue("city")),
		State:      strings.TrimSpace(r.FormValue("state")),
		PostalCode: strings.TrimSpace(r.FormValue("postal_code")),
		Country:    strings.TrimSpace(r.FormValue("country")),
		Notes:      strings.TrimSpace(r.FormValue("notes")),
		IsFavorite: r.FormValue("is_favorite") == "true" || r.FormValue("is_favorite") == "on",
	}

	if c.Email == "" {
		http.Error(w, "Email is required", http.StatusBadRequest)
		return
	}

	if err := s.DB.UpdateContact(r.Context(), c); err != nil {
		slog.Error("failed to update contact", "contact_id", contactID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	updated, _ := s.DB.GetContactByID(r.Context(), contactID, user.ID)
	contacts, _ := s.DB.ListContacts(r.Context(), user.ID)
	templates.ContactsPage(contacts, updated).Render(r.Context(), w)
}

func (s *Server) handleContactDelete(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	contactID, err := uuid.Parse(chi.URLParam(r, "contactID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	if err := s.DB.DeleteContact(r.Context(), contactID, user.ID); err != nil {
		slog.Error("failed to delete contact", "contact_id", contactID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	contacts, _ := s.DB.ListContacts(r.Context(), user.ID)
	templates.ContactsPage(contacts, nil).Render(r.Context(), w)
}

func (s *Server) handleContactToggleFavorite(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	contactID, err := uuid.Parse(chi.URLParam(r, "contactID"))
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	if err := s.DB.ToggleContactFavorite(r.Context(), contactID, user.ID); err != nil {
		slog.Error("failed to toggle favorite", "contact_id", contactID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	updated, _ := s.DB.GetContactByID(r.Context(), contactID, user.ID)
	contacts, _ := s.DB.ListContacts(r.Context(), user.ID)
	templates.ContactsPage(contacts, updated).Render(r.Context(), w)
}

func (s *Server) handleAddContactFromEmail(w http.ResponseWriter, r *http.Request) {
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

	addr, err := mail.ParseAddress(email.FromAddress)
	firstName := ""
	lastName := ""
	emailAddr := email.FromAddress
	if err == nil {
		emailAddr = addr.Address
		parts := strings.Fields(addr.Name)
		if len(parts) == 1 {
			firstName = parts[0]
		} else if len(parts) >= 2 {
			firstName = strings.Join(parts[:len(parts)-1], " ")
			lastName = parts[len(parts)-1]
		}
	}

	if err := s.DB.UpsertContactFromEmail(r.Context(), user.ID, emailAddr, firstName, lastName); err != nil {
		slog.Error("failed to upsert contact from email", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	contacts, _ := s.DB.ListContacts(r.Context(), user.ID)
	var selected *models.Contact
	for i := range contacts {
		if contacts[i].Email == emailAddr {
			selected = &contacts[i]
			break
		}
	}

	mailboxes, _ := s.DB.GetMailboxesByUserID(r.Context(), user.ID)
	s.render(w, r, user, mailboxes, uuid.Nil, "contacts", nil, templates.ContactsPage(contacts, selected))
}
