// Package grpcserver implements CoreInternalService consumed by checkin-service.
package grpcserver

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pbv1 "github.com/kosttiik/BuddyGym/core-service/internal/pb/buddygym/v1"

	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
	"github.com/kosttiik/BuddyGym/core-service/internal/storage"
)

type UsersRepo interface {
	Grant(ctx context.Context, userID int64, keys []string) ([]string, error)
	SetRank(ctx context.Context, id int64, rank string) error
	Stats(ctx context.Context, userID int64) (domain.Stats, error)
}

type RoomsRepo interface {
	Get(ctx context.Context, id int64) (domain.Room, error)
	MemberIDs(ctx context.Context, roomID int64) ([]int64, error)
}

type BuddiesRepo interface {
	UserIDs(ctx context.Context, checkinID string) ([]int64, error)
}

type ResultsRepo interface {
	Apply(ctx context.Context, checkinID string, roomID, userID int64, status string, createdAt time.Time) (bool, error)
	StreaksByUser(ctx context.Context, userID int64) ([]domain.StreakInput, error)
	PeriodCount(ctx context.Context, roomID, userID int64) (int, error)
}

type EventsRepo interface {
	Add(ctx context.Context, eventType string, roomID, actorID int64, subject map[string]any) error
}

type Server struct {
	pbv1.UnimplementedCoreInternalServiceServer
	users   UsersRepo
	rooms   RoomsRepo
	results ResultsRepo
	buddies BuddiesRepo
	events  EventsRepo
	log     *slog.Logger
	now     func() time.Time
}

func New(users UsersRepo, rooms RoomsRepo, results ResultsRepo, buddies BuddiesRepo, events EventsRepo, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{users: users, rooms: rooms, results: results, buddies: buddies, events: events, log: log, now: time.Now}
}

func (s *Server) emit(ctx context.Context, eventType string, roomID, actorID int64, subject map[string]any) {
	if s.events == nil {
		return
	}
	if err := s.events.Add(ctx, eventType, roomID, actorID, subject); err != nil {
		s.log.Error("emit event", "err", err, "type", eventType)
	}
}

var resultNames = map[pbv1.CheckinStatus]string{
	pbv1.CheckinStatus_CHECKIN_STATUS_APPROVED: storage.ResultApproved,
	pbv1.CheckinStatus_CHECKIN_STATUS_REJECTED: storage.ResultRejected,
	pbv1.CheckinStatus_CHECKIN_STATUS_EXPIRED:  storage.ResultExpired,
}

func (s *Server) ApplyCheckinResult(ctx context.Context, req *pbv1.ApplyCheckinResultRequest) (*pbv1.ApplyCheckinResultResponse, error) {
	if req.GetCheckinId() == "" || req.GetRoomId() <= 0 || req.GetUserId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "checkin_id, room_id and user_id are required")
	}
	result, ok := resultNames[req.GetStatus()]
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "status must be a final one")
	}

	createdAt := s.now()
	if ts := req.GetCheckinCreatedAt(); ts.IsValid() {
		createdAt = ts.AsTime()
	}

	applied, err := s.results.Apply(ctx, req.GetCheckinId(), req.GetRoomId(), req.GetUserId(), result, createdAt)
	if err != nil {
		s.log.Error("apply checkin result", "err", err, "checkin_id", req.GetCheckinId())
		return nil, status.Error(codes.Internal, "apply failed")
	}

	var granted []string
	if applied && result == storage.ResultApproved {
		granted, err = s.reward(ctx, req.GetUserId())
		if err != nil {
			s.log.Error("grant rewards", "err", err, "user_id", req.GetUserId())
		}
		if err := s.creditBuddies(ctx, req.GetCheckinId(), req.GetRoomId(), createdAt); err != nil {
			s.log.Error("credit buddies", "err", err, "checkin_id", req.GetCheckinId())
		}
	}
	count, err := s.results.PeriodCount(ctx, req.GetRoomId(), req.GetUserId())
	if err != nil {
		s.log.Error("period count", "err", err)
		return nil, status.Error(codes.Internal, "apply failed")
	}

	if applied {
		// the bot draws a progress bar on the verdict card, so the goal travels with the event
		goal := 0
		if room, err := s.rooms.Get(ctx, req.GetRoomId()); err == nil {
			goal = room.GoalPerPeriod
		}
		s.emit(ctx, "checkin."+result, req.GetRoomId(), req.GetUserId(), map[string]any{
			"checkin_id": req.GetCheckinId(),
			"granted":    granted,
			"done":       count,
			"goal":       goal,
		})
	}
	return &pbv1.ApplyCheckinResultResponse{
		WorkoutsCount:       int32(count),
		GrantedAchievements: granted,
	}, nil
}

func (s *Server) creditBuddies(ctx context.Context, checkinID string, roomID int64, createdAt time.Time) error {
	buddyIDs, err := s.buddies.UserIDs(ctx, checkinID)
	if err != nil {
		return err
	}
	for _, buddyID := range buddyIDs {
		id := checkinID + "#buddy:" + strconv.FormatInt(buddyID, 10)
		applied, err := s.results.Apply(ctx, id, roomID, buddyID, storage.ResultApproved, createdAt)
		if err != nil {
			return err
		}
		if !applied {
			continue
		}
		if _, err := s.reward(ctx, buddyID); err != nil {
			s.log.Error("grant rewards", "err", err, "user_id", buddyID)
		}
		s.emit(ctx, "buddy.credited", roomID, buddyID, map[string]any{"checkin_id": checkinID})
	}
	return nil
}

func (s *Server) reward(ctx context.Context, userID int64) ([]string, error) {
	stats, err := s.users.Stats(ctx, userID)
	if err != nil {
		return nil, err
	}
	streaks, err := s.results.StreaksByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	stats.BestStreak = domain.BestStreak(streaks, s.now())

	granted, err := s.users.Grant(ctx, userID, domain.EarnedAchievements(stats))
	if err != nil {
		return nil, err
	}
	if err := s.users.SetRank(ctx, userID, domain.RankFor(stats.TotalWorkouts)); err != nil {
		return granted, err
	}
	return granted, nil
}

func (s *Server) GetRoomVerification(ctx context.Context, req *pbv1.GetRoomVerificationRequest) (*pbv1.GetRoomVerificationResponse, error) {
	room, err := s.rooms.Get(ctx, req.GetRoomId())
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "room not found")
		}
		s.log.Error("get room", "err", err)
		return nil, status.Error(codes.Internal, "lookup failed")
	}
	ids, err := s.rooms.MemberIDs(ctx, room.ID)
	if err != nil {
		s.log.Error("member ids", "err", err)
		return nil, status.Error(codes.Internal, "lookup failed")
	}
	return &pbv1.GetRoomVerificationResponse{
		MemberIds:     ids,
		VotesRequired: int32(room.VotesRequired),
	}, nil
}
