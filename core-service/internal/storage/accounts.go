package storage

import (
	"context"
	"crypto/sha256"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
)

type Accounts struct {
	pool *pgxpool.Pool
}

func NewAccounts(pool *pgxpool.Pool) *Accounts {
	return &Accounts{pool: pool}
}

func (r *Accounts) UserByTelegram(ctx context.Context, telegramID int64) (domain.User, error) {
	return scanUser(r.pool.QueryRow(ctx, `
		SELECT `+prefixed("u", userColumns)+`
		FROM auth_identities i
		JOIN users u ON u.id = i.user_id
		WHERE i.provider = 'telegram' AND i.subject = $1`,
		strconv.FormatInt(telegramID, 10)))
}

func (r *Accounts) CreateTelegramUser(
	ctx context.Context,
	telegramID int64,
	username, firstName, photoURL string,
) (domain.User, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, err
	}
	defer tx.Rollback(ctx)

	user, err := scanUser(tx.QueryRow(ctx, `
		INSERT INTO users (username, first_name, photo_url)
		VALUES ($1, $2, $3)
		RETURNING `+userColumns,
		username, firstName, photoURL))
	if err != nil {
		return domain.User{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO auth_identities (provider, subject, user_id)
		VALUES ('telegram', $1, $2)`,
		strconv.FormatInt(telegramID, 10), user.ID); err != nil {
		return domain.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.User{}, err
	}
	return user, nil
}

func (r *Accounts) CreateLocalUser(
	ctx context.Context,
	username, firstName, passwordHash string,
) (domain.User, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, err
	}
	defer tx.Rollback(ctx)

	user, err := scanUser(tx.QueryRow(ctx, `
		INSERT INTO users (username, first_name)
		VALUES ($1, $2)
		RETURNING `+userColumns,
		username, firstName))
	if err != nil {
		return domain.User{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO password_credentials (user_id, password_hash)
		VALUES ($1, $2)`,
		user.ID, passwordHash); err != nil {
		return domain.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.User{}, err
	}
	return user, nil
}

func (r *Accounts) PasswordUser(ctx context.Context, username string) (domain.User, string, error) {
	var hash string
	user, err := scanUserWithExtra(r.pool.QueryRow(ctx, `
		SELECT `+prefixed("u", userColumns)+`, c.password_hash
		FROM password_credentials c
		JOIN users u ON u.id = c.user_id
		WHERE lower(u.username) = lower($1)`,
		username), &hash)
	return user, hash, err
}

func (r *Accounts) CredentialHash(ctx context.Context, userID int64) (string, error) {
	var hash string
	err := r.pool.QueryRow(ctx,
		"SELECT password_hash FROM password_credentials WHERE user_id = $1",
		userID,
	).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return hash, err
}

func scanUserWithExtra(row pgx.Row, extra ...any) (domain.User, error) {
	var u domain.User
	targets := []any{
		&u.ID, &u.Username, &u.FirstName, &u.PhotoURL, &u.Theme, &u.Language,
		&u.Timezone, &u.Rank, &u.StatusEmoji, &u.StatusText, &u.CreatedAt,
		&u.AvatarKey, &u.AvatarSource,
	}
	targets = append(targets, extra...)
	err := row.Scan(targets...)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, ErrNotFound
	}
	u.HasAvatar = u.AvatarKey != ""
	return u, err
}

func (r *Accounts) SetCredentials(
	ctx context.Context,
	userID int64,
	username, passwordHash string,
) (domain.User, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO password_credentials (user_id, password_hash)
		VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE
		SET password_hash = excluded.password_hash, updated_at = now()`,
		userID, passwordHash); err != nil {
		return domain.User{}, err
	}
	user, err := scanUser(tx.QueryRow(ctx,
		"UPDATE users SET username = $2 WHERE id = $1 RETURNING "+userColumns,
		userID, username))
	if err != nil {
		return domain.User{}, err
	}
	if _, err := tx.Exec(ctx,
		"UPDATE auth_sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL",
		userID); err != nil {
		return domain.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.User{}, err
	}
	return user, nil
}

func (r *Accounts) UpdateProfile(
	ctx context.Context,
	userID int64,
	username, firstName string,
) (domain.User, error) {
	return scanUser(r.pool.QueryRow(ctx, `
		UPDATE users
		SET username = $2, first_name = $3
		WHERE id = $1
		RETURNING `+userColumns,
		userID, username, firstName))
}

func (r *Accounts) State(ctx context.Context, userID int64) (domain.AccountState, error) {
	var state domain.AccountState
	err := r.pool.QueryRow(ctx, `
		SELECT
			EXISTS (SELECT 1 FROM password_credentials WHERE user_id = $1),
			EXISTS (SELECT 1 FROM auth_identities WHERE user_id = $1 AND provider = 'telegram'),
			COALESCE((SELECT push_enabled FROM notification_preferences WHERE user_id = $1), false),
			COALESCE((SELECT telegram_enabled FROM notification_preferences WHERE user_id = $1), true)`,
		userID).Scan(
		&state.WebLoginEnabled,
		&state.TelegramLinked,
		&state.PushEnabled,
		&state.TelegramNotificationsEnabled,
	)
	return state, err
}

func tokenDigest(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

func (r *Accounts) CreateSession(
	ctx context.Context,
	userID int64,
	raw string,
	expiresAt time.Time,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO auth_sessions (id, user_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4)`,
		uuid.New(), userID, tokenDigest(raw), expiresAt)
	return err
}

