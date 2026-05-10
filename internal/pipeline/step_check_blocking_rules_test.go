package pipeline

import (
	"context"
	"testing"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestCheckBlockingRules(t *testing.T) {
	ctx := context.Background()
	mockDB := new(MockDB)
	p := &Pipeline{db: mockDB}

	mailboxID := uuid.New()
	ictx := &IngestionContext{
		TargetMailboxID: mailboxID,
		FromAddress:     "spam@bad.com",
	}

	t.Run("blocked", func(t *testing.T) {
		rule := &models.MailboxBlockRule{ID: uuid.New(), MailboxID: mailboxID, AddressPattern: `.*@bad.com`}
		mockDB.On("IsBlockedByMailboxRules", mock.Anything, mailboxID, "spam@bad.com").Return(rule, nil).Once()
		status, details, err := CheckBlockingRules(ctx, p, ictx)
		assert.NoError(t, err)
		assert.Equal(t, StatusFail, status)
		d := details.(map[string]any)
		assert.Equal(t, true, d["blocked"])
		assert.Equal(t, rule.ID, d["rule_id"])
		assert.Equal(t, rule.AddressPattern, d["address_pattern"])
	})

	t.Run("not blocked", func(t *testing.T) {
		mockDB.On("IsBlockedByMailboxRules", mock.Anything, mailboxID, "spam@bad.com").Return(nil, nil).Once()
		status, _, err := CheckBlockingRules(ctx, p, ictx)
		assert.NoError(t, err)
		assert.Equal(t, StatusPass, status)
	})
}
