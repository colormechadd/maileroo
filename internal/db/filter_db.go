package db

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

func (db *DB) listFilterRules(ctx context.Context, mailboxID uuid.UUID, ruleID *uuid.UUID) ([]*models.FilterRule, error) {
	type ruleRow struct {
		models.FilterRule
		ConditionsJSON []byte `db:"conditions"`
	}

	var rows []ruleRow
	err := db.SelectContext(ctx, &rows, `
		SELECT
			r.id, r.mailbox_id, r.name, r.priority, r.is_active, r.match_all, r.action, r.stop_processing,
			COALESCE(jsonb_agg(
				jsonb_build_object(
					'id', c.id,
					'rule_id', c.rule_id,
					'field', c.field,
					'operator', c.operator,
					'value', c.value
				) ORDER BY c.id
			) FILTER (WHERE c.id IS NOT NULL), '[]') AS conditions
		FROM mailbox_filter_rule r
		LEFT JOIN mailbox_filter_condition c ON c.rule_id = r.id
		WHERE r.mailbox_id = $1
		AND ($2::uuid IS NULL OR r.id = $2)
		GROUP BY r.id
		ORDER BY r.priority ASC
	`, mailboxID, ruleID)
	if err != nil {
		return nil, err
	}

	rules := make([]*models.FilterRule, len(rows))
	for i, row := range rows {
		rule := row.FilterRule
		if err := json.Unmarshal(row.ConditionsJSON, &rule.Conditions); err != nil {
			return nil, err
		}
		rules[i] = &rule
	}
	return rules, nil
}

func (db *DB) ListFilterRules(ctx context.Context, mailboxID uuid.UUID) ([]*models.FilterRule, error) {
	return db.listFilterRules(ctx, mailboxID, nil)
}

func (db *DB) GetFilterRuleByID(ctx context.Context, ruleID, mailboxID uuid.UUID) (*models.FilterRule, error) {
	rules, err := db.listFilterRules(ctx, mailboxID, &ruleID)
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		return nil, sql.ErrNoRows
	}
	return rules[0], nil
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
