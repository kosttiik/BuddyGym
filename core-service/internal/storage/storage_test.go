package storage_test

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
	"github.com/kosttiik/BuddyGym/core-service/internal/storage"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}
	ctx := context.Background()
	pgc, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("core_db"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		log.Fatalf("start postgres container: %v", err)
	}
	defer testcontainers.TerminateContainer(pgc)

	dsn, err := pgc.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Fatalf("connection string: %v", err)
	}
	testPool, err = storage.Connect(ctx, dsn)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	if err := storage.Migrate(ctx, testPool); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	code := m.Run()
	testPool.Close()
	_ = testcontainers.TerminateContainer(pgc)
	os.Exit(code)
}

func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("storage tests need docker")
	}
	return testPool
}

func mustUser(t *testing.T, id int64) domain.User {
	t.Helper()
	u, err := storage.NewUsers(pool(t)).Upsert(context.Background(), id,
		fmt.Sprintf("user%d", id), "Test", "")
	if err != nil {
		t.Fatalf("upsert user %d: %v", id, err)
	}
	return u
}

func mustRoom(t *testing.T, creatorID int64) domain.Room {
	t.Helper()
	room, err := storage.NewRooms(pool(t)).Create(context.Background(), domain.Room{
		Name: "test room", Kind: domain.RoomOpen,
		GoalPerPeriod: 3, PeriodDays: 7, VotesRequired: 2, CreatorID: creatorID,
	})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	return room
}

func TestUsersUpsertGet(t *testing.T) {
	ctx := context.Background()
	users := storage.NewUsers(pool(t))

	u := mustUser(t, 101)
	if u.Theme != "default" || u.Status != domain.StatusNovice {
		t.Errorf("unexpected defaults: %+v", u)
	}

	u2, err := users.Upsert(ctx, 101, "renamed", "Test", "https://x/1.jpg")
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if u2.Username != "renamed" || u2.PhotoURL != "https://x/1.jpg" {
		t.Errorf("profile not refreshed: %+v", u2)
	}
	if !u2.CreatedAt.Equal(u.CreatedAt) {
		t.Errorf("created_at changed on upsert")
	}

	if _, err := users.Get(ctx, 999999); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Get missing: err = %v, want ErrNotFound", err)
	}
}

func TestUsersThemeStatusAchievements(t *testing.T) {
	ctx := context.Background()
	users := storage.NewUsers(pool(t))
	mustUser(t, 102)

	u, err := users.UpdateTheme(ctx, 102, "dark")
	if err != nil || u.Theme != "dark" {
		t.Fatalf("UpdateTheme: %v, theme=%q", err, u.Theme)
	}
	if err := users.SetStatus(ctx, 102, domain.StatusRegular); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	granted, err := users.Grant(ctx, 102, []string{domain.AchFirstCheckin, domain.AchWorkouts10})
	if err != nil || len(granted) != 2 {
		t.Fatalf("Grant: %v, granted=%v", err, granted)
	}
	granted, err = users.Grant(ctx, 102, []string{domain.AchFirstCheckin, domain.AchStreak7})
	if err != nil {
		t.Fatalf("Grant repeat: %v", err)
	}
	if len(granted) != 1 || granted[0] != domain.AchStreak7 {
		t.Errorf("repeat grant = %v, want only streak_7", granted)
	}
	achs, err := users.Achievements(ctx, 102)
	if err != nil || len(achs) != 3 {
		t.Errorf("Achievements: %v, got %d, want 3", err, len(achs))
	}
}

func TestRoomsCreateGetList(t *testing.T) {
	ctx := context.Background()
	rooms := storage.NewRooms(pool(t))
	mustUser(t, 103)
	room := mustRoom(t, 103)

	if len(room.InviteCode) != domain.InviteCodeLen {
		t.Errorf("invite code %q", room.InviteCode)
	}
	if ok, _ := rooms.IsMember(ctx, room.ID, 103); !ok {
		t.Error("creator is not a member")
	}

	got, err := rooms.Get(ctx, room.ID)
	if err != nil || got.ID != room.ID {
		t.Fatalf("Get: %v", err)
	}
	byCode, err := rooms.GetByInvite(ctx, room.InviteCode)
	if err != nil || byCode.ID != room.ID {
		t.Fatalf("GetByInvite: %v", err)
	}
	if _, err := rooms.GetByInvite(ctx, "NOPE1234"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("GetByInvite missing: %v", err)
	}

	list, err := rooms.ListByUser(ctx, 103)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListByUser: %v, len=%d", err, len(list))
	}
	if list[0].MembersCount != 1 || list[0].WorkoutsCount != 0 {
		t.Errorf("unexpected progress: %+v", list[0])
	}
}

