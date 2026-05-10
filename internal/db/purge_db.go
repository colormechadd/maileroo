package db

import (
	"context"
	"time"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
)

type PurgeDB interface {
	GetEmailsForPurge(ctx context.Context, deletedBefore time.Time) ([]models.Email, error)
	GetAttachmentStorageKeysByEmailID(ctx context.Context, emailID uuid.UUID) ([]string, error)
	MarkEmailPurged(ctx context.Context, emailID uuid.UUID) error
}

func (db *DB) GetEmailsForPurge(ctx context.Context, deletedBefore time.Time) ([]models.Email, error) {
	var emails []models.Email
	err := db.SelectContext(ctx, &emails, `
		SELECT id, mailbox_id, storage_key
		FROM email
		WHERE status = 'DELETED'
		  AND purged_datetime IS NULL
		  AND update_datetime < $1
	`, deletedBefore)
	return emails, err
}

func (db *DB) GetAttachmentStorageKeysByEmailID(ctx context.Context, emailID uuid.UUID) ([]string, error) {
	var keys []string
	err := db.SelectContext(ctx, &keys, `
		SELECT storage_key FROM email_attachment WHERE email_id = $1
	`, emailID)
	return keys, err
}

func (db *DB) MarkEmailPurged(ctx context.Context, emailID uuid.UUID) error {
	_, err := db.ExecContext(ctx, `
		UPDATE email SET purged_datetime = NOW(), update_datetime = NOW() WHERE id = $1
	`, emailID)
	return err
}
