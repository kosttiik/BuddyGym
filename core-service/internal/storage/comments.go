package storage

import (
	"context"
	"errors"
	"time"

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

const commentColumns = `
	c.id, c.checkin_id, c.user_id, c.body, c.photo_key, c.created_at,
	u.first_name, u.username, u.photo_url, u.avatar_key,
	(SELECT count(*)::int FROM checkin_comment_likes l WHERE l.comment_id = c.id),
	EXISTS (SELECT 1 FROM checkin_comment_likes l WHERE l.comment_id = c.id AND l.user_id = $2),
	c.reply_to,
	COALESCE(p.first_name, ''), COALESCE(p.id, 0), COALESCE(pc.body, ''),
	(pc.photo_key IS NOT NULL AND pc.photo_key <> '')`

func scanComment(row pgx.Row) (domain.Comment, error) {
	var c domain.Comment
	var parentPhoto bool
	err := row.Scan(&c.ID, &c.CheckinID, &c.UserID, &c.Body, &c.PhotoKey, &c.CreatedAt,
		&c.Author.FirstName, &c.Author.Username, &c.Author.PhotoURL, &c.Author.AvatarKey,
		&c.Likes, &c.LikedByMe,
		&c.ReplyTo, &c.ReplyToAuthor, &c.ReplyToAuthorID, &c.ReplyToBody, &parentPhoto)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Comment{}, ErrNotFound
	}
	if c.ReplyTo != nil && c.ReplyToBody == "" && parentPhoto {
		c.ReplyToBody = "photo"
	}
	c.Author.ID = c.UserID
	c.Author.HasAvatar = c.Author.AvatarKey != ""
	c.HasPhoto = c.PhotoKey != ""
	return c, err
}

func (r *Comments) Add(ctx context.Context, checkinID string, roomID, userID int64, body, photoKey string, replyTo *int64) (domain.Comment, error) {
	var id int64
	// a reply must stay inside the thread it answers, else it would quote a stranger's photo
	err := r.pool.QueryRow(ctx, `
		INSERT INTO checkin_comments (checkin_id, room_id, user_id, body, photo_key, reply_to)
		SELECT $1, $2, $3, $4, $5, $6
		WHERE $6::bigint IS NULL
		   OR EXISTS (SELECT 1 FROM checkin_comments p WHERE p.id = $6 AND p.checkin_id = $1)
		RETURNING id`,
		checkinID, roomID, userID, body, photoKey, replyTo).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Comment{}, ErrNotFound
	}
	if err != nil {
		return domain.Comment{}, err
	}
	return r.Get(ctx, id, userID)
}

func (r *Comments) Get(ctx context.Context, id, viewerID int64) (domain.Comment, error) {
	return scanComment(r.pool.QueryRow(ctx, `
		SELECT `+commentColumns+`
		FROM checkin_comments c JOIN users u ON u.id = c.user_id
		LEFT JOIN checkin_comments pc ON pc.id = c.reply_to
		LEFT JOIN users p ON p.id = pc.user_id
		WHERE c.id = $1`, id, viewerID))
}

func (r *Comments) List(ctx context.Context, checkinID string, viewerID int64, limit, offset int) ([]domain.Comment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+commentColumns+`
		FROM checkin_comments c JOIN users u ON u.id = c.user_id
		LEFT JOIN checkin_comments pc ON pc.id = c.reply_to
		LEFT JOIN users p ON p.id = pc.user_id
		WHERE c.checkin_id = $1
		ORDER BY c.created_at
		LIMIT $3 OFFSET $4`, checkinID, viewerID, limit, offset)
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

func (r *Comments) Delete(ctx context.Context, id, userID int64) (photoKey string, err error) {
	err = r.pool.QueryRow(ctx, `
		DELETE FROM checkin_comments c
		USING rooms r
		WHERE c.id = $1 AND r.id = c.room_id
		  AND (c.user_id = $2 OR r.creator_id = $2)
		RETURNING c.photo_key`, id, userID).Scan(&photoKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return photoKey, err
}

func (r *Comments) Like(ctx context.Context, commentID, userID int64) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO checkin_comment_likes (comment_id, user_id)
		VALUES ($1, $2) ON CONFLICT DO NOTHING`, commentID, userID)
	return err
}

func (r *Comments) Unlike(ctx context.Context, commentID, userID int64) error {
	_, err := r.pool.Exec(ctx,
		"DELETE FROM checkin_comment_likes WHERE comment_id = $1 AND user_id = $2",
		commentID, userID)
	return err
}

func (r *Comments) Summaries(ctx context.Context, checkinIDs []string, viewerID int64) (map[string]domain.CommentSummary, error) {
	out := map[string]domain.CommentSummary{}
	if len(checkinIDs) == 0 {
		return out, nil
	}

	rows, err := r.pool.Query(ctx, `
		SELECT checkin_id, count(*)::int
		FROM checkin_comments
		WHERE checkin_id = ANY($1)
		GROUP BY checkin_id`, checkinIDs)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return nil, err
		}
		out[id] = domain.CommentSummary{Count: n}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	top, err := r.pool.Query(ctx, `
		SELECT DISTINCT ON (c.checkin_id) `+commentColumns+`
		FROM checkin_comments c JOIN users u ON u.id = c.user_id
		LEFT JOIN checkin_comments pc ON pc.id = c.reply_to
		LEFT JOIN users p ON p.id = pc.user_id
		WHERE c.checkin_id = ANY($1)
		ORDER BY c.checkin_id,
		         (SELECT count(*) FROM checkin_comment_likes l WHERE l.comment_id = c.id) DESC,
		         c.created_at`, checkinIDs, viewerID)
	if err != nil {
		return nil, err
	}
	defer top.Close()

	for top.Next() {
		c, err := scanComment(top)
		if err != nil {
			return nil, err
		}
		summary := out[c.CheckinID]
		summary.Top = &c
		out[c.CheckinID] = summary
	}
	return out, top.Err()
}

func (r *Comments) ExpiredPhotos(ctx context.Context, before time.Time, limit int) ([]int64, []string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, photo_key FROM checkin_comments
		WHERE photo_key <> '' AND created_at < $1
		LIMIT $2`, before, limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var ids []int64
	var keys []string
	for rows.Next() {
		var id int64
		var key string
		if err := rows.Scan(&id, &key); err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
		keys = append(keys, key)
	}
	return ids, keys, rows.Err()
}

func (r *Comments) ClearPhotos(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx,
		"UPDATE checkin_comments SET photo_key = '' WHERE id = ANY($1)", ids)
	return err
}
