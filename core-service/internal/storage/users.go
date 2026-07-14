package storage

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
)

type Users struct {
	pool *pgxpool.Pool
}

func NewUsers(pool *pgxpool.Pool) *Users {
	return &Users{pool: pool}
}

const userColumns = "id, username, first_name, photo_url, theme, rank, status_emoji, status_text, created_at, avatar_key, avatar_source"

func scanUser(row pgx.Row) (domain.User, error) {
	var u domain.User
	err := row.Scan(&u.ID, &u.Username, &u.FirstName, &u.PhotoURL, &u.Theme, &u.Rank,
		&u.StatusEmoji, &u.StatusText, &u.CreatedAt, &u.AvatarKey, &u.AvatarSource)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, ErrNotFound
	}
	u.HasAvatar = u.AvatarKey != ""
	return u, err
}

// Upsert keeps telegram profile fields fresh on every authenticated request.
func (r *Users) Upsert(ctx context.Context, id int64, username, firstName, photoURL string) (domain.User, error) {
	return scanUser(r.pool.QueryRow(ctx, `
		INSERT INTO users (id, username, first_name, photo_url)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE
		SET username = $2, first_name = $3, photo_url = $4
		RETURNING `+userColumns,
		id, username, firstName, photoURL))
}

func (r *Users) Get(ctx context.Context, id int64) (domain.User, error) {
	return scanUser(r.pool.QueryRow(ctx,
		"SELECT "+userColumns+" FROM users WHERE id = $1", id))
}

func (r *Users) UpdateTheme(ctx context.Context, id int64, theme string) (domain.User, error) {
	return scanUser(r.pool.QueryRow(ctx,
		"UPDATE users SET theme = $2 WHERE id = $1 RETURNING "+userColumns, id, theme))
}

// PendingAvatars lists users whose telegram picture has not been mirrored yet.
// Their id and photo_url is all the mirror needs.
func (r *Users) PendingAvatars(ctx context.Context) ([]domain.User, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, photo_url, avatar_source FROM users
		WHERE photo_url <> '' AND photo_url IS DISTINCT FROM avatar_source
		ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.User
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(&u.ID, &u.PhotoURL, &u.AvatarSource); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (r *Users) SetAvatar(ctx context.Context, id int64, key, source string) error {
	_, err := r.pool.Exec(ctx,
		"UPDATE users SET avatar_key = $2, avatar_source = $3 WHERE id = $1", id, key, source)
	return err
}

func (r *Users) SetRank(ctx context.Context, id int64, rank string) error {
	_, err := r.pool.Exec(ctx, "UPDATE users SET rank = $2 WHERE id = $1", id, rank)
	return err
}

// SetStatus writes the member's own status line. Empty strings clear it.
func (r *Users) SetStatus(ctx context.Context, id int64, emoji, text string) (domain.User, error) {
	return scanUser(r.pool.QueryRow(ctx,
		"UPDATE users SET status_emoji = $2, status_text = $3 WHERE id = $1 RETURNING "+userColumns,
		id, emoji, text))
}

func (r *Users) Achievements(ctx context.Context, userID int64) ([]domain.Achievement, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT key, granted_at FROM achievements WHERE user_id = $1 ORDER BY granted_at", userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[domain.Achievement])
}

// Grant inserts keys and returns only the newly granted ones.
func (r *Users) Grant(ctx context.Context, userID int64, keys []string) ([]string, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		INSERT INTO achievements (user_id, key)
		SELECT $1, unnest($2::text[])
		ON CONFLICT DO NOTHING
		RETURNING key`, userID, keys)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// prefixed qualifies a column list with a table alias, so userColumns can be reused inside a join.
func prefixed(alias, columns string) string {
	parts := strings.Split(columns, ", ")
	for i, c := range parts {
		parts[i] = alias + "." + c
	}
	return strings.Join(parts, ", ")
}
