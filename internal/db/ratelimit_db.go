package db

import (
	"context"
	"net"
	"time"

	"github.com/colormechadd/mailaroo/pkg/models"
	"github.com/google/uuid"
)

type RateLimitDB interface {
	IsIPBlocked(ctx context.Context, ip net.IP) (bool, error)
	AddIPBlock(ctx context.Context, ip net.IP, reason string, blockedUntil *time.Time) error
	ListIPBlocks(ctx context.Context) ([]models.IPBlock, error)
	RemoveIPBlock(ctx context.Context, id uuid.UUID) error
	CheckAndUpdateGreylist(ctx context.Context, ip net.IP, from, to string, delay time.Duration) (pass bool, err error)
	CountOutboundByUserHour(ctx context.Context, userID uuid.UUID) (int, error)
	PurgeExpiredRateLimitData(ctx context.Context) error
}

func (db *DB) IsIPBlocked(ctx context.Context, ip net.IP) (bool, error) {
	var blocked bool
	err := db.GetContext(ctx, &blocked, `
		SELECT EXISTS(
			SELECT 1 FROM ip_block
			WHERE ip_address = $1
			  AND (is_permanent = TRUE OR blocked_until > NOW())
		)
	`, ip.String())
	return blocked, err
}

func (db *DB) AddIPBlock(ctx context.Context, ip net.IP, reason string, blockedUntil *time.Time) error {
	isPermanent := blockedUntil == nil
	_, err := db.ExecContext(ctx, `
		INSERT INTO ip_block (ip_address, reason, blocked_until, is_permanent)
		VALUES ($1, $2, $3, $4)
	`, ip.String(), reason, blockedUntil, isPermanent)
	return err
}

func (db *DB) ListIPBlocks(ctx context.Context) ([]models.IPBlock, error) {
	var blocks []models.IPBlock
	err := db.SelectContext(ctx, &blocks, `
		SELECT id, ip_address, reason, blocked_until, is_permanent, create_datetime
		FROM ip_block
		WHERE is_permanent = TRUE OR blocked_until > NOW()
		ORDER BY create_datetime DESC
	`)
	return blocks, err
}

func (db *DB) RemoveIPBlock(ctx context.Context, id uuid.UUID) error {
	_, err := db.ExecContext(ctx, `DELETE FROM ip_block WHERE id = $1`, id)
	return err
}

func (db *DB) CheckAndUpdateGreylist(ctx context.Context, ip net.IP, from, to string, delay time.Duration) (bool, error) {
	var firstSeen time.Time
	var passCount int

	err := db.QueryRowContext(ctx, `
		INSERT INTO greylist_entry (ip_address, from_address, to_address)
		VALUES ($1, $2, $3)
		ON CONFLICT (ip_address, from_address, to_address)
		DO UPDATE SET last_seen = CURRENT_TIMESTAMP
		RETURNING first_seen, pass_count
	`, ip.String(), from, to).Scan(&firstSeen, &passCount)
	if err != nil {
		return false, err
	}

	if time.Since(firstSeen) < delay {
		return false, nil
	}

	_, err = db.ExecContext(ctx, `
		UPDATE greylist_entry SET pass_count = pass_count + 1
		WHERE ip_address = $1 AND from_address = $2 AND to_address = $3
	`, ip.String(), from, to)
	return true, err
}

func (db *DB) CountOutboundByUserHour(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	err := db.GetContext(ctx, &count, `
		SELECT COUNT(*) FROM email
		WHERE user_id = $1
		  AND direction = 'OUTBOUND'
		  AND receive_datetime >= date_trunc('hour', NOW())
	`, userID)
	return count, err
}

func (db *DB) PurgeExpiredRateLimitData(ctx context.Context) error {
	_, err := db.ExecContext(ctx, `DELETE FROM ip_block WHERE is_permanent = FALSE AND blocked_until < NOW()`)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `DELETE FROM greylist_entry WHERE last_seen < NOW() - INTERVAL '30 days'`)
	return err
}
