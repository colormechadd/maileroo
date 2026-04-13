package db

import (
	"context"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
)

type AdminDB interface {
	CreateUser(ctx context.Context, user *models.User) error
	CreateMailbox(ctx context.Context, mb *models.Mailbox, userID uuid.UUID) error
	CreateAddressMapping(ctx context.Context, am *models.AddressMapping) error
	ListUsers(ctx context.Context) ([]models.User, error)
	ListMailboxes(ctx context.Context, userID uuid.UUID) ([]models.Mailbox, error)
	DeleteUser(ctx context.Context, userID uuid.UUID) error
	DeleteMailbox(ctx context.Context, mailboxID uuid.UUID) error
	GetUserByUsername(ctx context.Context, username string) (*models.User, error)

	AddUserToMailbox(ctx context.Context, mailboxID, userID uuid.UUID) error

	AddSendingAddress(ctx context.Context, sa *models.SendingAddress) error
	ListSendingAddresses(ctx context.Context, userID uuid.UUID) ([]models.SendingAddress, error)
	DeactivateSendingAddress(ctx context.Context, saID uuid.UUID) error

	InsertDKIMKey(ctx context.Context, key *models.DKIMKey) error
	GetActiveDKIMKey(ctx context.Context, domain string, selector *string) (*models.DKIMKey, error)
	ListDKIMKeys(ctx context.Context) ([]models.DKIMKey, error)
	UpdateDKIMKeyData(ctx context.Context, id uuid.UUID, keyData []byte) error
}

func (db *DB) CreateUser(ctx context.Context, user *models.User) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `INSERT INTO "user" (id, username, password_hash, is_active) VALUES ($1, $2, $3, $4)`, user.ID, user.Username, user.PasswordHash, user.IsActive)
	if err != nil {
		return err
	}

	// Create default mailboxes and associate with the user
	defaults := []string{"Inbox"}
	for _, name := range defaults {
		mbID := uuid.New()
		_, err = tx.ExecContext(ctx, "INSERT INTO mailbox (id, name) VALUES ($1, $2)", mbID, name)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO mailbox_user (mailbox_id, user_id) VALUES ($1, $2)", mbID, user.ID)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (db *DB) CreateMailbox(ctx context.Context, mb *models.Mailbox, userID uuid.UUID) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, "INSERT INTO mailbox (id, name) VALUES ($1, $2)", mb.ID, mb.Name)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO mailbox_user (mailbox_id, user_id) VALUES ($1, $2)", mb.ID, userID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) CreateAddressMapping(ctx context.Context, am *models.AddressMapping) error {
	_, err := db.ExecContext(ctx, "INSERT INTO address_mapping (id, address_pattern, mailbox_id, priority) VALUES ($1, $2, $3, $4)", am.ID, am.AddressPattern, am.MailboxID, am.Priority)
	return err
}

func (db *DB) ListUsers(ctx context.Context) ([]models.User, error) {
	var users []models.User
	err := db.SelectContext(ctx, &users, `SELECT id, username, password_hash, is_active FROM "user" ORDER BY username ASC`)
	return users, err
}

func (db *DB) ListMailboxes(ctx context.Context, userID uuid.UUID) ([]models.Mailbox, error) {
	var mailboxes []models.Mailbox
	err := db.SelectContext(ctx, &mailboxes, `
		SELECT m.id, m.name FROM mailbox m
		JOIN mailbox_user mu ON mu.mailbox_id = m.id
		WHERE mu.user_id = $1 AND mu.is_active = TRUE
		ORDER BY m.name ASC
	`, userID)
	return mailboxes, err
}

func (db *DB) DeleteUser(ctx context.Context, userID uuid.UUID) error {
	_, err := db.ExecContext(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
	return err
}

func (db *DB) DeleteMailbox(ctx context.Context, mailboxID uuid.UUID) error {
	_, err := db.ExecContext(ctx, "DELETE FROM mailbox WHERE id = $1", mailboxID)
	return err
}

func (db *DB) AddUserToMailbox(ctx context.Context, mailboxID, userID uuid.UUID) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO mailbox_user (mailbox_id, user_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, mailboxID, userID)
	return err
}

func (db *DB) AddSendingAddress(ctx context.Context, sa *models.SendingAddress) error {
	_, err := db.ExecContext(ctx, "INSERT INTO sending_address (id, user_id, mailbox_id, address, display_name, is_active) VALUES ($1, $2, $3, $4, $5, $6)", sa.ID, sa.UserID, sa.MailboxID, sa.Address, sa.DisplayName, sa.IsActive)
	return err
}

func (db *DB) ListSendingAddresses(ctx context.Context, userID uuid.UUID) ([]models.SendingAddress, error) {
	var addresses []models.SendingAddress
	err := db.SelectContext(ctx, &addresses, "SELECT id, user_id, mailbox_id, address, display_name, is_active FROM sending_address WHERE user_id = $1 ORDER BY address ASC", userID)
	return addresses, err
}

func (db *DB) DeactivateSendingAddress(ctx context.Context, saID uuid.UUID) error {
	_, err := db.ExecContext(ctx, "UPDATE sending_address SET is_active = FALSE WHERE id = $1", saID)
	return err
}

func (db *DB) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	var user models.User
	err := db.GetContext(ctx, &user, `SELECT id, username, password_hash, is_active FROM "user" WHERE username = $1`, username)
	return &user, err
}

func (db *DB) InsertDKIMKey(ctx context.Context, key *models.DKIMKey) error {
	_, err := db.ExecContext(ctx,
		"INSERT INTO dkim_key (id, domain, selector, key_data, is_active) VALUES ($1, $2, $3, $4, $5)",
		key.ID, key.Domain, key.Selector, key.KeyData, key.IsActive,
	)
	return err
}

func (db *DB) GetActiveDKIMKey(ctx context.Context, domain string, selector *string) (*models.DKIMKey, error) {
	var key models.DKIMKey
	var err error
	if selector != nil {
		err = db.GetContext(ctx, &key,
			"SELECT id, domain, selector, key_data, is_active FROM dkim_key WHERE domain = $1 AND selector = $2 AND is_active = TRUE",
			domain, *selector,
		)
	} else {
		err = db.GetContext(ctx, &key,
			"SELECT id, domain, selector, key_data, is_active FROM dkim_key WHERE domain = $1 AND is_active = TRUE ORDER BY create_datetime DESC LIMIT 1",
			domain,
		)
	}
	if err != nil {
		return nil, err
	}
	return &key, nil
}

func (db *DB) ListDKIMKeys(ctx context.Context) ([]models.DKIMKey, error) {
	var keys []models.DKIMKey
	err := db.SelectContext(ctx, &keys,
		"SELECT id, domain, selector, key_data, is_active FROM dkim_key ORDER BY domain ASC",
	)
	return keys, err
}

func (db *DB) UpdateDKIMKeyData(ctx context.Context, id uuid.UUID, keyData []byte) error {
	_, err := db.ExecContext(ctx,
		"UPDATE dkim_key SET key_data = $1, update_datetime = NOW() WHERE id = $2",
		keyData, id,
	)
	return err
}
