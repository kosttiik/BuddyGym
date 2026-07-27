// Package httpapi is the REST API consumed by the mini app frontend.
package httpapi

import (
	"context"
	"io"
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
	UpdateLanguage(ctx context.Context, id int64, language string) (domain.User, error)
	UpdateTimezone(ctx context.Context, id int64, timezone string) (domain.User, error)
	SetStatus(ctx context.Context, id int64, emoji, text string) (domain.User, error)
	Achievements(ctx context.Context, userID int64) ([]domain.Achievement, error)
	Stats(ctx context.Context, userID int64) (domain.Stats, error)
}

type AccountsRepo interface {
	UserByTelegram(ctx context.Context, telegramID int64) (domain.User, error)
	CreateTelegramUser(ctx context.Context, telegramID int64, username, firstName, photoURL string) (domain.User, error)
	CreateLocalUser(ctx context.Context, username, firstName, passwordHash string) (domain.User, error)
	PasswordUser(ctx context.Context, username string) (domain.User, string, error)
	CredentialHash(ctx context.Context, userID int64) (string, error)
	SetCredentials(ctx context.Context, userID int64, username, passwordHash string) (domain.User, error)
	UpdateProfile(ctx context.Context, userID int64, username, firstName string) (domain.User, error)
	State(ctx context.Context, userID int64) (domain.AccountState, error)
	CreateSession(ctx context.Context, userID int64, raw string, expiresAt time.Time) error
	RotateSession(ctx context.Context, current, next string, nextExpiresAt time.Time) (domain.User, error)
	RevokeSession(ctx context.Context, raw string) error
	CreateLinkToken(ctx context.Context, userID int64, raw string, expiresAt time.Time) error
	LinkTelegram(ctx context.Context, raw string, telegramID int64) (domain.User, error)
	SetCustomAvatar(ctx context.Context, userID int64, key string) (domain.User, string, error)
	ClearCustomAvatar(ctx context.Context, userID int64) (domain.User, string, error)
	UpsertPushSubscription(ctx context.Context, userID int64, endpoint, p256dh, auth string) (domain.PushSubscription, error)
	DeletePushSubscription(ctx context.Context, userID int64, id string) error
	UpdateNotificationPreferences(ctx context.Context, userID int64, push, telegram *bool) (domain.AccountState, error)
}

type RoomsRepo interface {
	Create(ctx context.Context, room domain.Room) (domain.Room, error)
	Get(ctx context.Context, id int64) (domain.Room, error)
	GetByInvite(ctx context.Context, code string) (domain.Room, error)
	Update(ctx context.Context, room domain.Room) (domain.Room, error)
	Delete(ctx context.Context, id int64) error
	ListByUser(ctx context.Context, userID int64) ([]domain.RoomWithProgress, error)
	ListOpen(ctx context.Context, userID int64) ([]domain.Room, error)
	Members(ctx context.Context, roomID int64) ([]domain.Member, error)
	IsMember(ctx context.Context, roomID, userID int64) (bool, error)
	UpdateMembership(ctx context.Context, roomID, userID int64, sportName, sportEmoji string, goal *int) error
	Join(ctx context.Context, roomID, userID int64) error
	Leave(ctx context.Context, roomID, userID int64) error
	AddAvatar(ctx context.Context, roomID, userID int64, key string) (domain.RoomAvatar, error)
	ListAvatars(ctx context.Context, roomID int64) ([]domain.RoomAvatar, error)
	GetAvatar(ctx context.Context, roomID, avatarID int64) (domain.RoomAvatar, error)
	DeleteAvatar(ctx context.Context, roomID, avatarID int64) (string, error)
}

type AvatarStore interface {
	Open(ctx context.Context, key string) (io.ReadCloser, string, error)
	Put(ctx context.Context, key string, data []byte) error
	Delete(ctx context.Context, key string) error
}

type AvatarMirror interface {
	SyncInBackground(userID, telegramID int64, photoURL, mirroredFrom string)
}

type BuddiesRepo interface {
	Tag(ctx context.Context, checkinID string, roomID, authorID int64, userIDs []int64) error
	Untag(ctx context.Context, checkinID string, userID int64) error
	ForCheckins(ctx context.Context, checkinIDs []string) (map[string][]domain.User, error)
}

type CommentsRepo interface {
	Add(ctx context.Context, checkinID string, roomID, userID int64, body, photoKey string, replyTo *int64) (domain.Comment, error)
	Get(ctx context.Context, id, viewerID int64) (domain.Comment, error)
	List(ctx context.Context, checkinID string, viewerID int64, limit, offset int) ([]domain.Comment, error)
	Delete(ctx context.Context, id, userID int64) (photoKey string, err error)
	Like(ctx context.Context, commentID, userID int64) error
	Unlike(ctx context.Context, commentID, userID int64) error
	Summaries(ctx context.Context, checkinIDs []string, viewerID int64) (map[string]domain.CommentSummary, error)
}

