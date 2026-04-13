package pipeline

import (
	"context"

	"github.com/colormechadd/mailaroo/internal/mail"
)

// Deliver handles both storage and database persistence in one logical step
func Deliver(ctx context.Context, p *Pipeline, ictx *IngestionContext) (StepStatus, any, error) {
	email, err := p.mail.Persist(ctx, mail.PersistOptions{
		MailboxID:        ictx.TargetMailboxID,
		RawMessage:       ictx.RawMessage,
		IsOutbound:       false,
		IsQuarantined:    true,
		IngestionID:      &ictx.ID,
		AddressMappingID: &ictx.AddressMappingID,
	})
	if err != nil {
		return StatusError, nil, err
	}

	ictx.StorageKey = email.StorageKey
	ictx.EmailID = email.ID

	return StatusPass, map[string]any{
		"email_id":    email.ID,
		"thread_id":   email.ThreadID,
		"storage_key": email.StorageKey,
	}, nil
}
