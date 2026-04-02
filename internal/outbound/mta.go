package outbound

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/emersion/go-message/mail"
	"github.com/google/uuid"
)

type Attachment struct {
	Filename    string
	ContentType string
	Content     io.Reader
}

type Message struct {
	From            string
	FromDisplayName string
	To              []string
	Cc              []string
	Bcc             []string
	Subject         string
	TextBody        string
	HTMLBody        string
	InReplyTo       string
	References      string
	Attachments     []Attachment
}

type Sender interface {
	Send(from string, to []string, msg []byte) error
	SendMessage(msg Message) ([]byte, error)
	BuildMessage(msg Message) (rawBytes []byte, from string, recipients []string, err error)
}

type MTA struct {
	hostname string
	relay    string // optional smarthost "host:port"; if set, skip MX lookup
	dkim     *DKIMSigner
}

func NewMTA(hostname string, relay string, dkim *DKIMSigner) *MTA {
	if hostname == "" {
		hostname = "localhost"
	}

	if relay != "" {
		slog.Debug("Using MTA relay", "relay", relay)
	} else {
		slog.Debug("Using MTA host", "host", hostname)
	}
	return &MTA{hostname: hostname, relay: relay, dkim: dkim}
}

func (m *MTA) BuildMessage(msg Message) (rawBytes []byte, from string, recipients []string, err error) {
	var buf bytes.Buffer
	var h mail.Header
	h.SetAddressList("From", []*mail.Address{{Name: msg.FromDisplayName, Address: msg.From}})

	toAddrs := make([]*mail.Address, len(msg.To))
	for i, a := range msg.To {
		toAddrs[i] = &mail.Address{Address: a}
	}
	h.SetAddressList("To", toAddrs)

	if len(msg.Cc) > 0 {
		ccAddrs := make([]*mail.Address, len(msg.Cc))
		for i, a := range msg.Cc {
			ccAddrs[i] = &mail.Address{Address: a}
		}
		h.SetAddressList("Cc", ccAddrs)
	}

	h.SetSubject(msg.Subject)
	h.SetDate(time.Now())
	msgIDHost := m.hostname
	if parts := strings.SplitN(msg.From, "@", 2); len(parts) == 2 {
		msgIDHost = parts[1]
	}
	h.Set("Message-ID", fmt.Sprintf("<%s@%s>", uuid.Must(uuid.NewV7()).String(), msgIDHost))
	h.Set("MIME-Version", "1.0")

	if msg.InReplyTo != "" {
		h.Set("In-Reply-To", msg.InReplyTo)
	}
	if msg.References != "" {
		h.Set("References", msg.References)
	}

	mw, err := mail.CreateWriter(&buf, h)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to create mail writer: %w", err)
	}

	if msg.HTMLBody != "" {
		var hh mail.InlineHeader
		hh.Set("Content-Type", "text/html; charset=utf-8")
		hw, err := mw.CreateSingleInline(hh)
		if err != nil {
			return nil, "", nil, fmt.Errorf("failed to create html part: %w", err)
		}
		io.WriteString(hw, msg.HTMLBody)
		hw.Close()
	} else {
		var th mail.InlineHeader
		th.Set("Content-Type", "text/plain; charset=utf-8")
		tw, err := mw.CreateSingleInline(th)
		if err != nil {
			return nil, "", nil, fmt.Errorf("failed to create text part: %w", err)
		}
		io.WriteString(tw, msg.TextBody)
		tw.Close()
	}

	for _, att := range msg.Attachments {
		var ah mail.AttachmentHeader
		ah.SetFilename(att.Filename)
		if att.ContentType != "" {
			ah.Set("Content-Type", att.ContentType)
		} else {
			ah.Set("Content-Type", "application/octet-stream")
		}
		aw, err := mw.CreateAttachment(ah)
		if err != nil {
			slog.Error("failed to create attachment part", "filename", att.Filename, "error", err)
			continue
		}
		io.Copy(aw, att.Content)
		aw.Close()
	}

	mw.Close()

	raw := buf.Bytes()

	if m.dkim != nil {
		domain := ""
		if parts := strings.SplitN(msg.From, "@", 2); len(parts) == 2 {
			domain = parts[1]
		}
		if domain != "" {
			signed, err := m.dkim.Sign(domain, raw)
			if err != nil {
				slog.Warn("dkim signing failed, sending unsigned", "domain", domain, "error", err)
			} else {
				raw = signed
			}
		}
	}

	allRecipients := append([]string{}, msg.To...)
	allRecipients = append(allRecipients, msg.Cc...)
	allRecipients = append(allRecipients, msg.Bcc...)

	return raw, msg.From, allRecipients, nil
}