type ObjectStore interface {
	Put(ctx context.Context, key string, data []byte) error
	Open(ctx context.Context, key string) (io.ReadCloser, string, error)
	Delete(ctx context.Context, key string) error
}

type StreaksRepo interface {
	StreaksByRoom(ctx context.Context, roomID int64) ([]domain.StreakInput, error)
	StreaksByUser(ctx context.Context, userID int64) ([]domain.StreakInput, error)
}

type CheckinClient interface {
	Create(ctx context.Context, userID int64, targets []checkin.Target, photo []byte, geo *checkin.Geo) ([]checkin.Checkin, error)
	Get(ctx context.Context, id string) (checkin.Checkin, error)
	List(ctx context.Context, roomID int64, status pbv1.CheckinStatus, limit, offset int32) ([]checkin.Checkin, error)
	Vote(ctx context.Context, checkinID string, voterID int64, approve bool) (checkin.Checkin, error)
	Cancel(ctx context.Context, checkinID string, userID int64) (checkin.Checkin, error)
	OpenPhoto(ctx context.Context, checkinID string) (checkin.Photo, error)
	SyncVotesRequired(ctx context.Context, roomID int64, votesRequired int) error
}

type FreezesRepo interface {
	Create(ctx context.Context, roomID, userID int64, startsAt, endsAt time.Time) (domain.Freeze, error)
	Cancel(ctx context.Context, roomID, userID int64, at time.Time) error
	ListByMember(ctx context.Context, roomID, userID int64) ([]domain.Freeze, error)
}

type EventsRepo interface {
	Add(ctx context.Context, eventType string, roomID, actorID int64, subject map[string]any) error
}

type PingFunc func(ctx context.Context) error

type RateLimiter interface {
	Allow(ctx context.Context, key string) (bool, error)
}

type Server struct {
	users          UsersRepo
	accounts       AccountsRepo
	rooms          RoomsRepo
	freezes        FreezesRepo
	events         EventsRepo
	streaks        StreaksRepo
	buddies        BuddiesRepo
	comments       CommentsRepo
	commentPhotos  ObjectStore
	checkins       CheckinClient
	avatars        AvatarStore
	avatarMirror   AvatarMirror
	botToken       string
	authTTL        time.Duration
	jwtSecret      []byte
	jwtTTL         time.Duration
	refreshTTL     time.Duration
	vapidPublicKey string
	authLimiter    RateLimiter
	apiLimiter     RateLimiter
	checkinLimiter RateLimiter
	dbPing         PingFunc
	redisPing      PingFunc
	log            *slog.Logger
	now            func() time.Time
}

type Options struct {
	Users          UsersRepo
	Accounts       AccountsRepo
	Rooms          RoomsRepo
	Freezes        FreezesRepo
	Events         EventsRepo
	Streaks        StreaksRepo
	Buddies        BuddiesRepo
	Comments       CommentsRepo
	CommentPhotos  ObjectStore
	Checkins       CheckinClient
	Avatars        AvatarStore
	AvatarMirror   AvatarMirror
	BotToken       string
	AuthTTL        time.Duration
	JWTSecret      []byte
	JWTTTL         time.Duration
	RefreshTTL     time.Duration
	VAPIDPublicKey string
	AuthLimiter    RateLimiter
	APILimiter     RateLimiter
	CheckinLimiter RateLimiter
	DBPing         PingFunc
	RedisPing      PingFunc
	Log            *slog.Logger
	Now            func() time.Time
}

