package db

import (
	"context"
	"testing"
	"time"

	"github.com/colormechadd/maileroo/pkg/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebDB(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Seed: two users, one mailbox owned by userID
	userID := uuid.New()
	otherUserID := uuid.New()
	_, err := db.ExecContext(ctx, `INSERT INTO "user" (id, username, password_hash) VALUES ($1, $2, $3)`, userID, "webuser", "hash")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO "user" (id, username, password_hash) VALUES ($1, $2, $3)`, otherUserID, "other", "hash2")
	require.NoError(t, err)

	mailboxID := uuid.New()
	_, err = db.ExecContext(ctx, `INSERT INTO mailbox (id, name) VALUES ($1, $2)`, mailboxID, "Inbox")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO mailbox_user (mailbox_id, user_id) VALUES ($1, $2)`, mailboxID, userID)
	require.NoError(t, err)

	threadID := uuid.New()
	_, err = db.ExecContext(ctx, `INSERT INTO thread (id, mailbox_id, subject) VALUES ($1, $2, $3)`, threadID, mailboxID, "Test Thread")
	require.NoError(t, err)

	// Sending address
	saID := uuid.New()
	_, err = db.ExecContext(ctx, `INSERT INTO sending_address (id, user_id, mailbox_id, address) VALUES ($1, $2, $3, $4)`,
		saID, userID, mailboxID, "me@example.com")
	require.NoError(t, err)

	// Seed emails covering all filter cases
	seedEmail := func(msgID, from, direction, status string, isRead, isStar bool, bodyPlain string) uuid.UUID {
		id := uuid.New()
		_, err := db.ExecContext(ctx, `
			INSERT INTO email (id, mailbox_id, thread_id, message_id, subject, from_address, to_address, storage_key, size, direction, status, is_read, is_star, body_plain)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::email_direction, $11::email_status, $12, $13, $14)
		`, id, mailboxID, threadID, msgID, "Subject: "+msgID, from, "to@example.com", "key/"+msgID+".eml", 1024,
			direction, status, isRead, isStar, bodyPlain)
		require.NoError(t, err)
		return id
	}

	inboxEmailID := seedEmail("msg-inbox", "alice@example.com", "INBOUND", "INBOX", false, false, "unique searchable body content for test")
	readEmailID := seedEmail("msg-read", "bob@example.com", "INBOUND", "INBOX", true, false, "")
	starredEmailID := seedEmail("msg-starred", "carol@example.com", "INBOUND", "INBOX", false, true, "")
	quarantinedEmailID := seedEmail("msg-quar", "spammer@bad.com", "INBOUND", "QUARANTINED", false, false, "")
	deletedEmailID := seedEmail("msg-del", "gone@example.com", "INBOUND", "DELETED", false, false, "")
	sentEmailID := seedEmail("msg-sent", "me@example.com", "OUTBOUND", "INBOX", true, false, "")

	// Attachment on inboxEmail
	attID := uuid.New()
	_, err = db.ExecContext(ctx, `INSERT INTO email_attachment (id, email_id, filename, content_type, size, storage_key) VALUES ($1, $2, $3, $4, $5, $6)`,
		attID, inboxEmailID, "report.pdf", "application/pdf", 512, "key/report.pdf")
	require.NoError(t, err)

	// Ingestion + step linked to inboxEmail
	ingestionID := uuid.New()
	_, err = db.ExecContext(ctx, `INSERT INTO ingestion (id, status) VALUES ($1, $2)`, ingestionID, "accepted")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE email SET ingestion_id = $1 WHERE id = $2`, ingestionID, inboxEmailID)
	require.NoError(t, err)
	stepID := uuid.New()
	_, err = db.ExecContext(ctx, `INSERT INTO ingestion_step (id, ingestion_id, step_name, status, details, duration_ms) VALUES ($1, $2, $3, $4, $5::jsonb, $6)`,
		stepID, ingestionID, "ValidateSPF", "PASS", `{"result":"pass"}`, 12)
	require.NoError(t, err)

	t.Run("GetUserByUsername", func(t *testing.T) {
		user, err := db.GetUserByUsername(ctx, "webuser")
		assert.NoError(t, err)
		assert.Equal(t, userID, user.ID)
		assert.Equal(t, "webuser", user.Username)

		_, err = db.GetUserByUsername(ctx, "nobody")
		assert.Error(t, err)
	})

	t.Run("GetUserByID", func(t *testing.T) {
		user, err := db.GetUserByID(ctx, userID)
		assert.NoError(t, err)
		assert.Equal(t, userID, user.ID)
		assert.Equal(t, "webuser", user.Username)

		_, err = db.GetUserByID(ctx, uuid.New())
		assert.Error(t, err)
	})

	t.Run("WebmailSession", func(t *testing.T) {
		token := "test-token-abc"
		remoteIP := "127.0.0.1"
		userAgent := "Mozilla/5.0"
		expires := time.Now().Add(1 * time.Hour).Round(time.Microsecond)

		err := db.CreateWebmailSession(ctx, userID, token, remoteIP, userAgent, expires)
		assert.NoError(t, err)

		session, err := db.GetWebmailSession(ctx, token)
		assert.NoError(t, err)
		assert.Equal(t, userID, session.UserID)
		assert.Equal(t, token, session.Token)
		assert.Equal(t, remoteIP, *session.RemoteIP)
		assert.Equal(t, userAgent, *session.UserAgent)
		assert.True(t, expires.Equal(session.ExpiresDatetime))

		err = db.ExpireWebmailSession(ctx, token)
		assert.NoError(t, err)

		session, err = db.GetWebmailSession(ctx, token)
		assert.NoError(t, err)
		assert.True(t, session.ExpiresDatetime.Before(time.Now().Add(time.Second)))
	})

	t.Run("GetMailboxesByUserID", func(t *testing.T) {
		boxes, err := db.GetMailboxesByUserID(ctx, userID)
		assert.NoError(t, err)
		assert.Len(t, boxes, 1)
		assert.Equal(t, mailboxID, boxes[0].ID)
		assert.Equal(t, "Inbox", boxes[0].Name)

		// other user has no mailboxes
		boxes, err = db.GetMailboxesByUserID(ctx, otherUserID)
		assert.NoError(t, err)
		assert.Empty(t, boxes)
	})

	t.Run("GetEmailsByMailboxID", func(t *testing.T) {
		t.Run("default/all returns INBOX emails", func(t *testing.T) {
			emails, err := db.GetEmailsByMailboxID(ctx, mailboxID, "all", 50, 0)
			assert.NoError(t, err)
			ids := emailIDs(emails)
			assert.Contains(t, ids, inboxEmailID)
			assert.Contains(t, ids, readEmailID)
			assert.Contains(t, ids, starredEmailID)
			assert.NotContains(t, ids, quarantinedEmailID)
			assert.NotContains(t, ids, deletedEmailID)
		})

		t.Run("unread", func(t *testing.T) {
			emails, err := db.GetEmailsByMailboxID(ctx, mailboxID, "unread", 50, 0)
			assert.NoError(t, err)
			ids := emailIDs(emails)
			assert.Contains(t, ids, inboxEmailID)
			assert.NotContains(t, ids, readEmailID)
		})

		t.Run("read", func(t *testing.T) {
			emails, err := db.GetEmailsByMailboxID(ctx, mailboxID, "read", 50, 0)
			assert.NoError(t, err)
			ids := emailIDs(emails)
			assert.Contains(t, ids, readEmailID)
			assert.NotContains(t, ids, inboxEmailID)
		})

		t.Run("starred", func(t *testing.T) {
			emails, err := db.GetEmailsByMailboxID(ctx, mailboxID, "starred", 50, 0)
			assert.NoError(t, err)
			ids := emailIDs(emails)
			assert.Contains(t, ids, starredEmailID)
			assert.NotContains(t, ids, inboxEmailID)
		})

		t.Run("quarantined", func(t *testing.T) {
			emails, err := db.GetEmailsByMailboxID(ctx, mailboxID, "quarantined", 50, 0)
			assert.NoError(t, err)
			ids := emailIDs(emails)
			assert.Contains(t, ids, quarantinedEmailID)
			assert.NotContains(t, ids, inboxEmailID)
		})

		t.Run("deleted", func(t *testing.T) {
			emails, err := db.GetEmailsByMailboxID(ctx, mailboxID, "deleted", 50, 0)
			assert.NoError(t, err)
			ids := emailIDs(emails)
			assert.Contains(t, ids, deletedEmailID)
			assert.NotContains(t, ids, inboxEmailID)
		})

		t.Run("sent", func(t *testing.T) {
			emails, err := db.GetEmailsByMailboxID(ctx, mailboxID, "sent", 50, 0)
			assert.NoError(t, err)
			ids := emailIDs(emails)
			assert.Contains(t, ids, sentEmailID)
			assert.NotContains(t, ids, inboxEmailID)
		})

		t.Run("limit and offset", func(t *testing.T) {
			emails, err := db.GetEmailsByMailboxID(ctx, mailboxID, "all", 1, 0)
			assert.NoError(t, err)
			assert.Len(t, emails, 1)

			emails2, err := db.GetEmailsByMailboxID(ctx, mailboxID, "all", 1, 1)
			assert.NoError(t, err)
			assert.Len(t, emails2, 1)
			if len(emails) > 0 && len(emails2) > 0 {
				assert.NotEqual(t, emails[0].ID, emails2[0].ID)
			}
		})
	})

	t.Run("GetEmailByID", func(t *testing.T) {
		email, err := db.GetEmailByID(ctx, inboxEmailID)
		assert.NoError(t, err)
		assert.Equal(t, inboxEmailID, email.ID)
		assert.Equal(t, "alice@example.com", email.FromAddress)

		_, err = db.GetEmailByID(ctx, uuid.New())
		assert.Error(t, err)
	})

	t.Run("GetEmailByIDForUser", func(t *testing.T) {
		email, err := db.GetEmailByIDForUser(ctx, inboxEmailID, userID)
		assert.NoError(t, err)
		assert.Equal(t, inboxEmailID, email.ID)

		// other user cannot access this mailbox
		_, err = db.GetEmailByIDForUser(ctx, inboxEmailID, otherUserID)
		assert.Error(t, err)
	})

	t.Run("GetAttachmentsByEmailID", func(t *testing.T) {
		atts, err := db.GetAttachmentsByEmailID(ctx, inboxEmailID)
		assert.NoError(t, err)
		assert.Len(t, atts, 1)
		assert.Equal(t, attID, atts[0].ID)
		assert.Equal(t, "report.pdf", atts[0].Filename)
		assert.Equal(t, "application/pdf", atts[0].ContentType)

		atts, err = db.GetAttachmentsByEmailID(ctx, readEmailID)
		assert.NoError(t, err)
		assert.Empty(t, atts)
	})

	t.Run("GetAttachmentByIDForUser", func(t *testing.T) {
		att, err := db.GetAttachmentByIDForUser(ctx, attID, userID)
		assert.NoError(t, err)
		assert.Equal(t, attID, att.ID)
		assert.Equal(t, inboxEmailID, att.EmailID)

		// other user cannot access
		_, err = db.GetAttachmentByIDForUser(ctx, attID, otherUserID)
		assert.Error(t, err)

		// nonexistent attachment
		_, err = db.GetAttachmentByIDForUser(ctx, uuid.New(), userID)
		assert.Error(t, err)
	})

	t.Run("GetIngestionStepsByEmailID", func(t *testing.T) {
		steps, err := db.GetIngestionStepsByEmailID(ctx, inboxEmailID, userID)
		assert.NoError(t, err)
		assert.Len(t, steps, 1)
		assert.Equal(t, stepID, steps[0].ID)
		assert.Equal(t, "ValidateSPF", steps[0].StepName)
		assert.Equal(t, "PASS", steps[0].Status)
		assert.Equal(t, 12, steps[0].DurationMS)

		// other user cannot access
		steps, err = db.GetIngestionStepsByEmailID(ctx, inboxEmailID, otherUserID)
		assert.NoError(t, err)
		assert.Empty(t, steps)
	})

	t.Run("MarkEmailRead", func(t *testing.T) {
		err := db.MarkEmailRead(ctx, inboxEmailID, userID, true)
		assert.NoError(t, err)

		var isRead bool
		err = db.GetContext(ctx, &isRead, "SELECT is_read FROM email WHERE id = $1", inboxEmailID)
		assert.NoError(t, err)
		assert.True(t, isRead)

		err = db.MarkEmailRead(ctx, inboxEmailID, userID, false)
		assert.NoError(t, err)

		err = db.GetContext(ctx, &isRead, "SELECT is_read FROM email WHERE id = $1", inboxEmailID)
		assert.NoError(t, err)
		assert.False(t, isRead)

		// other user cannot mark
		err = db.MarkEmailRead(ctx, inboxEmailID, otherUserID, true)
		assert.NoError(t, err) // no error, but no rows affected
		err = db.GetContext(ctx, &isRead, "SELECT is_read FROM email WHERE id = $1", inboxEmailID)
		assert.NoError(t, err)
		assert.False(t, isRead) // unchanged
	})

	t.Run("MarkEmailStarred", func(t *testing.T) {
		err := db.MarkEmailStarred(ctx, inboxEmailID, userID, true)
		assert.NoError(t, err)

		var isStar bool
		err = db.GetContext(ctx, &isStar, "SELECT is_star FROM email WHERE id = $1", inboxEmailID)
		assert.NoError(t, err)
		assert.True(t, isStar)

		err = db.MarkEmailStarred(ctx, inboxEmailID, userID, false)
		assert.NoError(t, err)

		err = db.GetContext(ctx, &isStar, "SELECT is_star FROM email WHERE id = $1", inboxEmailID)
		assert.NoError(t, err)
		assert.False(t, isStar)
	})

	t.Run("UpdateEmailStatus", func(t *testing.T) {
		err := db.UpdateEmailStatus(ctx, inboxEmailID, userID, models.StatusDeleted)
		assert.NoError(t, err)

		var status string
		err = db.GetContext(ctx, &status, "SELECT status FROM email WHERE id = $1", inboxEmailID)
		assert.NoError(t, err)
		assert.Equal(t, "DELETED", status)

		// Restore
		err = db.UpdateEmailStatus(ctx, inboxEmailID, userID, models.StatusInbox)
		assert.NoError(t, err)

		// Other user cannot update
		err = db.UpdateEmailStatus(ctx, inboxEmailID, otherUserID, models.StatusDeleted)
		assert.NoError(t, err) // no error, but no rows affected
		err = db.GetContext(ctx, &status, "SELECT status FROM email WHERE id = $1", inboxEmailID)
		assert.NoError(t, err)
		assert.Equal(t, "INBOX", status) // unchanged
	})

	t.Run("GetActiveSendingAddresses", func(t *testing.T) {
		addrs, err := db.GetActiveSendingAddresses(ctx, userID)
		assert.NoError(t, err)
		assert.Len(t, addrs, 1)
		assert.Equal(t, saID, addrs[0].ID)
		assert.Equal(t, "me@example.com", addrs[0].Address)

		addrs, err = db.GetActiveSendingAddresses(ctx, otherUserID)
		assert.NoError(t, err)
		assert.Empty(t, addrs)

		// Deactivated address is not returned
		_, _ = db.ExecContext(ctx, `UPDATE sending_address SET is_active = FALSE WHERE id = $1`, saID)
		addrs, err = db.GetActiveSendingAddresses(ctx, userID)
		assert.NoError(t, err)
		assert.Empty(t, addrs)
		_, _ = db.ExecContext(ctx, `UPDATE sending_address SET is_active = TRUE WHERE id = $1`, saID)
	})

	t.Run("IsAuthorizedSendingAddress", func(t *testing.T) {
		ok, err := db.IsAuthorizedSendingAddress(ctx, userID, "me@example.com")
		assert.NoError(t, err)
		assert.True(t, ok)

		ok, err = db.IsAuthorizedSendingAddress(ctx, userID, "notmine@example.com")
		assert.NoError(t, err)
		assert.False(t, ok)

		ok, err = db.IsAuthorizedSendingAddress(ctx, otherUserID, "me@example.com")
		assert.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("GetSendingAddressByID", func(t *testing.T) {
		sa, err := db.GetSendingAddressByID(ctx, saID, userID)
		assert.NoError(t, err)
		assert.Equal(t, saID, sa.ID)
		assert.Equal(t, "me@example.com", sa.Address)

		// Wrong user
		_, err = db.GetSendingAddressByID(ctx, saID, otherUserID)
		assert.Error(t, err)

		// Nonexistent
		_, err = db.GetSendingAddressByID(ctx, uuid.New(), userID)
		assert.Error(t, err)
	})

	t.Run("InsertOutboundJob", func(t *testing.T) {
		job, err := db.InsertOutboundJob(ctx, &inboxEmailID, "me@example.com", []string{"them@example.com"}, []byte("raw message"))
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, job.ID)
		assert.Equal(t, models.OutboundQueued, job.Status)
		assert.Equal(t, "me@example.com", job.FromAddress)
		assert.Equal(t, []string{"them@example.com"}, job.Recipients)
		assert.Equal(t, inboxEmailID, *job.EmailID)
	})

	t.Run("Drafts", func(t *testing.T) {
		draft := models.Draft{
			MailboxID: mailboxID,
			UserID:    userID,
			ToAddress: "recipient@example.com",
			Subject:   "Hello there",
			Body:      "Draft body text",
		}

		created, err := db.CreateDraft(ctx, draft)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, created.ID)
		assert.Equal(t, "Hello there", created.Subject)
		assert.Equal(t, userID, created.UserID)

		draftID := created.ID

		// GetDraftByIDForUser
		fetched, err := db.GetDraftByIDForUser(ctx, draftID, userID)
		assert.NoError(t, err)
		assert.Equal(t, draftID, fetched.ID)
		assert.Equal(t, "recipient@example.com", fetched.ToAddress)

		// Other user cannot fetch
		_, err = db.GetDraftByIDForUser(ctx, draftID, otherUserID)
		assert.Error(t, err)

		// UpdateDraft
		fetched.Subject = "Updated subject"
		fetched.Body = "Updated body"
		err = db.UpdateDraft(ctx, *fetched)
		assert.NoError(t, err)

		updated, err := db.GetDraftByIDForUser(ctx, draftID, userID)
		assert.NoError(t, err)
		assert.Equal(t, "Updated subject", updated.Subject)
		assert.Equal(t, "Updated body", updated.Body)

		// CountDraftsByMailboxID
		count, err := db.CountDraftsByMailboxID(ctx, mailboxID, userID)
		assert.NoError(t, err)
		assert.Equal(t, 1, count)

		// GetDraftsByMailboxID
		drafts, err := db.GetDraftsByMailboxID(ctx, mailboxID, userID)
		assert.NoError(t, err)
		assert.Len(t, drafts, 1)
		assert.Equal(t, draftID, drafts[0].ID)

		// Other user sees nothing
		drafts, err = db.GetDraftsByMailboxID(ctx, mailboxID, otherUserID)
		assert.NoError(t, err)
		assert.Empty(t, drafts)

		// DeleteDraft
		err = db.DeleteDraft(ctx, draftID, userID)
		assert.NoError(t, err)

		_, err = db.GetDraftByIDForUser(ctx, draftID, userID)
		assert.Error(t, err)

		count, err = db.CountDraftsByMailboxID(ctx, mailboxID, userID)
		assert.NoError(t, err)
		assert.Equal(t, 0, count)
	})

	t.Run("SearchEmailsByMailboxID", func(t *testing.T) {
		// inboxEmailID has body_plain "unique searchable body content for test"
		results, err := db.SearchEmailsByMailboxID(ctx, mailboxID, userID, "searchable", 50, 0)
		assert.NoError(t, err)
		assert.NotEmpty(t, results)
		assert.Equal(t, inboxEmailID, results[0].ID)

		// Search by sender
		results, err = db.SearchEmailsByMailboxID(ctx, mailboxID, userID, "alice", 50, 0)
		assert.NoError(t, err)
		assert.NotEmpty(t, results)
		assert.Equal(t, inboxEmailID, results[0].ID)

		// Query that matches nothing
		results, err = db.SearchEmailsByMailboxID(ctx, mailboxID, userID, "xyzzynotaword", 50, 0)
		assert.NoError(t, err)
		assert.Empty(t, results)

		// Other user gets nothing from this mailbox
		results, err = db.SearchEmailsByMailboxID(ctx, mailboxID, otherUserID, "searchable", 50, 0)
		assert.NoError(t, err)
		assert.Empty(t, results)

		// Deleted emails are excluded
		results, err = db.SearchEmailsByMailboxID(ctx, mailboxID, userID, "gone", 50, 0)
		assert.NoError(t, err)
		assert.Empty(t, results)
	})
}

// emailIDs extracts IDs from a slice of emails for assertion convenience.
func emailIDs(emails []models.Email) []uuid.UUID {
	ids := make([]uuid.UUID, len(emails))
	for i, e := range emails {
		ids[i] = e.ID
	}
	return ids
}
