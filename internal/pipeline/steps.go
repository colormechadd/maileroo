package pipeline

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/colormechadd/maileroo/pkg/models"
	"github.com/emersion/go-message/mail"
	"github.com/emersion/go-msgauth/dkim"
	"github.com/emersion/go-msgauth/dmarc"
	"github.com/google/uuid"
	"github.com/klauspost/compress/zstd"
	"github.com/zaccone/spf"
)

// ValidateSender performs SPF, DKIM and optionally DMARC checks.
// It requires either SPF or DKIM to pass.
func ValidateSender(ctx context.Context, p *Pipeline, ictx *IngestionContext) (StepStatus, any, error) {
	domain := extractDomain(ictx.FromAddress)

	// 1. SPF Check
	spfRes, spfExp, spfErr := spf.CheckHost(ictx.RemoteIP, domain, ictx.FromAddress)
	spfPass := (spfRes == spf.Pass)

	// 2. DKIM Check
	dkimStatus, dkimResults, _ := checkDKIM(ictx.RawMessage)
	dkimPass := (dkimStatus == StatusPass)

	results := map[string]any{
		"spf": map[string]any{
			"result":      spfRes.String(),
			"explanation": spfExp,
			"error":       spfErr,
		},
		"dkim": dkimResults,
	}

	if spfPass || dkimPass {
		return StatusPass, results, nil
	}

	// 3. DMARC Check (only if both failed)
	dmarcRecord, dmarcErr := dmarc.Lookup(domain)
	if dmarcErr == nil && dmarcRecord != nil {
		results["dmarc"] = map[string]any{
			"policy": string(dmarcRecord.Policy),
			"status": "found",
		}

		if dmarcRecord.Policy == dmarc.PolicyNone {
			return StatusPass, results, nil
		}

		if dmarcRecord.Policy == dmarc.PolicyReject || dmarcRecord.Policy == dmarc.PolicyQuarantine {
			return StatusFail, results, nil
		}
	} else {
		results["dmarc"] = map[string]any{
			"status": "not_found",
			"error":  dmarcErr,
		}
	}

	// If we got here, both SPF and DKIM failed, and either no DMARC or DMARC p=none.
	// We'll mark as fail because the requirement was "either dkim or spf to be valid".
	return StatusFail, results, nil
}

func checkDKIM(raw []byte) (StepStatus, []any, error) {
	r := bytes.NewReader(raw)
	verifications, err := dkim.Verify(r)
	if err != nil {
		return StatusError, nil, err
	}

	status := StatusNone
	results := []any{}
	for _, v := range verifications {
		vErr := v.Err
		vStatus := "pass"
		if vErr != nil {
			vStatus = "fail"
			status = StatusFail
		} else if status != StatusFail {
			status = StatusPass
		}
		results = append(results, map[string]any{
			"domain": v.Domain,
			"status": vStatus,
			"error":  vErr,
		})
	}
	return status, results, nil
}

func extractDomain(address string) string {
	parts := strings.Split(address, "@")
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}

// ValidateRBL checks the remote IP against configured RBL servers
func ValidateRBL(ctx context.Context, p *Pipeline, ictx *IngestionContext) (StepStatus, any, error) {
	if len(p.cfg.Spam.RBLServers) == 0 {
		return StatusSkipped, nil, nil
	}

	ip := ictx.RemoteIP
	if ip == nil {
		return StatusSkipped, nil, nil
	}

	// Reverse IP for DNS lookup (e.g. 1.2.3.4 -> 4.3.2.1)
	reversedIP := reverseIP(ip)
	if reversedIP == "" {
		return StatusSkipped, nil, nil
	}

	hits := []string{}
	for _, server := range p.cfg.Spam.RBLServers {
		lookup := fmt.Sprintf("%s.%s", reversedIP, server)
		ips, err := net.LookupIP(lookup)
		if err == nil && len(ips) > 0 {
			hits = append(hits, server)
		}
	}

	if len(hits) > 0 {
		return StatusFail, map[string]any{"rbl_hits": hits}, nil
	}

	return StatusPass, map[string]any{"rbl_hits": hits}, nil
}

func reverseIP(ip net.IP) string {
	if ipv4 := ip.To4(); ipv4 != nil {
		return fmt.Sprintf("%d.%d.%d.%d", ipv4[3], ipv4[2], ipv4[1], ipv4[0])
	}
	return ""
}

// CheckBlockingRules checks if the from address is blocked for the target mailbox
func CheckBlockingRules(ctx context.Context, p *Pipeline, ictx *IngestionContext) (StepStatus, any, error) {
	blocked, err := p.db.IsBlockedByMailboxRules(ctx, ictx.TargetMailboxID, ictx.FromAddress)
	if err != nil {
		return StatusError, nil, err
	}

	if blocked {
		return StatusFail, map[string]any{"blocked": true}, nil
	}

	return StatusPass, map[string]any{"blocked": false}, nil
}

