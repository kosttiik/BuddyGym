package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
)

type Rooms struct {
	pool *pgxpool.Pool
}

func NewRooms(pool *pgxpool.Pool) *Rooms {
	return &Rooms{pool: pool}
}

const roomColumns = "id, name, kind, invite_code, goal_per_period, period_days, votes_required, creator_id, created_at, avatar_key"

const roomColumnsR = "r.id, r.name, r.kind, r.invite_code, r.goal_per_period, r.period_days, r.votes_required, r.creator_id, r.created_at, r.avatar_key"

// period grid anchored on joined_at, the same grid domain.RoomStreak walks
const periodStartDate = `
	((m.joined_at AT TIME ZONE 'UTC')::date + (floor(
		(((now() AT TIME ZONE 'UTC')::date - (m.joined_at AT TIME ZONE 'UTC')::date))::numeric
		/ r.period_days
	)::int * r.period_days))`

const periodAwareCount = `(
	SELECT count(DISTINCT (cr.checkin_created_at AT TIME ZONE 'UTC')::date)::int
	FROM checkin_results cr
	WHERE cr.room_id = m.room_id AND cr.user_id = m.user_id AND cr.status = 'approved'
	  AND (cr.checkin_created_at AT TIME ZONE 'UTC')::date >= ` + periodStartDate + `
)`

const periodEndsAt = `((` + periodStartDate + ` + r.period_days)::timestamptz)`

const effectiveGoal = `COALESCE(m.goal_per_period, r.goal_per_period)`

func scanRoom(row pgx.Row) (domain.Room, error) {
	var rm domain.Room
	err := row.Scan(&rm.ID, &rm.Name, &rm.Kind, &rm.InviteCode, &rm.GoalPerPeriod,
		&rm.PeriodDays, &rm.VotesRequired, &rm.CreatorID, &rm.CreatedAt, &rm.AvatarKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Room{}, ErrNotFound
	}
	rm.HasAvatar = rm.AvatarKey != ""
	return rm, err
}

func (r *Rooms) Create(ctx context.Context, room domain.Room) (domain.Room, error) {
	for range 3 {
		room.InviteCode = domain.NewInviteCode()
		created, err := r.tryCreate(ctx, room)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "rooms_invite_code_key" {
			continue
		}
		return created, err
	}
	return domain.Room{}, errors.New("invite code collision")
}

