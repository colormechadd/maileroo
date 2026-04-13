package smtp

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/colormechadd/mailaroo/internal/config"
	"github.com/colormechadd/mailaroo/internal/db"
	"github.com/colormechadd/mailaroo/internal/pipeline"
	gosmtp "github.com/emersion/go-smtp"
	"github.com/google/uuid"
	"golang.org/x/time/rate"
)

// RecipientInfo stores mapping details for a recipient
type RecipientInfo struct {
	Address   string
	MailboxID uuid.UUID
	MappingID uuid.UUID
}

// Backend implements smtp.Backend
type Backend struct {
	cfg         config.SMTPConfig
	rateCfg     config.RateLimitConfig
	db          db.MailDB
	rateLimitDB db.RateLimitDB
	pipeline    *pipeline.Pipeline
	mu          sync.Mutex
	limiters    map[string]*rate.Limiter
	violations  map[string]int
}

func (bkd *Backend) getLimiter(ip string) *rate.Limiter {
	bkd.mu.Lock()
	defer bkd.mu.Unlock()
	if l, ok := bkd.limiters[ip]; ok {
		return l
	}
	r := rate.Every(time.Minute / time.Duration(bkd.rateCfg.SMTPConnectionsPerMinute))
	l := rate.NewLimiter(r, bkd.rateCfg.SMTPConnectionsPerMinute)
	bkd.limiters[ip] = l
	return l
}

func (bkd *Backend) NewSession(c *gosmtp.Conn) (gosmtp.Session, error) {
	remoteIP, _, _ := net.SplitHostPort(c.Conn().RemoteAddr().String())
	slog.Info("new smtp connection", "remote_addr", c.Conn().RemoteAddr().String())

	parsedIP := net.ParseIP(remoteIP)

	// // Check persistent IP block list
	// if parsedIP != nil {
	// 	blocked, err := bkd.rateLimitDB.IsIPBlocked(context.Background(), parsedIP)
	// 	if err != nil {
	// 		slog.Error("failed to check ip block", "ip", remoteIP, "error", err)
	// 	} else if blocked {
	// 		slog.Warn("smtp connection rejected: ip blocked", "ip", remoteIP)
	// 		return nil, &gosmtp.SMTPError{
	// 			Code:    421,
	// 			Message: "Your IP address is temporarily blocked",
	// 		}
	// 	}
	// }

	// // In-memory per-IP rate limiting
	// if parsedIP != nil && bkd.rateCfg.SMTPConnectionsPerMinute > 0 {
	// 	limiter := bkd.getLimiter(remoteIP)
	// 	if !limiter.Allow() {
	// 		bkd.mu.Lock()
	// 		bkd.violations[remoteIP]++
	// 		violations := bkd.violations[remoteIP]
	// 		bkd.mu.Unlock()

	// 		slog.Warn("smtp connection rate limited", "ip", remoteIP, "violations", violations)

	// 		if bkd.rateCfg.SMTPAutoBlockThreshold > 0 && violations >= bkd.rateCfg.SMTPAutoBlockThreshold {
	// 			until := time.Now().Add(bkd.rateCfg.SMTPAutoBlockDuration)
	// 			if err := bkd.rateLimitDB.AddIPBlock(context.Background(), parsedIP, "auto-blocked: rate limit exceeded", &until); err != nil {
	// 				slog.Error("failed to auto-block ip", "ip", remoteIP, "error", err)
	// 			} else {
	// 				slog.Warn("smtp ip auto-blocked", "ip", remoteIP, "until", until)
	// 				bkd.mu.Lock()
	// 				delete(bkd.violations, remoteIP)
	// 				bkd.mu.Unlock()
	// 			}
	// 		}

	// 		return nil, &gosmtp.SMTPError{
	// 			Code:    421,
	// 			Message: "Too many connections from your IP, please try again later",
	// 		}
	// 	}
	// }

	return &Session{
		backend:  bkd,
		remoteIP: parsedIP,
	}, nil
}

// Session implements smtp.Session
type Session struct {
	backend  *Backend
	remoteIP net.IP
	from     string
	to       []RecipientInfo
}

func (s *Session) AuthPlain(username, password string) error {
	return &gosmtp.SMTPError{
		Code:    503,
		Message: "AUTH not supported on this server",
	}
}

func (s *Session) Mail(from string, opts *gosmtp.MailOptions) error {
	slog.Info("smtp mail from", "from", from, "remote_ip", s.remoteIP)
	s.from = from
	return nil
}

