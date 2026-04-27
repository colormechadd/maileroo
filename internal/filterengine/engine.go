package filterengine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/colormechadd/mailaroo/pkg/models"
)

// ParsedMessage holds email fields that filter conditions are evaluated against.
type ParsedMessage struct {
	From          string
	To            string
	Subject       string
	Body          string
	HasAttachment bool
}

// RuleEngine evaluates filter rules against an email message.
type RuleEngine struct{}

// Match returns the first rule that matches msg, or nil if none do.
// Rules are evaluated in the order provided (caller is responsible for ordering by priority).
func (e *RuleEngine) Match(rules []*models.FilterRule, msg *ParsedMessage) (*models.FilterRule, error) {
	for _, rule := range rules {
		if !rule.IsActive {
			continue
		}
		matched, err := e.evaluateRule(rule, msg)
		if err != nil {
			return nil, fmt.Errorf("rule %s: %w", rule.ID, err)
		}
		if matched {
			return rule, nil
		}
	}
	return nil, nil
}

func (e *RuleEngine) evaluateRule(rule *models.FilterRule, msg *ParsedMessage) (bool, error) {
	if len(rule.Conditions) == 0 {
		return false, nil
	}

	for _, cond := range rule.Conditions {
		result, err := e.evaluateCondition(cond, msg)
		if err != nil {
			return false, err
		}
		if rule.MatchAll && !result {
			return false, nil // AND mode: one failure is enough
		}
		if !rule.MatchAll && result {
			return true, nil // OR mode: one success is enough
		}
	}

	// If we exhausted all conditions:
	// AND mode → all passed → true
	// OR mode → none passed → false
	return rule.MatchAll, nil
}

func (e *RuleEngine) evaluateCondition(cond models.FilterCondition, msg *ParsedMessage) (bool, error) {
	fieldValue := e.fieldValue(cond.Field, msg)

	if cond.Field == models.FilterFieldHasAttachment {
		switch cond.Operator {
		case models.FilterOperatorIs:
			return msg.HasAttachment, nil
		case models.FilterOperatorIsNot:
			return !msg.HasAttachment, nil
		default:
			return false, fmt.Errorf("unsupported operator %q for has_attachment", cond.Operator)
		}
	}

	val := ""
	if cond.Value != nil {
		val = *cond.Value
	}

	switch cond.Operator {
	case models.FilterOperatorContains:
		return strings.Contains(strings.ToLower(fieldValue), strings.ToLower(val)), nil
	case models.FilterOperatorNotContains:
		return !strings.Contains(strings.ToLower(fieldValue), strings.ToLower(val)), nil
	case models.FilterOperatorIs:
		return strings.EqualFold(fieldValue, val), nil
	case models.FilterOperatorIsNot:
		return !strings.EqualFold(fieldValue, val), nil
	case models.FilterOperatorMatchesRegex:
		re, err := regexp.Compile(val)
		if err != nil {
			return false, fmt.Errorf("invalid regex %q: %w", val, err)
		}
		return re.MatchString(fieldValue), nil
	default:
		return false, fmt.Errorf("unknown operator %q", cond.Operator)
	}
}

func (e *RuleEngine) fieldValue(field string, msg *ParsedMessage) string {
	switch field {
	case models.FilterFieldFrom:
		return msg.From
	case models.FilterFieldTo:
		return msg.To
	case models.FilterFieldSubject:
		return msg.Subject
	case models.FilterFieldBody:
		return msg.Body
	default:
		return ""
	}
}
