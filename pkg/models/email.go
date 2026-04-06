package models

import (
	"time"

	"github.com/google/uuid"
)

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
	ID               uuid.UUID      `db:"id" json:"id"`
	MailboxID        uuid.UUID      `db:"mailbox_id" json:"mailbox_id"`
	ThreadID         *uuid.UUID     `db:"thread_id" json:"thread_id"`
	AddressMappingID *uuid.UUID     `db:"address_mapping_id" json:"address_mapping_id"`
	IngestionID      *uuid.UUID     `db:"ingestion_id" json:"ingestion_id"`
	MessageID        string         `db:"message_id" json:"message_id"`
	InReplyTo        *string        `db:"in_reply_to" json:"in_reply_to"`
	References       *string        `db:"references" json:"references"`
	Subject          string         `db:"subject" json:"subject"`
	FromAddress      string         `db:"from_address" json:"from_address"`
	ToAddress        string         `db:"to_address" json:"to_address"`
	ReplyToAddress   *string        `db:"reply_to_address" json:"reply_to_address"`
	StorageKey       string         `db:"storage_key" json:"storage_key"`
	Size             int64          `db:"size" json:"size"`
	StoredSize       int64          `db:"stored_size" json:"stored_size"`
	ReceiveDatetime  time.Time      `db:"receive_datetime" json:"receive_datetime"`
	IsRead           bool           `db:"is_read" json:"is_read"`
	IsStar           bool           `db:"is_star" json:"is_star"`
	Direction        EmailDirection `db:"direction" json:"direction"`
	Status           EmailStatus    `db:"status" json:"status"`
	SendingAddressID *uuid.UUID     `db:"sending_address_id" json:"sending_address_id"`
	UserID           *uuid.UUID     `db:"user_id" json:"user_id,omitempty"`
	BodyPlain        *string        `db:"body_plain" json:"-"`
}

type EmailAttachment struct {
	ID          uuid.UUID `db:"id" json:"id"`
	EmailID     uuid.UUID `db:"email_id" json:"email_id"`
	Filename    string    `db:"filename" json:"filename"`
	ContentType string    `db:"content_type" json:"content_type"`
	Size        int64     `db:"size" json:"size"`
	StorageKey  string    `db:"storage_key" json:"storage_key"`
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
