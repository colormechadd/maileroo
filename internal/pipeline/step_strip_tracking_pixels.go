package pipeline

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"

	gomail "github.com/emersion/go-message/mail"

	"github.com/colormechadd/mailaroo/internal/mail"
)

// StripTrackingPixels parses the raw email message, removes tracking pixels
// from any HTML body part, and updates ictx.RawMessage with the cleaned
// message before it is stored by the Deliver step.
func StripTrackingPixels(ctx context.Context, p *Pipeline, ictx *IngestionContext) (StepStatus, any, error) {
	cleaned, originalHTML, pixels, err := rebuildMessageWithStrippedPixels(ictx.RawMessage)
	if err != nil {
		// Pixels were detected but the message could not be rebuilt. Log what
		// we found for the pipeline page but leave the stored message unchanged.
		slog.Warn("strip_tracking_pixels: detected pixels but could not rebuild message",
			"ingestion_id", ictx.ID, "count", len(pixels), "error", err)
		return StatusNeutral, map[string]any{
			"detected": len(pixels),
			"pixels":   pixels,
			"note":     "message could not be rebuilt; pixels remain in stored email",
		}, nil
	}
	if cleaned == nil {
		// No HTML part, or no tracking pixels found.
		return StatusSkipped, map[string]any{"removed": 0}, nil
	}

	// Store the original HTML before overwriting RawMessage so it can be
	// retrieved for audit purposes. The key is included in the step details
	// and will appear in the pipeline log page.
	originalKey := ictx.ID.String() + "/original_html"
	if err := p.storage.Save(ctx, originalKey, strings.NewReader(originalHTML)); err != nil {
		slog.Warn("strip_tracking_pixels: could not store original HTML",
			"ingestion_id", ictx.ID, "error", err)
		// Non-fatal: continue and strip the pixels even if archival fails.
		originalKey = ""
	}

	ictx.RawMessage = cleaned
	return StatusPass, map[string]any{
		"removed":       len(pixels),
		"pixels":        pixels,
		"original_html": originalKey,
	}, nil
}

// rebuildMessageWithStrippedPixels parses raw, strips tracking pixels from
// every HTML body part, and returns the rebuilt message bytes alongside the
// original HTML content (before stripping) and the list of removed pixels.
//
// Returns (nil, "", nil, nil) when the message has no HTML part or no
// tracking pixels were detected. Returns (nil, originalHTML, pixels, err)
// when pixels were found but reconstruction failed — the caller should log
// but not modify the raw message.
func rebuildMessageWithStrippedPixels(raw []byte) (cleaned []byte, originalHTML string, pixels []mail.StrippedPixel, err error) {
	mr, err := gomail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		// Not a structured mail message; nothing to do.
		return nil, "", nil, nil
	}
	defer mr.Close()

	type savedPart struct {
		inline     *gomail.InlineHeader
		attachment *gomail.AttachmentHeader
		body       []byte
	}

	var parts []savedPart
	var allPixels []mail.StrippedPixel
	var firstOriginalHTML string

	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Unparseable part — bail out without modifying the message.
			return nil, "", nil, nil
		}

		body, err := io.ReadAll(p.Body)
		if err != nil {
			return nil, "", nil, nil
		}

		switch h := p.Header.(type) {
		case *gomail.InlineHeader:
			ct, _, _ := h.ContentType()
			if ct == "text/html" {
				raw := string(body)
				stripped, pxs := mail.StripTrackingPixelsFromHTML(raw)
				if firstOriginalHTML == "" {
					firstOriginalHTML = raw
				}
				allPixels = append(allPixels, pxs...)
				body = []byte(stripped)
			}
			hh := gomail.InlineHeader{Header: h.Header.Copy()}
			parts = append(parts, savedPart{inline: &hh, body: body})

		case *gomail.AttachmentHeader:
			hh := gomail.AttachmentHeader{Header: h.Header.Copy()}
			parts = append(parts, savedPart{attachment: &hh, body: body})
		}
	}

	if len(allPixels) == 0 {
		return nil, "", nil, nil
	}

	// Reconstruct the message. CreateWriter wraps it in multipart/mixed and
	// preserves all original top-level headers (From, To, Subject, etc.).
	var buf bytes.Buffer
	mw, err := gomail.CreateWriter(&buf, mr.Header)
	if err != nil {
		return nil, firstOriginalHTML, allPixels, err
	}

	for _, pt := range parts {
		if pt.inline != nil {
			pw, err := mw.CreateSingleInline(*pt.inline)
			if err != nil {
				return nil, firstOriginalHTML, allPixels, err
			}
			if _, err := pw.Write(pt.body); err != nil {
				return nil, firstOriginalHTML, allPixels, err
			}
			pw.Close()
		} else if pt.attachment != nil {
			pw, err := mw.CreateAttachment(*pt.attachment)
			if err != nil {
				return nil, firstOriginalHTML, allPixels, err
			}
			if _, err := pw.Write(pt.body); err != nil {
				return nil, firstOriginalHTML, allPixels, err
			}
			pw.Close()
		}
	}

	if err := mw.Close(); err != nil {
		return nil, firstOriginalHTML, allPixels, err
	}

	return buf.Bytes(), firstOriginalHTML, allPixels, nil
}
