package mail

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/colormechadd/maileroo/internal/storage"
	"github.com/colormechadd/maileroo/pkg/models"
	gomail "github.com/emersion/go-message/mail"
	"github.com/google/uuid"
	"github.com/klauspost/compress/zstd"
	"github.com/microcosm-cc/bluemonday"
)

type Repository interface {
	FindThreadIDByMessageIDs(ctx context.Context, mailboxID uuid.UUID, messageIDs []string) (uuid.UUID, error)
	CreateThread(ctx context.Context, thread *models.Thread) error
	CreateEmail(ctx context.Context, email *models.Email) error
	CreateAttachment(ctx context.Context, att *models.EmailAttachment) error
}

type Service struct {
	repo        Repository
	storage     storage.Storage
	compression string // "zstd", "gzip", "none"
	policy      *bluemonday.Policy
}

func newEmailPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()

	// Structural and text elements
	p.AllowElements(
		"div", "span", "p", "br", "hr", "center",
		"h1", "h2", "h3", "h4", "h5", "h6",
		"ul", "ol", "li", "dl", "dt", "dd",
		"blockquote", "pre", "code", "tt",
		"b", "i", "u", "s", "em", "strong", "small", "big", "sub", "sup",
		"font", "section", "article", "header", "footer", "figure", "figcaption",
	)

	// Table layout (extremely common in email HTML)
	p.AllowElements("table", "thead", "tbody", "tfoot", "tr", "th", "td", "caption", "col", "colgroup")
	tableAttrs := []string{"width", "height", "cellpadding", "cellspacing", "border", "align", "valign", "bgcolor", "colspan", "rowspan", "nowrap", "scope"}
	p.AllowAttrs(tableAttrs...).OnElements("table", "thead", "tbody", "tfoot", "tr", "th", "td", "col", "colgroup")

	// Links
	p.AllowAttrs("href", "title", "target", "rel", "name").OnElements("a")
	p.AllowURLSchemes("http", "https", "mailto", "data", "cid")

	// Images
	p.AllowAttrs("src", "alt", "title", "width", "height", "border", "align", "hspace", "vspace").OnElements("img")

	// Font element
	p.AllowAttrs("color", "face", "size").OnElements("font")

	// Global attributes allowed on everything
	p.AllowAttrs("style", "class", "id", "dir", "lang", "title", "role",
		"align", "valign", "bgcolor", "color", "width", "height",
		"aria-label", "aria-hidden").Globally()

	return p
}

func NewService(repo Repository, storage storage.Storage, compression string) *Service {
	return &Service{
		repo:        repo,
		storage:     storage,
		compression: compression,
		policy:      newEmailPolicy(),
	}
}

type PersistOptions struct {
	MailboxID        uuid.UUID
	RawMessage       []byte
	IsOutbound       bool
	IsQuarantined    bool
	UserID           uuid.UUID
	IngestionID      *uuid.UUID
	AddressMappingID *uuid.UUID
	SendingAddressID *uuid.UUID
	InReplyTo        string
	References       string
}

