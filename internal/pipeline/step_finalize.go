package pipeline

import (
	"context"

	"github.com/colormechadd/mailaroo/pkg/models"
)

// Finalize sets the final email status and flags based on any matched filter rule action.
func Finalize(ctx context.Context, p *Pipeline, ictx *IngestionContext) (StepStatus, any, error) {
	status, isRead, isStar := resolveFilterAction(ictx.FilterAction)

	if err := p.db.SetEmailFields(ctx, ictx.EmailID, isRead, isStar, status); err != nil {
		return StatusError, nil, err
	}

	return StatusPass, map[string]any{"status": status}, nil
}

func resolveFilterAction(action string) (status models.EmailStatus, isRead bool, isStar bool) {
	switch action {
	case models.FilterActionArchive:
		return models.StatusArchived, false, false
	case models.FilterActionDelete:
		return models.StatusDeleted, false, false
	case models.FilterActionMarkRead:
		return models.StatusInbox, true, false
	case models.FilterActionStar:
		return models.StatusInbox, false, true
	case models.FilterActionQuarantine:
		return models.StatusQuarantined, false, false
	default:
		return models.StatusInbox, false, false
	}
}
