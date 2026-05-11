package pipeline

import (
	"context"
	"log/slog"
)

// Notify broadcasts a new-mail event to all active users of the mailbox, but only if the email landed as unread.
func Notify(ctx context.Context, p *Pipeline, ictx *IngestionContext) (StepStatus, any, error) {
	if ictx.IsRead {
		return StatusSkipped, nil, nil
	}

	userIDs, err := p.db.GetMailboxUserIDs(ctx, ictx.TargetMailboxID)
	if err != nil {
		slog.Error("failed to get mailbox user IDs for notification", "mailbox_id", ictx.TargetMailboxID, "error", err)
		return StatusError, nil, err
	}

	for _, userID := range userIDs {
		p.hub.Broadcast(Event{
			UserID:    userID,
			MailboxID: ictx.TargetMailboxID,
			Type:      "new-mail",
		})
	}

	return StatusPass, nil, nil
}
