package grpcserver_test

import (
	"context"
	"net"
	"slices"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"

	pbv1 "github.com/kosttiik/BuddyGym/core-service/internal/pb/buddygym/v1"

	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
	"github.com/kosttiik/BuddyGym/core-service/internal/grpcserver"
	"github.com/kosttiik/BuddyGym/core-service/internal/storage"
)

type fakeUsers struct {
	granted  map[int64][]string
	rank     map[int64]string
	workouts map[int64]int
}

func (f *fakeUsers) Grant(_ context.Context, userID int64, keys []string) ([]string, error) {
	var fresh []string
	for _, k := range keys {
		if !slices.Contains(f.granted[userID], k) {
			f.granted[userID] = append(f.granted[userID], k)
			fresh = append(fresh, k)
		}
	}
	return fresh, nil
}

func (f *fakeUsers) Stats(_ context.Context, userID int64) (domain.Stats, error) {
	return domain.Stats{TotalWorkouts: f.workouts[userID]}, nil
}

func (f *fakeUsers) SetRank(_ context.Context, id int64, rank string) error {
	f.rank[id] = rank
	return nil
}

type fakeRooms struct {
	rooms   map[int64]domain.Room
	members map[int64][]int64
}

func (f *fakeRooms) Get(_ context.Context, id int64) (domain.Room, error) {
	room, ok := f.rooms[id]
	if !ok {
		return domain.Room{}, storage.ErrNotFound
	}
	return room, nil
}

func (f *fakeRooms) MemberIDs(_ context.Context, roomID int64) ([]int64, error) {
	return f.members[roomID], nil
}

type resultKey struct {
	room, user int64
}

type fakeResults struct {
	users   *fakeUsers
	seen    map[string]bool
	counts  map[resultKey]int
	days    map[int64][]time.Time
	applied []time.Time
}

func (f *fakeResults) Apply(_ context.Context, checkinID string, roomID, userID int64, st string, createdAt time.Time) (bool, error) {
	if f.seen[checkinID] {
		return false, nil
	}
	f.seen[checkinID] = true
	if st == storage.ResultApproved {
		f.counts[resultKey{roomID, userID}]++
		f.users.workouts[userID]++
		f.days[userID] = append([]time.Time{createdAt}, f.days[userID]...)
		f.applied = append(f.applied, createdAt)
	}
	return true, nil
}

func (f *fakeResults) TotalApproved(_ context.Context, userID int64) (int, error) {
	return len(f.days[userID]), nil
}

func (f *fakeResults) StreaksByUser(_ context.Context, userID int64) ([]domain.StreakInput, error) {
	days := f.days[userID]
	if len(days) == 0 {
		return nil, nil
	}
	// one workout a day against a goal of one a day: the streak is the run of days
	return []domain.StreakInput{{
		RoomID: 1, UserID: userID, Goal: 1, PeriodDays: 1,
		JoinedAt: days[len(days)-1], Days: days,
	}}, nil
}

func (f *fakeResults) PeriodCount(_ context.Context, roomID, userID int64) (int, error) {
	return f.counts[resultKey{roomID, userID}], nil
}

type fakeBuddies struct {
	tagged map[string][]int64
}

func (f *fakeBuddies) UserIDs(_ context.Context, checkinID string) ([]int64, error) {
	return f.tagged[checkinID], nil
}

type env struct {
	users   *fakeUsers
	rooms   *fakeRooms
	results *fakeResults
	buddies *fakeBuddies
	client  pbv1.CoreInternalServiceClient
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{
		users:   &fakeUsers{granted: map[int64][]string{}, rank: map[int64]string{}, workouts: map[int64]int{}},
		rooms:   &fakeRooms{rooms: map[int64]domain.Room{}, members: map[int64][]int64{}},
		results: &fakeResults{seen: map[string]bool{}, counts: map[resultKey]int{}, days: map[int64][]time.Time{}},
		buddies: &fakeBuddies{tagged: map[string][]int64{}},
	}
	e.results.users = e.users
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	pbv1.RegisterCoreInternalServiceServer(srv, grpcserver.New(e.users, e.rooms, e.results, e.buddies, nil))
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	e.client = pbv1.NewCoreInternalServiceClient(conn)
	return e
}