func TestRoomsJoinLeaveMembers(t *testing.T) {
	ctx := context.Background()
	rooms := storage.NewRooms(pool(t))
	mustUser(t, 104)
	mustUser(t, 105)
	room := mustRoom(t, 104)

	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- rooms.Join(ctx, room.ID, 105)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent join: %v", err)
		}
	}

	members, err := rooms.Members(ctx, room.ID)
	if err != nil || len(members) != 2 {
		t.Fatalf("Members: %v, len=%d, want 2", err, len(members))
	}
	if members[0].ID != 104 || members[1].ID != 105 {
		t.Errorf("member order: %d, %d", members[0].ID, members[1].ID)
	}

	ids, err := rooms.MemberIDs(ctx, room.ID)
	if err != nil || len(ids) != 2 {
		t.Fatalf("MemberIDs: %v, %v", err, ids)
	}

	if err := rooms.Leave(ctx, room.ID, 105); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	if err := rooms.Leave(ctx, room.ID, 105); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("second Leave: %v, want ErrNotFound", err)
	}
}

func TestRoomsUpdateDelete(t *testing.T) {
	ctx := context.Background()
	rooms := storage.NewRooms(pool(t))
	mustUser(t, 109)
	room := mustRoom(t, 109)
	room.Name, room.Kind = "updated room", domain.RoomInvite
	room.GoalPerPeriod, room.PeriodDays, room.VotesRequired = 5, 14, 3

	updated, err := rooms.Update(ctx, room)
	if err != nil || updated.Name != room.Name || updated.Kind != room.Kind || updated.VotesRequired != room.VotesRequired {
		t.Fatalf("Update: %v, room=%+v", err, updated)
	}
	if err := rooms.Delete(ctx, room.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := rooms.Get(ctx, room.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Get deleted: %v, want ErrNotFound", err)
	}
}

func TestResultsApplyIdempotent(t *testing.T) {
	ctx := context.Background()
	results := storage.NewResults(pool(t))
	mustUser(t, 106)
	room := mustRoom(t, 106)

	applied, err := results.Apply(ctx, "chk-1", room.ID, 106, storage.ResultApproved)
	if err != nil || !applied {
		t.Fatalf("Apply: %v, applied=%v", err, applied)
	}
	applied, err = results.Apply(ctx, "chk-1", room.ID, 106, storage.ResultApproved)
	if err != nil || applied {
		t.Fatalf("repeat Apply: %v, applied=%v, want false", err, applied)
	}

	count, err := results.PeriodCount(ctx, room.ID, 106)
	if err != nil || count != 1 {
		t.Errorf("PeriodCount = %d (%v), want 1", count, err)
	}
	total, err := results.TotalApproved(ctx, 106)
	if err != nil || total != 1 {
		t.Errorf("TotalApproved = %d (%v), want 1", total, err)
	}

	applied, err = results.Apply(ctx, "chk-2", room.ID, 106, storage.ResultRejected)
	if err != nil || !applied {
		t.Fatalf("Apply rejected: %v", err)
	}
	if count, _ := results.PeriodCount(ctx, room.ID, 106); count != 1 {
		t.Errorf("rejected result bumped counter: %d", count)
	}
	if total, _ := results.TotalApproved(ctx, 106); total != 1 {
		t.Errorf("rejected counted as approved: %d", total)
	}
}

func TestResultsPeriodRollover(t *testing.T) {
	ctx := context.Background()
	results := storage.NewResults(pool(t))
	mustUser(t, 107)
	room := mustRoom(t, 107)

	if _, err := results.Apply(ctx, "chk-r1", room.ID, 107, storage.ResultApproved); err != nil {
		t.Fatal(err)
	}
	if _, err := results.Apply(ctx, "chk-r2", room.ID, 107, storage.ResultApproved); err != nil {
		t.Fatal(err)
	}

	_, err := pool(t).Exec(ctx,
		"UPDATE memberships SET period_start = now() - interval '8 days' WHERE room_id = $1", room.ID)
	if err != nil {
		t.Fatal(err)
	}

	if count, _ := results.PeriodCount(ctx, room.ID, 107); count != 0 {
		t.Errorf("stale period count = %d, want 0", count)
	}

	if _, err := results.Apply(ctx, "chk-r3", room.ID, 107, storage.ResultApproved); err != nil {
		t.Fatal(err)
	}
	if count, _ := results.PeriodCount(ctx, room.ID, 107); count != 1 {
		t.Errorf("count after rollover = %d, want 1", count)
	}
	if total, _ := results.TotalApproved(ctx, 107); total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
}

func TestResultsWorkoutDays(t *testing.T) {
	ctx := context.Background()
	results := storage.NewResults(pool(t))
	mustUser(t, 108)
	room := mustRoom(t, 108)

	for i, id := range []string{"chk-d1", "chk-d2", "chk-d3"} {
		if _, err := results.Apply(ctx, id, room.ID, 108, storage.ResultApproved); err != nil {
			t.Fatal(err)
		}
		_, err := pool(t).Exec(ctx,
			"UPDATE checkin_results SET applied_at = now() - $2::int * interval '1 day' WHERE checkin_id = $1",
			id, i)
		if err != nil {
			t.Fatal(err)
		}
	}
	// same day duplicate must collapse into one date
	if _, err := results.Apply(ctx, "chk-d4", room.ID, 108, storage.ResultApproved); err != nil {
		t.Fatal(err)
	}

	days, err := results.WorkoutDays(ctx, 108, 100)
	if err != nil {
		t.Fatalf("WorkoutDays: %v", err)
	}
	if len(days) != 3 {
		t.Fatalf("len(days) = %d, want 3", len(days))
	}
	for i := 1; i < len(days); i++ {
		if !days[i-1].After(days[i]) {
			t.Errorf("days not descending: %v", days)
		}
		if days[i-1].Sub(days[i]) != 24*time.Hour {
			t.Errorf("days not consecutive: %v", days)
		}
	}
}

func TestResultsPeriodCountCollapsesSameDayCheckins(t *testing.T) {
	ctx := context.Background()
	results := storage.NewResults(pool(t))
	mustUser(t, 110)
	room := mustRoom(t, 110)

	for _, id := range []string{"chk-same-day-1", "chk-same-day-2"} {
		if _, err := results.Apply(ctx, id, room.ID, 110, storage.ResultApproved); err != nil {
			t.Fatal(err)
		}
	}

	count, err := results.PeriodCount(ctx, room.ID, 110)
	if err != nil || count != 1 {
		t.Errorf("PeriodCount = %d (%v), want 1", count, err)
	}
}

// The last member walking out kills the room: it disappears from every read path right
// away, but the row lingers so the checkin side can still purge its photos.
func TestRoomIsMarkedDeletedWhenTheLastMemberLeaves(t *testing.T) {
	ctx := context.Background()
	rooms := storage.NewRooms(pool(t))
	mustUser(t, 140)
	mustUser(t, 141)
	room := mustRoom(t, 140)

	if err := rooms.Join(ctx, room.ID, 141); err != nil {
		t.Fatal(err)
	}

	if err := rooms.Leave(ctx, room.ID, 141); err != nil {
		t.Fatal(err)
	}
	if _, err := rooms.Get(ctx, room.ID); err != nil {
		t.Fatalf("a room with members left must stay visible: %v", err)
	}

	if err := rooms.Leave(ctx, room.ID, 140); err != nil {
		t.Fatal(err)
	}

	if _, err := rooms.Get(ctx, room.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("emptied room still readable: %v", err)
	}
	if _, err := rooms.GetByInvite(ctx, room.InviteCode); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("emptied room still joinable by code: %v", err)
	}
	list, err := rooms.ListByUser(ctx, 140)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range list {
		if r.ID == room.ID {
			t.Error("emptied room still listed")
		}
	}

	ids, err := rooms.ListDeletedBefore(ctx, time.Now().Add(time.Minute), 10)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(ids, room.ID) {
		t.Errorf("room not queued for purging: %v", ids)
	}

	if err := rooms.Purge(ctx, room.ID); err != nil {
		t.Fatal(err)
	}
	ids, err = rooms.ListDeletedBefore(ctx, time.Now().Add(time.Minute), 10)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(ids, room.ID) {
		t.Error("purged room still queued")
	}
}
