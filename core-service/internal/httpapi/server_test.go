package httpapi_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kosttiik/BuddyGym/core-service/internal/auth"
	"github.com/kosttiik/BuddyGym/core-service/internal/checkin"
	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
	"github.com/kosttiik/BuddyGym/core-service/internal/httpapi"
)

const testToken = "7000000000:AAtest-token-for-unit-tests"

var jwtSecret = []byte("httpapi-test-secret-32-bytes-min!!")

func initDataFor(userID int64) string {
	fields := map[string]string{
		"auth_date": fmt.Sprintf("%d", time.Now().Unix()),
		"user":      fmt.Sprintf(`{"id":%d,"first_name":"U%d","username":"user%d"}`, userID, userID, userID),
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+"="+fields[k])
	}
	secretMac := hmac.New(sha256.New, []byte("WebAppData"))
	secretMac.Write([]byte(testToken))
	mac := hmac.New(sha256.New, secretMac.Sum(nil))
	mac.Write([]byte(strings.Join(lines, "\n")))
	fields["hash"] = hex.EncodeToString(mac.Sum(nil))

	q := url.Values{}
	for k, v := range fields {
		q.Set(k, v)
	}
	return q.Encode()
}

type env struct {
	users         *fakeUsers
	rooms         *fakeRooms
	streaks       *fakeStreaks
	buddies       *fakeBuddies
	comments      *fakeComments
	commentPhotos *fakeObjects
	checkins      *fakeCheckins
	avatars       *fakeAvatars
	handler       http.Handler
	dbErr         error
	redisErr      error
}

func newEnv(opts ...func(*httpapi.Options)) *env {
	e := &env{
		users:    newFakeUsers(),
		rooms:    newFakeRooms(),
		streaks:  newFakeStreaks(),
		checkins: newFakeCheckins(),
		avatars:  newFakeAvatars(),
	}
	e.buddies = newFakeBuddies(e.users)
	e.comments = newFakeComments(e.users, e.rooms)
	e.commentPhotos = newFakeObjects()
	o := httpapi.Options{
		Users:         e.users,
		Rooms:         e.rooms,
		Streaks:       e.streaks,
		Buddies:       e.buddies,
		Comments:      e.comments,
		CommentPhotos: e.commentPhotos,
		Checkins:      e.checkins,
		Avatars:       e.avatars,
		BotToken:      testToken,
		AuthTTL:       24 * time.Hour,
		JWTSecret:     jwtSecret,
		JWTTTL:        time.Hour,
		DBPing:        func(context.Context) error { return e.dbErr },
		RedisPing:     func(context.Context) error { return e.redisErr },
	}
	for _, fn := range opts {
		fn(&o)
	}
	e.handler = httpapi.New(o).Handler()
	return e
}

// bearer registers the user and issues a valid token for it.
func (e *env) bearer(t *testing.T, userID int64) string {
	t.Helper()
	if _, err := e.users.Upsert(context.Background(), userID,
		fmt.Sprintf("user%d", userID), fmt.Sprintf("U%d", userID), ""); err != nil {
		t.Fatal(err)
	}
	token, err := auth.IssueToken(jwtSecret, userID, time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return "Bearer " + token
}

type reqOpts struct {
	userID      int64
	contentType string
	noAuth      bool
}

func (e *env) do(t *testing.T, method, path string, body any, opts reqOpts) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	case *bytes.Buffer:
		rd = b
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rd)
	if opts.contentType != "" {
		req.Header.Set("Content-Type", opts.contentType)
	}
	if !opts.noAuth {
		if opts.userID == 0 {
			opts.userID = 1
		}
		req.Header.Set("Authorization", e.bearer(t, opts.userID))
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func (e *env) createRoom(t *testing.T, creator int64, kind string) domain.Room {
	t.Helper()
	rec := e.do(t, "POST", "/api/v1/rooms", httpapi.CreateRoomRequest{
		Name: "room", Kind: kind, GoalPerPeriod: 3, PeriodDays: 7, VotesRequired: 2,
	}, reqOpts{userID: creator})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create room: %d %s", rec.Code, rec.Body.String())
	}
	return decode[domain.Room](t, rec)
}

func TestAuthTelegramExchange(t *testing.T) {
	e := newEnv()

	rec := e.do(t, "POST", "/api/v1/auth/telegram",
		httpapi.AuthTelegramRequest{InitData: initDataFor(42)}, reqOpts{noAuth: true})
	if rec.Code != http.StatusOK {
		t.Fatalf("exchange: %d %s", rec.Code, rec.Body.String())
	}
	resp := decode[httpapi.AuthTelegramResponse](t, rec)
	if resp.Token == "" || resp.User.ID != 42 {
		t.Fatalf("unexpected auth response: %+v", resp)
	}

	req := httptest.NewRequest("GET", "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+resp.Token)
	rec2 := httptest.NewRecorder()
	e.handler.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("me with issued token: %d %s", rec2.Code, rec2.Body.String())
	}
	me := decode[httpapi.MeResponse](t, rec2)
	if me.User.ID != 42 || me.User.Username != "user42" {
		t.Errorf("unexpected me: %+v", me.User)
	}

	rec = e.do(t, "POST", "/api/v1/auth/telegram",
		httpapi.AuthTelegramRequest{InitData: "garbage=1&hash=beef"}, reqOpts{noAuth: true})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("garbage initdata: %d", rec.Code)
	}
	rec = e.do(t, "POST", "/api/v1/auth/telegram",
		httpapi.AuthTelegramRequest{}, reqOpts{noAuth: true})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty init_data: %d", rec.Code)
	}
}

func TestBearerAuth(t *testing.T) {
	e := newEnv()

	rec := e.do(t, "GET", "/api/v1/me", nil, reqOpts{noAuth: true})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no header: %d", rec.Code)
	}

	for name, header := range map[string]string{
		"garbage token": "Bearer not.a.jwt",
		"wrong scheme":  "tma " + initDataFor(1),
	} {
		req := httptest.NewRequest("GET", "/api/v1/me", nil)
		req.Header.Set("Authorization", header)
		rec := httptest.NewRecorder()
		e.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}

	// valid token but the user is gone from db
	token, _ := auth.IssueToken(jwtSecret, 777, time.Hour, time.Now())
	req := httptest.NewRequest("GET", "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	e.handler.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusUnauthorized {
		t.Errorf("unknown user: %d", rec2.Code)
	}

	rec = e.do(t, "GET", "/api/v1/me", nil, reqOpts{userID: 42})
	if rec.Code != http.StatusOK {
		t.Errorf("valid auth: %d %s", rec.Code, rec.Body.String())
	}
	if me := decode[httpapi.MeResponse](t, rec); me.Achievements == nil {
		t.Error("achievements must be [] not null")
	}
}

