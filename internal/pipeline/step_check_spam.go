package pipeline

import (
	"context"
	"math"
	"sort"
)

type spamSymbolEntry struct {
	Name        string  `json:"name"`
	Score       float64 `json:"score"`
	Description string  `json:"description,omitempty"`
}

// CheckSpam scans the message via rspamd and quarantines if the score meets
// or exceeds cfg.Spam.QuarantineThreshold, or rspamd's action is "reject".
func CheckSpam(ctx context.Context, p *Pipeline, ictx *IngestionContext) (StepStatus, any, error) {
	if p.rspamd == nil {
		return StatusSkipped, nil, nil
	}

	result, err := p.rspamd.Check(ctx, ictx.RawMessage)
	if err != nil {
		return StatusError, nil, err
	}

	// Top-10 symbols by absolute score
	var symbols []spamSymbolEntry
	for name, sym := range result.Symbols {
		symbols = append(symbols, spamSymbolEntry{
			Name:        name,
			Score:       sym.Score,
			Description: sym.Description,
		})
	}
	sort.Slice(symbols, func(i, j int) bool {
		return math.Abs(symbols[i].Score) > math.Abs(symbols[j].Score)
	})
	if len(symbols) > 10 {
		symbols = symbols[:10]
	}

	details := map[string]any{
		"score":          result.Score,
		"required_score": result.RequiredScore,
		"action":         result.Action,
		"symbols":        symbols,
	}

	threshold := p.cfg.Spam.QuarantineThreshold
	if result.Score >= threshold || result.Action == "reject" {
		return StatusFail, details, nil
	}

	return StatusPass, details, nil
}