func (r *Rooms) tryCreate(ctx context.Context, room domain.Room) (domain.Room, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Room{}, err
	}
	defer tx.Rollback(ctx)

	created, err := scanRoom(tx.QueryRow(ctx, `
		INSERT INTO rooms (name, kind, invite_code, goal_per_period, period_days, votes_required, creator_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+roomColumns,
		room.Name, room.Kind, room.InviteCode, room.GoalPerPeriod,
		room.PeriodDays, room.VotesRequired, room.CreatorID))
	if err != nil {
		return domain.Room{}, err
	}
	if _, err := tx.Exec(ctx,
		"INSERT INTO memberships (room_id, user_id) VALUES ($1, $2)",
		created.ID, room.CreatorID); err != nil {
		return domain.Room{}, err
	}
	return created, tx.Commit(ctx)
}

func (r *Rooms) Get(ctx context.Context, id int64) (domain.Room, error) {
	return scanRoom(r.pool.QueryRow(ctx,
		"SELECT "+roomColumns+" FROM rooms WHERE id = $1 AND deleted_at IS NULL", id))
}

func (r *Rooms) GetByInvite(ctx context.Context, code string) (domain.Room, error) {
	return scanRoom(r.pool.QueryRow(ctx,
		"SELECT "+roomColumns+" FROM rooms WHERE invite_code = $1 AND deleted_at IS NULL", code))
}

func (r *Rooms) Update(ctx context.Context, room domain.Room) (domain.Room, error) {
	return scanRoom(r.pool.QueryRow(ctx, `
		UPDATE rooms
		SET name = $2, kind = $3, goal_per_period = $4, period_days = $5, votes_required = $6
		WHERE id = $1
		RETURNING `+roomColumns,
		room.ID, room.Name, room.Kind, room.GoalPerPeriod, room.PeriodDays, room.VotesRequired))
}

func (r *Rooms) AddAvatar(ctx context.Context, roomID, userID int64, key string) (domain.RoomAvatar, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.RoomAvatar{}, err
	}
	defer tx.Rollback(ctx)

	var added domain.RoomAvatar
	err = tx.QueryRow(ctx, `
		INSERT INTO room_avatars (room_id, object_key, uploaded_by)
		VALUES ($1, $2, $3)
		RETURNING id, uploaded_by, created_at, object_key`,
		roomID, key, userID).Scan(&added.ID, &added.UploadedBy, &added.CreatedAt, &added.ObjectKey)
	if err != nil {
		return domain.RoomAvatar{}, err
	}

	tag, err := tx.Exec(ctx,
		"UPDATE rooms SET avatar_key = $2 WHERE id = $1 AND deleted_at IS NULL", roomID, key)
	if err != nil {
		return domain.RoomAvatar{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.RoomAvatar{}, ErrNotFound
	}

	added.IsCurrent = true
	return added, tx.Commit(ctx)
}

func (r *Rooms) ListAvatars(ctx context.Context, roomID int64) ([]domain.RoomAvatar, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT a.id, a.uploaded_by, a.created_at, a.object_key,
		       a.object_key = (SELECT avatar_key FROM rooms WHERE id = a.room_id)
		FROM room_avatars a
		WHERE a.room_id = $1
		ORDER BY a.created_at DESC, a.id DESC`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.RoomAvatar
	for rows.Next() {
		var a domain.RoomAvatar
		if err := rows.Scan(&a.ID, &a.UploadedBy, &a.CreatedAt, &a.ObjectKey, &a.IsCurrent); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Rooms) GetAvatar(ctx context.Context, roomID, avatarID int64) (domain.RoomAvatar, error) {
	var a domain.RoomAvatar
	err := r.pool.QueryRow(ctx, `
		SELECT id, uploaded_by, created_at, object_key
		FROM room_avatars WHERE room_id = $1 AND id = $2`, roomID, avatarID).
		Scan(&a.ID, &a.UploadedBy, &a.CreatedAt, &a.ObjectKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RoomAvatar{}, ErrNotFound
	}
	return a, err
}

func (r *Rooms) DeleteAvatar(ctx context.Context, roomID, avatarID int64) (string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var key string
	err = tx.QueryRow(ctx,
		"DELETE FROM room_avatars WHERE room_id = $1 AND id = $2 RETURNING object_key",
		roomID, avatarID).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE rooms SET avatar_key = COALESCE((
			SELECT object_key FROM room_avatars
			WHERE room_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1
		), '')
		WHERE id = $1 AND avatar_key = $2`, roomID, key); err != nil {
		return "", err
	}

	return key, tx.Commit(ctx)
}

func (r *Rooms) Delete(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, "DELETE FROM rooms WHERE id = $1", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Rooms) ListByUser(ctx context.Context, userID int64) ([]domain.RoomWithProgress, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+roomColumnsR+`, `+periodAwareCount+`,
		       (SELECT count(*) FROM memberships m2 WHERE m2.room_id = r.id),
		       `+periodEndsAt+`, `+effectiveGoal+`
		FROM memberships m
		JOIN rooms r ON r.id = m.room_id
		WHERE m.user_id = $1 AND r.deleted_at IS NULL
		ORDER BY m.joined_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.RoomWithProgress
	for rows.Next() {
		var rp domain.RoomWithProgress
		if err := rows.Scan(&rp.ID, &rp.Name, &rp.Kind, &rp.InviteCode, &rp.GoalPerPeriod,
			&rp.PeriodDays, &rp.VotesRequired, &rp.CreatorID, &rp.CreatedAt, &rp.AvatarKey,
			&rp.WorkoutsCount, &rp.MembersCount, &rp.PeriodEndsAt, &rp.MyGoal); err != nil {
			return nil, err
		}
		rp.HasAvatar = rp.AvatarKey != ""
		out = append(out, rp)
	}
	return out, rows.Err()
}

func (r *Rooms) ListOpen(ctx context.Context, userID int64) ([]domain.Room, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+roomColumns+`
		FROM rooms r
		WHERE r.kind = $1 AND r.deleted_at IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM memberships m WHERE m.room_id = r.id AND m.user_id = $2
		  )
		ORDER BY r.created_at DESC`, domain.RoomOpen, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Room
	for rows.Next() {
		var room domain.Room
		if err := rows.Scan(&room.ID, &room.Name, &room.Kind, &room.InviteCode, &room.GoalPerPeriod,
			&room.PeriodDays, &room.VotesRequired, &room.CreatorID, &room.CreatedAt,
			&room.AvatarKey); err != nil {
			return nil, err
		}
		room.HasAvatar = room.AvatarKey != ""
		room.InviteCode = ""
		out = append(out, room)
	}
	return out, rows.Err()
}

func (r *Rooms) Members(ctx context.Context, roomID int64) ([]domain.Member, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT u.id, u.username, u.first_name, u.photo_url, u.theme, u.rank,
		       u.status_emoji, u.status_text, u.created_at, u.avatar_key, `+periodAwareCount+`, m.joined_at, `+periodEndsAt+`,
		       m.sport_name, m.sport_emoji, m.goal_per_period, `+effectiveGoal+`
		FROM memberships m
		JOIN users u ON u.id = m.user_id
		JOIN rooms r ON r.id = m.room_id
		WHERE m.room_id = $1
		ORDER BY m.joined_at`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Member
	for rows.Next() {
		var mb domain.Member
		if err := rows.Scan(&mb.ID, &mb.Username, &mb.FirstName, &mb.PhotoURL, &mb.Theme,
			&mb.Rank, &mb.StatusEmoji, &mb.StatusText, &mb.CreatedAt, &mb.AvatarKey,
			&mb.WorkoutsCount, &mb.JoinedAt, &mb.PeriodEndsAt,
			&mb.SportName, &mb.SportEmoji, &mb.GoalPerPeriod, &mb.EffectiveGoal); err != nil {
			return nil, err
		}
		mb.HasAvatar = mb.AvatarKey != ""
		out = append(out, mb)
	}
	return out, rows.Err()
}

func (r *Rooms) MemberIDs(ctx context.Context, roomID int64) ([]int64, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT user_id FROM memberships WHERE room_id = $1 ORDER BY joined_at", roomID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

func (r *Rooms) IsMember(ctx context.Context, roomID, userID int64) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM memberships WHERE room_id = $1 AND user_id = $2)",
		roomID, userID).Scan(&ok)
	return ok, err
}

func (r *Rooms) UpdateMembership(ctx context.Context, roomID, userID int64, sportName, sportEmoji string, goal *int) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE memberships SET sport_name = $3, sport_emoji = $4, goal_per_period = $5
		WHERE room_id = $1 AND user_id = $2`,
		roomID, userID, sportName, sportEmoji, goal)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Rooms) Join(ctx context.Context, roomID, userID int64) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO memberships (room_id, user_id)
		VALUES ($1, $2) ON CONFLICT DO NOTHING`, roomID, userID)
	return err
}

func (r *Rooms) Leave(ctx context.Context, roomID, userID int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		"DELETE FROM memberships WHERE room_id = $1 AND user_id = $2", roomID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	if _, err := tx.Exec(ctx, `
		UPDATE rooms SET deleted_at = now()
		WHERE id = $1
		  AND deleted_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM memberships WHERE room_id = $1)`, roomID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *Rooms) ListDeletedBefore(ctx context.Context, cutoff time.Time, limit int) ([]int64, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT id FROM rooms WHERE deleted_at IS NOT NULL AND deleted_at <= $1 ORDER BY deleted_at LIMIT $2",
		cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *Rooms) Purge(ctx context.Context, roomID int64) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM rooms WHERE id = $1 AND deleted_at IS NOT NULL", roomID)
	return err
}