type denyLimiter struct{}

func (denyLimiter) Allow(context.Context, string) (bool, error) { return false, nil }

func TestRateLimited(t *testing.T) {
	e := newEnv(func(o *httpapi.Options) { o.AuthLimiter = denyLimiter{} })
	rec := e.do(t, "POST", "/api/v1/auth/telegram",
		httpapi.AuthTelegramRequest{InitData: initDataFor(1)}, reqOpts{noAuth: true})
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("auth limiter: %d, want 429", rec.Code)
	}

	e = newEnv(func(o *httpapi.Options) { o.APILimiter = denyLimiter{} })
	rec = e.do(t, "GET", "/api/v1/me", nil, reqOpts{userID: 1})
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("api limiter: %d, want 429", rec.Code)
	}
}

func TestHealth(t *testing.T) {
	e := newEnv()
	rec := e.do(t, "GET", "/api/v1/health", nil, reqOpts{noAuth: true})
	if rec.Code != http.StatusOK {
		t.Fatalf("health: %d", rec.Code)
	}
	e.dbErr = errors.New("db down")
	rec = e.do(t, "GET", "/api/v1/health", nil, reqOpts{noAuth: true})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("degraded health: %d", rec.Code)
	}
	h := decode[httpapi.HealthResponse](t, rec)
	if h.Status != "degraded" || h.Redis != "ok" {
		t.Errorf("unexpected health: %+v", h)
	}
}

func ptr[T any](v T) *T { return &v }

func TestPatchMe(t *testing.T) {
	e := newEnv()

	rec := e.do(t, "PATCH", "/api/v1/me", httpapi.UpdateMeRequest{Theme: ptr("dark")}, reqOpts{})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	if u := decode[domain.User](t, rec); u.Theme != "dark" {
		t.Errorf("theme = %q", u.Theme)
	}

	rec = e.do(t, "PATCH", "/api/v1/me", httpapi.UpdateMeRequest{Theme: ptr("gold")}, reqOpts{})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown theme: %d", rec.Code)
	}
	rec = e.do(t, "PATCH", "/api/v1/me", "{bad json", reqOpts{})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad json: %d", rec.Code)
	}
}

func TestPatchMeStatus(t *testing.T) {
	e := newEnv()

	rec := e.do(t, "PATCH", "/api/v1/me", httpapi.UpdateMeRequest{
		StatusEmoji: ptr("💪"), StatusText: ptr("  На массе  "),
	}, reqOpts{})
	if rec.Code != http.StatusOK {
		t.Fatalf("set status: %d %s", rec.Code, rec.Body.String())
	}
	u := decode[domain.User](t, rec)
	if u.StatusEmoji != "💪" || u.StatusText != "На массе" {
		t.Errorf("status = %q %q, want the trimmed line", u.StatusEmoji, u.StatusText)
	}

	// the theme is a separate field: touching the status must not wipe it
	rec = e.do(t, "PATCH", "/api/v1/me", httpapi.UpdateMeRequest{Theme: ptr("dark")}, reqOpts{})
	if u := decode[domain.User](t, rec); u.StatusText != "На массе" || u.Theme != "dark" {
		t.Errorf("patching the theme dropped the status: %+v", u)
	}

	bad := []httpapi.UpdateMeRequest{
		{StatusEmoji: ptr("💪🔥")},
		{StatusEmoji: ptr("x")},
		{StatusText: ptr(strings.Repeat("я", 61))},
		{StatusText: ptr("две\nстроки")},
	}
	for i, req := range bad {
		if rec := e.do(t, "PATCH", "/api/v1/me", req, reqOpts{}); rec.Code != http.StatusBadRequest {
			t.Errorf("case %d: %d, want 400", i, rec.Code)
		}
	}

	rec = e.do(t, "PATCH", "/api/v1/me", httpapi.UpdateMeRequest{
		StatusEmoji: ptr(""), StatusText: ptr(""),
	}, reqOpts{})
	if u := decode[domain.User](t, rec); u.StatusEmoji != "" || u.StatusText != "" {
		t.Errorf("status was not cleared: %+v", u)
	}
}

func TestCreateRoomValidation(t *testing.T) {
	e := newEnv()
	bad := []httpapi.CreateRoomRequest{
		{Name: "", Kind: "open", GoalPerPeriod: 3, PeriodDays: 7, VotesRequired: 2},
		{Name: strings.Repeat("x", 65), Kind: "open", GoalPerPeriod: 3, PeriodDays: 7, VotesRequired: 2},
		{Name: "r", Kind: "secret", GoalPerPeriod: 3, PeriodDays: 7, VotesRequired: 2},
		{Name: "r", Kind: "open", GoalPerPeriod: 0, PeriodDays: 7, VotesRequired: 2},
		{Name: "r", Kind: "open", GoalPerPeriod: 3, PeriodDays: 91, VotesRequired: 2},
		{Name: "r", Kind: "open", GoalPerPeriod: 3, PeriodDays: 7, VotesRequired: 0},
	}
	for i, req := range bad {
		if rec := e.do(t, "POST", "/api/v1/rooms", req, reqOpts{}); rec.Code != http.StatusBadRequest {
			t.Errorf("case %d: %d, want 400", i, rec.Code)
		}
	}

	room := e.createRoom(t, 1, domain.RoomInvite)
	if room.InviteCode == "" || room.CreatorID != 1 {
		t.Errorf("unexpected room: %+v", room)
	}
}

func TestListRooms(t *testing.T) {
	e := newEnv()
	rec := e.do(t, "GET", "/api/v1/rooms", nil, reqOpts{})
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("empty list: %d %q", rec.Code, rec.Body.String())
	}
	e.createRoom(t, 1, domain.RoomOpen)
	list := decode[[]domain.RoomWithProgress](t, e.do(t, "GET", "/api/v1/rooms", nil, reqOpts{}))
	if len(list) != 1 || list[0].MembersCount != 1 {
		t.Errorf("list: %+v", list)
	}
}