func (s *Service) Persist(ctx context.Context, opts PersistOptions) (*models.Email, error) {
	// 1. Compression
	data, suffix, err := s.CompressData(opts.RawMessage, s.compression)
	if err != nil {
		return nil, fmt.Errorf("compression failed: %w", err)
	}

	emailID := uuid.New()
	storageKey := fmt.Sprintf("%s/%s.eml%s", opts.MailboxID, emailID, suffix)

	// 2. Storage
	if err := s.storage.Save(ctx, storageKey, bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("storage save failed: %w", err)
	}

	// 3. Parsing
	mr, err := gomail.CreateReader(bytes.NewReader(opts.RawMessage))
	if err != nil {
		return nil, fmt.Errorf("failed to create mail reader: %w", err)
	}
	defer mr.Close()

	subject, _ := mr.Header.Subject()
	msgID, _ := mr.Header.MessageID()
	if msgID == "" {
		msgID = emailID.String()
	}

	fromAddrs, _ := mr.Header.AddressList("From")
	from := ""
	if len(fromAddrs) > 0 {
		from = fromAddrs[0].String()
	}

	toAddrs, _ := mr.Header.AddressList("To")
	to := ""
	if len(toAddrs) > 0 {
		to = toAddrs[0].String()
	}

	inReplyTo, _ := mr.Header.Text("In-Reply-To")
	referencesRaw, _ := mr.Header.Text("References")
	references := s.ParseReferences(referencesRaw)

	// 4. Threading
	lookups := []string{}
	if inReplyTo != "" {
		lookups = append(lookups, strings.Trim(inReplyTo, "<> "))
	}
	for _, r := range references {
		lookups = append(lookups, strings.Trim(r, "<> "))
	}

	var threadID uuid.UUID
	if len(lookups) > 0 {
		threadID, _ = s.repo.FindThreadIDByMessageIDs(ctx, opts.MailboxID, lookups)
	}

	if threadID == uuid.Nil {
		threadID = uuid.New()
		newThread := &models.Thread{
			ID:        threadID,
			MailboxID: opts.MailboxID,
			Subject:   subject,
		}
		if err := s.repo.CreateThread(ctx, newThread); err != nil {
			return nil, fmt.Errorf("thread creation failed: %w", err)
		}
	}

	// 5. DB Persistence
	email := &models.Email{
		ID:               emailID,
		MailboxID:        opts.MailboxID,
		ThreadID:         &threadID,
		AddressMappingID: opts.AddressMappingID,
		IngestionID:      opts.IngestionID,
		SendingAddressID: opts.SendingAddressID,
		MessageID:        msgID,
		InReplyTo:        &inReplyTo,
		References:       &referencesRaw,
		Subject:          subject,
		FromAddress:      from,
		ToAddress:        to,
		StorageKey:       storageKey,
		Size:             int64(len(opts.RawMessage)),
		StoredSize:       int64(len(data)),
		ReceiveDatetime:  time.Now(),
		IsRead:           opts.IsOutbound, // Sent mail is read
		IsStar:           false,
		Direction:        models.DirectionInbound,
		Status:           models.StatusInbox,
	}

	if opts.IsOutbound {
		email.Direction = models.DirectionOutbound
		if opts.UserID != uuid.Nil {
			email.UserID = &opts.UserID
		}
	}
	if opts.IsQuarantined {
		email.Status = models.StatusQuarantined
	}

	if opts.InReplyTo != "" {
		email.InReplyTo = &opts.InReplyTo
	}
	if opts.References != "" {
		email.References = &opts.References
	}

	bodyPlain := extractBodyPlain(opts.RawMessage)
	if bodyPlain != "" {
		email.BodyPlain = &bodyPlain
	}

	if err := s.repo.CreateEmail(ctx, email); err != nil {
		return nil, fmt.Errorf("email creation failed: %w", err)
	}

	// 6. Attachments
	for {
		pPart, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			slog.Error("error reading message part", "email_id", emailID, "error", err)
			break
		}

		switch h := pPart.Header.(type) {
		case *gomail.AttachmentHeader:
			filename, _ := h.Filename()
			contentType, _, _ := h.ContentType()

			attData, err := io.ReadAll(pPart.Body)
			if err != nil {
				slog.Error("failed to read attachment body", "email_id", emailID, "filename", filename, "error", err)
				continue
			}

			cData, aSuffix, err := s.CompressData(attData, s.compression)
			if err != nil {
				slog.Error("failed to compress attachment", "filename", filename, "error", err)
				continue
			}

			attID := uuid.New()
			attKey := fmt.Sprintf("%s/attachments/%s/%s_%s%s", opts.MailboxID, emailID, attID, filename, aSuffix)

			if err := s.storage.Save(ctx, attKey, bytes.NewReader(cData)); err != nil {
				slog.Error("failed to save attachment to storage", "email_id", emailID, "filename", filename, "error", err)
				continue
			}

			att := &models.EmailAttachment{
				ID:          attID,
				EmailID:     emailID,
				Filename:    filename,
				ContentType: contentType,
				Size:        int64(len(attData)),
				StorageKey:  attKey,
			}

			if err := s.repo.CreateAttachment(ctx, att); err != nil {
				slog.Error("failed to save attachment metadata", "email_id", emailID, "filename", filename, "error", err)
				continue
			}
		}
	}

	return email, nil
}

func (s *Service) GetCcAddresses(ctx context.Context, email *models.Email) ([]string, error) {
	rc, err := s.storage.Get(ctx, email.StorageKey)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	bodyReader, err := s.DecompressReader(rc, email.StorageKey)
	if err != nil {
		return nil, err
	}
	if closer, ok := bodyReader.(io.Closer); ok {
		defer closer.Close()
	}

	mr, err := gomail.CreateReader(bodyReader)
	if err != nil {
		return nil, err
	}
	defer mr.Close()

	ccAddrs, _ := mr.Header.AddressList("Cc")
	var res []string
	for _, a := range ccAddrs {
		res = append(res, a.Address)
	}
	return res, nil
}

func (s *Service) FetchBody(ctx context.Context, email *models.Email) (string, bool, error) {
	rc, err := s.storage.Get(ctx, email.StorageKey)
	if err != nil {
		return "", false, err
	}
	defer rc.Close()

	bodyReader, err := s.DecompressReader(rc, email.StorageKey)
	if err != nil {
		return "", false, err
	}
	if closer, ok := bodyReader.(io.Closer); ok {
		defer closer.Close()
	}

	mr, err := gomail.CreateReader(bodyReader)
	if err != nil {
		b, _ := io.ReadAll(bodyReader)
		return string(b), false, nil
	}
	defer mr.Close()

	var content string
	var isHTML bool
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}

		var ct string
		switch h := p.Header.(type) {
		case *gomail.InlineHeader:
			ct, _, _ = h.ContentType()
		case *gomail.AttachmentHeader:
			ct, _, _ = h.ContentType()
		}

		if ct == "text/html" {
			b, _ := io.ReadAll(p.Body)
			content = s.policy.Sanitize(string(b))
			isHTML = true
			break
		}
		if ct == "text/plain" && content == "" {
			b, _ := io.ReadAll(p.Body)
			content = string(b)
		}
	}
	return content, isHTML, nil
}

