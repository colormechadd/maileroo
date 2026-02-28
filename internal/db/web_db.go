package db

import (
	"context"
	"time"

	"github.com/colormechadd/maileroo/pkg/models"
	"github.com/google/uuid"
)

type WebDB interface {
	GetUserByUsername(ctx context.Context, username string) (*models.User, error)
	CreateWebmailSession(ctx context.Context, userID uuid.UUID, token string, remoteIP, userAgent string, expires time.Time) error
	GetWebmailSession(ctx context.Context, token string) (*models.WebmailSession, error)
	ExpireWebmailSession(ctx context.Context, token string) error
	GetUserByID(ctx context.Context, id uuid.UUID) (*models.User, error)
	GetMailboxesByUserID(ctx context.Context, userID uuid.UUID) ([]models.Mailbox, error)
	GetEmailsByMailboxID(ctx context.Context, mailboxID uuid.UUID, limit, offset int) ([]models.Email, error)
	GetEmailByID(ctx context.Context, emailID uuid.UUID) (*models.Email, error)
	GetAttachmentsByEmailID(ctx context.Context, emailID uuid.UUID) ([]models.EmailAttachment, error)
}

func (db *DB) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	var user models.User
	err := db.GetContext(ctx, &user, `SELECT id, username, password_hash, is_active FROM "user" WHERE username = $1`, username)
	return &user, err
}

func (db *DB) GetUserByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	var user models.User
	err := db.GetContext(ctx, &user, `SELECT id, username, password_hash, is_active FROM "user" WHERE id = $1`, id)
	return &user, err
}

func (db *DB) CreateWebmailSession(ctx context.Context, userID uuid.UUID, token string, remoteIP, userAgent string, expires time.Time) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO webmail_session (user_id, token, remote_ip, user_agent, expires_datetime)
		VALUES ($1, $2, $3, $4, $5)
	`, userID, token, remoteIP, userAgent, expires)
	return err
}

func (db *DB) GetWebmailSession(ctx context.Context, token string) (*models.WebmailSession, error) {
	var session models.WebmailSession
	err := db.GetContext(ctx, &session, `SELECT id, user_id, token, remote_ip, user_agent, expires_datetime FROM webmail_session WHERE token = $1`, token)
	return &session, err
}

func (db *DB) ExpireWebmailSession(ctx context.Context, token string) error {
	_, err := db.ExecContext(ctx, `UPDATE webmail_session SET expires_datetime = CURRENT_TIMESTAMP WHERE token = $1`, token)
	return err
}

func (db *DB) GetMailboxesByUserID(ctx context.Context, userID uuid.UUID) ([]models.Mailbox, error) {
	var mailboxes []models.Mailbox
	err := db.SelectContext(ctx, &mailboxes, "SELECT id, user_id, name FROM mailbox WHERE user_id = $1 ORDER BY name ASC", userID)
	return mailboxes, err
}

func (db *DB) GetEmailsByMailboxID(ctx context.Context, mailboxID uuid.UUID, limit, offset int) ([]models.Email, error) {
	var emails []models.Email
	err := db.SelectContext(ctx, &emails, `
		SELECT 
			id, mailbox_id, thread_id, address_mapping_id, ingestion_id, message_id, 
			in_reply_to, "references", subject, from_address, to_address, 
			reply_to_address, storage_key, size, receive_datetime, is_read, is_star
		FROM email 
		WHERE mailbox_id = $1 
		ORDER BY receive_datetime DESC 
		LIMIT $2 OFFSET $3
	`, mailboxID, limit, offset)
	return emails, err
}

func (db *DB) GetEmailByID(ctx context.Context, emailID uuid.UUID) (*models.Email, error) {
	var email models.Email
	err := db.GetContext(ctx, &email, `
		SELECT 
			id, mailbox_id, thread_id, address_mapping_id, ingestion_id, message_id, 
			in_reply_to, "references", subject, from_address, to_address, 
			reply_to_address, storage_key, size, receive_datetime, is_read, is_star
		FROM email 
		WHERE id = $1
	`, emailID)
	return &email, err
}

func (db *DB) GetAttachmentsByEmailID(ctx context.Context, emailID uuid.UUID) ([]models.EmailAttachment, error) {
	var attachments []models.EmailAttachment
	err := db.SelectContext(ctx, &attachments, "SELECT id, email_id, filename, content_type, size, storage_key FROM email_attachment WHERE email_id = $1", emailID)
	return attachments, err
}
