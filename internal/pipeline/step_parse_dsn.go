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
	"net/textproto"
	"strings"

	"github.com/google/uuid"
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
