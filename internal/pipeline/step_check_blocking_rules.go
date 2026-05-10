package pipeline

import "context"

// CheckBlockingRules checks if the from address is blocked for the target mailbox
func CheckBlockingRules(ctx context.Context, p *Pipeline, ictx *IngestionContext) (StepStatus, any, error) {
	rule, err := p.db.IsBlockedByMailboxRules(ctx, ictx.TargetMailboxID, ictx.FromAddress)
	if err != nil {
		return StatusError, nil, err
	}

	if rule != nil {
		return StatusFail, map[string]any{
			"blocked":         true,
			"rule_id":         rule.ID,
			"address_pattern": rule.AddressPattern,
		}, nil
	}

	return StatusPass, map[string]any{"blocked": false}, nil
}
