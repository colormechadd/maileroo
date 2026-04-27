package pipeline

import (
	"bytes"
	"context"
	"io"

	"github.com/colormechadd/mailaroo/internal/filterengine"
	gomail "github.com/emersion/go-message/mail"
)

// ApplyFilterRules evaluates user-defined filter rules against the incoming email.
// On match it stores the action in IngestionContext; Finalize applies it.
func ApplyFilterRules(ctx context.Context, p *Pipeline, ictx *IngestionContext) (StepStatus, any, error) {
	rules, err := p.db.GetActiveFilterRulesForMailbox(ctx, ictx.TargetMailboxID)
	if err != nil {
		return StatusError, nil, err
	}
	if len(rules) == 0 {
		return StatusPass, map[string]any{"matched": false}, nil
	}

	msg := parsedMessageFromRaw(ictx.FromAddress, ictx.ToAddresses, ictx.RawMessage)

	engine := &filterengine.RuleEngine{}
	matched, err := engine.Match(rules, msg)
	if err != nil {
		return StatusError, nil, err
	}

	if matched == nil {
		return StatusPass, map[string]any{"matched": false}, nil
	}

	ictx.MatchedFilterRuleID = &matched.ID
	ictx.FilterAction = matched.Action

	return StatusPass, map[string]any{
		"matched":  true,
		"rule_id":  matched.ID,
		"rule":     matched.Name,
		"action":   matched.Action,
	}, nil
}

func parsedMessageFromRaw(from string, to []string, raw []byte) *filterengine.ParsedMessage {
	msg := &filterengine.ParsedMessage{
		From: from,
	}
	if len(to) > 0 {
		msg.To = to[0]
	}

	mr, err := gomail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return msg
	}
	defer mr.Close()

	h := mr.Header
	if subject, err := h.Subject(); err == nil {
		msg.Subject = subject
	}

	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		switch ph := part.Header.(type) {
		case *gomail.InlineHeader:
			ct, _, _ := ph.ContentType()
			if (ct == "text/plain" || ct == "text/html") && msg.Body == "" {
				b, _ := io.ReadAll(part.Body)
				msg.Body = string(b)
			}
		case *gomail.AttachmentHeader:
			_ = ph
			msg.HasAttachment = true
		}
	}

	return msg
}
