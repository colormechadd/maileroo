package outbound

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
)

// QueueDB is the subset of database operations needed by the delivery worker.
type QueueDB interface {
	ClaimOutboundJobs(ctx context.Context, limit int) ([]models.OutboundJob, error)
	UpdateOutboundJobDelivered(ctx context.Context, id uuid.UUID) error
	UpdateOutboundJobFailed(ctx context.Context, id uuid.UUID, lastError string) error
	UpdateOutboundJobDeferred(ctx context.Context, id uuid.UUID, lastError string, attemptCount int, nextAttemptAt time.Time) error
}

// retryDelays maps attempt number (1-based) to the delay before the next retry.
var retryDelays = []time.Duration{
	5 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
	8 * time.Hour,
	24 * time.Hour,
}

// Queue is the outbound delivery worker. It polls for queued jobs and delivers them.
type Queue struct {
	db  QueueDB
	mta *MTA
}

func NewQueue(db QueueDB, mta *MTA) *Queue {
	return &Queue{db: db, mta: mta}
}

// Start launches the worker goroutine. It stops when ctx is cancelled.
func (q *Queue) Start(ctx context.Context) {
	go q.run(ctx)
}

func (q *Queue) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
			q.process(ctx)
		}
	}
}

func (q *Queue) process(ctx context.Context) {
	jobs, err := q.db.ClaimOutboundJobs(ctx, 10)
	if err != nil {
		slog.Error("failed to claim outbound jobs", "error", err)
		return
	}
	for _, job := range jobs {
		q.deliver(ctx, job)
	}
}

// bounceAddress builds the SMTP MAIL FROM for an outbound job.
// Remote MTAs send DSNs back to this address, allowing us to match them to the job.
func bounceAddress(fromAddress string, jobID uuid.UUID) string {
	if parts := strings.SplitN(fromAddress, "@", 2); len(parts) == 2 {
		return fmt.Sprintf("bounces+%s@%s", jobID.String(), parts[1])
	}
	return fromAddress
}

func (q *Queue) deliver(ctx context.Context, job models.OutboundJob) {
	mailFrom := bounceAddress(job.FromAddress, job.ID)
	err := q.mta.Send(mailFrom, job.Recipients, job.RawMessage)
	if err != nil {
		nextAttempt := job.AttemptCount + 1
		if nextAttempt >= job.MaxAttempts {
			if dbErr := q.db.UpdateOutboundJobFailed(ctx, job.ID, err.Error()); dbErr != nil {
				slog.Error("failed to mark job as FAILED", "job_id", job.ID, "error", dbErr)
			}
			slog.Warn("outbound job permanently failed", "job_id", job.ID, "attempts", nextAttempt, "error", err)
			return
		}

		delayIdx := nextAttempt - 1
		if delayIdx >= len(retryDelays) {
			delayIdx = len(retryDelays) - 1
		}
		nextAttemptAt := time.Now().Add(retryDelays[delayIdx])
		if dbErr := q.db.UpdateOutboundJobDeferred(ctx, job.ID, err.Error(), nextAttempt, nextAttemptAt); dbErr != nil {
			slog.Error("failed to mark job as DEFERRED", "job_id", job.ID, "error", dbErr)
		}
		slog.Warn("outbound job deferred", "job_id", job.ID, "attempt", nextAttempt, "next_attempt_at", nextAttemptAt, "error", err)
		return
	}

	if dbErr := q.db.UpdateOutboundJobDelivered(ctx, job.ID); dbErr != nil {
		slog.Error("failed to mark job as DELIVERED", "job_id", job.ID, "error", dbErr)
	}
	slog.Info("outbound job delivered", "job_id", job.ID)
}