func (m *MTA) SendMessage(msg Message) ([]byte, error) {
	raw, from, recipients, err := m.BuildMessage(msg)
	if err != nil {
		return nil, err
	}
	if err := m.Send(from, recipients, raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (m *MTA) Send(from string, to []string, msg []byte) error {
	if m.relay != "" {
		slog.Debug("delivering via relay", "relay", m.relay)
		return m.deliverToRelay(m.relay, from, to, msg)
	}

	domains := make(map[string][]string)
	for _, rcpt := range to {
		parts := strings.Split(rcpt, "@")
		if len(parts) != 2 {
			slog.Warn("invalid recipient address", "address", rcpt)
			continue
		}
		domain := parts[1]
		domains[domain] = append(domains[domain], rcpt)
	}

	var errs []error
	for domain, recipients := range domains {
		if err := m.deliverToDomain(domain, from, recipients, msg); err != nil {
			slog.Error("failed to deliver email", "domain", domain, "error", err)
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to deliver to some recipients: %v", errs)
	}
	return nil
}

func (m *MTA) deliverToRelay(address string, from string, to []string, msg []byte) error {
	conn, err := net.DialTimeout("tcp", address, 30*time.Second)
	if err != nil {
		return fmt.Errorf("relay connection failed: %w", err)
	}
	defer conn.Close()

	host, _, _ := net.SplitHostPort(address)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("relay smtp client: %w", err)
	}
	defer c.Close()

	if err := c.Hello(m.hostname); err != nil {
		return err
	}

	if ok, _ := c.Extension("STARTTLS"); ok {
		config := &tls.Config{ServerName: host}
		if err := c.StartTLS(config); err != nil {
			slog.Warn("relay starttls failed, falling back to plain", "relay", address, "error", err)
		}
	}

	if err := c.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			slog.Warn("relay rejected recipient", "rcpt", rcpt, "error", err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	c.Quit()
	return nil
}

func (m *MTA) deliverToDomain(domain string, from string, to []string, msg []byte) error {
	mxs, err := net.LookupMX(domain)
	if err != nil {
		return fmt.Errorf("mx lookup failed: %w", err)
	}
	if len(mxs) == 0 {
		mxs = []*net.MX{{Host: domain, Pref: 0}}
	}

	var lastErr error
	for _, mx := range mxs {
		address := fmt.Sprintf("%s:25", mx.Host)
		slog.Debug("attempting delivery", "mx", address, "domain", domain)

		conn, err := net.DialTimeout("tcp", address, 30*time.Second)
		if err != nil {
			lastErr = err
			continue
		}
		defer conn.Close()

		c, err := smtp.NewClient(conn, mx.Host)
		if err != nil {
			lastErr = err
			continue
		}
		defer c.Close()

		if err := c.Hello(m.hostname); err != nil {
			lastErr = err
			continue
		}

		if ok, _ := c.Extension("STARTTLS"); ok {
			config := &tls.Config{ServerName: mx.Host}
			if err := c.StartTLS(config); err != nil {
				slog.Warn("starttls failed, falling back to plain", "mx", mx.Host, "error", err)
			}
		}

		if err := c.Mail(from); err != nil {
			lastErr = err
			continue
		}

		for _, rcpt := range to {
			if err := c.Rcpt(rcpt); err != nil {
				lastErr = err
				slog.Warn("recipient rejected by server", "mx", mx.Host, "rcpt", rcpt, "error", err)
			}
		}

		w, err := c.Data()
		if err != nil {
			lastErr = err
			continue
		}

		if _, err := w.Write(msg); err != nil {
			lastErr = err
			continue
		}

		if err := w.Close(); err != nil {
			lastErr = err
			continue
		}

		c.Quit()
		return nil
	}

	return fmt.Errorf("all MX records failed, last error: %w", lastErr)
}
