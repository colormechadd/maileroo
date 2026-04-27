package models

import (
	"github.com/google/uuid"
)

type FilterRule struct {
	ID             uuid.UUID `db:"id" json:"id"`
	MailboxID      uuid.UUID `db:"mailbox_id" json:"mailbox_id"`
	Name           string    `db:"name" json:"name"`
	Priority       int       `db:"priority" json:"priority"`
	IsActive       bool      `db:"is_active" json:"is_active"`
	MatchAll       bool      `db:"match_all" json:"match_all"`
	Action         string    `db:"action" json:"action"`
	StopProcessing bool      `db:"stop_processing" json:"stop_processing"`

	Conditions []FilterCondition `db:"-" json:"conditions"`
}

type FilterCondition struct {
	ID       uuid.UUID `db:"id" json:"id"`
	RuleID   uuid.UUID `db:"rule_id" json:"rule_id"`
	Field    string    `db:"field" json:"field"`
	Operator string    `db:"operator" json:"operator"`
	Value    *string   `db:"value" json:"value"`
}

const (
	FilterActionArchive    = "archive"
	FilterActionDelete     = "delete"
	FilterActionMarkRead   = "mark_read"
	FilterActionStar       = "star"
	FilterActionQuarantine = "quarantine"
)

const (
	FilterFieldFrom          = "from"
	FilterFieldTo            = "to"
	FilterFieldSubject       = "subject"
	FilterFieldBody          = "body"
	FilterFieldHasAttachment = "has_attachment"
)

const (
	FilterOperatorContains      = "contains"
	FilterOperatorNotContains   = "not_contains"
	FilterOperatorMatchesRegex  = "matches_regex"
	FilterOperatorIs            = "is"
	FilterOperatorIsNot         = "is_not"
)
