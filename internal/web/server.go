package web

import (
	"encoding/base64"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/colormechadd/mailaroo/internal/config"
	"github.com/colormechadd/mailaroo/internal/db"
	"github.com/colormechadd/mailaroo/internal/mail"
	"github.com/colormechadd/mailaroo/internal/outbound"
	"github.com/colormechadd/mailaroo/internal/proxy"
	"github.com/colormechadd/mailaroo/internal/rspamd"
	"github.com/colormechadd/mailaroo/internal/storage"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/csrf"
	"golang.org/x/time/rate"
)

type Server struct {
	ServerConfig

	loginMu       sync.Mutex
	loginLimiters map[string]*loginEntry
	proxyKey      []byte
	secureCookies bool
}

type loginEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type ServerConfig struct {
	Config      config.Config
	DB          db.WebDB
	RateLimitDB db.RateLimitDB
	Storage     storage.Storage
	Hub         *Hub
	Sender      outbound.Sender
	Mail        *mail.Service
	Rspamd      *rspamd.Client
}

func NewServer(cfg ServerConfig) *Server {
	s := &Server{
		ServerConfig:  cfg,
		loginLimiters: make(map[string]*loginEntry),
	}
	go s.cleanupLoginLimiters()
	return s
}

func (s *Server) loginLimiter(ip string) *rate.Limiter {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	if e, ok := s.loginLimiters[ip]; ok {
		e.lastSeen = time.Now()
		return e.limiter
	}
	e := &loginEntry{
		limiter:  rate.NewLimiter(rate.Every(time.Minute), 5),
		lastSeen: time.Now(),
	}
	s.loginLimiters[ip] = e
	return e.limiter
}

func (s *Server) cleanupLoginLimiters() {
	for {
		time.Sleep(5 * time.Minute)
		s.loginMu.Lock()
		for ip, e := range s.loginLimiters {
			if time.Since(e.lastSeen) > time.Hour {
				delete(s.loginLimiters, ip)
			}
		}
		s.loginMu.Unlock()
	}
}

