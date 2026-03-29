package pipeline

import (
	"context"

	"github.com/colormechadd/maileroo/pkg/models"
)

// Finalize sets the status to INBOX once all checks pass
func Finalize(ctx context.Context, p *Pipeline, ictx *IngestionContext) (StepStatus, any, error) {
	if err := p.db.SetEmailStatus(ctx, ictx.EmailID, models.StatusInbox); err != nil {
		return StatusError, nil, err
	}

	return StatusPass, nil, nil
}
