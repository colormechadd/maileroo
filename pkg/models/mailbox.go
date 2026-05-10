package models

import "github.com/google/uuid"

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
	ID          uuid.UUID `db:"id" json:"id"`
	UserID      uuid.UUID `db:"user_id" json:"user_id"`
	MailboxID   uuid.UUID `db:"mailbox_id" json:"mailbox_id"`
	Address     string    `db:"address" json:"address"`
	DisplayName *string   `db:"display_name" json:"display_name"`
	IsActive    bool      `db:"is_active" json:"is_active"`
}

type AddressMapping struct {
	ID             uuid.UUID `db:"id" json:"id"`
	AddressPattern string    `db:"address_pattern" json:"address_pattern"`
	MailboxID      uuid.UUID `db:"mailbox_id" json:"mailbox_id"`
	Priority       int       `db:"priority" json:"priority"`
	IsActive       bool      `db:"is_active" json:"is_active"`
}

type Thread struct {
	ID        uuid.UUID `db:"id" json:"id"`
	MailboxID uuid.UUID `db:"mailbox_id" json:"mailbox_id"`
	Subject   string    `db:"subject" json:"subject"`
}

type MailboxBlockRule struct {
	ID               uuid.UUID  `db:"id" json:"id"`
	MailboxID        uuid.UUID  `db:"mailbox_id" json:"mailbox_id"`
	AddressPattern   string     `db:"address_pattern" json:"address_pattern"`
	IsActive         bool       `db:"is_active" json:"is_active"`
	UserID           *uuid.UUID `db:"user_id" json:"user_id"`
	BlockedByUsername *string   `db:"username" json:"username"`
}
