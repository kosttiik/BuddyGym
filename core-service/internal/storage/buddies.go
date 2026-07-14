package storage

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
)

type Buddies struct {
	pool *pgxpool.Pool
}

func NewBuddies(pool *pgxpool.Pool) *Buddies {
	return &Buddies{pool: pool}
}

// Tag records the people who trained with the author. Tagging the same person twice is not an
// error, so the "add more buddies" button can be pressed as many times as the author likes.
func (b *Buddies) Tag(ctx context.Context, checkinID string, roomID, authorID int64, userIDs []int64) error {
	if len(userIDs) == 0 {
		return nil
	}
	_, err := b.pool.Exec(ctx, `
		INSERT INTO checkin_buddies (checkin_id, room_id, user_id, author_id)
		SELECT $1, $2, unnest($3::bigint[]), $4
		ON CONFLICT DO NOTHING`, checkinID, roomID, userIDs, authorID)
	return err
}

func (b *Buddies) Untag(ctx context.Context, checkinID string, userID int64) error {
	tag, err := b.pool.Exec(ctx,
		"DELETE FROM checkin_buddies WHERE checkin_id = $1 AND user_id = $2", checkinID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UserIDs lists who was tagged on one checkin.
func (b *Buddies) UserIDs(ctx context.Context, checkinID string) ([]int64, error) {
	rows, err := b.pool.Query(ctx,
		"SELECT user_id FROM checkin_buddies WHERE checkin_id = $1 ORDER BY created_at", checkinID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

// ForCheckins returns the tagged users of every listed checkin, so a whole page of the room
// feed costs one query rather than one per card.
func (b *Buddies) ForCheckins(ctx context.Context, checkinIDs []string) (map[string][]domain.User, error) {
	if len(checkinIDs) == 0 {
		return map[string][]domain.User{}, nil
	}
	rows, err := b.pool.Query(ctx, `
		SELECT b.checkin_id, `+prefixed("u", userColumns)+`
		FROM checkin_buddies b
		JOIN users u ON u.id = b.user_id
		WHERE b.checkin_id = ANY($1)
		ORDER BY b.created_at`, checkinIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string][]domain.User{}
	for rows.Next() {
		var checkinID string
		var u domain.User
		if err := rows.Scan(&checkinID, &u.ID, &u.Username, &u.FirstName, &u.PhotoURL, &u.Theme,
			&u.Rank, &u.StatusEmoji, &u.StatusText, &u.CreatedAt, &u.AvatarKey, &u.AvatarSource); err != nil {
			return nil, err
		}
		u.HasAvatar = u.AvatarKey != ""
		out[checkinID] = append(out[checkinID], u)
	}
	return out, rows.Err()
}
