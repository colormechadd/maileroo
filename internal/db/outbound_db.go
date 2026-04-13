package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// InsertOutboundJob enqueues a new outbound delivery job and returns it.
func (db *DB) InsertOutboundJob(ctx context.Context, emailID *uuid.UUID, fromAddress string, recipients []string, rawMessage []byte) (*models.OutboundJob, error) {
	recipientsJSON, err := json.Marshal(recipients)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal recipients: %w", err)
	}

	var job models.OutboundJob
	var recipientsRaw []byte
	err = db.QueryRowContext(ctx, `
		INSERT INTO outbound_job (email_id, from_address, recipients, raw_message)
		VALUES ($1, $2, $3, $4)
		RETURNING id, email_id, from_address, recipients, raw_message, status, attempt_count, max_attempts, last_error, next_attempt_datetime, delivery_datetime
	`, emailID, fromAddress, recipientsJSON, rawMessage).Scan(
		&job.ID, &job.EmailID, &job.FromAddress, &recipientsRaw, &job.RawMessage,
		&job.Status, &job.AttemptCount, &job.MaxAttempts, &job.LastError,
		&job.NextAttemptDatetime, &job.DeliveryDatetime,
	)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(recipientsRaw, &job.Recipients); err != nil {
		return nil, fmt.Errorf("failed to unmarshal recipients: %w", err)
	}
	return &job, nil
}

// outboundJobRow is a scan target that handles the JSONB recipients column.
type outboundJobRow struct {
	ID                  uuid.UUID             `db:"id"`
	EmailID             *uuid.UUID            `db:"email_id"`
	FromAddress         string                `db:"from_address"`
	Recipients          []byte                `db:"recipients"`
	RawMessage          []byte                `db:"raw_message"`
	Status              models.OutboundStatus `db:"status"`
	AttemptCount        int                   `db:"attempt_count"`
	MaxAttempts         int                   `db:"max_attempts"`
	LastError           *string               `db:"last_error"`
	NextAttemptDatetime time.Time             `db:"next_attempt_datetime"`
	DeliveryDatetime    *time.Time            `db:"delivery_datetime"`
}

func (r *outboundJobRow) toModel() (models.OutboundJob, error) {
	job := models.OutboundJob{
		ID:                  r.ID,
		EmailID:             r.EmailID,
		FromAddress:         r.FromAddress,
		RawMessage:          r.RawMessage,
		Status:              r.Status,
		AttemptCount:        r.AttemptCount,
		MaxAttempts:         r.MaxAttempts,
		LastError:           r.LastError,
		NextAttemptDatetime: r.NextAttemptDatetime,
		DeliveryDatetime:    r.DeliveryDatetime,
	}
	if err := json.Unmarshal(r.Recipients, &job.Recipients); err != nil {
		return job, fmt.Errorf("failed to unmarshal recipients: %w", err)
	}
	return job, nil
}

// ClaimOutboundJobs atomically selects up to limit ready jobs, marks them SENDING, and returns them.
func (db *DB) ClaimOutboundJobs(ctx context.Context, limit int) ([]models.OutboundJob, error) {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var rows []outboundJobRow
	err = tx.SelectContext(ctx, &rows, `
		SELECT id, email_id, from_address, recipients, raw_message, status, attempt_count, max_attempts, last_error, next_attempt_datetime, delivery_datetime
		FROM outbound_job
		WHERE status IN ('QUEUED', 'DEFERRED') AND next_attempt_datetime <= NOW()
		ORDER BY next_attempt_datetime ASC
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`, limit)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, tx.Commit()
	}

	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	_, err = tx.ExecContext(ctx,
		`UPDATE outbound_job SET status = 'SENDING', update_datetime = NOW() WHERE id = ANY($1)`,
		pq.Array(ids),
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	jobs := make([]models.OutboundJob, 0, len(rows))
	for i := range rows {
		job, err := rows[i].toModel()
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (db *DB) UpdateOutboundJobDelivered(ctx context.Context, id uuid.UUID) error {
	_, err := db.ExecContext(ctx, `
		UPDATE outbound_job
		SET status = 'DELIVERED', delivery_datetime = NOW(), update_datetime = NOW()
		WHERE id = $1
	`, id)
	return err
}

func (db *DB) UpdateOutboundJobFailed(ctx context.Context, id uuid.UUID, lastError string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE outbound_job
		SET status = 'FAILED', last_error = $2, update_datetime = NOW()
		WHERE id = $1
	`, id, lastError)
	return err
}

func (db *DB) UpdateOutboundJobDeferred(ctx context.Context, id uuid.UUID, lastError string, attemptCount int, nextAttemptAt time.Time) error {
	_, err := db.ExecContext(ctx, `
		UPDATE outbound_job
		SET status = 'DEFERRED', last_error = $2, attempt_count = $3, next_attempt_datetime = $4, update_datetime = NOW()
		WHERE id = $1
	`, id, lastError, attemptCount, nextAttemptAt)
	return err
}
