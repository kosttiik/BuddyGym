package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
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
func (r *Results) Apply(ctx context.Context, checkinID string, roomID, userID int64, status string, createdAt time.Time) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		INSERT INTO checkin_results (checkin_id, room_id, user_id, status, checkin_created_at)
		VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`,
		checkinID, roomID, userID, status, createdAt)
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
				                      THEN 1 ELSE (
						SELECT count(DISTINCT (cr.checkin_created_at AT TIME ZONE 'UTC')::date)::int
						FROM checkin_results cr
						WHERE cr.room_id = m.room_id AND cr.user_id = m.user_id
						  AND cr.status = 'approved' AND cr.checkin_created_at >= m.period_start
					) END,
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

const streakInputQuery = `
	SELECT m.room_id, m.user_id, r.goal_per_period, r.period_days, m.joined_at,
	       (cr.checkin_created_at AT TIME ZONE 'UTC')::date AS day
	FROM memberships m
	JOIN rooms r ON r.id = m.room_id
	LEFT JOIN checkin_results cr
	       ON cr.room_id = m.room_id AND cr.user_id = m.user_id AND cr.status = 'approved'
	WHERE r.deleted_at IS NULL AND %s
	GROUP BY m.room_id, m.user_id, r.goal_per_period, r.period_days, m.joined_at, day
	ORDER BY m.room_id, m.user_id, day`

// StreaksByRoom returns one input per member of the room.
func (r *Results) StreaksByRoom(ctx context.Context, roomID int64) ([]domain.StreakInput, error) {
	return r.streakInputs(ctx, fmt.Sprintf(streakInputQuery, "m.room_id = $1"), roomID)
}

// StreaksByUser returns one input per room the user belongs to.
func (r *Results) StreaksByUser(ctx context.Context, userID int64) ([]domain.StreakInput, error) {
	return r.streakInputs(ctx, fmt.Sprintf(streakInputQuery, "m.user_id = $1"), userID)
}

func (r *Results) streakInputs(ctx context.Context, query string, arg int64) ([]domain.StreakInput, error) {
	rows, err := r.pool.Query(ctx, query, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.StreakInput
	for rows.Next() {
		var roomID, userID int64
		var goal, periodDays int
		var joinedAt time.Time
		// members with no approved checkin yet come back from the LEFT JOIN with a null day
		var day *time.Time
		if err := rows.Scan(&roomID, &userID, &goal, &periodDays, &joinedAt, &day); err != nil {
			return nil, err
		}
		if n := len(out); n > 0 && out[n-1].RoomID == roomID && out[n-1].UserID == userID {
			if day != nil {
				out[n-1].Days = append(out[n-1].Days, *day)
			}
			continue
		}
		in := domain.StreakInput{RoomID: roomID, UserID: userID, Goal: goal, PeriodDays: periodDays, JoinedAt: joinedAt}
		if day != nil {
			in.Days = []time.Time{*day}
		}
		out = append(out, in)
	}
	return out, rows.Err()
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