func TestListOpenRooms(t *testing.T) {
	e := newEnv()
	open := e.createRoom(t, 1, domain.RoomOpen)
	e.createRoom(t, 1, domain.RoomInvite)

	rooms := decode[[]domain.Room](t, e.do(t, "GET", "/api/v1/rooms/open", nil, reqOpts{userID: 2}))
	if len(rooms) != 1 || rooms[0].ID != open.ID || rooms[0].InviteCode != "" {
		t.Errorf("open rooms: %+v", rooms)
	}

	// the creator is already a member: nothing left to join
	mine := decode[[]domain.Room](t, e.do(t, "GET", "/api/v1/rooms/open", nil, reqOpts{userID: 1}))
	if len(mine) != 0 {
		t.Errorf("open rooms for a member: %+v", mine)
	}
}

// Telegram avatar hosts are unreachable for our users, so the bytes are proxied by core.
func TestGetAvatar(t *testing.T) {
	e := newEnv()
	e.users.users[9] = domain.User{ID: 9, FirstName: "Ann", AvatarKey: "avatars/9", HasAvatar: true}
	e.avatars.objects["avatars/9"] = []byte("jpeg-bytes")

	rec := e.do(t, "GET", "/api/v1/users/9/avatar", nil, reqOpts{userID: 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("get avatar: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "jpeg-bytes" {
		t.Errorf("body = %q, want the stored bytes", rec.Body.String())
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("nosniff header = %q", got)
	}

	e.users.users[10] = domain.User{ID: 10, FirstName: "Bob"}
	rec = e.do(t, "GET", "/api/v1/users/10/avatar", nil, reqOpts{userID: 1})
	if rec.Code != http.StatusNotFound {
		t.Errorf("avatar of a user without one: %d, want 404", rec.Code)
	}
}

func TestStreakReachesTheClient(t *testing.T) {
	e := newEnv()
	room := e.createRoom(t, 1, domain.RoomOpen)
	e.streaks.set(room.ID, 1, 5)

	rec := e.do(t, "GET", "/api/v1/rooms/"+strconv.FormatInt(room.ID, 10), nil, reqOpts{userID: 1})
	detail := decode[httpapi.RoomDetailResponse](t, rec)
	if len(detail.Members) != 1 || detail.Members[0].Streak != 5 {
		t.Errorf("member streak = %+v, want 5", detail.Members)
	}

	rec = e.do(t, "GET", "/api/v1/rooms", nil, reqOpts{userID: 1})
	rooms := decode[[]domain.RoomWithProgress](t, rec)
	if len(rooms) != 1 || rooms[0].Streak != 5 {
		t.Errorf("room streak = %+v, want 5", rooms)
	}

	rec = e.do(t, "GET", "/api/v1/me", nil, reqOpts{userID: 1})
	if me := decode[httpapi.MeResponse](t, rec); me.BestStreak != 5 {
		t.Errorf("best streak = %d, want 5", me.BestStreak)
	}
}

func TestGetRoomVisibility(t *testing.T) {
	e := newEnv()
	open := e.createRoom(t, 1, domain.RoomOpen)
	invite := e.createRoom(t, 1, domain.RoomInvite)

	detail := decode[httpapi.RoomDetailResponse](t, e.do(t, "GET",
		fmt.Sprintf("/api/v1/rooms/%d", open.ID), nil, reqOpts{userID: 2}))
	if detail.Room.InviteCode != "" {
		t.Error("invite code leaked to non-member")
	}

	rec := e.do(t, "GET", fmt.Sprintf("/api/v1/rooms/%d", invite.ID), nil, reqOpts{userID: 2})
	if rec.Code != http.StatusForbidden {
		t.Errorf("invite room for stranger: %d", rec.Code)
	}

	detail = decode[httpapi.RoomDetailResponse](t, e.do(t, "GET",
		fmt.Sprintf("/api/v1/rooms/%d", invite.ID), nil, reqOpts{userID: 1}))
	if detail.Room.InviteCode == "" || len(detail.Members) != 1 {
		t.Errorf("member view: %+v", detail)
	}

	if rec := e.do(t, "GET", "/api/v1/rooms/999", nil, reqOpts{}); rec.Code != http.StatusNotFound {
		t.Errorf("missing room: %d", rec.Code)
	}
	if rec := e.do(t, "GET", "/api/v1/rooms/abc", nil, reqOpts{}); rec.Code != http.StatusBadRequest {
		t.Errorf("bad id: %d", rec.Code)
	}
}

func TestUpdateAndDeleteRoom(t *testing.T) {
	e := newEnv()
	room := e.createRoom(t, 1, domain.RoomOpen)
	path := fmt.Sprintf("/api/v1/rooms/%d", room.ID)

	rec := e.do(t, "PATCH", path, httpapi.UpdateRoomRequest{
		Name: "updated", Kind: domain.RoomInvite, GoalPerPeriod: 5, PeriodDays: 14, VotesRequired: 3,
	}, reqOpts{userID: 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	updated := decode[domain.Room](t, rec)
	if updated.Name != "updated" || updated.Kind != domain.RoomInvite || updated.GoalPerPeriod != 5 || updated.PeriodDays != 14 || updated.VotesRequired != 3 {
		t.Errorf("updated room: %+v", updated)
	}

	if rec := e.do(t, "PATCH", path, httpapi.UpdateRoomRequest{
		Name: "nope", Kind: domain.RoomOpen, GoalPerPeriod: 3, PeriodDays: 7, VotesRequired: 2,
	}, reqOpts{userID: 2}); rec.Code != http.StatusForbidden {
		t.Errorf("non-creator update: %d", rec.Code)
	}
	if rec := e.do(t, "DELETE", path, nil, reqOpts{userID: 2}); rec.Code != http.StatusForbidden {
		t.Errorf("non-creator delete: %d", rec.Code)
	}
	if rec := e.do(t, "DELETE", path, nil, reqOpts{userID: 1}); rec.Code != http.StatusNoContent {
		t.Errorf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if rec := e.do(t, "GET", path, nil, reqOpts{userID: 1}); rec.Code != http.StatusNotFound {
		t.Errorf("deleted room: %d", rec.Code)
	}
}

func TestJoinAndLeave(t *testing.T) {
	e := newEnv()
	open := e.createRoom(t, 1, domain.RoomOpen)
	invite := e.createRoom(t, 1, domain.RoomInvite)

	rec := e.do(t, "POST", fmt.Sprintf("/api/v1/rooms/%d/join", open.ID), nil, reqOpts{userID: 2})
	if rec.Code != http.StatusOK {
		t.Fatalf("join open: %d", rec.Code)
	}
	rec = e.do(t, "POST", fmt.Sprintf("/api/v1/rooms/%d/join", invite.ID), nil, reqOpts{userID: 2})
	if rec.Code != http.StatusForbidden {
		t.Errorf("join invite by id: %d, want 403", rec.Code)
	}

	rec = e.do(t, "POST", "/api/v1/rooms/join",
		httpapi.JoinByCodeRequest{InviteCode: strings.ToLower(invite.InviteCode)}, reqOpts{userID: 2})
	if rec.Code != http.StatusOK {
		t.Errorf("join by lowercase code: %d %s", rec.Code, rec.Body.String())
	}
	rec = e.do(t, "POST", "/api/v1/rooms/join",
		httpapi.JoinByCodeRequest{InviteCode: "WRONG123"}, reqOpts{userID: 2})
	if rec.Code != http.StatusNotFound {
		t.Errorf("bad code: %d", rec.Code)
	}
	rec = e.do(t, "POST", "/api/v1/rooms/join", httpapi.JoinByCodeRequest{}, reqOpts{userID: 2})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty code: %d", rec.Code)
	}

	rec = e.do(t, "POST", fmt.Sprintf("/api/v1/rooms/%d/leave", open.ID), nil, reqOpts{userID: 2})
	if rec.Code != http.StatusNoContent {
		t.Errorf("leave: %d", rec.Code)
	}
	rec = e.do(t, "POST", fmt.Sprintf("/api/v1/rooms/%d/leave", open.ID), nil, reqOpts{userID: 2})
	if rec.Code != http.StatusNotFound {
		t.Errorf("leave twice: %d", rec.Code)
	}
}

func TestCreateCheckinGeo(t *testing.T) {
	e := newEnv()
	room := e.createRoom(t, 1, domain.RoomOpen)

	rec := e.do(t, "POST", "/api/v1/checkins", httpapi.CreateCheckinGeoRequest{
		RoomIDs: []int64{room.ID}, Geo: checkin.Geo{Lat: 55.75, Lon: 37.61, HorizontalAccuracy: 10},
	}, reqOpts{userID: 1})
	if rec.Code != http.StatusCreated {
		t.Fatalf("geo checkin: %d %s", rec.Code, rec.Body.String())
	}
	list := decode[[]checkin.Checkin](t, rec)
	if len(list) != 1 || list[0].VotesRequired != 2 || list[0].Geo == nil || list[0].Geo.HorizontalAccuracy != 10 {
		t.Errorf("unexpected checkin: %+v", list)
	}

	rec = e.do(t, "POST", "/api/v1/checkins", httpapi.CreateCheckinGeoRequest{
		RoomIDs: []int64{room.ID}, Geo: checkin.Geo{Lat: 99, Lon: 0},
	}, reqOpts{userID: 1})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad geo: %d", rec.Code)
	}
	rec = e.do(t, "POST", "/api/v1/checkins", httpapi.CreateCheckinGeoRequest{
		RoomIDs: []int64{room.ID}, Geo: checkin.Geo{Lat: 55, Lon: 37, HorizontalAccuracy: 10},
	}, reqOpts{userID: 2})
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-member checkin: %d", rec.Code)
	}
	rec = e.do(t, "POST", "/api/v1/checkins", httpapi.CreateCheckinGeoRequest{
		RoomIDs: nil, Geo: checkin.Geo{Lat: 55, Lon: 37, HorizontalAccuracy: 10},
	}, reqOpts{userID: 1})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("no rooms: %d", rec.Code)
	}
}

func photoForm(t *testing.T, content []byte, roomIDs ...int64) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("photo", "gym.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatal(err)
	}
	for _, id := range roomIDs {
		if err := mw.WriteField("room_ids", strconv.FormatInt(id, 10)); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

func TestCreateCheckinWithBuddies(t *testing.T) {
	e := newEnv()
	room := e.createRoom(t, 1, domain.RoomOpen)
	e.users.users[2] = domain.User{ID: 2, FirstName: "Ann"}
	e.users.users[3] = domain.User{ID: 3, FirstName: "Bob"}
	if err := e.rooms.Join(t.Context(), room.ID, 2); err != nil {
		t.Fatal(err)
	}

	// 3 never joined this room, so tagging them is quietly dropped instead of failing the checkin
	rec := e.do(t, "POST", "/api/v1/checkins", httpapi.CreateCheckinGeoRequest{
		RoomIDs: []int64{room.ID}, BuddyIDs: []int64{2, 3}, Geo: checkin.Geo{Lat: 55, Lon: 37, HorizontalAccuracy: 10},
	}, reqOpts{userID: 1})
	if rec.Code != http.StatusCreated {
		t.Fatalf("checkin: %d %s", rec.Code, rec.Body.String())
	}
	list := decode[[]checkin.Checkin](t, rec)
	if len(list) != 1 || len(list[0].Buddies) != 1 || list[0].Buddies[0].ID != 2 {
		t.Fatalf("buddies = %+v, want only the room member", list[0].Buddies)
	}
}

func TestBuddyTagging(t *testing.T) {
	e := newEnv()
	room := e.createRoom(t, 1, domain.RoomOpen)
	e.users.users[2] = domain.User{ID: 2, FirstName: "Ann"}
	if err := e.rooms.Join(t.Context(), room.ID, 2); err != nil {
		t.Fatal(err)
	}

	buf, ct := photoForm(t, pngBytes, room.ID)
	rec := e.do(t, "POST", "/api/v1/checkins", buf, reqOpts{userID: 1, contentType: ct})
	id := decode[[]checkin.Checkin](t, rec)[0].ID

	rec = e.do(t, "POST", "/api/v1/checkins/"+id+"/buddies",
		httpapi.BuddiesRequest{UserIDs: []int64{2}}, reqOpts{userID: 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("tag: %d %s", rec.Code, rec.Body.String())
	}
	if buddies := decode[[]domain.User](t, rec); len(buddies) != 1 || buddies[0].ID != 2 {
		t.Fatalf("buddies = %+v", buddies)
	}

	// tagging twice is how the "add more" button behaves, and must not duplicate anyone
	rec = e.do(t, "POST", "/api/v1/checkins/"+id+"/buddies",
		httpapi.BuddiesRequest{UserIDs: []int64{2}}, reqOpts{userID: 1})
	if buddies := decode[[]domain.User](t, rec); len(buddies) != 1 {
		t.Errorf("buddies after a repeat tag = %+v, want one", buddies)
	}

	rec = e.do(t, "POST", "/api/v1/checkins/"+id+"/buddies",
		httpapi.BuddiesRequest{UserIDs: []int64{1}}, reqOpts{userID: 1})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("self tag: %d, want 400", rec.Code)
	}

	rec = e.do(t, "POST", "/api/v1/checkins/"+id+"/buddies",
		httpapi.BuddiesRequest{UserIDs: []int64{2}}, reqOpts{userID: 2})
	if rec.Code != http.StatusForbidden {
		t.Errorf("tag on someone else's checkin: %d, want 403", rec.Code)
	}

	rec = e.do(t, "DELETE", "/api/v1/checkins/"+id+"/buddies/2", nil, reqOpts{userID: 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("untag: %d %s", rec.Code, rec.Body.String())
	}
	if buddies := decode[[]domain.User](t, rec); len(buddies) != 0 {
		t.Errorf("buddies after untag = %+v", buddies)
	}
}

func TestCreateCheckinPhoto(t *testing.T) {
	e := newEnv()
	room := e.createRoom(t, 1, domain.RoomOpen)

	buf, ct := photoForm(t, pngBytes, room.ID)
	rec := e.do(t, "POST", "/api/v1/checkins", buf, reqOpts{userID: 1, contentType: ct})
	if rec.Code != http.StatusCreated {
		t.Fatalf("photo checkin: %d %s", rec.Code, rec.Body.String())
	}
	list := decode[[]checkin.Checkin](t, rec)
	if len(list) != 1 || !list[0].HasPhoto {
		t.Errorf("photo flag missing: %+v", list)
	}

	var empty bytes.Buffer
	mw2 := multipart.NewWriter(&empty)
	mw2.WriteField("other", "x")
	mw2.Close()
	rec = e.do(t, "POST", "/api/v1/checkins", &empty, reqOpts{userID: 1, contentType: mw2.FormDataContentType()})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing photo field: %d", rec.Code)
	}
}

// One proof submitted to several rooms must produce one checkin per room while the
// photo itself is uploaded exactly once.
func TestCreateCheckinAcrossRoomsStoresPhotoOnce(t *testing.T) {
	e := newEnv()
	first := e.createRoom(t, 1, domain.RoomOpen)
	second := e.createRoom(t, 1, domain.RoomOpen)
	third := e.createRoom(t, 1, domain.RoomOpen)

	buf, ct := photoForm(t, pngBytes, first.ID, second.ID, third.ID)
	rec := e.do(t, "POST", "/api/v1/checkins", buf, reqOpts{userID: 1, contentType: ct})
	if rec.Code != http.StatusCreated {
		t.Fatalf("multi-room checkin: %d %s", rec.Code, rec.Body.String())
	}

	list := decode[[]checkin.Checkin](t, rec)
	if len(list) != 3 {
		t.Fatalf("want 3 checkins, got %d", len(list))
	}
	if got := []int64{list[0].RoomID, list[1].RoomID, list[2].RoomID}; got[0] != first.ID || got[1] != second.ID || got[2] != third.ID {
		t.Errorf("rooms out of order: %v", got)
	}
	if len(e.checkins.photos) != 1 {
		t.Errorf("photo stored %d times, want 1", len(e.checkins.photos))
	}
}

// Membership is checked for every room, so a member of one room cannot slip a
// checkin into a room they do not belong to.
func TestCreateCheckinRejectsRoomsUserIsNotIn(t *testing.T) {
	e := newEnv()
	mine := e.createRoom(t, 1, domain.RoomOpen)
	stranger := e.createRoom(t, 99, domain.RoomOpen)

	buf, ct := photoForm(t, pngBytes, mine.ID, stranger.ID)
	rec := e.do(t, "POST", "/api/v1/checkins", buf, reqOpts{userID: 1, contentType: ct})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403 for foreign room, got %d", rec.Code)
	}
	if len(e.checkins.checkins) != 0 {
		t.Errorf("no checkin may be created when one room is rejected")
	}
}

func TestCreateCheckinRejectsDuplicateRooms(t *testing.T) {
	e := newEnv()
	room := e.createRoom(t, 1, domain.RoomOpen)

	buf, ct := photoForm(t, pngBytes, room.ID, room.ID)
	if rec := e.do(t, "POST", "/api/v1/checkins", buf, reqOpts{userID: 1, contentType: ct}); rec.Code != http.StatusBadRequest {
		t.Errorf("duplicate room: %d, want 400", rec.Code)
	}
}

// A declared image content type proves nothing: the bytes must actually be an image,
// otherwise an SVG or HTML payload could be stored and later served as active content.
func TestCreateCheckinRejectsNonImagePayload(t *testing.T) {
	e := newEnv()
	room := e.createRoom(t, 1, domain.RoomOpen)

	for name, payload := range map[string][]byte{
		"svg":   []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"html":  []byte("<!DOCTYPE html><script>alert(1)</script>"),
		"plain": []byte("fake-jpeg-bytes"),
	} {
		buf, ct := photoForm(t, payload, room.ID)
		rec := e.do(t, "POST", "/api/v1/checkins", buf, reqOpts{userID: 1, contentType: ct})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s payload accepted: %d, want 400", name, rec.Code)
		}
	}
}

