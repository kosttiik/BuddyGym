package storage

import (
	"context"
	"errors"

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

const roomColumns = "id, name, kind, invite_code, goal_per_period, period_days, votes_required, creator_id, created_at"

// counter is zeroed on read once the room period has rolled over;
// the row itself is reset lazily on the next approved checkin
const periodAwareCount = `
	CASE WHEN now() >= m.period_start + r.period_days * interval '1 day'
	     THEN 0 ELSE m.workouts_count END`

func scanRoom(row pgx.Row) (domain.Room, error) {
	var rm domain.Room
	err := row.Scan(&rm.ID, &rm.Name, &rm.Kind, &rm.InviteCode, &rm.GoalPerPeriod,
		&rm.PeriodDays, &rm.VotesRequired, &rm.CreatorID, &rm.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Room{}, ErrNotFound
	}
	return rm, err
}

// Create inserts the room and enrolls the creator in one transaction.
// Invite code generation retries on the rare unique collision.
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
		"SELECT "+roomColumns+" FROM rooms WHERE id = $1", id))
}

func (r *Rooms) GetByInvite(ctx context.Context, code string) (domain.Room, error) {
	return scanRoom(r.pool.QueryRow(ctx,
		"SELECT "+roomColumns+" FROM rooms WHERE invite_code = $1", code))
}

func (r *Rooms) ListByUser(ctx context.Context, userID int64) ([]domain.RoomWithProgress, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+roomColumns+`, `+periodAwareCount+`,
		       (SELECT count(*) FROM memberships m2 WHERE m2.room_id = r.id)
		FROM memberships m
		JOIN rooms r ON r.id = m.room_id
		WHERE m.user_id = $1
		ORDER BY m.joined_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.RoomWithProgress
	for rows.Next() {
		var rp domain.RoomWithProgress
		if err := rows.Scan(&rp.ID, &rp.Name, &rp.Kind, &rp.InviteCode, &rp.GoalPerPeriod,
			&rp.PeriodDays, &rp.VotesRequired, &rp.CreatorID, &rp.CreatedAt,
			&rp.WorkoutsCount, &rp.MembersCount); err != nil {
			return nil, err
		}
		out = append(out, rp)
	}
	return out, rows.Err()
}

func (r *Rooms) Members(ctx context.Context, roomID int64) ([]domain.Member, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT u.id, u.username, u.first_name, u.photo_url, u.theme, u.status, u.created_at,
		       `+periodAwareCount+`, m.joined_at
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
			&mb.Status, &mb.CreatedAt, &mb.WorkoutsCount, &mb.JoinedAt); err != nil {
			return nil, err
		}
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

// Join is idempotent: joining a room twice is not an error.
func (r *Rooms) Join(ctx context.Context, roomID, userID int64) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO memberships (room_id, user_id)
		VALUES ($1, $2) ON CONFLICT DO NOTHING`, roomID, userID)
	return err
}

func (r *Rooms) Leave(ctx context.Context, roomID, userID int64) error {
	tag, err := r.pool.Exec(ctx,
		"DELETE FROM memberships WHERE room_id = $1 AND user_id = $2", roomID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
