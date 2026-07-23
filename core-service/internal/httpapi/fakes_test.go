package httpapi_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
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
	stats map[int64]domain.Stats
}

func newFakeUsers() *fakeUsers {
	return &fakeUsers{
		users: map[int64]domain.User{},
		achs:  map[int64][]domain.Achievement{},
		stats: map[int64]domain.Stats{},
	}
}

func (f *fakeUsers) Upsert(_ context.Context, id int64, username, firstName, photoURL string) (domain.User, error) {
	u, ok := f.users[id]
	if !ok {
		u = domain.User{ID: id, Theme: "default", Rank: domain.RankNovice, CreatedAt: time.Now()}
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

func (f *fakeUsers) SetStatus(_ context.Context, id int64, emoji, text string) (domain.User, error) {
	u, ok := f.users[id]
	if !ok {
		return domain.User{}, storage.ErrNotFound
	}
	u.StatusEmoji, u.StatusText = emoji, text
	f.users[id] = u
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

func (f *fakeUsers) Stats(_ context.Context, userID int64) (domain.Stats, error) {
	return f.stats[userID], nil
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

func (f *fakeAvatars) Put(_ context.Context, key string, data []byte) error {
	f.objects[key] = data
	return nil
}

func (f *fakeAvatars) Delete(_ context.Context, key string) error {
	delete(f.objects, key)
	return nil
}

type fakeBuddies struct {
	tagged map[string][]int64
	users  *fakeUsers
}

func newFakeBuddies(users *fakeUsers) *fakeBuddies {
	return &fakeBuddies{tagged: map[string][]int64{}, users: users}
}

func (f *fakeBuddies) Tag(_ context.Context, checkinID string, _, _ int64, userIDs []int64) error {
	for _, id := range userIDs {
		if !slices.Contains(f.tagged[checkinID], id) {
			f.tagged[checkinID] = append(f.tagged[checkinID], id)
		}
	}
	return nil
}

func (f *fakeBuddies) Untag(_ context.Context, checkinID string, userID int64) error {
	kept := slices.DeleteFunc(slices.Clone(f.tagged[checkinID]), func(id int64) bool { return id == userID })
	if len(kept) == len(f.tagged[checkinID]) {
		return storage.ErrNotFound
	}
	f.tagged[checkinID] = kept
	return nil
}

func (f *fakeBuddies) ForCheckins(_ context.Context, checkinIDs []string) (map[string][]domain.User, error) {
	out := map[string][]domain.User{}
	for _, cid := range checkinIDs {
		for _, id := range f.tagged[cid] {
			out[cid] = append(out[cid], f.users.users[id])
		}
	}
	return out, nil
}

type fakeComments struct {
	byCheckin map[string][]domain.Comment
	likes     map[int64]map[int64]bool
	users     *fakeUsers
	rooms     *fakeRooms
	nextID    int64
}

func newFakeComments(users *fakeUsers, rooms *fakeRooms) *fakeComments {
	return &fakeComments{
		byCheckin: map[string][]domain.Comment{},
		likes:     map[int64]map[int64]bool{},
		users:     users,
		rooms:     rooms,
	}
}

func (f *fakeComments) view(c domain.Comment, viewerID int64) domain.Comment {
	c.Likes = len(f.likes[c.ID])
	c.LikedByMe = f.likes[c.ID][viewerID]
	c.HasPhoto = c.PhotoKey != ""
	return c
}

func (f *fakeComments) Add(_ context.Context, checkinID string, roomID, userID int64, body, photoKey string, replyTo *int64) (domain.Comment, error) {
	if replyTo != nil && !slices.ContainsFunc(f.byCheckin[checkinID], func(c domain.Comment) bool {
		return c.ID == *replyTo
	}) {
		return domain.Comment{}, storage.ErrNotFound
	}
	f.nextID++
	c := domain.Comment{
		ID: f.nextID, CheckinID: checkinID, UserID: userID,
		Author: f.users.users[userID], Body: body, PhotoKey: photoKey, CreatedAt: time.Now(),
		ReplyTo: replyTo,
	}
	if replyTo != nil {
		for _, parent := range f.byCheckin[checkinID] {
			if parent.ID == *replyTo {
				c.ReplyToAuthor = parent.Author.FirstName
				c.ReplyToBody = parent.Body
			}
		}
	}
	f.byCheckin[checkinID] = append(f.byCheckin[checkinID], c)
	f.rooms.commentRoom[c.ID] = roomID
	return f.view(c, userID), nil
}

func (f *fakeComments) Get(_ context.Context, id, viewerID int64) (domain.Comment, error) {
	for _, list := range f.byCheckin {
		for _, c := range list {
			if c.ID == id {
				return f.view(c, viewerID), nil
			}
		}
	}
	return domain.Comment{}, storage.ErrNotFound
}

func (f *fakeComments) List(_ context.Context, checkinID string, viewerID int64, limit, offset int) ([]domain.Comment, error) {
	all := f.byCheckin[checkinID]
	if offset >= len(all) {
		return nil, nil
	}
	out := make([]domain.Comment, 0, limit)
	for _, c := range all[offset:min(offset+limit, len(all))] {
		out = append(out, f.view(c, viewerID))
	}
	return out, nil
}

func (f *fakeComments) Delete(_ context.Context, id, userID int64) (string, error) {
	for checkinID, list := range f.byCheckin {
		for i, c := range list {
			if c.ID != id {
				continue
			}
			creator := f.rooms.rooms[f.rooms.commentRoom[id]].CreatorID
			if c.UserID != userID && creator != userID {
				return "", storage.ErrNotFound
			}
			f.byCheckin[checkinID] = append(list[:i], list[i+1:]...)
			return c.PhotoKey, nil
		}
	}
	return "", storage.ErrNotFound
}

func (f *fakeComments) Like(_ context.Context, commentID, userID int64) error {
	if f.likes[commentID] == nil {
		f.likes[commentID] = map[int64]bool{}
	}
	f.likes[commentID][userID] = true
	return nil
}

func (f *fakeComments) Unlike(_ context.Context, commentID, userID int64) error {
	delete(f.likes[commentID], userID)
	return nil
}

func (f *fakeComments) Summaries(_ context.Context, checkinIDs []string, viewerID int64) (map[string]domain.CommentSummary, error) {
	out := map[string]domain.CommentSummary{}
	for _, id := range checkinIDs {
		list := f.byCheckin[id]
		summary := domain.CommentSummary{Count: len(list)}
		for _, c := range list {
			viewed := f.view(c, viewerID)
			if summary.Top == nil || viewed.Likes > summary.Top.Likes {
				top := viewed
				summary.Top = &top
			}
		}
		out[id] = summary
	}
	return out, nil
}

type fakeObjects struct {
	objects map[string][]byte
}

func newFakeObjects() *fakeObjects {
	return &fakeObjects{objects: map[string][]byte{}}
}

func (f *fakeObjects) Put(_ context.Context, key string, data []byte) error {
	f.objects[key] = data
	return nil
}

func (f *fakeObjects) Open(_ context.Context, key string) (io.ReadCloser, string, error) {
	data, ok := f.objects[key]
	if !ok {
		return nil, "", storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), "image/png", nil
}

func (f *fakeObjects) Delete(_ context.Context, key string) error {
	delete(f.objects, key)
	return nil
}

type fakeRooms struct {
	rooms        map[int64]domain.Room
	members      map[int64]map[int64]time.Time
	commentRoom  map[int64]int64
	avatars      map[int64][]domain.RoomAvatar
	settings     map[[2]int64]membershipSettings
	nextID       int64
	nextAvatarID int64
}

func newFakeRooms() *fakeRooms {
	return &fakeRooms{
		rooms:       map[int64]domain.Room{},
		members:     map[int64]map[int64]time.Time{},
		commentRoom: map[int64]int64{},
		avatars:     map[int64][]domain.RoomAvatar{},
	}
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

func (f *fakeRooms) AddAvatar(_ context.Context, roomID, userID int64, key string) (domain.RoomAvatar, error) {
	room, ok := f.rooms[roomID]
	if !ok {
		return domain.RoomAvatar{}, storage.ErrNotFound
	}
	f.nextAvatarID++
	added := domain.RoomAvatar{
		ID:         f.nextAvatarID,
		UploadedBy: userID,
		CreatedAt:  time.Now(),
		IsCurrent:  true,
		ObjectKey:  key,
	}
	f.avatars[roomID] = append([]domain.RoomAvatar{added}, f.avatars[roomID]...)
	room.AvatarKey, room.HasAvatar = key, true
	f.rooms[roomID] = room
	return added, nil
}

func (f *fakeRooms) ListAvatars(_ context.Context, roomID int64) ([]domain.RoomAvatar, error) {
	out := make([]domain.RoomAvatar, 0, len(f.avatars[roomID]))
	for _, a := range f.avatars[roomID] {
		a.IsCurrent = a.ObjectKey == f.rooms[roomID].AvatarKey
		out = append(out, a)
	}
	return out, nil
}

func (f *fakeRooms) GetAvatar(_ context.Context, roomID, avatarID int64) (domain.RoomAvatar, error) {
	for _, a := range f.avatars[roomID] {
		if a.ID == avatarID {
			return a, nil
		}
	}
	return domain.RoomAvatar{}, storage.ErrNotFound
}

func (f *fakeRooms) DeleteAvatar(_ context.Context, roomID, avatarID int64) (string, error) {
	list := f.avatars[roomID]
	for i, a := range list {
		if a.ID != avatarID {
			continue
		}
		f.avatars[roomID] = append(append([]domain.RoomAvatar{}, list[:i]...), list[i+1:]...)
		room := f.rooms[roomID]
		if room.AvatarKey == a.ObjectKey {
			room.AvatarKey = ""
			if left := f.avatars[roomID]; len(left) > 0 {
				room.AvatarKey = left[0].ObjectKey
			}
			room.HasAvatar = room.AvatarKey != ""
			f.rooms[roomID] = room
		}
		return a.ObjectKey, nil
	}
	return "", storage.ErrNotFound
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

type membershipSettings struct {
	SportName  string
	SportEmoji string
	Goal       *int
}

func (f *fakeRooms) UpdateMembership(_ context.Context, roomID, userID int64, sportName, sportEmoji string, goal *int) error {
	if _, ok := f.members[roomID][userID]; !ok {
		return storage.ErrNotFound
	}
	if f.settings == nil {
		f.settings = map[[2]int64]membershipSettings{}
	}
	f.settings[[2]int64{roomID, userID}] = membershipSettings{sportName, sportEmoji, goal}
	return nil
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
	synced    map[int64]int
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

func (f *fakeCheckins) SyncVotesRequired(_ context.Context, roomID int64, votesRequired int) error {
	if f.err != nil {
		return f.err
	}
	if f.synced == nil {
		f.synced = map[int64]int{}
	}
	f.synced[roomID] = votesRequired
	return nil
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

func (f *fakeCheckins) Cancel(_ context.Context, checkinID string, userID int64) (checkin.Checkin, error) {
	if f.err != nil {
		return checkin.Checkin{}, f.err
	}
	c, ok := f.checkins[checkinID]
	if !ok {
		return checkin.Checkin{}, status.Error(codes.NotFound, "checkin not found")
	}
	if c.UserID != userID {
		return checkin.Checkin{}, status.Error(codes.PermissionDenied, "only the author can cancel")
	}
	c.Status = "expired"
	f.checkins[checkinID] = c
	return c, nil
}

type fakeFreezes struct {
	freezes map[[2]int64][]domain.Freeze
	nextID  int64
}

func newFakeFreezes() *fakeFreezes {
	return &fakeFreezes{freezes: map[[2]int64][]domain.Freeze{}}
}

func (f *fakeFreezes) Create(_ context.Context, roomID, userID int64, startsAt, endsAt time.Time) (domain.Freeze, error) {
	f.nextID++
	fz := domain.Freeze{ID: f.nextID, RoomID: roomID, UserID: userID,
		StartsAt: startsAt, EndsAt: endsAt, CreatedAt: time.Now()}
	k := [2]int64{roomID, userID}
	f.freezes[k] = append(f.freezes[k], fz)
	return fz, nil
}

func (f *fakeFreezes) Cancel(_ context.Context, roomID, userID int64, at time.Time) error {
	list := f.freezes[[2]int64{roomID, userID}]
	for i := range list {
		if list[i].CanceledAt == nil && list[i].EndsAt.After(at) {
			list[i].CanceledAt = &at
			return nil
		}
	}
	return storage.ErrNotFound
}

func (f *fakeFreezes) ListByMember(_ context.Context, roomID, userID int64) ([]domain.Freeze, error) {
	return f.freezes[[2]int64{roomID, userID}], nil
}

type recordedEvent struct {
	Type    string
	RoomID  int64
	ActorID int64
	Subject map[string]any
}

type fakeEvents struct {
	events []recordedEvent
}

func (f *fakeEvents) Add(_ context.Context, eventType string, roomID, actorID int64, subject map[string]any) error {
	f.events = append(f.events, recordedEvent{eventType, roomID, actorID, subject})
	return nil
}

func (f *fakeEvents) types() []string {
	out := make([]string, 0, len(f.events))
	for _, e := range f.events {
		out = append(out, e.Type)
	}
	return out
}
