package pipeline

import (
	"context"
	"testing"

	"github.com/colormechadd/maileroo/internal/config"
	"github.com/colormechadd/maileroo/internal/mail"
	"github.com/colormechadd/maileroo/pkg/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestDeliver(t *testing.T) {
	ctx := context.Background()
	mockDB := new(MockDB)
	mockStorage := new(MockStorage)
	cfg := &config.Config{}
	cfg.Compression = "none"

	mailSvc := mail.NewService(mockDB, mockStorage, "none", nil)
	p := &Pipeline{cfg: cfg, db: mockDB, storage: mockStorage, mail: mailSvc}

	mailboxID := uuid.New()
	ingestionID := uuid.New()
	rawMsg := []byte("Message-ID: <test@msg.id>\nIn-Reply-To: <parent@msg.id>\nSubject: Test\n\nHello World")

	ictx := &IngestionContext{
		ID:               ingestionID,
		TargetMailboxID:  mailboxID,
		FromAddress:      "sender@test.com",
		ToAddresses:      []string{"rcpt@test.com"},
		RawMessage:       rawMsg,
		AddressMappingID: uuid.New(),
	}

	t.Run("successful delivery", func(t *testing.T) {
		// Mock storage
		mockStorage.On("Save", mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

		// Mock DB threading lookup
		mockDB.On("FindThreadIDByMessageIDs", mock.Anything, mailboxID, mock.AnythingOfType("[]string")).Return(uuid.Nil, nil).Once()

		// Mock DB thread creation
		mockDB.On("CreateThread", mock.Anything, mock.Anything).Return(nil).Once()

		// Mock DB email creation
		mockDB.On("CreateEmail", mock.Anything, mock.MatchedBy(func(e *models.Email) bool {
			return e.Status == models.StatusQuarantined && e.Direction == models.DirectionInbound
		})).Return(nil).Once()

		status, _, err := Deliver(ctx, p, ictx)
		assert.NoError(t, err)
		assert.Equal(t, StatusPass, status)
		assert.NotEmpty(t, ictx.StorageKey)
		assert.NotEqual(t, uuid.Nil, ictx.EmailID)

		mockDB.AssertExpectations(t)
		mockStorage.AssertExpectations(t)
	})
}