func (s *Service) FetchRaw(ctx context.Context, email *models.Email) ([]byte, error) {
	rc, err := s.storage.Get(ctx, email.StorageKey)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	bodyReader, err := s.DecompressReader(rc, email.StorageKey)
	if err != nil {
		return nil, err
	}
	if closer, ok := bodyReader.(io.Closer); ok {
		defer closer.Close()
	}

	return io.ReadAll(bodyReader)
}

func (s *Service) FetchHeaders(ctx context.Context, email *models.Email) (string, error) {
	rc, err := s.storage.Get(ctx, email.StorageKey)
	if err != nil {
		return "", err
	}
	defer rc.Close()

	bodyReader, err := s.DecompressReader(rc, email.StorageKey)
	if err != nil {
		return "", err
	}
	if closer, ok := bodyReader.(io.Closer); ok {
		defer closer.Close()
	}

	mr, err := gomail.CreateReader(bodyReader)
	if err != nil {
		return "", err
	}
	defer mr.Close()

	var sb strings.Builder
	fields := mr.Header.Fields()
	for fields.Next() {
		sb.WriteString(fmt.Sprintf("%s: %s\n", fields.Key(), fields.Value()))
	}
	return sb.String(), nil
}

func (s *Service) FetchUnsubscribeInfo(ctx context.Context, email *models.Email) (*models.UnsubscribeInfo, error) {
	rc, err := s.storage.Get(ctx, email.StorageKey)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	bodyReader, err := s.DecompressReader(rc, email.StorageKey)
	if err != nil {
		return nil, err
	}
	if closer, ok := bodyReader.(io.Closer); ok {
		defer closer.Close()
	}

	mr, err := gomail.CreateReader(bodyReader)
	if err != nil {
		return nil, err
	}
	defer mr.Close()

	listUnsub := mr.Header.Get("List-Unsubscribe")
	if listUnsub == "" {
		return nil, nil
	}

	info := &models.UnsubscribeInfo{
		OneClick: strings.Contains(mr.Header.Get("List-Unsubscribe-Post"), "List-Unsubscribe=One-Click"),
	}

	for _, part := range strings.Split(listUnsub, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "<") && strings.HasSuffix(part, ">") {
			u := part[1 : len(part)-1]
			switch {
			case (strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")) && info.URL == "":
				info.URL = u
			case strings.HasPrefix(u, "mailto:") && info.Mailto == "":
				info.Mailto = u
			}
		}
	}

	if info.URL == "" && info.Mailto == "" {
		return nil, nil
	}

	return info, nil
}

func (s *Service) DecompressReader(r io.Reader, key string) (io.Reader, error) {
	if strings.HasSuffix(key, ".zst") {
		zr, err := zstd.NewReader(r)
		if err != nil {
			return nil, err
		}
		return zr, nil
	} else if strings.HasSuffix(key, ".gz") {
		gr, err := gzip.NewReader(r)
		if err != nil {
			return nil, err
		}
		return gr, nil
	}
	return r, nil
}

func (s *Service) CompressData(data []byte, algorithm string) ([]byte, string, error) {
	switch strings.ToLower(algorithm) {
	case "zstd":
		var buf bytes.Buffer
		zw, _ := zstd.NewWriter(&buf)
		if _, err := zw.Write(data); err != nil {
			return nil, "", err
		}
		if err := zw.Close(); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), ".zst", nil
	case "gzip":
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		if _, err := gw.Write(data); err != nil {
			return nil, "", err
		}
		if err := gw.Close(); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), ".gz", nil
	default:
		return data, "", nil
	}
}

func extractBodyPlain(rawMessage []byte) string {
	mr, err := gomail.CreateReader(bytes.NewReader(rawMessage))
	if err != nil {
		return ""
	}
	defer mr.Close()

	var plain string
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		h, ok := p.Header.(*gomail.InlineHeader)
		if !ok {
			continue
		}
		ct, _, _ := h.ContentType()
		if ct == "text/plain" {
			b, _ := io.ReadAll(p.Body)
			return string(b)
		}
		if ct == "text/html" && plain == "" {
			b, _ := io.ReadAll(p.Body)
			plain = bluemonday.StripTagsPolicy().Sanitize(string(b))
		}
	}
	return plain
}

func (s *Service) ParseReferences(raw string) []string {
	if raw == "" {
		return nil
	}
	return strings.Fields(raw)
}
