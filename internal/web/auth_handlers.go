package web

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/colormechadd/mailaroo/pkg/auth"
	"github.com/colormechadd/mailaroo/templates"
	"github.com/gorilla/csrf"
)

func (s *Server) handleLoginGet(w http.ResponseWriter, r *http.Request) {
	templates.LoginPage("", csrf.Token(r)).Render(r.Context(), w)
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	remoteIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	if remoteIP == "" {
		remoteIP = r.RemoteAddr
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	// recordFailure consumes a token from the per-IP limiter and, if exhausted,
	// returns true to signal that the caller should reject with 429.
	recordFailure := func() bool {
		limiter := s.loginLimiter(remoteIP)
		if !limiter.Allow() {
			slog.Warn("login rate limited", "ip", remoteIP)
			http.Error(w, "Too many failed login attempts, please try again later", http.StatusTooManyRequests)
			return true
		}
		return false
	}

	user, err := s.DB.GetUserByUsername(r.Context(), username)
	if err != nil || !user.IsActive {
		slog.Warn("login failed: user not found or inactive", "username", username)
		if recordFailure() {
			return
		}
		templates.LoginPage("Invalid credentials", csrf.Token(r)).Render(r.Context(), w)
		return
	}

	match, err := auth.ComparePassword(password, user.PasswordHash)
	if err != nil || !match {
		slog.Warn("login failed: incorrect password", "username", username)
		if recordFailure() {
			return
		}
		templates.LoginPage("Invalid credentials", csrf.Token(r)).Render(r.Context(), w)
		return
	}

	token := generateToken()
	expires := time.Now().Add(24 * time.Hour)
	if err := s.DB.CreateWebmailSession(r.Context(), user.ID, token, r.RemoteAddr, r.UserAgent(), expires); err != nil {
		slog.Error("failed to create session", "user_id", user.ID, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "mailaroo_session",
		Value:    token,
		Expires:  expires,
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("mailaroo_session")
	if err == nil {
		if err := s.DB.ExpireWebmailSession(r.Context(), cookie.Value); err != nil {
			slog.Error("failed to expire session on logout", "error", err)
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "mailaroo_session",
		Value:    "",
		Expires:  time.Now().Add(-1 * time.Hour),
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
	})

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func generateToken() string {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic("failed to generate random token: " + err.Error())
	}
	return hex.EncodeToString(b)
}