func New(opts Options) *Server {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	return &Server{
		users:          opts.Users,
		accounts:       opts.Accounts,
		rooms:          opts.Rooms,
		freezes:        opts.Freezes,
		events:         opts.Events,
		streaks:        opts.Streaks,
		buddies:        opts.Buddies,
		comments:       opts.Comments,
		commentPhotos:  opts.CommentPhotos,
		checkins:       opts.Checkins,
		avatars:        opts.Avatars,
		avatarMirror:   opts.AvatarMirror,
		botToken:       opts.BotToken,
		authTTL:        opts.AuthTTL,
		jwtSecret:      opts.JWTSecret,
		jwtTTL:         opts.JWTTTL,
		refreshTTL:     opts.RefreshTTL,
		vapidPublicKey: opts.VAPIDPublicKey,
		authLimiter:    opts.AuthLimiter,
		apiLimiter:     opts.APILimiter,
		checkinLimiter: opts.CheckinLimiter,
		dbPing:         opts.DBPing,
		redisPing:      opts.RedisPing,
		log:            opts.Log,
		now:            opts.Now,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("POST /api/v1/auth/telegram", s.handleAuthTelegram)
	mux.HandleFunc("POST /api/v1/auth/register", s.handleAuthRegister)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleAuthLogin)
	mux.HandleFunc("POST /api/v1/auth/refresh", s.handleAuthRefresh)
	mux.HandleFunc("POST /api/v1/auth/logout", s.handleAuthLogout)
	mux.HandleFunc("GET /api/v1/push/public-key", s.withAuth(s.handlePushPublicKey))

	mux.HandleFunc("GET /api/v1/me", s.withAuth(s.handleGetMe))
	mux.HandleFunc("PATCH /api/v1/me", s.withAuth(s.handlePatchMe))
	mux.HandleFunc("PATCH /api/v1/me/credentials", s.withAuth(s.handleSetCredentials))
	mux.HandleFunc("POST /api/v1/me/telegram-link", s.withAuth(s.handleCreateTelegramLink))
	mux.HandleFunc("PUT /api/v1/me/avatar", s.withAuth(s.handlePutMyAvatar))
	mux.HandleFunc("DELETE /api/v1/me/avatar", s.withAuth(s.handleDeleteMyAvatar))
	mux.HandleFunc("POST /api/v1/me/push-subscriptions", s.withAuth(s.handleUpsertPushSubscription))
	mux.HandleFunc("DELETE /api/v1/me/push-subscriptions/{id}", s.withAuth(s.handleDeletePushSubscription))
	mux.HandleFunc("PATCH /api/v1/me/notification-preferences", s.withAuth(s.handleNotificationPreferences))

	mux.HandleFunc("GET /api/v1/users/{id}", s.withAuth(s.handleGetUser))
	mux.HandleFunc("GET /api/v1/users/{id}/avatar", s.withAuth(s.handleGetAvatar))

	mux.HandleFunc("POST /api/v1/rooms", s.withAuth(s.handleCreateRoom))
	mux.HandleFunc("GET /api/v1/rooms", s.withAuth(s.handleListRooms))
	mux.HandleFunc("GET /api/v1/rooms/open", s.withAuth(s.handleListOpenRooms))
	mux.HandleFunc("GET /api/v1/rooms/{id}", s.withAuth(s.handleGetRoom))
	mux.HandleFunc("PATCH /api/v1/rooms/{id}", s.withAuth(s.handleUpdateRoom))
	mux.HandleFunc("DELETE /api/v1/rooms/{id}", s.withAuth(s.handleDeleteRoom))
	mux.HandleFunc("GET /api/v1/rooms/{id}/avatar", s.withAuth(s.handleGetRoomAvatar))
	mux.HandleFunc("PUT /api/v1/rooms/{id}/avatar", s.withAuth(s.handleAddRoomAvatar))
	mux.HandleFunc("GET /api/v1/rooms/{id}/avatars", s.withAuth(s.handleListRoomAvatars))
	mux.HandleFunc("GET /api/v1/rooms/{id}/avatars/{avatarId}", s.withAuth(s.handleGetRoomAvatarByID))
	mux.HandleFunc("DELETE /api/v1/rooms/{id}/avatars/{avatarId}", s.withAuth(s.handleDeleteRoomAvatar))
	mux.HandleFunc("PATCH /api/v1/rooms/{id}/membership", s.withAuth(s.handleUpdateMembership))
	mux.HandleFunc("POST /api/v1/rooms/{id}/freeze", s.withAuth(s.handleCreateFreeze))
	mux.HandleFunc("DELETE /api/v1/rooms/{id}/freeze", s.withAuth(s.handleCancelFreeze))
	mux.HandleFunc("POST /api/v1/rooms/join", s.withAuth(s.handleJoinByCode))
	mux.HandleFunc("POST /api/v1/rooms/{id}/join", s.withAuth(s.handleJoinRoom))
	mux.HandleFunc("POST /api/v1/rooms/{id}/leave", s.withAuth(s.handleLeaveRoom))

	mux.HandleFunc("POST /api/v1/checkins", s.withAuth(s.handleCreateCheckin))
	mux.HandleFunc("GET /api/v1/rooms/{id}/checkins", s.withAuth(s.handleListCheckins))
	mux.HandleFunc("GET /api/v1/checkins/{id}/photo", s.withAuth(s.handleGetCheckinPhoto))
	mux.HandleFunc("POST /api/v1/checkins/{id}/vote", s.withAuth(s.handleVote))
	mux.HandleFunc("POST /api/v1/checkins/{id}/buddies", s.withAuth(s.handleAddBuddies))
	mux.HandleFunc("DELETE /api/v1/checkins/{id}/buddies/{userId}", s.withAuth(s.handleRemoveBuddy))
	mux.HandleFunc("GET /api/v1/checkins/{id}/comments", s.withAuth(s.handleListComments))
	mux.HandleFunc("POST /api/v1/checkins/{id}/comments", s.withAuth(s.handleAddComment))
	mux.HandleFunc("DELETE /api/v1/checkins/{id}/comments/{commentId}", s.withAuth(s.handleDeleteComment))
	mux.HandleFunc("POST /api/v1/checkins/{id}/comments/{commentId}/like", s.withAuth(s.handleLikeComment))
	mux.HandleFunc("DELETE /api/v1/checkins/{id}/comments/{commentId}/like", s.withAuth(s.handleUnlikeComment))
	mux.HandleFunc("GET /api/v1/checkins/{id}/comments/{commentId}/photo", s.withAuth(s.handleGetCommentPhoto))

	return s.withLogging(mux)
}
