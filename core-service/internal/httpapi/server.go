package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	pbv1 "github.com/kosttiik/BuddyGym/core-service/internal/pb/buddygym/v1"

	"github.com/kosttiik/BuddyGym/core-service/internal/checkin"
	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
)

type UsersRepo interface {
	Upsert(ctx context.Context, id int64, username, firstName, photoURL string) (domain.User, error)
	Get(ctx context.Context, id int64) (domain.User, error)
	UpdateTheme(ctx context.Context, id int64, theme string) (domain.User, error)
	Achievements(ctx context.Context, userID int64) ([]domain.Achievement, error)
}

type RoomsRepo interface {
	Create(ctx context.Context, room domain.Room) (domain.Room, error)
	Get(ctx context.Context, id int64) (domain.Room, error)
	GetByInvite(ctx context.Context, code string) (domain.Room, error)
	ListByUser(ctx context.Context, userID int64) ([]domain.RoomWithProgress, error)
	Members(ctx context.Context, roomID int64) ([]domain.Member, error)
	IsMember(ctx context.Context, roomID, userID int64) (bool, error)
	Join(ctx context.Context, roomID, userID int64) error
	Leave(ctx context.Context, roomID, userID int64) error
}

type CheckinClient interface {
	Create(ctx context.Context, roomID, userID int64, votesRequired int32, photo []byte, geo *checkin.Geo) (checkin.Checkin, error)
	Get(ctx context.Context, id string) (checkin.Checkin, error)
	List(ctx context.Context, roomID int64, status pbv1.CheckinStatus, limit, offset int32) ([]checkin.Checkin, error)
	Vote(ctx context.Context, checkinID string, voterID int64, approve bool) (checkin.Checkin, error)
}

type PingFunc func(ctx context.Context) error

type Server struct {
	users     UsersRepo
	rooms     RoomsRepo
	checkins  CheckinClient
	botToken  string
	authTTL   time.Duration
	dbPing    PingFunc
	redisPing PingFunc
	log       *slog.Logger
	now       func() time.Time
}

type Options struct {
	Users     UsersRepo
	Rooms     RoomsRepo
	Checkins  CheckinClient
	BotToken  string
	AuthTTL   time.Duration
	DBPing    PingFunc
	RedisPing PingFunc
	Log       *slog.Logger
	Now       func() time.Time
}

func New(opts Options) *Server {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	return &Server{
		users:     opts.Users,
		rooms:     opts.Rooms,
		checkins:  opts.Checkins,
		botToken:  opts.BotToken,
		authTTL:   opts.AuthTTL,
		dbPing:    opts.DBPing,
		redisPing: opts.RedisPing,
		log:       opts.Log,
		now:       opts.Now,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", s.handleHealth)

	mux.HandleFunc("GET /api/v1/me", s.withAuth(s.handleGetMe))
	mux.HandleFunc("PATCH /api/v1/me", s.withAuth(s.handlePatchMe))

	mux.HandleFunc("POST /api/v1/rooms", s.withAuth(s.handleCreateRoom))
	mux.HandleFunc("GET /api/v1/rooms", s.withAuth(s.handleListRooms))
	mux.HandleFunc("GET /api/v1/rooms/{id}", s.withAuth(s.handleGetRoom))
	mux.HandleFunc("POST /api/v1/rooms/join", s.withAuth(s.handleJoinByCode))
	mux.HandleFunc("POST /api/v1/rooms/{id}/join", s.withAuth(s.handleJoinRoom))
	mux.HandleFunc("POST /api/v1/rooms/{id}/leave", s.withAuth(s.handleLeaveRoom))

	mux.HandleFunc("POST /api/v1/rooms/{id}/checkins", s.withAuth(s.handleCreateCheckin))
	mux.HandleFunc("GET /api/v1/rooms/{id}/checkins", s.withAuth(s.handleListCheckins))
	mux.HandleFunc("POST /api/v1/checkins/{id}/vote", s.withAuth(s.handleVote))

	return s.withLogging(mux)
}