func TestGetCheckinPhotoRequiresMembership(t *testing.T) {
	e := newEnv()
	room := e.createRoom(t, 1, domain.RoomOpen)
	e.do(t, "POST", fmt.Sprintf("/api/v1/rooms/%d/join", room.ID), nil, reqOpts{userID: 2})

	buf, ct := photoForm(t, pngBytes, room.ID)
	rec := e.do(t, "POST", "/api/v1/checkins", buf, reqOpts{userID: 1, contentType: ct})
	created := decode[[]checkin.Checkin](t, rec)[0]
	path := "/api/v1/checkins/" + created.ID + "/photo"

	rec = e.do(t, "GET", path, nil, reqOpts{userID: 2})
	if rec.Code != http.StatusOK {
		t.Fatalf("member fetch: %d %s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), pngBytes) {
		t.Errorf("photo bytes mismatch")
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("content type = %q", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("nosniff missing: %q", got)
	}

	if rec := e.do(t, "GET", path, nil, reqOpts{userID: 77}); rec.Code != http.StatusForbidden {
		t.Errorf("stranger photo fetch: %d, want 403", rec.Code)
	}
	if rec := e.do(t, "GET", path, nil, reqOpts{noAuth: true}); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous photo fetch: %d, want 401", rec.Code)
	}
}

func TestListCheckins(t *testing.T) {
	e := newEnv()
	room := e.createRoom(t, 1, domain.RoomOpen)
	path := fmt.Sprintf("/api/v1/rooms/%d/checkins", room.ID)

	e.do(t, "POST", "/api/v1/checkins", httpapi.CreateCheckinGeoRequest{
		RoomIDs: []int64{room.ID}, Geo: checkin.Geo{Lat: 55, Lon: 37, HorizontalAccuracy: 10},
	}, reqOpts{userID: 1})

	rec := e.do(t, "GET", path, nil, reqOpts{userID: 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	if list := decode[[]checkin.Checkin](t, rec); len(list) != 1 {
		t.Errorf("list len = %d", len(list))
	}

	if rec := e.do(t, "GET", path+"?status=weird", nil, reqOpts{userID: 1}); rec.Code != http.StatusBadRequest {
		t.Errorf("bad status filter: %d", rec.Code)
	}
	if rec := e.do(t, "GET", path+"?limit=1000", nil, reqOpts{userID: 1}); rec.Code != http.StatusBadRequest {
		t.Errorf("bad limit: %d", rec.Code)
	}
	if rec := e.do(t, "GET", path, nil, reqOpts{userID: 5}); rec.Code != http.StatusForbidden {
		t.Errorf("non-member list: %d", rec.Code)
	}
}

func TestVote(t *testing.T) {
	e := newEnv()
	room := e.createRoom(t, 1, domain.RoomOpen)
	e.do(t, "POST", fmt.Sprintf("/api/v1/rooms/%d/join", room.ID), nil, reqOpts{userID: 2})

	rec := e.do(t, "POST", "/api/v1/checkins", httpapi.CreateCheckinGeoRequest{
		RoomIDs: []int64{room.ID}, Geo: checkin.Geo{Lat: 55, Lon: 37, HorizontalAccuracy: 10},
	}, reqOpts{userID: 1})
	c := decode[[]checkin.Checkin](t, rec)[0]
	votePath := "/api/v1/checkins/" + c.ID + "/vote"

	rec = e.do(t, "POST", votePath, httpapi.VoteRequest{Approve: true}, reqOpts{userID: 2})
	if rec.Code != http.StatusOK {
		t.Fatalf("vote: %d %s", rec.Code, rec.Body.String())
	}
	if voted := decode[checkin.Checkin](t, rec); voted.VotesApprove != 1 {
		t.Errorf("votes: %+v", voted)
	}

	rec = e.do(t, "POST", votePath, httpapi.VoteRequest{Approve: true}, reqOpts{userID: 1})
	if rec.Code != http.StatusForbidden {
		t.Errorf("self vote: %d", rec.Code)
	}
	rec = e.do(t, "POST", votePath, httpapi.VoteRequest{Approve: true}, reqOpts{userID: 5})
	if rec.Code != http.StatusForbidden {
		t.Errorf("stranger vote: %d", rec.Code)
	}
	rec = e.do(t, "POST", "/api/v1/checkins/nope/vote", httpapi.VoteRequest{Approve: true}, reqOpts{userID: 2})
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing checkin: %d", rec.Code)
	}
}

func TestCheckinServiceDown(t *testing.T) {
	e := newEnv()
	room := e.createRoom(t, 1, domain.RoomOpen)
	e.checkins.err = status.Error(codes.Unavailable, "connection refused")

	rec := e.do(t, "POST", "/api/v1/checkins", httpapi.CreateCheckinGeoRequest{
		RoomIDs: []int64{room.ID}, Geo: checkin.Geo{Lat: 55, Lon: 37, HorizontalAccuracy: 10},
	}, reqOpts{userID: 1})
	if rec.Code != http.StatusBadGateway {
		t.Errorf("unavailable mapping: %d, want 502", rec.Code)
	}
}

// smallest valid PNG; photo validation sniffs magic bytes, not the declared type
var pngBytes = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

func TestCheckinComments(t *testing.T) {
	e := newEnv()
	room := e.createRoom(t, 1, domain.RoomOpen)
	e.users.users[2] = domain.User{ID: 2, FirstName: "Ann"}
	e.users.users[3] = domain.User{ID: 3, FirstName: "Stranger"}
	if err := e.rooms.Join(t.Context(), room.ID, 2); err != nil {
		t.Fatal(err)
	}

	buf, ct := photoForm(t, pngBytes, room.ID)
	rec := e.do(t, "POST", "/api/v1/checkins", buf, reqOpts{userID: 1, contentType: ct})
	id := decode[[]checkin.Checkin](t, rec)[0].ID

	rec = e.do(t, "POST", "/api/v1/checkins/"+id+"/comments",
		httpapi.CommentRequest{Body: "  Красавчик  "}, reqOpts{userID: 2})
	if rec.Code != http.StatusCreated {
		t.Fatalf("comment: %d %s", rec.Code, rec.Body.String())
	}
	comment := decode[domain.Comment](t, rec)
	if comment.Body != "Красавчик" || comment.Author.ID != 2 {
		t.Errorf("comment = %+v, want the trimmed body and the author", comment)
	}

	bad := []httpapi.CommentRequest{{Body: "   "}, {Body: strings.Repeat("я", 501)}}
	for i, req := range bad {
		if rec := e.do(t, "POST", "/api/v1/checkins/"+id+"/comments", req, reqOpts{userID: 2}); rec.Code != http.StatusBadRequest {
			t.Errorf("case %d: %d, want 400", i, rec.Code)
		}
	}

	rec = e.do(t, "POST", "/api/v1/checkins/"+id+"/comments",
		httpapi.CommentRequest{Body: "hi"}, reqOpts{userID: 3})
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-member comment: %d, want 403", rec.Code)
	}

	// the room creator moderates: they can delete a comment they did not write
	path := "/api/v1/checkins/" + id + "/comments/" + strconv.FormatInt(comment.ID, 10)
	if rec := e.do(t, "DELETE", path, nil, reqOpts{userID: 1}); rec.Code != http.StatusNoContent {
		t.Fatalf("delete by the room creator: %d %s", rec.Code, rec.Body.String())
	}
	rec = e.do(t, "GET", "/api/v1/checkins/"+id+"/comments", nil, reqOpts{userID: 1})
	if list := decode[[]domain.Comment](t, rec); len(list) != 0 {
		t.Errorf("list after delete = %+v", list)
	}
}

func TestCommentLikes(t *testing.T) {
	e := newEnv()
	room := e.createRoom(t, 1, domain.RoomOpen)
	e.users.users[2] = domain.User{ID: 2, FirstName: "Ann"}
	if err := e.rooms.Join(t.Context(), room.ID, 2); err != nil {
		t.Fatal(err)
	}

	buf, ct := photoForm(t, pngBytes, room.ID)
	rec := e.do(t, "POST", "/api/v1/checkins", buf, reqOpts{userID: 1, contentType: ct})
	id := decode[[]checkin.Checkin](t, rec)[0].ID

	rec = e.do(t, "POST", "/api/v1/checkins/"+id+"/comments",
		httpapi.CommentRequest{Body: "мем"}, reqOpts{userID: 2})
	comment := decode[domain.Comment](t, rec)
	like := "/api/v1/checkins/" + id + "/comments/" + strconv.FormatInt(comment.ID, 10) + "/like"

	rec = e.do(t, "POST", like, nil, reqOpts{userID: 1})
	if got := decode[domain.Comment](t, rec); got.Likes != 1 || !got.LikedByMe {
		t.Fatalf("after liking = %+v, want one like and a filled heart", got)
	}
	// the heart is a toggle, not a counter: a double tap must not stack
	rec = e.do(t, "POST", like, nil, reqOpts{userID: 1})
	if got := decode[domain.Comment](t, rec); got.Likes != 1 {
		t.Errorf("liking twice = %d likes, want 1", got.Likes)
	}

	// the card shows the most liked comment without fetching the thread
	rec = e.do(t, "GET", "/api/v1/rooms/"+strconv.FormatInt(room.ID, 10)+"/checkins", nil, reqOpts{userID: 1})
	feed := decode[[]checkin.Checkin](t, rec)
	if len(feed) != 1 || feed[0].CommentsCount != 1 || feed[0].TopComment == nil {
		t.Fatalf("feed = %+v, want a count and a top comment", feed)
	}
	if feed[0].TopComment.Body != "мем" {
		t.Errorf("top comment = %+v", feed[0].TopComment)
	}

	rec = e.do(t, "DELETE", like, nil, reqOpts{userID: 1})
	if got := decode[domain.Comment](t, rec); got.Likes != 0 || got.LikedByMe {
		t.Errorf("after unliking = %+v, want no likes", got)
	}
}

// A meme needs no caption: an empty body is fine when a photo carries the comment.
func TestCommentWithPhoto(t *testing.T) {
	e := newEnv()
	room := e.createRoom(t, 1, domain.RoomOpen)

	buf, ct := photoForm(t, pngBytes, room.ID)
	rec := e.do(t, "POST", "/api/v1/checkins", buf, reqOpts{userID: 1, contentType: ct})
	id := decode[[]checkin.Checkin](t, rec)[0].ID

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("photo", "meme.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(pngBytes); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	rec = e.do(t, "POST", "/api/v1/checkins/"+id+"/comments", &body,
		reqOpts{userID: 1, contentType: mw.FormDataContentType()})
	if rec.Code != http.StatusCreated {
		t.Fatalf("photo comment: %d %s", rec.Code, rec.Body.String())
	}
	comment := decode[domain.Comment](t, rec)
	if !comment.HasPhoto || comment.Body != "" {
		t.Fatalf("comment = %+v, want a photo and no caption", comment)
	}

	photoPath := "/api/v1/checkins/" + id + "/comments/" + strconv.FormatInt(comment.ID, 10) + "/photo"
	rec = e.do(t, "GET", photoPath, nil, reqOpts{userID: 1})
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), pngBytes) {
		t.Fatalf("photo: %d, %d bytes", rec.Code, rec.Body.Len())
	}

	// deleting the comment must take the object with it, not leak it into the bucket
	del := "/api/v1/checkins/" + id + "/comments/" + strconv.FormatInt(comment.ID, 10)
	if rec := e.do(t, "DELETE", del, nil, reqOpts{userID: 1}); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	if len(e.commentPhotos.objects) != 0 {
		t.Errorf("objects left behind: %v", e.commentPhotos.objects)
	}
}

