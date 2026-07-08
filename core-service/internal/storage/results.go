package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	ResultApproved = "approved"
	ResultRejected = "rejected"
	ResultExpired  = "expired"
)

type Results struct {
	pool *pgxpool.Pool
}

func NewResults(pool *pgxpool.Pool) *Results {
	return &Results{pool: pool}
}

// Apply records a final checkin result exactly once. On an approved result
// it bumps the member period counter, resetting it when the period rolled over.
// Returns applied=false when this checkin_id was already processed.
func (r *Results) Apply(ctx context.Context, checkinID string, roomID, userID int64, status string) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		INSERT INTO checkin_results (checkin_id, room_id, user_id, status)
		VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
		checkinID, roomID, userID, status)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}

	if status == ResultApproved {
		// user may have left the room by now, then there is nothing to bump
		_, err = tx.Exec(ctx, `
			UPDATE memberships m SET
				workouts_count = CASE WHEN now() >= m.period_start + r.period_days * interval '1 day'
				                      THEN 1 ELSE m.workouts_count + 1 END,
				period_start = CASE WHEN now() >= m.period_start + r.period_days * interval '1 day'
				                    THEN now() ELSE m.period_start END
			FROM rooms r
			WHERE r.id = m.room_id AND m.room_id = $1 AND m.user_id = $2`,
			roomID, userID)
		if err != nil {
			return false, err
		}
	}
	return true, tx.Commit(ctx)
}

func (r *Results) TotalApproved(ctx context.Context, userID int64) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		"SELECT count(*) FROM checkin_results WHERE user_id = $1 AND status = $2",
		userID, ResultApproved).Scan(&n)
	return n, err
}

// WorkoutDays returns distinct UTC dates of approved checkins, newest first.
func (r *Results) WorkoutDays(ctx context.Context, userID int64, limit int) ([]time.Time, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT (applied_at AT TIME ZONE 'UTC')::date AS day
		FROM checkin_results
		WHERE user_id = $1 AND status = $2
		ORDER BY day DESC
		LIMIT $3`, userID, ResultApproved, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[time.Time])
}

func (r *Results) PeriodCount(ctx context.Context, roomID, userID int64) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT `+periodAwareCount+`
		FROM memberships m JOIN rooms r ON r.id = m.room_id
		WHERE m.room_id = $1 AND m.user_id = $2`, roomID, userID).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return n, err
}
