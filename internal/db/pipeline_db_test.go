package db

import (
	"context"
	"testing"
	"time"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestPipelineDB(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Seed user and mailbox
	userID := uuid.New()
	db.ExecContext(ctx, `INSERT INTO "user" (id, username, password_hash) VALUES ($1, $2, $3)`, userID, "puser", "hash")
	mailboxID := uuid.New()
	db.ExecContext(ctx, `INSERT INTO mailbox (id, name) VALUES ($1, $2)`, mailboxID, "PBox")
	db.ExecContext(ctx, `INSERT INTO mailbox_user (mailbox_id, user_id) VALUES ($1, $2)`, mailboxID, userID)

	t.Run("CreateIngestion and UpdateStatus", func(t *testing.T) {
		ingestionID := uuid.New()
		from := "sender@test.com"
		ing := &models.Ingestion{
			ID:          ingestionID,
			FromAddress: &from,
			Status:      "processing",
		}
		err := db.CreateIngestion(ctx, ing)
		assert.NoError(t, err)

		err = db.UpdateIngestionStatus(ctx, ingestionID, "accepted")
		assert.NoError(t, err)

		var status string
		err = db.GetContext(ctx, &status, "SELECT status FROM ingestion WHERE id = $1", ingestionID)
		assert.NoError(t, err)
		assert.Equal(t, "accepted", status)
	})

	t.Run("CreateEmail and Threading", func(t *testing.T) {
		threadID := uuid.New()
		err := db.CreateThread(ctx, &models.Thread{ID: threadID, MailboxID: mailboxID, Subject: "Thread 1"})
		assert.NoError(t, err)

		msgID := "msg-123"
		email := &models.Email{
			ID:              uuid.New(),
			MailboxID:       mailboxID,
			ThreadID:        &threadID,
			MessageID:       msgID,
			FromAddress:     "a@b.com",
			ToAddress:       "c@d.com",
			StorageKey:      "key",
			Size:            100,
			ReceiveDatetime: time.Now(),
			Direction:       models.DirectionInbound,
			Status:          models.StatusInbox,
		}
		err = db.CreateEmail(ctx, email)
		assert.NoError(t, err)

		// Test FindThreadIDByMessageIDs
		foundThreadID, err := db.FindThreadIDByMessageIDs(ctx, mailboxID, []string{msgID, "other"})
		assert.NoError(t, err)
		assert.Equal(t, threadID, foundThreadID)
	})

	t.Run("IsBlockedByMailboxRules", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `INSERT INTO mailbox_block_rule (mailbox_id, address_pattern) VALUES ($1, $2)`, mailboxID, `.*@spam.com`)
		assert.NoError(t, err)

		blocked, err := db.IsBlockedByMailboxRules(ctx, mailboxID, "bad@spam.com")
		assert.NoError(t, err)
		assert.True(t, blocked)

		blocked, err = db.IsBlockedByMailboxRules(ctx, mailboxID, "good@friend.com")
		assert.NoError(t, err)
		assert.False(t, blocked)
	})
}
