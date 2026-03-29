package smtp

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"

	"github.com/colormechadd/maileroo/internal/config"
	"github.com/colormechadd/maileroo/internal/db"
	"github.com/colormechadd/maileroo/internal/pipeline"
	gosmtp "github.com/emersion/go-smtp"
	"github.com/google/uuid"
)

// RecipientInfo stores mapping details for a recipient
type RecipientInfo struct {
	Address   string
	UserID    uuid.UUID
	MailboxID uuid.UUID
	MappingID uuid.UUID
}

// Backend implements smtp.Backend
type Backend struct {
	cfg      config.SMTPConfig
	db       db.MailDB
	pipeline *pipeline.Pipeline
}

func (bkd *Backend) NewSession(c *gosmtp.Conn) (gosmtp.Session, error) {
	remoteIP, _, _ := net.SplitHostPort(c.Conn().RemoteAddr().String())
	slog.Info("new smtp connection", "remote_addr", c.Conn().RemoteAddr().String())
	return &Session{
		backend:  bkd,
		remoteIP: net.ParseIP(remoteIP),
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

	slog.Info("recipient accepted", "to", to, "mailbox_id", mb.ID)
	s.to = append(s.to, RecipientInfo{
		Address:   to,
		UserID:    mb.UserID,
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
			UserID:           rcpt.UserID,
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
func StartServers(cfg config.SMTPConfig, mailDB db.MailDB, p *pipeline.Pipeline) ([]*gosmtp.Server, error) {
	var servers []*gosmtp.Server
	be := &Backend{cfg: cfg, db: mailDB, pipeline: p}

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
