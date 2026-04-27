package db

import (
	"context"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

func (db *DB) ListFilterRules(ctx context.Context, mailboxID uuid.UUID) ([]*models.FilterRule, error) {
	var rules []*models.FilterRule
	err := db.SelectContext(ctx, &rules, `
		SELECT id, mailbox_id, name, priority, is_active, match_all, action, stop_processing
		FROM mailbox_filter_rule
		WHERE mailbox_id = $1
		ORDER BY priority ASC
	`, mailboxID)
	if err != nil {
		return nil, err
	}

	if len(rules) == 0 {
		return rules, nil
	}

	ruleIDs := make([]uuid.UUID, len(rules))
	ruleIndex := make(map[uuid.UUID]*models.FilterRule, len(rules))
	for i, r := range rules {
		ruleIDs[i] = r.ID
		ruleIndex[r.ID] = r
	}

	query, args, err := sqlx.In(`
		SELECT id, rule_id, field, operator, value
		FROM mailbox_filter_condition
		WHERE rule_id IN (?)
		ORDER BY id ASC
	`, ruleIDs)
	if err != nil {
		return nil, err
	}
	query = db.Rebind(query)

	var conditions []models.FilterCondition
	if err := db.SelectContext(ctx, &conditions, query, args...); err != nil {
		return nil, err
	}
	for _, c := range conditions {
		r := ruleIndex[c.RuleID]
		r.Conditions = append(r.Conditions, c)
	}

	return rules, nil
}

func (db *DB) GetFilterRuleByID(ctx context.Context, ruleID, mailboxID uuid.UUID) (*models.FilterRule, error) {
	var rule models.FilterRule
	err := db.GetContext(ctx, &rule, `
		SELECT id, mailbox_id, name, priority, is_active, match_all, action, stop_processing
		FROM mailbox_filter_rule
		WHERE id = $1 AND mailbox_id = $2
	`, ruleID, mailboxID)
	if err != nil {
		return nil, err
	}

	err = db.SelectContext(ctx, &rule.Conditions, `
		SELECT id, rule_id, field, operator, value
		FROM mailbox_filter_condition
		WHERE rule_id = $1
		ORDER BY id ASC
	`, ruleID)
	return &rule, err
}

func (db *DB) CreateFilterRule(ctx context.Context, rule *models.FilterRule) error {
	return db.WithTx(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.NamedExecContext(ctx, `
			INSERT INTO mailbox_filter_rule
				(id, mailbox_id, name, priority, is_active, match_all, action, stop_processing)
			VALUES
				(:id, :mailbox_id, :name, :priority, :is_active, :match_all, :action, :stop_processing)
		`, rule)
		if err != nil {
			return err
		}

		for i := range rule.Conditions {
			rule.Conditions[i].RuleID = rule.ID
			if _, err := tx.NamedExecContext(ctx, `
				INSERT INTO mailbox_filter_condition (id, rule_id, field, operator, value)
				VALUES (:id, :rule_id, :field, :operator, :value)
			`, rule.Conditions[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

func (db *DB) UpdateFilterRule(ctx context.Context, rule *models.FilterRule) error {
	return db.WithTx(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.NamedExecContext(ctx, `
			UPDATE mailbox_filter_rule SET
				name = :name,
				priority = :priority,
				is_active = :is_active,
				match_all = :match_all,
				action = :action,
				stop_processing = :stop_processing,
				update_datetime = CURRENT_TIMESTAMP
			WHERE id = :id AND mailbox_id = :mailbox_id
		`, rule)
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM mailbox_filter_condition WHERE rule_id = $1`, rule.ID); err != nil {
			return err
		}

		for i := range rule.Conditions {
			rule.Conditions[i].RuleID = rule.ID
			if _, err := tx.NamedExecContext(ctx, `
				INSERT INTO mailbox_filter_condition (id, rule_id, field, operator, value)
				VALUES (:id, :rule_id, :field, :operator, :value)
			`, rule.Conditions[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

func (db *DB) DeleteFilterRule(ctx context.Context, ruleID, mailboxID uuid.UUID) error {
	_, err := db.ExecContext(ctx, `
		DELETE FROM mailbox_filter_rule WHERE id = $1 AND mailbox_id = $2
	`, ruleID, mailboxID)
	return err
}

func (db *DB) ReorderFilterRules(ctx context.Context, mailboxID uuid.UUID, orderedIDs []uuid.UUID) error {
	return db.WithTx(ctx, func(tx *sqlx.Tx) error {
		for i, id := range orderedIDs {
			if _, err := tx.ExecContext(ctx, `
				UPDATE mailbox_filter_rule SET priority = $1, update_datetime = CURRENT_TIMESTAMP
				WHERE id = $2 AND mailbox_id = $3
			`, i, id, mailboxID); err != nil {
				return err
			}
		}
		return nil
	})
}
