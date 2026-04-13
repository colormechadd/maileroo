package db

import (
	"context"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
)

func (db *DB) ListContacts(ctx context.Context, userID uuid.UUID) ([]models.Contact, error) {
	var contacts []models.Contact
	err := db.SelectContext(ctx, &contacts, `
		SELECT id, user_id, first_name, last_name, email, phone,
		       street, city, state, postal_code, country, notes, is_favorite,
		       create_datetime, update_datetime
		FROM contact
		WHERE user_id = $1
		ORDER BY is_favorite DESC, lower(last_name) ASC, lower(first_name) ASC
	`, userID)
	return contacts, err
}

func (db *DB) SearchContacts(ctx context.Context, userID uuid.UUID, query string) ([]models.Contact, error) {
	var contacts []models.Contact
	err := db.SelectContext(ctx, &contacts, `
		SELECT id, user_id, first_name, last_name, email, phone,
		       street, city, state, postal_code, country, notes, is_favorite,
		       create_datetime, update_datetime
		FROM contact
		WHERE user_id = $1
		  AND (
		    first_name ILIKE '%' || $2 || '%'
		    OR last_name ILIKE '%' || $2 || '%'
		    OR email ILIKE '%' || $2 || '%'
		    OR (first_name || ' ' || last_name) ILIKE '%' || $2 || '%'
		  )
		ORDER BY is_favorite DESC, lower(last_name) ASC, lower(first_name) ASC
		LIMIT 10
	`, userID, query)
	return contacts, err
}

func (db *DB) GetContactByID(ctx context.Context, contactID, userID uuid.UUID) (*models.Contact, error) {
	var c models.Contact
	err := db.GetContext(ctx, &c, `
		SELECT id, user_id, first_name, last_name, email, phone,
		       street, city, state, postal_code, country, notes, is_favorite,
		       create_datetime, update_datetime
		FROM contact
		WHERE id = $1 AND user_id = $2
	`, contactID, userID)
	return &c, err
}

func (db *DB) CreateContact(ctx context.Context, c models.Contact) (*models.Contact, error) {
	var result models.Contact
	err := db.GetContext(ctx, &result, `
		INSERT INTO contact (user_id, first_name, last_name, email, phone, street, city, state, postal_code, country, notes, is_favorite)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, user_id, first_name, last_name, email, phone,
		          street, city, state, postal_code, country, notes, is_favorite,
		          create_datetime, update_datetime
	`, c.UserID, c.FirstName, c.LastName, c.Email, c.Phone,
		c.Street, c.City, c.State, c.PostalCode, c.Country, c.Notes, c.IsFavorite)
	return &result, err
}

func (db *DB) UpdateContact(ctx context.Context, c models.Contact) error {
	_, err := db.ExecContext(ctx, `
		UPDATE contact
		SET first_name = $1, last_name = $2, email = $3, phone = $4,
		    street = $5, city = $6, state = $7, postal_code = $8, country = $9,
		    notes = $10, is_favorite = $11, update_datetime = NOW()
		WHERE id = $12 AND user_id = $13
	`, c.FirstName, c.LastName, c.Email, c.Phone,
		c.Street, c.City, c.State, c.PostalCode, c.Country,
		c.Notes, c.IsFavorite, c.ID, c.UserID)
	return err
}

func (db *DB) DeleteContact(ctx context.Context, contactID, userID uuid.UUID) error {
	_, err := db.ExecContext(ctx, `DELETE FROM contact WHERE id = $1 AND user_id = $2`, contactID, userID)
	return err
}

func (db *DB) ToggleContactFavorite(ctx context.Context, contactID, userID uuid.UUID) error {
	_, err := db.ExecContext(ctx, `
		UPDATE contact SET is_favorite = NOT is_favorite, update_datetime = NOW()
		WHERE id = $1 AND user_id = $2
	`, contactID, userID)
	return err
}

func (db *DB) UpsertContactFromEmail(ctx context.Context, userID uuid.UUID, email, firstName, lastName string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO contact (user_id, email, first_name, last_name)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, email) DO NOTHING
	`, userID, email, firstName, lastName)
	return err
}
