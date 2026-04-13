package db

import (
	"context"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
)

func (db *DB) CreateDraft(ctx context.Context, draft models.Draft) (*models.Draft, error) {
	var result models.Draft
	err := db.GetContext(ctx, &result, `
		INSERT INTO draft (mailbox_id, user_id, sending_address_id, to_address, cc_address, bcc_address, subject, body, body_html, in_reply_to, "references")
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, mailbox_id, user_id, sending_address_id, to_address, cc_address, bcc_address, subject, body, body_html, in_reply_to, "references", create_datetime, update_datetime
	`, draft.MailboxID, draft.UserID, draft.SendingAddressID, draft.ToAddress, draft.CcAddress, draft.BccAddress, draft.Subject, draft.Body, draft.BodyHTML, draft.InReplyTo, draft.References)
	return &result, err
}

func (db *DB) UpdateDraft(ctx context.Context, draft models.Draft) error {
	_, err := db.ExecContext(ctx, `
		UPDATE draft
		SET sending_address_id = $1, to_address = $2, cc_address = $3, bcc_address = $4,
		    subject = $5, body = $6, body_html = $7, in_reply_to = $8, "references" = $9,
		    update_datetime = NOW()
		WHERE id = $10 AND user_id = $11
	`, draft.SendingAddressID, draft.ToAddress, draft.CcAddress, draft.BccAddress,
		draft.Subject, draft.Body, draft.BodyHTML, draft.InReplyTo, draft.References,
		draft.ID, draft.UserID)
	return err
}

func (db *DB) GetDraftByIDForUser(ctx context.Context, draftID, userID uuid.UUID) (*models.Draft, error) {
	var draft models.Draft
	err := db.GetContext(ctx, &draft, `
		SELECT id, mailbox_id, user_id, sending_address_id, to_address, cc_address, bcc_address,
		       subject, body, body_html, in_reply_to, "references", create_datetime, update_datetime
		FROM draft
		WHERE id = $1 AND user_id = $2
	`, draftID, userID)
	return &draft, err
}

func (db *DB) DeleteDraft(ctx context.Context, draftID, userID uuid.UUID) error {
	_, err := db.ExecContext(ctx, `DELETE FROM draft WHERE id = $1 AND user_id = $2`, draftID, userID)
	return err
}

func (db *DB) GetDraftsByMailboxID(ctx context.Context, mailboxID, userID uuid.UUID) ([]models.Draft, error) {
	var drafts []models.Draft
	err := db.SelectContext(ctx, &drafts, `
		SELECT id, mailbox_id, user_id, sending_address_id, to_address, cc_address, bcc_address,
		       subject, body, body_html, in_reply_to, "references", create_datetime, update_datetime
		FROM draft
		WHERE mailbox_id = $1 AND user_id = $2
		ORDER BY update_datetime DESC
	`, mailboxID, userID)
	return drafts, err
}

func (db *DB) CountDraftsByMailboxID(ctx context.Context, mailboxID, userID uuid.UUID) (int, error) {
	var count int
	err := db.GetContext(ctx, &count, `SELECT COUNT(*) FROM draft WHERE mailbox_id = $1 AND user_id = $2`, mailboxID, userID)
	return count, err
}