// Any member may add a room picture; the older ones stay browsable in the gallery.
func TestRoomAvatarGalleryOverHTTP(t *testing.T) {
	e := newEnv()
	room := e.createRoom(t, 1, domain.RoomOpen)
	if err := e.rooms.Join(t.Context(), room.ID, 2); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/rooms/" + strconv.FormatInt(room.ID, 10)

	if rec := e.do(t, "GET", base+"/avatar", nil, reqOpts{userID: 1}); rec.Code != http.StatusNotFound {
		t.Fatalf("picture before upload: %d, want 404", rec.Code)
	}

	buf, ct := photoForm(t, pngBytes)
	rec := e.do(t, "PUT", base+"/avatar", buf, reqOpts{userID: 1, contentType: ct})
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload by the creator: %d %s", rec.Code, rec.Body.String())
	}
	first := decode[domain.RoomAvatar](t, rec)

	// a plain member replaces it, and the room now wears their picture
	buf, ct = photoForm(t, []byte("GIF89a-second-picture"))
	rec = e.do(t, "PUT", base+"/avatar", buf, reqOpts{userID: 2, contentType: ct})
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload by a member: %d %s", rec.Code, rec.Body.String())
	}
	second := decode[domain.RoomAvatar](t, rec)

	gallery := decode[[]domain.RoomAvatar](t, e.do(t, "GET", base+"/avatars", nil, reqOpts{userID: 2}))
	if len(gallery) != 2 || gallery[0].ID != second.ID || !gallery[0].IsCurrent {
		t.Fatalf("gallery: %+v", gallery)
	}

	rec = e.do(t, "GET", base+"/avatar", nil, reqOpts{userID: 1})
	if rec.Code != http.StatusOK || rec.Body.String() != "GIF89a-second-picture" {
		t.Errorf("current picture: %d %q", rec.Code, rec.Body.String())
	}
	// the older picture is still reachable by id, which is what the gallery swipes through
	path := base + "/avatars/" + strconv.FormatInt(first.ID, 10)
	rec = e.do(t, "GET", path, nil, reqOpts{userID: 2})
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), pngBytes) {
		t.Errorf("older picture: %d %q", rec.Code, rec.Body.String())
	}

	rooms := decode[[]domain.RoomWithProgress](t, e.do(t, "GET", "/api/v1/rooms", nil, reqOpts{userID: 1}))
	if len(rooms) != 1 || !rooms[0].HasAvatar {
		t.Errorf("room list lost has_avatar: %+v", rooms)
	}
}

