package models

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

type Contact struct {
	ID             uuid.UUID `db:"id" json:"id"`
	MailboxID      uuid.UUID `db:"mailbox_id" json:"mailbox_id"`
	FirstName      string    `db:"first_name" json:"first_name"`
	LastName       string    `db:"last_name" json:"last_name"`
	Email          string    `db:"email" json:"email"`
	Phone          string    `db:"phone" json:"phone"`
	Street         string    `db:"street" json:"street"`
	City           string    `db:"city" json:"city"`
	State          string    `db:"state" json:"state"`
	PostalCode     string    `db:"postal_code" json:"postal_code"`
	Country        string    `db:"country" json:"country"`
	Notes          string    `db:"notes" json:"notes"`
	IsFavorite     bool      `db:"is_favorite" json:"is_favorite"`
	CreateDatetime time.Time `db:"create_datetime" json:"create_datetime"`
	UpdateDatetime time.Time `db:"update_datetime" json:"update_datetime"`
}

func (c Contact) DisplayName() string {
	name := strings.TrimSpace(c.FirstName + " " + c.LastName)
	if name == "" {
		return c.Email
	}
	return name
}

func (c Contact) Initials() string {
	f := ""
	l := ""
	if len(c.FirstName) > 0 {
		f = string([]rune(c.FirstName)[0])
	}
	if len(c.LastName) > 0 {
		l = string([]rune(c.LastName)[0])
	}
	init := strings.ToUpper(f + l)
	if init == "" {
		if len(c.Email) > 0 {
			return strings.ToUpper(string([]rune(c.Email)[0]))
		}
		return "?"
	}
	return init
}
