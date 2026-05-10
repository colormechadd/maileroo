package web

import (
	"io"
	"log/slog"
	"mime"
	"net/http"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/colormechadd/mailaroo/templates"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

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

	currentMailboxID := uuid.Nil
	if len(mailboxes) > 0 {
		currentMailboxID = mailboxes[0].ID
	}
	s.render(w, r, user, mailboxes, currentMailboxID, "all", nil, templates.UserInfo(user, mailboxes, sendingAddresses), "Account")
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