func TestRoomAvatarDeleteRules(t *testing.T) {
	e := newEnv()
	room := e.createRoom(t, 1, domain.RoomOpen)
	if err := e.rooms.Join(t.Context(), room.ID, 2); err != nil {
		t.Fatal(err)
	}
	if err := e.rooms.Join(t.Context(), room.ID, 3); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/rooms/" + strconv.FormatInt(room.ID, 10)

	buf, ct := photoForm(t, pngBytes)
	mine := decode[domain.RoomAvatar](t, e.do(t, "PUT", base+"/avatar", buf, reqOpts{userID: 2, contentType: ct}))
	path := base + "/avatars/" + strconv.FormatInt(mine.ID, 10)

	if rec := e.do(t, "DELETE", path, nil, reqOpts{userID: 3}); rec.Code != http.StatusForbidden {
		t.Errorf("delete by an unrelated member: %d, want 403", rec.Code)
	}
	// the room creator moderates, the uploader owns their own picture
	if rec := e.do(t, "DELETE", path, nil, reqOpts{userID: 1}); rec.Code != http.StatusNoContent {
		t.Fatalf("delete by the creator: %d %s", rec.Code, rec.Body.String())
	}
	if _, ok := e.avatars.objects[mine.ObjectKey]; ok {
		t.Error("object survived the delete")
	}
	if rec := e.do(t, "GET", base+"/avatar", nil, reqOpts{userID: 1}); rec.Code != http.StatusNotFound {
		t.Errorf("picture after the gallery is emptied: %d, want 404", rec.Code)
	}
}

