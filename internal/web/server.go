package web

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/colormechadd/maileroo/internal/config"
	"github.com/colormechadd/maileroo/internal/db"
	"github.com/colormechadd/maileroo/internal/storage"
	"github.com/colormechadd/maileroo/pkg/auth"
	"github.com/colormechadd/maileroo/pkg/models"
	"github.com/colormechadd/maileroo/templates"
	"github.com/emersion/go-message/mail"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/klauspost/compress/zstd"
)

type Server struct {
	cfg     config.Config
	db      db.WebDB
	storage storage.Storage
}

func NewServer(cfg config.Config, webDB db.WebDB, storage storage.Storage) *Server {
	return &Server{
		cfg:     cfg,
		db:      webDB,
		storage: storage,
	}
}

func (s *Server) Routes() chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Serve static files
	fs := http.FileServer(http.Dir("static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fs))

	r.Get("/login", templ.Handler(templates.LoginPage("")).ServeHTTP)
	r.Post("/login", s.handleLoginPost)
	r.Post("/logout", s.handleLogout)

	r.Group(func(r chi.Router) {
		r.Use(s.AuthMiddleware)
		r.Get("/", s.handleDashboard)
		r.Get("/mailbox/{mailboxID}", s.handleMailboxView)
		r.Get("/email/{emailID}", s.handleEmailView)
	})

	return r
}

func (s *Server) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("maileroo_session")
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		session, err := s.db.GetWebmailSession(r.Context(), cookie.Value)
		if err != nil || session.ExpiresDatetime.Before(time.Now()) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		user, err := s.db.GetUserByID(r.Context(), session.UserID)
		if err != nil || !user.IsActive {
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
		templates.LoginPage("Invalid credentials").Render(r.Context(), w)
		return
	}

	match, err := auth.ComparePassword(password, user.PasswordHash)
	if err != nil || !match {
		templates.LoginPage("Invalid credentials").Render(r.Context(), w)
		return
	}

	token := generateToken()
	expires := time.Now().Add(24 * time.Hour)
	remoteIP := r.RemoteAddr
	userAgent := r.UserAgent()

	if err := s.db.CreateWebmailSession(r.Context(), user.ID, token, remoteIP, userAgent, expires); err != nil {
		slog.Error("failed to create webmail session", "error", err)
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
			slog.Error("failed to expire webmail session", "error", err)
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

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)

	mailboxes, err := s.db.GetMailboxesByUserID(r.Context(), user.ID)
	if err != nil {
		slog.Error("failed to fetch mailboxes", "user_id", user.ID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if len(mailboxes) > 0 {
		http.Redirect(w, r, "/mailbox/"+mailboxes[0].ID.String(), http.StatusSeeOther)
		return
	}

	templates.Dashboard(user, mailboxes, uuid.Nil, nil).Render(r.Context(), w)
}

func (s *Server) handleMailboxView(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	mailboxIDStr := chi.URLParam(r, "mailboxID")
	mailboxID, err := uuid.Parse(mailboxIDStr)
	if err != nil {
		http.Error(w, "Invalid mailbox ID", http.StatusBadRequest)
		return
	}

	mailboxes, err := s.db.GetMailboxesByUserID(r.Context(), user.ID)
	if err != nil {
		slog.Error("failed to fetch mailboxes", "user_id", user.ID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	var found bool
	for _, mb := range mailboxes {
		if mb.ID == mailboxID {
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "Mailbox not found", http.StatusNotFound)
		return
	}

	emails, err := s.db.GetEmailsByMailboxID(r.Context(), mailboxID, 50, 0)
	if err != nil {
		slog.Error("failed to fetch emails", "mailbox_id", mailboxID, "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		templates.MailboxContent(mailboxes, mailboxID, emails).Render(r.Context(), w)
		return
	}

	templates.Dashboard(user, mailboxes, mailboxID, emails).Render(r.Context(), w)
}

func (s *Server) handleEmailView(w http.ResponseWriter, r *http.Request) {
	emailIDStr := chi.URLParam(r, "emailID")
	emailID, err := uuid.Parse(emailIDStr)
	if err != nil {
		http.Error(w, "Invalid email ID", http.StatusBadRequest)
		return
	}

	email, err := s.db.GetEmailByID(r.Context(), emailID)
	if err != nil {
		slog.Error("failed to fetch email", "email_id", emailID, "error", err)
		http.Error(w, "Email not found", http.StatusNotFound)
		return
	}

	attachments, err := s.db.GetAttachmentsByEmailID(r.Context(), emailID)
	if err != nil {
		slog.Warn("failed to fetch attachments", "email_id", emailID, "error", err)
	}

	rc, err := s.storage.Get(r.Context(), email.StorageKey)
	if err != nil {
		slog.Error("failed to fetch email body from storage", "key", email.StorageKey, "error", err)
		http.Error(w, "Failed to load email content", http.StatusInternalServerError)
		return
	}
	defer rc.Close()

	var bodyReader io.Reader = rc
	if strings.HasSuffix(email.StorageKey, ".zst") {
		zr, err := zstd.NewReader(rc)
		if err != nil {
			slog.Error("failed to create zstd reader", "key", email.StorageKey, "error", err)
		} else {
			defer zr.Close()
			bodyReader = zr
		}
	} else if strings.HasSuffix(email.StorageKey, ".gz") {
		gr, err := gzip.NewReader(rc)
		if err != nil {
			slog.Error("failed to create gzip reader", "key", email.StorageKey, "error", err)
		} else {
			defer gr.Close()
			bodyReader = gr
		}
	}

	mr, err := mail.CreateReader(bodyReader)
	var content string
	if err == nil {
		defer mr.Close()
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}

			var contentType string
			switch h := p.Header.(type) {
			case *mail.InlineHeader:
				contentType, _, _ = h.ContentType()
			case *mail.AttachmentHeader:
				contentType, _, _ = h.ContentType()
			}

			if contentType == "text/plain" || contentType == "text/html" {
				b, _ := io.ReadAll(p.Body)
				content = string(b)
				break
			}
		}
	} else {
		b, _ := io.ReadAll(bodyReader)
		content = string(b)
	}

	templates.EmailDetail(email, attachments, content).Render(r.Context(), w)
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
