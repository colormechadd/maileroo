package pipeline

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net"
	"net/textproto"
	"strings"

	"github.com/colormechadd/maileroo/internal/mail"
	"github.com/colormechadd/maileroo/pkg/models"
	"github.com/emersion/go-msgauth/dkim"
	"github.com/emersion/go-msgauth/dmarc"
	"github.com/google/uuid"
	"github.com/zaccone/spf"
)

// ParseDSN detects inbound Delivery Status Notifications addressed to a tagged
// bounce address (bounces+<job-id>@domain) and marks the corresponding outbound
// job as FAILED. Non-bounce messages are passed through unchanged.
func ParseDSN(ctx context.Context, p *Pipeline, ictx *IngestionContext) (StepStatus, any, error) {
	if len(ictx.ToAddresses) == 0 {
		return StatusSkipped, nil, nil
	}
	jobID, ok := extractBounceJobID(ictx.ToAddresses[0])
	if !ok {
		return StatusSkipped, nil, nil
	}

	dsnStatus, diagnostic := parseDSNMessage(ictx.RawMessage)
	lastError := fmt.Sprintf("bounce: status=%s diagnostic=%s", dsnStatus, diagnostic)

	if err := p.db.UpdateOutboundJobFailed(ctx, jobID, lastError); err != nil {
		slog.Error("failed to update outbound job via bounce DSN", "job_id", jobID, "error", err)
	}

	return StatusPass, map[string]any{
		"bounce_job_id": jobID.String(),
		"dsn_status":    dsnStatus,
		"diagnostic":    diagnostic,
	}, nil
}

// extractBounceJobID parses a bounce address of the form bounces+<uuid>@domain
// and returns the embedded job UUID.
func extractBounceJobID(address string) (uuid.UUID, bool) {
	local, _, found := strings.Cut(address, "@")
	if !found || !strings.HasPrefix(local, "bounces+") {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(local[len("bounces+"):])
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// parseDSNMessage walks a multipart/report message and extracts the Status and
// Diagnostic-Code fields from the message/delivery-status part (RFC 3464).
func parseDSNMessage(raw []byte) (status, diagnostic string) {
	br := bufio.NewReader(bytes.NewReader(raw))
	tp := textproto.NewReader(br)

	headers, err := tp.ReadMIMEHeader()
	if err != nil {
		return
	}

	mediaType, params, err := mime.ParseMediaType(headers.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "multipart/report") {
		return
	}
	boundary := params["boundary"]
	if boundary == "" {
		return
	}

	mr := multipart.NewReader(br, boundary)
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		partMediaType, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if !strings.EqualFold(partMediaType, "message/delivery-status") {
			continue
		}
		body, _ := io.ReadAll(part)
		return parseDSNFields(string(body))
	}
	return
}

// parseDSNFields extracts Status and Diagnostic-Code from a delivery-status body.
func parseDSNFields(body string) (status, diagnostic string) {
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "status:") {
			status = strings.TrimSpace(line[7:])
		} else if strings.HasPrefix(lower, "diagnostic-code:") {
			diagnostic = strings.TrimSpace(line[16:])
		}
	}
	return
}

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

// Notify broadcasts a new-mail event to the hub
func Notify(ctx context.Context, p *Pipeline, ictx *IngestionContext) (StepStatus, any, error) {
	p.hub.Broadcast(Event{
		UserID:    ictx.UserID,
		MailboxID: ictx.TargetMailboxID,
		Type:      "new-mail",
	})

	return StatusPass, nil, nil
}

// Finalize sets the status to INBOX once all checks pass
func Finalize(ctx context.Context, p *Pipeline, ictx *IngestionContext) (StepStatus, any, error) {
	if err := p.db.SetEmailStatus(ctx, ictx.EmailID, models.StatusInbox); err != nil {
		return StatusError, nil, err
	}

	return StatusPass, nil, nil
}

// Deliver handles both storage and database persistence in one logical step
func Deliver(ctx context.Context, p *Pipeline, ictx *IngestionContext) (StepStatus, any, error) {
	email, err := p.mail.Persist(ctx, mail.PersistOptions{
		MailboxID:        ictx.TargetMailboxID,
		RawMessage:       ictx.RawMessage,
		IsOutbound:       false,
		IsQuarantined:    true,
		UserID:           ictx.UserID,
		IngestionID:      &ictx.ID,
		AddressMappingID: &ictx.AddressMappingID,
	})
	if err != nil {
		return StatusError, nil, err
	}

	ictx.StorageKey = email.StorageKey
	ictx.EmailID = email.ID

	return StatusPass, map[string]any{
		"email_id":    email.ID,
		"thread_id":   email.ThreadID,
		"storage_key": email.StorageKey,
	}, nil
}
