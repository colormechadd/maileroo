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
)

type Attachment struct {
	Filename    string
	ContentType string
	Content     io.Reader
}

type Message struct {
	From        string
	To          []string
	Subject     string
	TextBody    string
	HTMLBody    string
	InReplyTo   string
	References  string
	Attachments []Attachment
}

type Sender interface {
	Send(from string, to []string, msg []byte) error
	SendMessage(msg Message) ([]byte, error)
}

type MTA struct {
	hostname string
}

func NewMTA(hostname string) *MTA {
	if hostname == "" {
		hostname = "localhost"
	}
	return &MTA{hostname: hostname}
}

func (m *MTA) SendMessage(msg Message) ([]byte, error) {
	var buf bytes.Buffer
	var h mail.Header
	h.SetAddressList("From", []*mail.Address{{Address: msg.From}})

	toAddrs := make([]*mail.Address, len(msg.To))
	for i, a := range msg.To {
		toAddrs[i] = &mail.Address{Address: a}
	}
	h.SetAddressList("To", toAddrs)
	h.SetSubject(msg.Subject)
	h.SetDate(time.Now())
	h.Set("MIME-Version", "1.0")

	if msg.InReplyTo != "" {
		h.Set("In-Reply-To", msg.InReplyTo)
	}
	if msg.References != "" {
		h.Set("References", msg.References)
	}

	mw, err := mail.CreateWriter(&buf, h)
	if err != nil {
		return nil, fmt.Errorf("failed to create mail writer: %w", err)
	}

	// Create body part
	if msg.HTMLBody != "" {
		var hh mail.InlineHeader
		hh.Set("Content-Type", "text/html; charset=utf-8")
		hw, err := mw.CreateSingleInline(hh)
		if err != nil {
			return nil, fmt.Errorf("failed to create html part: %w", err)
		}
		io.WriteString(hw, msg.HTMLBody)
		hw.Close()
	} else {
		var th mail.InlineHeader
		th.Set("Content-Type", "text/plain; charset=utf-8")
		tw, err := mw.CreateSingleInline(th)
		if err != nil {
			return nil, fmt.Errorf("failed to create text part: %w", err)
		}
		io.WriteString(tw, msg.TextBody)
		tw.Close()
	}

	// Attachments
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
	if err := m.Send(msg.From, msg.To, raw); err != nil {
		return nil, err
	}

	return raw, nil
}

func (m *MTA) Send(from string, to []string, msg []byte) error {
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
			config := &tls.Config{ServerName: mx.Host, InsecureSkipVerify: true}
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
