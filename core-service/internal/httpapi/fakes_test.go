package httpapi_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pbv1 "github.com/kosttiik/BuddyGym/core-service/internal/pb/buddygym/v1"

	"github.com/kosttiik/BuddyGym/core-service/internal/checkin"
	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
	"github.com/kosttiik/BuddyGym/core-service/internal/storage"
)

type streakKey struct{ roomID, userID int64 }

// fakeStreaks turns a wanted streak into the days that produce it: with a goal of one
// workout a day, a run of N consecutive days ending today is a streak of N.
type fakeStreaks struct {
	want map[streakKey]int
}

func newFakeStreaks() *fakeStreaks {
	return &fakeStreaks{want: map[streakKey]int{}}
}

func (f *fakeStreaks) set(roomID, userID int64, streak int) {
	f.want[streakKey{roomID, userID}] = streak
}

func (f *fakeStreaks) input(k streakKey, streak int) domain.StreakInput {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	in := domain.StreakInput{
		RoomID: k.roomID, UserID: k.userID, Goal: 1, PeriodDays: 1,
		JoinedAt: today.AddDate(0, 0, -streak),
	}
	for i := range streak {
		in.Days = append(in.Days, today.AddDate(0, 0, -i))
	}
	return in
}

func (f *fakeStreaks) StreaksByRoom(_ context.Context, roomID int64) ([]domain.StreakInput, error) {
	var out []domain.StreakInput
	for k, streak := range f.want {
		if k.roomID == roomID {
			out = append(out, f.input(k, streak))
		}
	}
	return out, nil
}

func (f *fakeStreaks) StreaksByUser(_ context.Context, userID int64) ([]domain.StreakInput, error) {
	var out []domain.StreakInput
	for k, streak := range f.want {
		if k.userID == userID {
			out = append(out, f.input(k, streak))
		}
	}
	return out, nil
}

type fakeUsers struct {
	users map[int64]domain.User
	achs  map[int64][]domain.Achievement
}

func newFakeUsers() *fakeUsers {
	return &fakeUsers{users: map[int64]domain.User{}, achs: map[int64][]domain.Achievement{}}
}

func (f *fakeUsers) Upsert(_ context.Context, id int64, username, firstName, photoURL string) (domain.User, error) {
	u, ok := f.users[id]
	if !ok {
		u = domain.User{ID: id, Theme: "default", Status: domain.StatusNovice, CreatedAt: time.Now()}
	}
	u.Username, u.FirstName, u.PhotoURL = username, firstName, photoURL
	f.users[id] = u
	return u, nil
}

func (f *fakeUsers) Get(_ context.Context, id int64) (domain.User, error) {
	u, ok := f.users[id]
	if !ok {
		return domain.User{}, storage.ErrNotFound
	}
	return u, nil
}

func (f *fakeUsers) UpdateTheme(_ context.Context, id int64, theme string) (domain.User, error) {
	u, ok := f.users[id]
	if !ok {
		return domain.User{}, storage.ErrNotFound
	}
	u.Theme = theme
	f.users[id] = u
	return u, nil
}

func (f *fakeUsers) Achievements(_ context.Context, userID int64) ([]domain.Achievement, error) {
	return f.achs[userID], nil
}

type fakeAvatars struct {
	objects map[string][]byte
}

func newFakeAvatars() *fakeAvatars {
	return &fakeAvatars{objects: map[string][]byte{}}
}

func (f *fakeAvatars) Open(_ context.Context, key string) (io.ReadCloser, string, error) {
	data, ok := f.objects[key]
	if !ok {
		return nil, "", storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), "image/jpeg", nil
}

type fakeRooms struct {
	rooms   map[int64]domain.Room
	members map[int64]map[int64]time.Time
	nextID  int64
}

func newFakeRooms() *fakeRooms {
	return &fakeRooms{rooms: map[int64]domain.Room{}, members: map[int64]map[int64]time.Time{}}
}

func (f *fakeRooms) Create(_ context.Context, room domain.Room) (domain.Room, error) {
	f.nextID++
	room.ID = f.nextID
	room.InviteCode = domain.NewInviteCode()
	room.CreatedAt = time.Now()
	f.rooms[room.ID] = room
	f.members[room.ID] = map[int64]time.Time{room.CreatorID: time.Now()}
	return room, nil
}

func (f *fakeRooms) Get(_ context.Context, id int64) (domain.Room, error) {
	room, ok := f.rooms[id]
	if !ok {
		return domain.Room{}, storage.ErrNotFound
	}
	return room, nil
}

func (f *fakeRooms) GetByInvite(_ context.Context, code string) (domain.Room, error) {
	for _, room := range f.rooms {
		if room.InviteCode == code {
			return room, nil
		}
	}
	return domain.Room{}, storage.ErrNotFound
}

func (f *fakeRooms) Update(_ context.Context, room domain.Room) (domain.Room, error) {
	if _, ok := f.rooms[room.ID]; !ok {
		return domain.Room{}, storage.ErrNotFound
	}
	f.rooms[room.ID] = room
	return room, nil
}