func TestApplyCheckinResultValidation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	bad := []*pbv1.ApplyCheckinResultRequest{
		{RoomId: 1, UserId: 1, Status: pbv1.CheckinStatus_CHECKIN_STATUS_APPROVED},
		{CheckinId: "c1", UserId: 1, Status: pbv1.CheckinStatus_CHECKIN_STATUS_APPROVED},
		{CheckinId: "c1", RoomId: 1, Status: pbv1.CheckinStatus_CHECKIN_STATUS_APPROVED},
		{CheckinId: "c1", RoomId: 1, UserId: 1, Status: pbv1.CheckinStatus_CHECKIN_STATUS_PENDING},
		{CheckinId: "c1", RoomId: 1, UserId: 1, Status: pbv1.CheckinStatus_CHECKIN_STATUS_UNSPECIFIED},
	}
	for i, req := range bad {
		_, err := e.client.ApplyCheckinResult(ctx, req)
		if status.Code(err) != codes.InvalidArgument {
			t.Errorf("case %d: code = %v, want InvalidArgument", i, status.Code(err))
		}
	}
}

func TestApplyCheckinResultApproved(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	resp, err := e.client.ApplyCheckinResult(ctx, &pbv1.ApplyCheckinResultRequest{
		CheckinId: "c1", RoomId: 1, UserId: 7,
		Status: pbv1.CheckinStatus_CHECKIN_STATUS_APPROVED,
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if resp.WorkoutsCount != 1 {
		t.Errorf("workouts = %d, want 1", resp.WorkoutsCount)
	}
	if !slices.Contains(resp.GrantedAchievements, domain.AchFirstCheckin) {
		t.Errorf("granted = %v, want first_checkin", resp.GrantedAchievements)
	}
	if e.users.rank[7] != domain.RankNovice {
		t.Errorf("rank = %q", e.users.rank[7])
	}

	// same checkin again: idempotent, nothing granted, counter unchanged
	resp, err = e.client.ApplyCheckinResult(ctx, &pbv1.ApplyCheckinResultRequest{
		CheckinId: "c1", RoomId: 1, UserId: 7,
		Status: pbv1.CheckinStatus_CHECKIN_STATUS_APPROVED,
	})
	if err != nil {
		t.Fatalf("repeat apply: %v", err)
	}
	if resp.WorkoutsCount != 1 || len(resp.GrantedAchievements) != 0 {
		t.Errorf("repeat: count=%d granted=%v", resp.WorkoutsCount, resp.GrantedAchievements)
	}
}

func TestApplyCheckinResultUsesWorkoutTime(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	loggedAt := time.Date(2026, 3, 4, 23, 50, 0, 0, time.UTC)
	_, err := e.client.ApplyCheckinResult(ctx, &pbv1.ApplyCheckinResultRequest{
		CheckinId: "c1", RoomId: 1, UserId: 7,
		Status:           pbv1.CheckinStatus_CHECKIN_STATUS_APPROVED,
		CheckinCreatedAt: timestamppb.New(loggedAt),
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(e.results.applied) != 1 || !e.results.applied[0].Equal(loggedAt) {
		t.Fatalf("recorded %v, want the workout time %v", e.results.applied, loggedAt)
	}

	_, err = e.client.ApplyCheckinResult(ctx, &pbv1.ApplyCheckinResultRequest{
		CheckinId: "c2", RoomId: 1, UserId: 7,
		Status: pbv1.CheckinStatus_CHECKIN_STATUS_APPROVED,
	})
	if err != nil {
		t.Fatalf("apply without timestamp: %v", err)
	}
	if len(e.results.applied) != 2 || e.results.applied[1].IsZero() {
		t.Fatalf("fallback recorded %v, want a real time", e.results.applied)
	}
}

func TestApprovedCheckinCreditsTaggedBuddies(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.buddies.tagged["c1"] = []int64{8, 9}

	_, err := e.client.ApplyCheckinResult(ctx, &pbv1.ApplyCheckinResultRequest{
		CheckinId: "c1", RoomId: 1, UserId: 7,
		Status: pbv1.CheckinStatus_CHECKIN_STATUS_APPROVED,
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	for _, id := range []int64{7, 8, 9} {
		if got := e.results.counts[resultKey{1, id}]; got != 1 {
			t.Errorf("user %d has %d workouts, want 1", id, got)
		}
		if !slices.Contains(e.users.granted[id], domain.AchFirstCheckin) {
			t.Errorf("user %d was not rewarded: %v", id, e.users.granted[id])
		}
	}

	// checkin-service retries delivery, and a retry must not hand out a second workout
	if _, err := e.client.ApplyCheckinResult(ctx, &pbv1.ApplyCheckinResultRequest{
		CheckinId: "c1", RoomId: 1, UserId: 7,
		Status: pbv1.CheckinStatus_CHECKIN_STATUS_APPROVED,
	}); err != nil {
		t.Fatalf("redeliver: %v", err)
	}
	for _, id := range []int64{7, 8, 9} {
		if got := e.results.counts[resultKey{1, id}]; got != 1 {
			t.Errorf("redelivery gave user %d %d workouts, want 1", id, got)
		}
	}
}

// A rejected photo proves nothing, so the people on it earn nothing either.
func TestRejectedCheckinCreditsNobody(t *testing.T) {
	e := newEnv(t)
	e.buddies.tagged["c1"] = []int64{8}

	_, err := e.client.ApplyCheckinResult(context.Background(), &pbv1.ApplyCheckinResultRequest{
		CheckinId: "c1", RoomId: 1, UserId: 7,
		Status: pbv1.CheckinStatus_CHECKIN_STATUS_REJECTED,
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := e.results.counts[resultKey{1, 8}]; got != 0 {
		t.Errorf("buddy got %d workouts from a rejected checkin, want 0", got)
	}
}

func TestApplyCheckinResultRejected(t *testing.T) {
	e := newEnv(t)
	resp, err := e.client.ApplyCheckinResult(context.Background(), &pbv1.ApplyCheckinResultRequest{
		CheckinId: "c2", RoomId: 1, UserId: 7,
		Status: pbv1.CheckinStatus_CHECKIN_STATUS_REJECTED,
	})
	if err != nil {
		t.Fatalf("apply rejected: %v", err)
	}
	if resp.WorkoutsCount != 0 || len(resp.GrantedAchievements) != 0 {
		t.Errorf("rejected must not grant: %+v", resp)
	}
}

func TestApplyCheckinResultStatusUpgrade(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for i := range 10 {
		_, err := e.client.ApplyCheckinResult(ctx, &pbv1.ApplyCheckinResultRequest{
			CheckinId: "bulk-" + string(rune('a'+i)), RoomId: 1, UserId: 9,
			Status: pbv1.CheckinStatus_CHECKIN_STATUS_APPROVED,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if e.users.rank[9] != domain.RankRegular {
		t.Errorf("rank after 10 workouts = %q, want regular", e.users.rank[9])
	}
	if !slices.Contains(e.users.granted[9], domain.AchWorkouts10) {
		t.Errorf("granted = %v, want workouts_10", e.users.granted[9])
	}
}

func TestGetRoomVerification(t *testing.T) {
	e := newEnv(t)
	e.rooms.rooms[5] = domain.Room{ID: 5, VotesRequired: 3}
	e.rooms.members[5] = []int64{1, 2, 3}

	resp, err := e.client.GetRoomVerification(context.Background(),
		&pbv1.GetRoomVerificationRequest{RoomId: 5})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.VotesRequired != 3 || len(resp.MemberIds) != 3 {
		t.Errorf("unexpected: %+v", resp)
	}

	_, err = e.client.GetRoomVerification(context.Background(),
		&pbv1.GetRoomVerificationRequest{RoomId: 404})
	if status.Code(err) != codes.NotFound {
		t.Errorf("missing room: %v, want NotFound", err)
	}
}