func (s *Server) Routes() http.Handler {
	csrfKey, err := base64.StdEncoding.DecodeString(s.Config.Web.CSRFAuthKey)
	if err != nil || len(csrfKey) != 32 {
		panic("WEB_CSRF_AUTH_KEY must be a base64-encoded 32-byte key")
	}
	if pk, err := proxy.DeriveKey(csrfKey); err != nil {
		panic("failed to derive proxy signing key: " + err.Error())
	} else {
		s.proxyKey = pk
	}
	s.secureCookies = len(s.Config.Web.CertFile) > 0 || s.Config.Web.TrustProxy
	csrfMiddleware := csrf.Protect(
		csrfKey,
		csrf.Secure(s.secureCookies),
		csrf.RequestHeader("X-CSRF-Token"),
		csrf.FieldName("gorilla.csrf.Token"),
		csrf.ErrorHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			slog.Warn("CSRF token invalid", "method", r.Method, "path", r.URL.Path, "reason", csrf.FailureReason(r))
			http.Error(w, "Forbidden - CSRF token invalid", http.StatusForbidden)
		})),
	)

	r := chi.NewRouter()

	r.Use(chiMiddleware.RequestID)
	if s.Config.Web.TrustProxy {
		r.Use(chiMiddleware.RealIP)
	}
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(securityHeaders)
	r.Use(htmxCSRFMiddleware)

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		slog.Warn("route not found", "method", r.Method, "path", r.URL.Path)
		http.Error(w, "Not found", http.StatusNotFound)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		slog.Warn("method not allowed", "method", r.Method, "path", r.URL.Path)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	})

	fs := http.FileServer(http.Dir("static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fs))

	r.Get("/login", s.handleLoginGet)
	r.Post("/login", s.handleLoginPost)
	r.Get("/logout", s.handleLogout)
	r.Get("/proxy/image", s.handleProxyImage)

	r.Group(func(r chi.Router) {
		r.Use(s.AuthMiddleware)
		r.Get("/", s.handleDashboard)
		r.Get("/events", s.handleEvents)
		r.Get("/mailbox/{mailboxID}", s.handleMailboxView)
		r.Get("/mailbox/{mailboxID}/more", s.handleMailboxMore)
		r.Get("/mailbox/{mailboxID}/search", s.handleMailboxSearch)
		r.Get("/mailbox/{mailboxID}/search/more", s.handleSearchMore)
		r.Get("/mailbox/{mailboxID}/unread-count", s.handleMailboxUnreadCount)
		r.Group(func(r chi.Router) {
			r.Use(s.validateUserAccessToEmailID)

			r.Get("/email/{emailID}", s.handleEmailView)
			r.Get("/email/{emailID}/headers", s.handleEmailHeaders)
			r.Get("/email/{emailID}/pipeline", s.handleEmailPipeline)
			r.Post("/email/{emailID}/star", s.handleEmailStar)
			r.Post("/email/{emailID}/delete", s.handleEmailDelete)
			r.Post("/email/{emailID}/release", s.handleEmailRelease)
			r.Post("/email/{emailID}/mark-spam", s.handleEmailMarkSpam)
			r.Post("/email/{emailID}/mark-ham", s.handleEmailMarkHam)
			r.Post("/email/{emailID}/unsubscribe", s.handleEmailUnsubscribe)
			r.Post("/email/{emailID}/block", s.handleEmailBlockSender)
			r.Post("/email/{emailID}/delete-and-block", s.handleEmailDeleteAndBlock)
		})
		r.Get("/attachment/{attachmentID}", s.handleAttachmentDownload)

		r.Get("/compose", s.handleCompose)
		r.Post("/send", s.handleEmailSend)

		r.Get("/user-info", s.handleUserInfo)
		r.Post("/user/sending-address/{saID}/display-name", s.handleUpdateDisplayName)

		r.Post("/draft", s.handleDraftSave)
		r.Delete("/draft/{draftID}", s.handleDraftDelete)

		r.Get("/contacts/search", s.handleContactSearch)
		r.Get("/mailbox/{mailboxID}/contacts", s.handleContactsPage)
		r.Post("/mailbox/{mailboxID}/contacts", s.handleContactCreate)
		r.Get("/mailbox/{mailboxID}/contacts/{contactID}", s.handleContactView)
		r.Put("/mailbox/{mailboxID}/contacts/{contactID}", s.handleContactUpdate)
		r.Delete("/mailbox/{mailboxID}/contacts/{contactID}", s.handleContactDelete)
		r.Post("/mailbox/{mailboxID}/contacts/{contactID}/favorite", s.handleContactToggleFavorite)
		r.Post("/email/{emailID}/add-contact", s.handleAddContactFromEmail)

		r.Post("/mailbox/{mailboxID}/bulk", s.handleBulkEmailAction)
		r.Get("/mailbox/{mailboxID}/filters", s.handleFilterRulesList)
		r.Post("/mailbox/{mailboxID}/filters", s.handleFilterRuleCreate)
		r.Get("/mailbox/{mailboxID}/filters/new", s.handleFilterRuleNew)
		r.Get("/mailbox/{mailboxID}/filters/{ruleID}/edit", s.handleFilterRuleEdit)
		r.Put("/mailbox/{mailboxID}/filters/{ruleID}", s.handleFilterRuleUpdate)
		r.Delete("/mailbox/{mailboxID}/filters/{ruleID}", s.handleFilterRuleDelete)
		r.Post("/mailbox/{mailboxID}/filters/reorder", s.handleFilterRuleReorder)
		r.Post("/mailbox/{mailboxID}/block-rules", s.handleManualBlockSender)
		r.Delete("/mailbox/{mailboxID}/block-rules/{blockRuleID}", s.handleUnblockSender)
	})

	handler := csrfMiddleware(r)
	if s.Config.Web.TrustProxy {
		handler = forwardedHostMiddleware(handler)
	}
	return handler
}