func (s *Session) Rcpt(to string, opts *gosmtp.RcptOptions) error {
	mb, mappingID, err := s.backend.db.LookupMailboxByAddress(context.Background(), to)
	if err != nil {
		slog.Warn("recipient rejected: mailbox not found", "to", to, "from", s.from, "error", err)
		return &gosmtp.SMTPError{
			Code:         550,
			EnhancedCode: gosmtp.EnhancedCode{5, 1, 1},
			Message:      "User unknown",
		}
	}

	// Greylisting: check (ip, from, to) triplet before accepting the recipient
	if s.remoteIP != nil && s.backend.rateCfg.GreylistEnabled {
		pass, err := s.backend.rateLimitDB.CheckAndUpdateGreylist(
			context.Background(), s.remoteIP, s.from, to, s.backend.rateCfg.GreylistDelay,
		)
		if err != nil {
			slog.Error("greylist check failed", "ip", s.remoteIP, "from", s.from, "to", to, "error", err)
		} else if !pass {
			slog.Info("smtp recipient greylisted", "ip", s.remoteIP, "from", s.from, "to", to)
			return &gosmtp.SMTPError{
				Code:         451,
				EnhancedCode: gosmtp.EnhancedCode{4, 7, 1},
				Message:      "Greylisted, please try again later",
			}
		}
	}

	slog.Info("recipient accepted", "to", to, "mailbox_id", mb.ID)
	s.to = append(s.to, RecipientInfo{
		Address:   to,
		MailboxID: mb.ID,
		MappingID: mappingID,
	})
	return nil
}

func (s *Session) Data(r io.Reader) error {
	slog.Info("smtp data received, processing message", "from", s.from, "rcpt_count", len(s.to))
	data, err := io.ReadAll(r)
	if err != nil {
		slog.Error("failed to read email data", "error", err)
		return err
	}

	for _, rcpt := range s.to {
		ictx := &pipeline.IngestionContext{
			ID:               uuid.New(),
			RemoteIP:         s.remoteIP,
			FromAddress:      s.from,
			ToAddresses:      []string{rcpt.Address},
			RawMessage:       data,
			TargetMailboxID:  rcpt.MailboxID,
			AddressMappingID: rcpt.MappingID,
		}

		slog.Info("queueing ingestion", "ingestion_id", ictx.ID, "from", s.from, "to", rcpt.Address)

		go func(ctx *pipeline.IngestionContext) {
			if err := s.backend.pipeline.Process(context.Background(), ctx); err != nil {
				slog.Error("pipeline processing failed", "ingestion_id", ctx.ID, "error", err)
			}
		}(ictx)
	}

	return nil
}

func (s *Session) Reset() {
	slog.Info("smtp session reset", "from", s.from)
	s.from = ""
	s.to = nil
}

func (s *Session) Logout() error {
	slog.Info("smtp session logout", "from", s.from)
	return nil
}

// StartServers initializes and starts the SMTP servers on the configured ports
func StartServers(cfg config.SMTPConfig, rateCfg config.RateLimitConfig, mailDB db.MailDB, rateLimitDB db.RateLimitDB, p *pipeline.Pipeline) ([]*gosmtp.Server, error) {
	var servers []*gosmtp.Server
	be := &Backend{
		cfg:         cfg,
		rateCfg:     rateCfg,
		db:          mailDB,
		rateLimitDB: rateLimitDB,
		pipeline:    p,
		limiters:    make(map[string]*rate.Limiter),
		violations:  make(map[string]int),
	}

	var tlsConfig *tls.Config
	if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
		slog.Info("loading smtp tls certificates", "cert", cfg.TLSCertFile, "key", cfg.TLSKeyFile)
		cert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS key pair: %w", err)
		}
		tlsConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
	} else {
		slog.Warn("no smtp tls certificates provided, STARTTLS will not be available")
	}

	for _, port := range cfg.Ports {
		s := gosmtp.NewServer(be)
		s.Addr = fmt.Sprintf(":%d", port)
		s.Domain = cfg.Domain
		s.ReadTimeout = cfg.ReadTimeout
		s.WriteTimeout = cfg.WriteTimeout
		s.MaxMessageBytes = cfg.MaxMessageSize
		s.MaxRecipients = cfg.MaxRecipients
		s.AllowInsecureAuth = false
		s.TLSConfig = tlsConfig

		servers = append(servers, s)
	}

	return servers, nil
}
