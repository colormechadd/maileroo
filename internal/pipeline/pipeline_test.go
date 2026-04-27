package pipeline

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/colormechadd/mailaroo/internal/config"
	"github.com/colormechadd/mailaroo/internal/mail"
	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func TestProcess_Success(t *testing.T) {
	ctx := context.Background()
	mockDB := new(MockDB)
	mockStorage := new(MockStorage)
	mockHub := new(MockHub)
	cfg := &config.Config{}
	cfg.Compression = "none"

	mailSvc := mail.NewService(mockDB, mockStorage, "none", nil)
	p := NewPipeline(cfg, mockDB, mockStorage, mockHub, mailSvc, nil)

	// Override steps for controlled test
	p.steps = []struct {
		name string
		fn   Step
	}{
		{"deliver", Deliver},
		{"finalize", Finalize},
		{"notify", Notify},
	}

	mailboxID := uuid.New()
	ictx := &IngestionContext{
		ID:               uuid.New(),
		RemoteIP:         net.ParseIP("127.0.0.1"),
		FromAddress:      "sender@test.com",
		ToAddresses:      []string{"rcpt@test.com"},
		RawMessage:       []byte("Subject: Test\nIn-Reply-To: <parent@test.com>\n\nHello"),
		TargetMailboxID:  mailboxID,
		AddressMappingID: uuid.New(),
	}

	userID := uuid.New()
	mockDB.On("CreateIngestion", mock.Anything, mock.Anything).Return(nil).Once()
	mockStorage.On("Save", mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	mockDB.On("FindThreadIDByMessageIDs", mock.Anything, mock.Anything, mock.Anything).Return(uuid.Nil, nil).Once()
	mockDB.On("CreateThread", mock.Anything, mock.Anything).Return(nil).Once()
	mockDB.On("CreateEmail", mock.Anything, mock.MatchedBy(func(e *models.Email) bool {
		return e.Status == models.StatusQuarantined && e.Direction == models.DirectionInbound
	})).Return(nil).Once()
	mockDB.On("CreateIngestionStep", mock.Anything, mock.MatchedBy(func(s *models.IngestionStep) bool {
		return s.StepName == "deliver" && s.Status == "pass"
	})).Return(nil).Once()

	mockDB.On("SetEmailFields", mock.Anything, mock.Anything, false, false, models.StatusInbox).Return(nil).Once()
	mockDB.On("CreateIngestionStep", mock.Anything, mock.MatchedBy(func(s *models.IngestionStep) bool {
		return s.StepName == "finalize" && s.Status == "pass"
	})).Return(nil).Once()

	mockDB.On("GetMailboxUserIDs", mock.Anything, mailboxID).Return([]uuid.UUID{userID}, nil).Once()
	mockHub.On("Broadcast", mock.Anything).Return().Once()
	mockDB.On("CreateIngestionStep", mock.Anything, mock.MatchedBy(func(s *models.IngestionStep) bool {
		return s.StepName == "notify"
	})).Return(nil).Once()

	mockDB.On("UpdateIngestionStatus", mock.Anything, ictx.ID, "accepted").Return(nil).Once()

	err := p.Process(ctx, ictx)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	mockDB.AssertExpectations(t)
	mockStorage.AssertExpectations(t)
	mockHub.AssertExpectations(t)
}

func TestProcess_FailureLeavesQuarantined(t *testing.T) {
	ctx := context.Background()
	mockDB := new(MockDB)
	mockStorage := new(MockStorage)
	mockHub := new(MockHub)
	cfg := &config.Config{}
	cfg.Compression = "none"

	mailSvc := mail.NewService(mockDB, mockStorage, "none", nil)
	p := NewPipeline(cfg, mockDB, mockStorage, mockHub, mailSvc, nil)

	failStep := func(ctx context.Context, p *Pipeline, ictx *IngestionContext) (StepStatus, any, error) {
		return StatusFail, nil, errors.New("validation failed")
	}

	p.steps = []struct {
		name string
		fn   Step
	}{
		{"deliver", Deliver},
		{"fail", failStep},
		{"finalize", Finalize},
	}

	ictx := &IngestionContext{
		ID:               uuid.New(),
		RemoteIP:         net.ParseIP("127.0.0.1"),
		FromAddress:      "sender@test.com",
		ToAddresses:      []string{"rcpt@test.com"},
		RawMessage:       []byte("Subject: Test\nIn-Reply-To: <parent@test.com>\n\nHello"),
		TargetMailboxID:  uuid.New(),
		AddressMappingID: uuid.New(),
	}

	mockDB.On("CreateIngestion", mock.Anything, mock.Anything).Return(nil).Once()
	mockStorage.On("Save", mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	mockDB.On("FindThreadIDByMessageIDs", mock.Anything, mock.Anything, mock.Anything).Return(uuid.Nil, nil).Once()
	mockDB.On("CreateThread", mock.Anything, mock.Anything).Return(nil).Once()
	mockDB.On("CreateEmail", mock.Anything, mock.MatchedBy(func(e *models.Email) bool {
		return e.Status == models.StatusQuarantined && e.Direction == models.DirectionInbound
	})).Return(nil).Once()
	mockDB.On("CreateIngestionStep", mock.Anything, mock.MatchedBy(func(s *models.IngestionStep) bool {
		return s.StepName == "deliver"
	})).Return(nil).Once()

	mockDB.On("CreateIngestionStep", mock.Anything, mock.MatchedBy(func(s *models.IngestionStep) bool {
		return s.StepName == "fail"
	})).Return(nil).Once()

	mockDB.On("UpdateIngestionStatus", mock.Anything, ictx.ID, "rejected").Return(nil).Once()

	// UpdateEmailQuarantineStatus should NOT be called
	// We don't need to explicitly mock it if we use AssertExpectations

	err := p.Process(ctx, ictx)
	if err != nil {
		t.Logf("Process returned expected error/rejection: %v", err)
	}

	mockDB.AssertExpectations(t)
	// Verify UpdateEmailQuarantineStatus was NOT called
	for _, call := range mockDB.Calls {
		if call.Method == "UpdateEmailQuarantineStatus" {
			t.Errorf("UpdateEmailQuarantineStatus was called but should not have been")
		}
	}
}
