package pipeline

import "context"

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
