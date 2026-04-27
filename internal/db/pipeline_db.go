package db

import (
	"context"
	"database/sql"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type PipelineDB interface {
	CreateIngestion(ctx context.Context, ingestion *models.Ingestion) error
	CreateIngestionStep(ctx context.Context, step *models.IngestionStep) error
	UpdateIngestionStatus(ctx context.Context, id uuid.UUID, status string) error
	IsBlockedByMailboxRules(ctx context.Context, mailboxID uuid.UUID, fromAddress string) (bool, error)
	CreateEmail(ctx context.Context, email *models.Email) error
	SetEmailStatus(ctx context.Context, id uuid.UUID, status models.EmailStatus) error
	CreateAttachment(ctx context.Context, attachment *models.EmailAttachment) error
	CreateThread(ctx context.Context, thread *models.Thread) error
	FindThreadIDByMessageIDs(ctx context.Context, mailboxID uuid.UUID, messageIDs []string) (uuid.UUID, error)
	UpdateOutboundJobFailed(ctx context.Context, id uuid.UUID, lastError string) error
	GetMailboxUserIDs(ctx context.Context, mailboxID uuid.UUID) ([]uuid.UUID, error)
	GetActiveFilterRulesForMailbox(ctx context.Context, mailboxID uuid.UUID) ([]*models.FilterRule, error)
	SetEmailFields(ctx context.Context, id uuid.UUID, isRead, isStar bool, status models.EmailStatus) error
}

func (db *DB) CreateIngestion(ctx context.Context, ingestion *models.Ingestion) error {
	_, err := db.NamedExecContext(ctx, `
		INSERT INTO ingestion (id, from_address, to_address, status)
		VALUES (:id, :from_address, :to_address, :status)
	`, ingestion)
	return err
}

func (db *DB) CreateIngestionStep(ctx context.Context, step *models.IngestionStep) error {
	_, err := db.NamedExecContext(ctx, `
		INSERT INTO ingestion_step (ingestion_id, step_name, status, details, duration_ms)
		VALUES (:ingestion_id, :step_name, :status, :details, :duration_ms)
	`, step)
	return err
}

func (db *DB) UpdateIngestionStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := db.ExecContext(ctx, "UPDATE ingestion SET status = $1, update_datetime = CURRENT_TIMESTAMP WHERE id = $2", status, id)
	return err
}

func (db *DB) CreateBlockRule(ctx context.Context, mailboxID uuid.UUID, addressPattern string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO mailbox_block_rule (mailbox_id, address_pattern)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, mailboxID, addressPattern)
	return err
}

