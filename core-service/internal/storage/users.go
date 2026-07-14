package storage

import (
	"context"
	"errors"

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

const userColumns = "id, username, first_name, photo_url, theme, status, created_at, avatar_key, avatar_source"

func scanUser(row pgx.Row) (domain.User, error) {
	var u domain.User
	err := row.Scan(&u.ID, &u.Username, &u.FirstName, &u.PhotoURL, &u.Theme, &u.Status, &u.CreatedAt,
		&u.AvatarKey, &u.AvatarSource)
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

func (r *Users) SetAvatar(ctx context.Context, id int64, key, source string) error {
	_, err := r.pool.Exec(ctx,
		"UPDATE users SET avatar_key = $2, avatar_source = $3 WHERE id = $1", id, key, source)
	return err
}

func (r *Users) SetStatus(ctx context.Context, id int64, status string) error {
	_, err := r.pool.Exec(ctx, "UPDATE users SET status = $2 WHERE id = $1", id, status)
	return err
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
