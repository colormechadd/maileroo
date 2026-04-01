package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID           uuid.UUID `db:"id" json:"id"`
	Username     string    `db:"username" json:"username"`
	PasswordHash string    `db:"password_hash" json:"-"`
	IsActive     bool      `db:"is_active" json:"is_active"`
}

type Mailbox struct {
	ID   uuid.UUID `db:"id" json:"id"`
	Name string    `db:"name" json:"name"`
}

type MailboxUser struct {
	ID        uuid.UUID `db:"id" json:"id"`
	MailboxID uuid.UUID `db:"mailbox_id" json:"mailbox_id"`
	UserID    uuid.UUID `db:"user_id" json:"user_id"`
	IsActive  bool      `db:"is_active" json:"is_active"`
}

type SendingAddress struct {
	ID             uuid.UUID `db:"id" json:"id"`
	UserID         uuid.UUID `db:"user_id" json:"user_id"`
	MailboxID      uuid.UUID `db:"mailbox_id" json:"mailbox_id"`
	Address        string    `db:"address" json:"address"`
	IsActive       bool      `db:"is_active" json:"is_active"`
}

type AddressMapping struct {
	ID             uuid.UUID `db:"id" json:"id"`
	AddressPattern string    `db:"address_pattern" json:"address_pattern"`
	MailboxID      uuid.UUID `db:"mailbox_id" json:"mailbox_id"`
	Priority       int       `db:"priority" json:"priority"`
	IsActive       bool      `db:"is_active" json:"is_active"`
}

type WebmailSession struct {
	ID              uuid.UUID `db:"id" json:"id"`
	UserID          uuid.UUID `db:"user_id" json:"user_id"`
	Token           string    `db:"token" json:"token"`
	RemoteIP        *string   `db:"remote_ip" json:"remote_ip"`
	UserAgent       *string   `db:"user_agent" json:"user_agent"`
	ExpiresDatetime time.Time `db:"expires_datetime" json:"expires_datetime"`
}

type Thread struct {
	ID        uuid.UUID `db:"id" json:"id"`
	MailboxID uuid.UUID `db:"mailbox_id" json:"mailbox_id"`
	Subject   string    `db:"subject" json:"subject"`
}

type Ingestion struct {
	ID          uuid.UUID `db:"id" json:"id"`
	MessageID   *string   `db:"message_id" json:"message_id"`
	FromAddress *string   `db:"from_address" json:"from_address"`
	ToAddress   *string   `db:"to_address" json:"to_address"`
	Status      string    `db:"status" json:"status"`
}

type IngestionStep struct {
	ID          uuid.UUID       `db:"id" json:"id"`
	IngestionID uuid.UUID       `db:"ingestion_id" json:"ingestion_id"`
	StepName    string          `db:"step_name" json:"step_name"`
	Status      string          `db:"status" json:"status"`
	Details     json.RawMessage `db:"details" json:"details"`
	DurationMS  int             `db:"duration_ms" json:"duration_ms"`
}

type EmailDirection string

const (
	DirectionInbound  EmailDirection = "INBOUND"
	DirectionOutbound EmailDirection = "OUTBOUND"
)

type EmailStatus string

const (
	StatusQuarantined EmailStatus = "QUARANTINED"
	StatusDeleted     EmailStatus = "DELETED"
	StatusInbox       EmailStatus = "INBOX"
	StatusArchived    EmailStatus = "ARCHIVED"
)

type Email struct {
	ID               uuid.UUID  `db:"id" json:"id"`
	MailboxID        uuid.UUID  `db:"mailbox_id" json:"mailbox_id"`
	ThreadID         *uuid.UUID `db:"thread_id" json:"thread_id"`
	AddressMappingID *uuid.UUID `db:"address_mapping_id" json:"address_mapping_id"`
	IngestionID      *uuid.UUID `db:"ingestion_id" json:"ingestion_id"`
	MessageID        string     `db:"message_id" json:"message_id"`
	InReplyTo        *string    `db:"in_reply_to" json:"in_reply_to"`
	References       *string    `db:"references" json:"references"`
	Subject          string     `db:"subject" json:"subject"`
	FromAddress      string     `db:"from_address" json:"from_address"`
	ToAddress        string     `db:"to_address" json:"to_address"`
	ReplyToAddress   *string    `db:"reply_to_address" json:"reply_to_address"`
	StorageKey       string     `db:"storage_key" json:"storage_key"`
	Size             int64      `db:"size" json:"size"`
	ReceiveDatetime  time.Time  `db:"receive_datetime" json:"receive_datetime"`
	IsRead           bool       `db:"is_read" json:"is_read"`
	IsStar           bool       `db:"is_star" json:"is_star"`
	Direction        EmailDirection `db:"direction" json:"direction"`
	Status           EmailStatus    `db:"status" json:"status"`
	SendingAddressID *uuid.UUID     `db:"sending_address_id" json:"sending_address_id"`
	UserID           *uuid.UUID     `db:"user_id" json:"user_id,omitempty"`
	BodyPlain        *string        `db:"body_plain" json:"-"`
}

type Draft struct {
	ID               uuid.UUID  `db:"id" json:"id"`
	MailboxID        uuid.UUID  `db:"mailbox_id" json:"mailbox_id"`
	UserID           uuid.UUID  `db:"user_id" json:"user_id"`
	SendingAddressID *uuid.UUID `db:"sending_address_id" json:"sending_address_id"`
	ToAddress        string     `db:"to_address" json:"to_address"`
	CcAddress        string     `db:"cc_address" json:"cc_address"`
	BccAddress       string     `db:"bcc_address" json:"bcc_address"`
	Subject          string     `db:"subject" json:"subject"`
	Body             string     `db:"body" json:"body"`
	BodyHTML         string     `db:"body_html" json:"body_html"`
	InReplyTo        *string    `db:"in_reply_to" json:"in_reply_to"`
	References       *string    `db:"references" json:"references"`
	CreateDatetime   time.Time  `db:"create_datetime" json:"create_datetime"`
	UpdateDatetime   time.Time  `db:"update_datetime" json:"update_datetime"`
}

type DKIMKey struct {
	ID       uuid.UUID `db:"id" json:"id"`
	Domain   string    `db:"domain" json:"domain"`
	Selector string    `db:"selector" json:"selector"`
	KeyData  []byte    `db:"key_data" json:"-"`
	IsActive bool      `db:"is_active" json:"is_active"`
}

type EmailAttachment struct {
	ID          uuid.UUID `db:"id" json:"id"`
	EmailID     uuid.UUID `db:"email_id" json:"email_id"`
	Filename    string    `db:"filename" json:"filename"`
	ContentType string    `db:"content_type" json:"content_type"`
	Size        int64     `db:"size" json:"size"`
	StorageKey  string    `db:"storage_key" json:"storage_key"`
}

type OutboundStatus string

const (
	OutboundQueued    OutboundStatus = "QUEUED"
	OutboundSending   OutboundStatus = "SENDING"
	OutboundDelivered OutboundStatus = "DELIVERED"
	OutboundDeferred  OutboundStatus = "DEFERRED"
	OutboundFailed    OutboundStatus = "FAILED"
)

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
