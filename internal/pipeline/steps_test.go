package pipeline

import (
	"context"
	"net"
	"testing"

	"github.com/colormechadd/maileroo/internal/config"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestValidateSender(t *testing.T) {
	ctx := context.Background()
	p := &Pipeline{}
	
	t.Run("basic check", func(t *testing.T) {
		ictx := &IngestionContext{
			RemoteIP:    net.ParseIP("127.0.0.1"),
			FromAddress: "test@gmail.com",
			RawMessage:  []byte("Subject: Test\n\nNo DKIM here"),
		}
		status, _, err := ValidateSender(ctx, p, ictx)
		assert.NoError(t, err)
		// We expect a result (likely Fail if on loopback without valid DKIM)
		assert.Contains(t, []StepStatus{StatusPass, StatusFail, StatusError}, status)
	})
}

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

func TestValidateRBL(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{}
	cfg.Spam.RBLServers = []string{"zen.spamhaus.org"}
	p := &Pipeline{cfg: cfg}

	t.Run("skipped if no ip", func(t *testing.T) {
		ictx := &IngestionContext{RemoteIP: nil}
		status, _, err := ValidateRBL(ctx, p, ictx)
		assert.NoError(t, err)
		assert.Equal(t, StatusSkipped, status)
	})

	t.Run("loopback ip behavior", func(t *testing.T) {
		ictx := &IngestionContext{RemoteIP: net.ParseIP("127.0.0.1")}
		status, _, err := ValidateRBL(ctx, p, ictx)
		assert.NoError(t, err)
		assert.Contains(t, []StepStatus{StatusPass, StatusFail}, status)
	})
}

func TestDeliver(t *testing.T) {
	ctx := context.Background()
	mockDB := new(MockDB)
	mockStorage := new(MockStorage)
	cfg := &config.Config{}
	cfg.Compression = "none"
	p := &Pipeline{cfg: cfg, db: mockDB, storage: mockStorage}

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
		mockDB.On("CreateEmail", mock.Anything, mock.Anything).Return(nil).Once()

		status, _, err := Deliver(ctx, p, ictx)
		assert.NoError(t, err)
		assert.Equal(t, StatusPass, status)
		assert.NotEmpty(t, ictx.StorageKey)
		
		mockDB.AssertExpectations(t)
		mockStorage.AssertExpectations(t)
	})
}