func (db *DB) IsBlockedByMailboxRules(ctx context.Context, mailboxID uuid.UUID, fromAddress string) (bool, error) {
	var count int
	err := db.GetContext(ctx, &count, `
		SELECT COUNT(*)
		FROM mailbox_block_rule
		WHERE mailbox_id = $1 
		  AND is_active = TRUE 
		  AND $2 ~ address_pattern
	`, mailboxID, fromAddress)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (db *DB) CreateEmail(ctx context.Context, email *models.Email) error {
	_, err := db.NamedExecContext(ctx, `
		INSERT INTO email (
			id, mailbox_id, thread_id, address_mapping_id, ingestion_id, message_id,
			in_reply_to, "references", subject, from_address, to_address,
			reply_to_address, storage_key, size, stored_size, receive_datetime, is_read, is_star,
			direction, status, sending_address_id, user_id, body_plain
		) VALUES (
			:id, :mailbox_id, :thread_id, :address_mapping_id, :ingestion_id, :message_id,
			:in_reply_to, :references, :subject, :from_address, :to_address,
			:reply_to_address, :storage_key, :size, :stored_size, :receive_datetime, :is_read, :is_star,
			:direction, :status, :sending_address_id, :user_id, :body_plain
		)
	`, email)
	return err
}

func (db *DB) SetEmailStatus(ctx context.Context, id uuid.UUID, status models.EmailStatus) error {
	_, err := db.ExecContext(ctx, "UPDATE email SET status = $1, update_datetime = CURRENT_TIMESTAMP WHERE id = $2", status, id)
	return err
}

func (db *DB) CreateAttachment(ctx context.Context, attachment *models.EmailAttachment) error {
	_, err := db.NamedExecContext(ctx, `
		INSERT INTO email_attachment (
			id, email_id, filename, content_type, size, storage_key
		) VALUES (
			:id, :email_id, :filename, :content_type, :size, :storage_key
		)
	`, attachment)
	return err
}

func (db *DB) CreateThread(ctx context.Context, thread *models.Thread) error {
	_, err := db.NamedExecContext(ctx, `
		INSERT INTO thread (id, mailbox_id, subject)
		VALUES (:id, :mailbox_id, :subject)
	`, thread)
	return err
}

func (db *DB) FindThreadIDByMessageIDs(ctx context.Context, mailboxID uuid.UUID, messageIDs []string) (uuid.UUID, error) {
	if len(messageIDs) == 0 {
		return uuid.Nil, sql.ErrNoRows
	}

	var threadID uuid.UUID
	query, args, err := sqlx.In(`
		SELECT thread_id 
		FROM email 
		WHERE mailbox_id = ? AND message_id IN (?) AND thread_id IS NOT NULL 
		LIMIT 1
	`, mailboxID, messageIDs)
	if err != nil {
		return uuid.Nil, err
	}

	query = db.Rebind(query)
	err = db.GetContext(ctx, &threadID, query, args...)
	return threadID, err
}

func (db *DB) GetMailboxUserIDs(ctx context.Context, mailboxID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := db.SelectContext(ctx, &ids, `
		SELECT user_id FROM mailbox_user WHERE mailbox_id = $1 AND is_active = TRUE
	`, mailboxID)
	return ids, err
}

func (db *DB) GetActiveFilterRulesForMailbox(ctx context.Context, mailboxID uuid.UUID) ([]*models.FilterRule, error) {
	var rules []*models.FilterRule
	err := db.SelectContext(ctx, &rules, `
		SELECT id, mailbox_id, name, priority, is_active, match_all, action, stop_processing,
		       created_by_user_id, updated_by_user_id, create_datetime, update_datetime
		FROM mailbox_filter_rule
		WHERE mailbox_id = $1 AND is_active = TRUE
		ORDER BY priority ASC
	`, mailboxID)
	if err != nil {
		return nil, err
	}

	if len(rules) == 0 {
		return rules, nil
	}

	ruleIDs := make([]uuid.UUID, len(rules))
	ruleIndex := make(map[uuid.UUID]*models.FilterRule, len(rules))
	for i, r := range rules {
		ruleIDs[i] = r.ID
		ruleIndex[r.ID] = r
	}

	query, args, err := sqlx.In(`
		SELECT id, rule_id, field, operator, value, create_datetime
		FROM mailbox_filter_condition
		WHERE rule_id IN (?)
		ORDER BY create_datetime ASC
	`, ruleIDs)
	if err != nil {
		return nil, err
	}
	query = db.Rebind(query)

	var conditions []models.FilterCondition
	if err := db.SelectContext(ctx, &conditions, query, args...); err != nil {
		return nil, err
	}

	for _, c := range conditions {
		r := ruleIndex[c.RuleID]
		r.Conditions = append(r.Conditions, c)
	}

	return rules, nil
}

func (db *DB) SetEmailFields(ctx context.Context, id uuid.UUID, isRead, isStar bool, status models.EmailStatus) error {
	_, err := db.ExecContext(ctx, `
		UPDATE email SET is_read = $1, is_star = $2, status = $3, update_datetime = CURRENT_TIMESTAMP
		WHERE id = $4
	`, isRead, isStar, status, id)
	return err
}
