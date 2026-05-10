package db

import (
	"context"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
)

func (db *DB) ListContacts(ctx context.Context, mailboxID uuid.UUID) ([]models.Contact, error) {
	var contacts []models.Contact
	err := db.SelectContext(ctx, &contacts, `
		SELECT id, mailbox_id, first_name, last_name, email, phone,
		       street, city, state, postal_code, country, notes, is_favorite,
		       create_datetime, update_datetime
		FROM contact
		WHERE mailbox_id = $1
		ORDER BY is_favorite DESC, lower(last_name) ASC, lower(first_name) ASC
	`, mailboxID)
	return contacts, err
}

func (db *DB) SearchContacts(ctx context.Context, mailboxID uuid.UUID, query string) ([]models.Contact, error) {
	var contacts []models.Contact
	err := db.SelectContext(ctx, &contacts, `
		SELECT id, mailbox_id, first_name, last_name, email, phone,
		       street, city, state, postal_code, country, notes, is_favorite,
		       create_datetime, update_datetime
		FROM contact
		WHERE mailbox_id = $1
		  AND (
		    first_name ILIKE '%' || $2 || '%'
		    OR last_name ILIKE '%' || $2 || '%'
		    OR email ILIKE '%' || $2 || '%'
		    OR (first_name || ' ' || last_name) ILIKE '%' || $2 || '%'
		  )
		ORDER BY is_favorite DESC, lower(last_name) ASC, lower(first_name) ASC
		LIMIT 10
	`, mailboxID, query)
	return contacts, err
}

// SearchContactsForUser searches contacts across all mailboxes accessible to a user.
// Used by compose autocomplete where no single mailbox context exists.
func (db *DB) SearchContactsForUser(ctx context.Context, userID uuid.UUID, query string) ([]models.Contact, error) {
	var contacts []models.Contact
	err := db.SelectContext(ctx, &contacts, `
		SELECT c.id, c.mailbox_id, c.first_name, c.last_name, c.email, c.phone,
		       c.street, c.city, c.state, c.postal_code, c.country, c.notes, c.is_favorite,
		       c.create_datetime, c.update_datetime
		FROM contact c
		JOIN mailbox_user mu ON mu.mailbox_id = c.mailbox_id
		WHERE mu.user_id = $1
		  AND mu.is_active = TRUE
		  AND (
		    c.first_name ILIKE '%' || $2 || '%'
		    OR c.last_name ILIKE '%' || $2 || '%'
		    OR c.email ILIKE '%' || $2 || '%'
		    OR (c.first_name || ' ' || c.last_name) ILIKE '%' || $2 || '%'
		  )
		ORDER BY c.is_favorite DESC, lower(c.last_name) ASC, lower(c.first_name) ASC
		LIMIT 10
	`, userID, query)
	return contacts, err
}

func (db *DB) GetContactByID(ctx context.Context, contactID, mailboxID uuid.UUID) (*models.Contact, error) {
	var c models.Contact
	err := db.GetContext(ctx, &c, `
		SELECT id, mailbox_id, first_name, last_name, email, phone,
		       street, city, state, postal_code, country, notes, is_favorite,
		       create_datetime, update_datetime
		FROM contact
		WHERE id = $1 AND mailbox_id = $2
	`, contactID, mailboxID)
	return &c, err
}

func (db *DB) CreateContact(ctx context.Context, c models.Contact) (*models.Contact, error) {
	var result models.Contact
	err := db.GetContext(ctx, &result, `
		INSERT INTO contact (mailbox_id, first_name, last_name, email, phone, street, city, state, postal_code, country, notes, is_favorite)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, mailbox_id, first_name, last_name, email, phone,
		          street, city, state, postal_code, country, notes, is_favorite,
		          create_datetime, update_datetime
	`, c.MailboxID, c.FirstName, c.LastName, c.Email, c.Phone,
		c.Street, c.City, c.State, c.PostalCode, c.Country, c.Notes, c.IsFavorite)
	return &result, err
}

func (db *DB) UpdateContact(ctx context.Context, c models.Contact) error {
	_, err := db.ExecContext(ctx, `
		UPDATE contact
		SET first_name = $1, last_name = $2, email = $3, phone = $4,
		    street = $5, city = $6, state = $7, postal_code = $8, country = $9,
		    notes = $10, is_favorite = $11, update_datetime = NOW()
		WHERE id = $12 AND mailbox_id = $13
	`, c.FirstName, c.LastName, c.Email, c.Phone,
		c.Street, c.City, c.State, c.PostalCode, c.Country,
		c.Notes, c.IsFavorite, c.ID, c.MailboxID)
	return err
}

func (db *DB) DeleteContact(ctx context.Context, contactID, mailboxID uuid.UUID) error {
	_, err := db.ExecContext(ctx, `DELETE FROM contact WHERE id = $1 AND mailbox_id = $2`, contactID, mailboxID)
	return err
}

func (db *DB) ToggleContactFavorite(ctx context.Context, contactID, mailboxID uuid.UUID) error {
	_, err := db.ExecContext(ctx, `
		UPDATE contact SET is_favorite = NOT is_favorite, update_datetime = NOW()
		WHERE id = $1 AND mailbox_id = $2
	`, contactID, mailboxID)
	return err
}

func (db *DB) GetContactByEmail(ctx context.Context, mailboxID uuid.UUID, email string) (*models.Contact, error) {
	var c models.Contact
	err := db.GetContext(ctx, &c, `
		SELECT id, mailbox_id, first_name, last_name, email, phone,
		       street, city, state, postal_code, country, notes, is_favorite,
		       create_datetime, update_datetime
		FROM contact
		WHERE mailbox_id = $1 AND lower(email) = lower($2)
	`, mailboxID, email)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (db *DB) GetRecentEmailsByContact(ctx context.Context, mailboxID uuid.UUID, contactEmail string, limit int) ([]models.Email, error) {
	var emails []models.Email
	err := db.SelectContext(ctx, &emails, `
		SELECT
			id, mailbox_id, thread_id, address_mapping_id, ingestion_id, message_id,
			in_reply_to, "references", subject, from_address, to_address,
			reply_to_address, storage_key, size, receive_datetime, is_read, is_star, direction, status, sending_address_id, user_id, body_plain
		FROM email
		WHERE mailbox_id = $1
		  AND status != 'DELETED'
		  AND (lower(from_address) LIKE '%' || lower($2) || '%' OR lower(to_address) LIKE '%' || lower($2) || '%')
		ORDER BY receive_datetime DESC, id DESC
		LIMIT $3
	`, mailboxID, contactEmail, limit)
	return emails, err
}

func (db *DB) UpsertContactFromEmail(ctx context.Context, mailboxID uuid.UUID, email, firstName, lastName string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO contact (mailbox_id, email, first_name, last_name)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (mailbox_id, email) DO NOTHING
	`, mailboxID, email, firstName, lastName)
	return err
}
