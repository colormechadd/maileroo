package pipeline

import (
	"context"
	"testing"

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
		mockDB.On("IsBlockedByMailboxRules", mock.Anything, mailboxID, "spam@bad.com").Return(true, nil).Once()
		status, _, err := CheckBlockingRules(ctx, p, ictx)
		assert.NoError(t, err)
		assert.Equal(t, StatusFail, status)
	})

	t.Run("not blocked", func(t *testing.T) {
		mockDB.On("IsBlockedByMailboxRules", mock.Anything, mailboxID, "spam@bad.com").Return(false, nil).Once()
		status, _, err := CheckBlockingRules(ctx, p, ictx)
		assert.NoError(t, err)
		assert.Equal(t, StatusPass, status)
	})
}