func compressData(data []byte, algorithm string) ([]byte, string, error) {
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

// Deliver handles both storage and database persistence in one logical step
func Deliver(ctx context.Context, p *Pipeline, ictx *IngestionContext) (StepStatus, any, error) {
	// 1. Store raw email
	data, suffix, err := compressData(ictx.RawMessage, p.cfg.Compression)
	if err != nil {
		return StatusError, nil, fmt.Errorf("compression failed: %w", err)
	}

	ictx.StorageKey = fmt.Sprintf("%s/%s.eml%s", ictx.TargetMailboxID, ictx.ID, suffix)

	if err := p.storage.Save(ctx, ictx.StorageKey, bytes.NewReader(data)); err != nil {
		return StatusError, nil, fmt.Errorf("storage save failed: %w", err)
	}

	// 2. Parse and Persist Metadata
	mr, err := mail.CreateReader(bytes.NewReader(ictx.RawMessage))
	if err != nil {
		return StatusError, nil, fmt.Errorf("failed to create mail reader: %w", err)
	}
	defer mr.Close()

	subject, _ := mr.Header.Subject()
	msgID, _ := mr.Header.MessageID()
	if msgID == "" {
		msgID = ictx.ID.String()
	}

	inReplyTo, _ := mr.Header.Text("In-Reply-To")
	referencesRaw, _ := mr.Header.Text("References")
	references := parseReferences(referencesRaw)

	// Threading logic
	lookups := []string{}
	if inReplyTo != "" {
		lookups = append(lookups, strings.Trim(inReplyTo, "<> "))
	}
	for _, r := range references {
		lookups = append(lookups, strings.Trim(r, "<> "))
	}

	var threadID uuid.UUID
	if len(lookups) > 0 {
		threadID, _ = p.db.FindThreadIDByMessageIDs(ctx, ictx.TargetMailboxID, lookups)
	}

	if threadID == uuid.Nil {
		threadID = uuid.New()
		newThread := &models.Thread{
			ID:        threadID,
			MailboxID: ictx.TargetMailboxID,
			Subject:   subject,
		}
		if err := p.db.CreateThread(ctx, newThread); err != nil {
			return StatusError, nil, fmt.Errorf("thread creation failed: %w", err)
		}
	}

	emailID := uuid.New()
	email := &models.Email{
		ID:               emailID,
		MailboxID:        ictx.TargetMailboxID,
		ThreadID:         &threadID,
		AddressMappingID: &ictx.AddressMappingID,
		IngestionID:      &ictx.ID,
		MessageID:        msgID,
		InReplyTo:        &inReplyTo,
		References:       &referencesRaw,
		Subject:          subject,
		FromAddress:      ictx.FromAddress,
		ToAddress:        ictx.ToAddresses[0],
		StorageKey:       ictx.StorageKey,
		Size:             int64(len(ictx.RawMessage)),
		ReceiveDatetime:  time.Now(),
		IsRead:           false,
		IsStar:           false,
	}

	if err := p.db.CreateEmail(ctx, email); err != nil {
		return StatusError, nil, fmt.Errorf("email creation failed: %w", err)
	}

	// 3. Process attachments
	attachmentCount := 0
	for {
		pPart, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			slog.Error("error reading message part", "ingestion_id", ictx.ID, "error", err)
			break
		}

		switch h := pPart.Header.(type) {
		case *mail.AttachmentHeader:
			filename, _ := h.Filename()
			contentType, _, _ := h.ContentType()

			// Buffer attachment
			attData, err := io.ReadAll(pPart.Body)
			if err != nil {
				slog.Error("failed to read attachment body", "ingestion_id", ictx.ID, "filename", filename, "error", err)
				continue
			}

			// Compress attachment
			cData, aSuffix, err := compressData(attData, p.cfg.Compression)
			if err != nil {
				slog.Error("failed to compress attachment", "filename", filename, "error", err)
				continue
			}

			attID := uuid.New()
			attKey := fmt.Sprintf("%s/attachments/%s/%s_%s%s", ictx.TargetMailboxID, emailID, attID, filename, aSuffix)

			if err := p.storage.Save(ctx, attKey, bytes.NewReader(cData)); err != nil {
				slog.Error("failed to save attachment to storage", "ingestion_id", ictx.ID, "filename", filename, "error", err)
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

			if err := p.db.CreateAttachment(ctx, att); err != nil {
				slog.Error("failed to save attachment metadata", "ingestion_id", ictx.ID, "filename", filename, "error", err)
				continue
			}
			attachmentCount++
		}
	}

	return StatusPass, map[string]any{
		"email_id":    email.ID,
		"thread_id":   threadID,
		"attachments": attachmentCount,
		"storage_key": ictx.StorageKey,
		"compression": p.cfg.Compression,
	}, nil
}

func parseReferences(raw string) []string {
	if raw == "" {
		return nil
	}
	return strings.Fields(raw)
}
