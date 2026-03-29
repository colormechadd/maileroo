package pipeline

import "context"

// Notify broadcasts a new-mail event to the hub
func Notify(ctx context.Context, p *Pipeline, ictx *IngestionContext) (StepStatus, any, error) {
	p.hub.Broadcast(Event{
		UserID:    ictx.UserID,
		MailboxID: ictx.TargetMailboxID,
		Type:      "new-mail",
	})

	return StatusPass, nil, nil
}
