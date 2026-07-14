package storage

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
)

type Comments struct {
	pool *pgxpool.Pool
}

func NewComments(pool *pgxpool.Pool) *Comments {
	return &Comments{pool: pool}
}

const commentColumns = `c.id, c.checkin_id, c.user_id, c.body, c.created_at, ` +
	`u.first_name, u.username, u.photo_url, u.avatar_key`

func scanComment(row pgx.Row) (domain.Comment, error) {
	var c domain.Comment
	err := row.Scan(&c.ID, &c.CheckinID, &c.UserID, &c.Body, &c.CreatedAt,
		&c.Author.FirstName, &c.Author.Username, &c.Author.PhotoURL, &c.Author.AvatarKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Comment{}, ErrNotFound
	}
	c.Author.ID = c.UserID
	c.Author.HasAvatar = c.Author.AvatarKey != ""
	return c, err
}

func (r *Comments) Add(ctx context.Context, checkinID string, roomID, userID int64, body string) (domain.Comment, error) {
	return scanComment(r.pool.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO checkin_comments (checkin_id, room_id, user_id, body)
			VALUES ($1, $2, $3, $4)
			RETURNING id, checkin_id, user_id, body, created_at
		)
		SELECT `+commentColumns+`
		FROM inserted c JOIN users u ON u.id = c.user_id`,
		checkinID, roomID, userID, body))
}

func (r *Comments) List(ctx context.Context, checkinID string, limit, offset int) ([]domain.Comment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+commentColumns+`
		FROM checkin_comments c
		JOIN users u ON u.id = c.user_id
		WHERE c.checkin_id = $1
		ORDER BY c.created_at
		LIMIT $2 OFFSET $3`, checkinID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Comment
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Delete removes a comment. The author can delete their own; the room creator can delete any,
// which is the only moderation the room has.
func (r *Comments) Delete(ctx context.Context, id, userID int64) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM checkin_comments c
		USING rooms r
		WHERE c.id = $1 AND r.id = c.room_id
		  AND (c.user_id = $2 OR r.creator_id = $2)`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CountsFor returns how many comments each listed checkin has, so a page of the feed costs
// one query rather than one per card.
func (r *Comments) CountsFor(ctx context.Context, checkinIDs []string) (map[string]int, error) {
	if len(checkinIDs) == 0 {
		return map[string]int{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT checkin_id, count(*)::int
		FROM checkin_comments
		WHERE checkin_id = ANY($1)
		GROUP BY checkin_id`, checkinIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