func (f *fakeRooms) Delete(_ context.Context, id int64) error {
	if _, ok := f.rooms[id]; !ok {
		return storage.ErrNotFound
	}
	delete(f.rooms, id)
	delete(f.members, id)
	return nil
}

func (f *fakeRooms) ListByUser(_ context.Context, userID int64) ([]domain.RoomWithProgress, error) {
	var out []domain.RoomWithProgress
	for id, members := range f.members {
		if _, ok := members[userID]; ok {
			out = append(out, domain.RoomWithProgress{Room: f.rooms[id], MembersCount: len(members)})
		}
	}
	return out, nil
}

func (f *fakeRooms) ListOpen(_ context.Context, userID int64) ([]domain.Room, error) {
	var out []domain.Room
	for _, room := range f.rooms {
		if _, joined := f.members[room.ID][userID]; room.Kind == domain.RoomOpen && !joined {
			room.InviteCode = ""
			out = append(out, room)
		}
	}
	return out, nil
}

func (f *fakeRooms) Members(_ context.Context, roomID int64) ([]domain.Member, error) {
	var out []domain.Member
	for uid, joined := range f.members[roomID] {
		out = append(out, domain.Member{User: domain.User{ID: uid}, JoinedAt: joined})
	}
	return out, nil
}

func (f *fakeRooms) IsMember(_ context.Context, roomID, userID int64) (bool, error) {
	_, ok := f.members[roomID][userID]
	return ok, nil
}

func (f *fakeRooms) Join(_ context.Context, roomID, userID int64) error {
	f.members[roomID][userID] = time.Now()
	return nil
}

func (f *fakeRooms) Leave(_ context.Context, roomID, userID int64) error {
	if _, ok := f.members[roomID][userID]; !ok {
		return storage.ErrNotFound
	}
	delete(f.members[roomID], userID)
	return nil
}

type fakeCheckins struct {
	checkins map[string]checkin.Checkin
	nextID   int
	err      error
	// object storage stand-in: key -> bytes, to prove one photo is stored once
	photos    map[string][]byte
	photoKeys map[string]string
	nextPhoto int
}

func newFakeCheckins() *fakeCheckins {
	return &fakeCheckins{
		checkins:  map[string]checkin.Checkin{},
		photos:    map[string][]byte{},
		photoKeys: map[string]string{},
	}
}

func (f *fakeCheckins) Create(_ context.Context, userID int64, targets []checkin.Target, photo []byte, geo *checkin.Geo) ([]checkin.Checkin, error) {
	if f.err != nil {
		return nil, f.err
	}

	var photoKey string
	if len(photo) > 0 {
		f.nextPhoto++
		photoKey = fmt.Sprintf("checkins/photo-%d", f.nextPhoto)
		f.photos[photoKey] = photo
	}

	out := make([]checkin.Checkin, 0, len(targets))
	for _, t := range targets {
		f.nextID++
		c := checkin.Checkin{
			ID: fmt.Sprintf("chk-%d", f.nextID), RoomID: t.RoomID, UserID: userID,
			Status: "pending", Geo: geo, VotesRequired: t.VotesRequired,
			HasPhoto:  photoKey != "",
			CreatedAt: time.Now(), ExpiresAt: time.Now().Add(24 * time.Hour),
		}
		if geo != nil {
			c.Status = "approved"
		}
		f.checkins[c.ID] = c
		f.photoKeys[c.ID] = photoKey
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeCheckins) OpenPhoto(_ context.Context, checkinID string) (checkin.Photo, error) {
	if f.err != nil {
		return checkin.Photo{}, f.err
	}
	key, ok := f.photoKeys[checkinID]
	if !ok || key == "" {
		return checkin.Photo{}, status.Error(codes.NotFound, "photo not found")
	}
	return checkin.Photo{
		ContentType: "image/png",
		Body:        bytes.NewReader(f.photos[key]),
	}, nil
}

func (f *fakeCheckins) Get(_ context.Context, id string) (checkin.Checkin, error) {
	if f.err != nil {
		return checkin.Checkin{}, f.err
	}
	c, ok := f.checkins[id]
	if !ok {
		return checkin.Checkin{}, status.Error(codes.NotFound, "checkin not found")
	}
	return c, nil
}

func (f *fakeCheckins) List(_ context.Context, roomID int64, st pbv1.CheckinStatus, limit, offset int32) ([]checkin.Checkin, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []checkin.Checkin
	for _, c := range f.checkins {
		if c.RoomID == roomID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeCheckins) Vote(_ context.Context, checkinID string, voterID int64, approve bool) (checkin.Checkin, error) {
	if f.err != nil {
		return checkin.Checkin{}, f.err
	}
	c, ok := f.checkins[checkinID]
	if !ok {
		return checkin.Checkin{}, status.Error(codes.NotFound, "checkin not found")
	}
	if approve {
		c.VotesApprove++
	} else {
		c.VotesReject++
	}
	f.checkins[checkinID] = c
	return c, nil
}