func (r *Accounts) RotateSession(
	ctx context.Context,
	current, next string,
	nextExpiresAt time.Time,
) (domain.User, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, err
	}
	defer tx.Rollback(ctx)
	var userID int64
	err = tx.QueryRow(ctx, `
		UPDATE auth_sessions
		SET revoked_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now()
		RETURNING user_id`,
		tokenDigest(current)).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, ErrNotFound
	}
	if err != nil {
		return domain.User{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO auth_sessions (id, user_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4)`,
		uuid.New(), userID, tokenDigest(next), nextExpiresAt); err != nil {
		return domain.User{}, err
	}
	user, err := scanUser(tx.QueryRow(ctx,
		"SELECT "+userColumns+" FROM users WHERE id = $1", userID))
	if err != nil {
		return domain.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.User{}, err
	}
	return user, nil
}

func (r *Accounts) RevokeSession(ctx context.Context, raw string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE auth_sessions SET revoked_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL`,
		tokenDigest(raw))
	return err
}

func (r *Accounts) CreateLinkToken(
	ctx context.Context,
	userID int64,
	raw string,
	expiresAt time.Time,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO telegram_link_tokens (token_hash, user_id, expires_at)
		VALUES ($1, $2, $3)`,
		tokenDigest(raw), userID, expiresAt)
	return err
}

func (r *Accounts) LinkTelegram(
	ctx context.Context,
	raw string,
	telegramID int64,
) (domain.User, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, err
	}
	defer tx.Rollback(ctx)
	var userID int64
	err = tx.QueryRow(ctx, `
		UPDATE telegram_link_tokens
		SET used_at = now()
		WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()
		RETURNING user_id`,
		tokenDigest(raw)).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, ErrNotFound
	}
	if err != nil {
		return domain.User{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO auth_identities (provider, subject, user_id)
		VALUES ('telegram', $1, $2)`,
		strconv.FormatInt(telegramID, 10), userID); err != nil {
		return domain.User{}, err
	}
	user, err := scanUser(tx.QueryRow(ctx,
		"SELECT "+userColumns+" FROM users WHERE id = $1", userID))
	if err != nil {
		return domain.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.User{}, err
	}
	return user, nil
}

func (r *Accounts) SetCustomAvatar(
	ctx context.Context,
	userID int64,
	key string,
) (domain.User, string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, "", err
	}
	defer tx.Rollback(ctx)
	var oldKey string
	if err := tx.QueryRow(ctx,
		"SELECT avatar_key FROM users WHERE id = $1 FOR UPDATE", userID).Scan(&oldKey); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, "", ErrNotFound
		}
		return domain.User{}, "", err
	}
	user, err := scanUser(tx.QueryRow(ctx, `
		UPDATE users
		SET avatar_key = $2, avatar_source = '', avatar_custom = true
		WHERE id = $1
		RETURNING `+userColumns,
		userID, key))
	if err != nil {
		return domain.User{}, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.User{}, "", err
	}
	return user, oldKey, nil
}

func (r *Accounts) ClearCustomAvatar(
	ctx context.Context,
	userID int64,
) (domain.User, string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, "", err
	}
	defer tx.Rollback(ctx)
	var oldKey string
	if err := tx.QueryRow(ctx,
		"SELECT avatar_key FROM users WHERE id = $1 FOR UPDATE", userID).Scan(&oldKey); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, "", ErrNotFound
		}
		return domain.User{}, "", err
	}
	user, err := scanUser(tx.QueryRow(ctx, `
		UPDATE users
		SET avatar_key = '', avatar_source = '', avatar_custom = false
		WHERE id = $1
		RETURNING `+userColumns,
		userID))
	if err != nil {
		return domain.User{}, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.User{}, "", err
	}
	return user, oldKey, nil
}

func (r *Accounts) UpsertPushSubscription(
	ctx context.Context,
	userID int64,
	endpoint, p256dh, auth string,
) (domain.PushSubscription, error) {
	var sub domain.PushSubscription
	err := r.pool.QueryRow(ctx, `
		INSERT INTO push_subscriptions (id, user_id, endpoint, p256dh, auth)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (endpoint) DO UPDATE
		SET user_id = excluded.user_id,
		    p256dh = excluded.p256dh,
		    auth = excluded.auth,
		    updated_at = now()
		RETURNING id::text, endpoint, p256dh, auth`,
		uuid.New(), userID, endpoint, p256dh, auth).
		Scan(&sub.ID, &sub.Endpoint, &sub.P256DH, &sub.Auth)
	return sub, err
}

func (r *Accounts) DeletePushSubscription(ctx context.Context, userID int64, id string) error {
	tag, err := r.pool.Exec(ctx,
		"DELETE FROM push_subscriptions WHERE id = $1 AND user_id = $2", id, userID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (r *Accounts) UpdateNotificationPreferences(
	ctx context.Context,
	userID int64,
	push, telegram *bool,
) (domain.AccountState, error) {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO notification_preferences (user_id, push_enabled, telegram_enabled)
		VALUES ($1, COALESCE($2, false), COALESCE($3, true))
		ON CONFLICT (user_id) DO UPDATE
		SET push_enabled = COALESCE($2, notification_preferences.push_enabled),
		    telegram_enabled = COALESCE($3, notification_preferences.telegram_enabled)`,
		userID, push, telegram)
	if err != nil {
		return domain.AccountState{}, err
	}
	return r.State(ctx, userID)
}
