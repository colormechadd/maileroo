package filterengine

import (
	"testing"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
)

func strPtr(s string) *string { return &s }

func rule(action string, matchAll bool, stopProcessing bool, conditions ...models.FilterCondition) *models.FilterRule {
	return &models.FilterRule{
		ID:             uuid.New(),
		IsActive:       true,
		Action:         action,
		MatchAll:       matchAll,
		StopProcessing: stopProcessing,
		Conditions:     conditions,
	}
}

func cond(field, operator string, value *string) models.FilterCondition {
	return models.FilterCondition{
		ID:       uuid.New(),
		Field:    field,
		Operator: operator,
		Value:    value,
	}
}

func TestMatch_NoRules(t *testing.T) {
	e := &RuleEngine{}
	matched, err := e.Match(nil, &ParsedMessage{From: "a@b.com"})
	if err != nil || matched != nil {
		t.Fatalf("expected no match, got %v %v", matched, err)
	}
}

func TestMatch_Contains(t *testing.T) {
	e := &RuleEngine{}
	r := rule(models.FilterActionArchive, true, true,
		cond(models.FilterFieldSubject, models.FilterOperatorContains, strPtr("newsletter")),
	)
	msg := &ParsedMessage{Subject: "Weekly Newsletter"}
	matched, err := e.Match([]*models.FilterRule{r}, msg)
	if err != nil {
		t.Fatal(err)
	}
	if matched == nil {
		t.Fatal("expected match")
	}
}

func TestMatch_ANDAllMustPass(t *testing.T) {
	e := &RuleEngine{}
	r := rule(models.FilterActionDelete, true, true,
		cond(models.FilterFieldFrom, models.FilterOperatorContains, strPtr("spam")),
		cond(models.FilterFieldSubject, models.FilterOperatorContains, strPtr("offer")),
	)
	msg := &ParsedMessage{From: "spam@example.com", Subject: "no match here"}
	matched, err := e.Match([]*models.FilterRule{r}, msg)
	if err != nil {
		t.Fatal(err)
	}
	if matched != nil {
		t.Fatal("expected no match — second condition fails")
	}
}

func TestMatch_OROneEnough(t *testing.T) {
	e := &RuleEngine{}
	r := rule(models.FilterActionStar, false, true,
		cond(models.FilterFieldFrom, models.FilterOperatorContains, strPtr("boss")),
		cond(models.FilterFieldSubject, models.FilterOperatorContains, strPtr("urgent")),
	)
	msg := &ParsedMessage{From: "boss@company.com", Subject: "routine update"}
	matched, err := e.Match([]*models.FilterRule{r}, msg)
	if err != nil {
		t.Fatal(err)
	}
	if matched == nil {
		t.Fatal("expected match via first OR condition")
	}
}

func TestMatch_Regex(t *testing.T) {
	e := &RuleEngine{}
	r := rule(models.FilterActionQuarantine, true, true,
		cond(models.FilterFieldFrom, models.FilterOperatorMatchesRegex, strPtr(`.*@suspicious\.io$`)),
	)
	msg := &ParsedMessage{From: "bad@suspicious.io"}
	matched, err := e.Match([]*models.FilterRule{r}, msg)
	if err != nil {
		t.Fatal(err)
	}
	if matched == nil {
		t.Fatal("expected match")
	}
}

func TestMatch_HasAttachment(t *testing.T) {
	e := &RuleEngine{}
	r := rule(models.FilterActionStar, true, true,
		cond(models.FilterFieldHasAttachment, models.FilterOperatorIs, nil),
	)
	msg := &ParsedMessage{HasAttachment: true}
	matched, err := e.Match([]*models.FilterRule{r}, msg)
	if err != nil {
		t.Fatal(err)
	}
	if matched == nil {
		t.Fatal("expected match")
	}

	msg.HasAttachment = false
	matched, err = e.Match([]*models.FilterRule{r}, msg)
	if err != nil {
		t.Fatal(err)
	}
	if matched != nil {
		t.Fatal("expected no match")
	}
}

func TestMatch_InactiveRuleSkipped(t *testing.T) {
	e := &RuleEngine{}
	r := rule(models.FilterActionArchive, true, true,
		cond(models.FilterFieldSubject, models.FilterOperatorContains, strPtr("test")),
	)
	r.IsActive = false
	msg := &ParsedMessage{Subject: "test subject"}
	matched, err := e.Match([]*models.FilterRule{r}, msg)
	if err != nil || matched != nil {
		t.Fatalf("inactive rule should be skipped, got %v %v", matched, err)
	}
}
