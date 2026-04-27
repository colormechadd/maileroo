package pipeline

import (
	"context"
	"io"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

type MockDB struct {
	mock.Mock
}

func (m *MockDB) CreateIngestion(ctx context.Context, ingestion *models.Ingestion) error {
	args := m.Called(ctx, ingestion)
	return args.Error(0)
}

func (m *MockDB) CreateIngestionStep(ctx context.Context, step *models.IngestionStep) error {
	args := m.Called(ctx, step)
	return args.Error(0)
}

func (m *MockDB) UpdateIngestionStatus(ctx context.Context, id uuid.UUID, status string) error {
	args := m.Called(ctx, id, status)
	return args.Error(0)
}

func (m *MockDB) IsBlockedByMailboxRules(ctx context.Context, mailboxID uuid.UUID, fromAddress string) (bool, error) {
	args := m.Called(ctx, mailboxID, fromAddress)
	return args.Bool(0), args.Error(1)
}

func (m *MockDB) CreateEmail(ctx context.Context, email *models.Email) error {
	args := m.Called(ctx, email)
	return args.Error(0)
}

func (m *MockDB) SetEmailStatus(ctx context.Context, id uuid.UUID, status models.EmailStatus) error {
	args := m.Called(ctx, id, status)
	return args.Error(0)
}

func (m *MockDB) CreateAttachment(ctx context.Context, attachment *models.EmailAttachment) error {
	args := m.Called(ctx, attachment)
	return args.Error(0)
}

func (m *MockDB) CreateThread(ctx context.Context, thread *models.Thread) error {
	args := m.Called(ctx, thread)
	return args.Error(0)
}

func (m *MockDB) FindThreadIDByMessageIDs(ctx context.Context, mailboxID uuid.UUID, messageIDs []string) (uuid.UUID, error) {
	args := m.Called(ctx, mailboxID, messageIDs)
	return args.Get(0).(uuid.UUID), args.Error(1)
}

func (m *MockDB) UpdateOutboundJobFailed(ctx context.Context, id uuid.UUID, lastError string) error {
	args := m.Called(ctx, id, lastError)
	return args.Error(0)
}

func (m *MockDB) GetMailboxUserIDs(ctx context.Context, mailboxID uuid.UUID) ([]uuid.UUID, error) {
	args := m.Called(ctx, mailboxID)
	return args.Get(0).([]uuid.UUID), args.Error(1)
}

func (m *MockDB) GetActiveFilterRulesForMailbox(ctx context.Context, mailboxID uuid.UUID) ([]*models.FilterRule, error) {
	args := m.Called(ctx, mailboxID)
	return args.Get(0).([]*models.FilterRule), args.Error(1)
}

func (m *MockDB) SetEmailFields(ctx context.Context, id uuid.UUID, isRead, isStar bool, status models.EmailStatus) error {
	args := m.Called(ctx, id, isRead, isStar, status)
	return args.Error(0)
}

type MockHub struct {
	mock.Mock
}

func (m *MockHub) Broadcast(event Event) {
	m.Called(event)
}

type MockStorage struct {
	mock.Mock
}

func (m *MockStorage) Save(ctx context.Context, key string, reader io.Reader) error {
	args := m.Called(ctx, key, reader)
	return args.Error(0)
}

func (m *MockStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	args := m.Called(ctx, key)
	return args.Get(0).(io.ReadCloser), args.Error(1)
}

func (m *MockStorage) Delete(ctx context.Context, key string) error {
	args := m.Called(ctx, key)
	return args.Error(0)
}

func (m *MockStorage) Exists(ctx context.Context, key string) (bool, error) {
	args := m.Called(ctx, key)
	return args.Bool(0), args.Error(1)
}
