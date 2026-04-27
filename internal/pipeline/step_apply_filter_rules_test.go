package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func strPtr(s string) *string { return &s }

func activeRule(action string, conditions ...models.FilterCondition) *models.FilterRule {
	return &models.FilterRule{
		ID:             uuid.New(),
		Name:           "test rule",
		IsActive:       true,
		MatchAll:       true,
		Action:         action,
		StopProcessing: true,
		Conditions:     conditions,
	}
}

func filterCond(field, operator string, value *string) models.FilterCondition {
	return models.FilterCondition{
		ID:       uuid.New(),
		Field:    field,
		Operator: operator,
		Value:    value,
	}
}

// minimal RFC 5322 message for use as RawMessage
var simpleRawMessage = []byte("From: sender@example.com\r\nTo: inbox@example.com\r\nSubject: Hello world\r\nContent-Type: text/plain\r\n\r\nbody text")

func TestApplyFilterRules_NoRules(t *testing.T) {
	ctx := context.Background()
	mockDB := new(MockDB)
	p := &Pipeline{db: mockDB}
	mailboxID := uuid.New()

	mockDB.On("GetActiveFilterRulesForMailbox", mock.Anything, mailboxID).
		Return([]*models.FilterRule{}, nil).Once()

	ictx := &IngestionContext{TargetMailboxID: mailboxID, RawMessage: simpleRawMessage}
	status, result, err := ApplyFilterRules(ctx, p, ictx)

	assert.NoError(t, err)
	assert.Equal(t, StatusPass, status)
	assert.Equal(t, map[string]any{"matched": false}, result)
	assert.Empty(t, ictx.FilterAction)
	assert.Nil(t, ictx.MatchedFilterRuleID)
	mockDB.AssertExpectations(t)
}

func TestApplyFilterRules_DBError(t *testing.T) {
	ctx := context.Background()
	mockDB := new(MockDB)
	p := &Pipeline{db: mockDB}
	mailboxID := uuid.New()

	mockDB.On("GetActiveFilterRulesForMailbox", mock.Anything, mailboxID).
		Return([]*models.FilterRule{}, errors.New("db failure")).Once()

	ictx := &IngestionContext{TargetMailboxID: mailboxID}
	status, _, err := ApplyFilterRules(ctx, p, ictx)

	assert.Error(t, err)
	assert.Equal(t, StatusError, status)
	mockDB.AssertExpectations(t)
}

func TestApplyFilterRules_NoMatch(t *testing.T) {
	ctx := context.Background()
	mockDB := new(MockDB)
	p := &Pipeline{db: mockDB}
	mailboxID := uuid.New()

	rule := activeRule(models.FilterActionArchive,
		filterCond("subject", "contains", strPtr("newsletter")),
	)
	mockDB.On("GetActiveFilterRulesForMailbox", mock.Anything, mailboxID).
		Return([]*models.FilterRule{rule}, nil).Once()

	ictx := &IngestionContext{
		TargetMailboxID: mailboxID,
		FromAddress:     "sender@example.com",
		RawMessage:      simpleRawMessage, // subject: "Hello world", no "newsletter"
	}
	status, result, err := ApplyFilterRules(ctx, p, ictx)

	assert.NoError(t, err)
	assert.Equal(t, StatusPass, status)
	assert.Equal(t, map[string]any{"matched": false}, result)
	assert.Empty(t, ictx.FilterAction)
	assert.Nil(t, ictx.MatchedFilterRuleID)
	mockDB.AssertExpectations(t)
}

func TestApplyFilterRules_Match(t *testing.T) {
	ctx := context.Background()
	mockDB := new(MockDB)
	p := &Pipeline{db: mockDB}
	mailboxID := uuid.New()

	rule := activeRule(models.FilterActionArchive,
		filterCond("subject", "contains", strPtr("Hello")),
	)
	mockDB.On("GetActiveFilterRulesForMailbox", mock.Anything, mailboxID).
		Return([]*models.FilterRule{rule}, nil).Once()

	ictx := &IngestionContext{
		TargetMailboxID: mailboxID,
		FromAddress:     "sender@example.com",
		RawMessage:      simpleRawMessage, // subject: "Hello world"
	}
	status, result, err := ApplyFilterRules(ctx, p, ictx)

	assert.NoError(t, err)
	assert.Equal(t, StatusPass, status)
	assert.Equal(t, models.FilterActionArchive, ictx.FilterAction)
	assert.Equal(t, rule.ID, *ictx.MatchedFilterRuleID)

	m := result.(map[string]any)
	assert.Equal(t, true, m["matched"])
	assert.Equal(t, rule.ID, m["rule_id"])
	assert.Equal(t, models.FilterActionArchive, m["action"])
	mockDB.AssertExpectations(t)
}

func TestApplyFilterRules_MatchFromAddress(t *testing.T) {
	ctx := context.Background()
	mockDB := new(MockDB)
	p := &Pipeline{db: mockDB}
	mailboxID := uuid.New()

	rule := activeRule(models.FilterActionDelete,
		filterCond("from", "contains", strPtr("spam")),
	)
	mockDB.On("GetActiveFilterRulesForMailbox", mock.Anything, mailboxID).
		Return([]*models.FilterRule{rule}, nil).Once()

	ictx := &IngestionContext{
		TargetMailboxID: mailboxID,
		FromAddress:     "spam@bad.com",
		RawMessage:      simpleRawMessage,
	}
	status, _, err := ApplyFilterRules(ctx, p, ictx)

	assert.NoError(t, err)
	assert.Equal(t, StatusPass, status)
	assert.Equal(t, models.FilterActionDelete, ictx.FilterAction)
	mockDB.AssertExpectations(t)
}
