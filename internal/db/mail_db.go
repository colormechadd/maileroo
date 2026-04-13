package db

import (
	"context"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
)

type MailDB interface {
	LookupMailboxByAddress(ctx context.Context, address string) (*models.Mailbox, uuid.UUID, error)
}

func (db *DB) LookupMailboxByAddress(ctx context.Context, address string) (*models.Mailbox, uuid.UUID, error) {
	var result struct {
		models.Mailbox
		MappingID uuid.UUID `db:"mapping_id"`
	}

	// Use PostgreSQL's regex operator (~) to match the address
	err := db.GetContext(ctx, &result, `
		SELECT
			m.id, m.name,
			am.id as mapping_id
		FROM address_mapping am
		JOIN mailbox m ON am.mailbox_id = m.id
		WHERE am.is_active = TRUE
		  AND $1 ~ am.address_pattern
		ORDER BY am.priority DESC
		LIMIT 1
	`, address)

	if err != nil {
		return nil, uuid.Nil, err
	}

	return &result.Mailbox, result.MappingID, nil
}
