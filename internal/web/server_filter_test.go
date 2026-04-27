package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func formRequest(vals url.Values) *http.Request {
	r, _ := http.NewRequest("POST", "/", strings.NewReader(vals.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func TestParseFilterRuleForm_Defaults(t *testing.T) {
	mailboxID := uuid.New()
	r := formRequest(url.Values{
		"action":    {"archive"},
		"is_active": {"on"},
	})
	rule, err := parseFilterRuleForm(r, mailboxID)
	require.NoError(t, err)

	assert.Equal(t, mailboxID, rule.MailboxID)
	assert.Equal(t, "Filter rule", rule.Name)
	assert.Equal(t, models.FilterActionArchive, rule.Action)
	assert.True(t, rule.IsActive)
	assert.True(t, rule.MatchAll)
	assert.True(t, rule.StopProcessing)
	assert.Empty(t, rule.Conditions)
}

func TestParseFilterRuleForm_Name(t *testing.T) {
	mailboxID := uuid.New()
	r := formRequest(url.Values{
		"name":   {"  Archive newsletters  "},
		"action": {"archive"},
	})
	rule, err := parseFilterRuleForm(r, mailboxID)
	require.NoError(t, err)
	assert.Equal(t, "Archive newsletters", rule.Name)
}

func TestParseFilterRuleForm_BoolFlags(t *testing.T) {
	mailboxID := uuid.New()
	r := formRequest(url.Values{
		"action":          {"delete"},
		"match_all":       {"false"},
		"stop_processing": {"false"},
	})
	rule, err := parseFilterRuleForm(r, mailboxID)
	require.NoError(t, err)

	assert.False(t, rule.IsActive)
	assert.False(t, rule.MatchAll)
	assert.False(t, rule.StopProcessing)
}

func TestParseFilterRuleForm_Conditions(t *testing.T) {
	mailboxID := uuid.New()
	r := formRequest(url.Values{
		"action":             {"star"},
		"is_active":          {"on"},
		"condition_field":    {"from", "subject"},
		"condition_operator": {"contains", "is"},
		"condition_value":    {"boss@company.com", "urgent"},
	})
	rule, err := parseFilterRuleForm(r, mailboxID)
	require.NoError(t, err)

	require.Len(t, rule.Conditions, 2)
	assert.Equal(t, "from", rule.Conditions[0].Field)
	assert.Equal(t, "contains", rule.Conditions[0].Operator)
	require.NotNil(t, rule.Conditions[0].Value)
	assert.Equal(t, "boss@company.com", *rule.Conditions[0].Value)

	assert.Equal(t, "subject", rule.Conditions[1].Field)
	assert.Equal(t, "is", rule.Conditions[1].Operator)
	require.NotNil(t, rule.Conditions[1].Value)
	assert.Equal(t, "urgent", *rule.Conditions[1].Value)
}

func TestParseFilterRuleForm_EmptyConditionValue(t *testing.T) {
	mailboxID := uuid.New()
	r := formRequest(url.Values{
		"action":             {"archive"},
		"condition_field":    {"has_attachment"},
		"condition_operator": {"is"},
		"condition_value":    {""},
	})
	rule, err := parseFilterRuleForm(r, mailboxID)
	require.NoError(t, err)

	require.Len(t, rule.Conditions, 1)
	assert.Nil(t, rule.Conditions[0].Value, "empty value should be stored as nil")
}

func TestParseFilterRuleForm_MismatchedConditionSlices(t *testing.T) {
	mailboxID := uuid.New()
	// more fields than operators — extra fields should be silently dropped
	r := formRequest(url.Values{
		"action":             {"archive"},
		"condition_field":    {"from", "subject", "body"},
		"condition_operator": {"contains", "is"},
		"condition_value":    {"a@b.com", "hello", "extra"},
	})
	rule, err := parseFilterRuleForm(r, mailboxID)
	require.NoError(t, err)
	assert.Len(t, rule.Conditions, 2)
}
