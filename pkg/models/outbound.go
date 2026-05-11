package models

import (
	"time"

	"github.com/google/uuid"
)

type DKIMKey struct {
	ID       uuid.UUID `db:"id" json:"id"`
	Domain   string    `db:"domain" json:"domain"`
	Selector string    `db:"selector" json:"selector"`
	KeyData  []byte    `db:"key_data" json:"-"`
	IsActive bool      `db:"is_active" json:"is_active"`
}

type OutboundStatus string

const (
	OutboundQueued    OutboundStatus = "QUEUED"
	OutboundSending   OutboundStatus = "SENDING"
	OutboundDelivered OutboundStatus = "DELIVERED"
	OutboundDeferred  OutboundStatus = "DEFERRED"
	OutboundFailed    OutboundStatus = "FAILED"
)

type OutboundJobAttempt struct {
	ID             uuid.UUID `db:"id"`
	JobID          uuid.UUID `db:"job_id"`
	AttemptNumber  int       `db:"attempt_number"`
	Outcome        string    `db:"outcome"`
	ServerResponse *string   `db:"server_response"`
	AttemptDatetime time.Time `db:"attempt_datetime"`
}

type OutboundJob struct {
	ID                  uuid.UUID      `db:"id"`
	EmailID             *uuid.UUID     `db:"email_id"`
	FromAddress         string         `db:"from_address"`
	Recipients          []string       `db:"recipients"`
	RawMessage          []byte         `db:"raw_message"`
	Status              OutboundStatus `db:"status"`
	AttemptCount        int            `db:"attempt_count"`
	MaxAttempts         int            `db:"max_attempts"`
	LastError           *string        `db:"last_error"`
	NextAttemptDatetime time.Time      `db:"next_attempt_datetime"`
	DeliveryDatetime    *time.Time     `db:"delivery_datetime"`
}
