package storage

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
)

type Freezes struct {
	pool *pgxpool.Pool
}

func NewFreezes(pool *pgxpool.Pool) *Freezes {
	return &Freezes{pool: pool}
}

const freezeColumns = "id, room_id, user_id, starts_at, ends_at, canceled_at, created_at"

func (f *Freezes) Create(ctx context.Context, roomID, userID int64, startsAt, endsAt time.Time) (domain.Freeze, error) {
	var fz domain.Freeze
	err := f.pool.QueryRow(ctx, `
		INSERT INTO freezes (room_id, user_id, starts_at, ends_at)
		VALUES ($1, $2, $3, $4)
		RETURNING `+freezeColumns,
		roomID, userID, startsAt, endsAt).
		Scan(&fz.ID, &fz.RoomID, &fz.UserID, &fz.StartsAt, &fz.EndsAt, &fz.CanceledAt, &fz.CreatedAt)
	return fz, err
}

func (f *Freezes) Cancel(ctx context.Context, roomID, userID int64, at time.Time) error {
	tag, err := f.pool.Exec(ctx, `
		UPDATE freezes SET canceled_at = $3
		WHERE room_id = $1 AND user_id = $2 AND canceled_at IS NULL AND ends_at > $3`,
		roomID, userID, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (f *Freezes) ListByMember(ctx context.Context, roomID, userID int64) ([]domain.Freeze, error) {
	rows, err := f.pool.Query(ctx,
		"SELECT "+freezeColumns+" FROM freezes WHERE room_id = $1 AND user_id = $2 ORDER BY starts_at",
		roomID, userID)
	if err != nil {
		return nil, err
	}
	return collectFreezes(rows)
}

func collectFreezes(rows pgx.Rows) ([]domain.Freeze, error) {
	defer rows.Close()
	var out []domain.Freeze
	for rows.Next() {
		var fz domain.Freeze
		if err := rows.Scan(&fz.ID, &fz.RoomID, &fz.UserID, &fz.StartsAt, &fz.EndsAt,
			&fz.CanceledAt, &fz.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, fz)
	}
	return out, rows.Err()
}