func TestRoomAvatarOutsidersAndJunk(t *testing.T) {
	e := newEnv()
	room := e.createRoom(t, 1, domain.RoomInvite)
	base := "/api/v1/rooms/" + strconv.FormatInt(room.ID, 10)

	buf, ct := photoForm(t, pngBytes)
	if rec := e.do(t, "PUT", base+"/avatar", buf, reqOpts{userID: 7, contentType: ct}); rec.Code != http.StatusForbidden {
		t.Errorf("upload by a non-member: %d, want 403", rec.Code)
	}
	buf, ct = photoForm(t, []byte("not an image at all"))
	if rec := e.do(t, "PUT", base+"/avatar", buf, reqOpts{userID: 1, contentType: ct}); rec.Code != http.StatusBadRequest {
		t.Errorf("non-image upload: %d, want 400", rec.Code)
	}
}

func TestUpdateMembership(t *testing.T) {
	e := newEnv()
	room := e.createRoom(t, 1, domain.RoomOpen)
	base := fmt.Sprintf("/api/v1/rooms/%d/membership", room.ID)

	goal := 2
	rec := e.do(t, "PATCH", base, httpapi.UpdateMembershipRequest{
		SportName: "climbing", SportEmoji: "🧗", GoalPerPeriod: &goal,
	}, reqOpts{userID: 1})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("update membership: %d %s", rec.Code, rec.Body.String())
	}
	got := e.rooms.settings[[2]int64{room.ID, 1}]
	if got.SportName != "climbing" || got.SportEmoji != "🧗" || got.Goal == nil || *got.Goal != 2 {
		t.Errorf("settings not stored: %+v", got)
	}

	rec = e.do(t, "PATCH", base, httpapi.UpdateMembershipRequest{}, reqOpts{userID: 1})
	if rec.Code != http.StatusNoContent {
		t.Errorf("reset to defaults: %d %s", rec.Code, rec.Body.String())
	}
	if got := e.rooms.settings[[2]int64{room.ID, 1}]; got.SportName != "" || got.Goal != nil {
		t.Errorf("settings not reset: %+v", got)
	}

	if rec := e.do(t, "PATCH", base, httpapi.UpdateMembershipRequest{}, reqOpts{userID: 9}); rec.Code != http.StatusForbidden {
		t.Errorf("non-member: %d, want 403", rec.Code)
	}
	if rec := e.do(t, "PATCH", base, httpapi.UpdateMembershipRequest{SportEmoji: "🚗"}, reqOpts{userID: 1}); rec.Code != http.StatusBadRequest {
		t.Errorf("non-sport emoji: %d, want 400", rec.Code)
	}
	bad := 0
	if rec := e.do(t, "PATCH", base, httpapi.UpdateMembershipRequest{GoalPerPeriod: &bad}, reqOpts{userID: 1}); rec.Code != http.StatusBadRequest {
		t.Errorf("zero goal: %d, want 400", rec.Code)
	}
	long := strings.Repeat("x", 33)
	if rec := e.do(t, "PATCH", base, httpapi.UpdateMembershipRequest{SportName: long}, reqOpts{userID: 1}); rec.Code != http.StatusBadRequest {
		t.Errorf("long sport name: %d, want 400", rec.Code)
	}
}
