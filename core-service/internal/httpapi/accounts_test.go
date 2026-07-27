package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kosttiik/BuddyGym/core-service/internal/auth"
	"github.com/kosttiik/BuddyGym/core-service/internal/domain"
	"github.com/kosttiik/BuddyGym/core-service/internal/httpapi"
	"github.com/kosttiik/BuddyGym/core-service/internal/storage"
)

type fakeAccounts struct {
	users         *fakeUsers
	passwords     map[int64]string
	telegram      map[int64]int64
	sessions      map[string]int64
	links         map[string]int64
	states        map[int64]domain.AccountState
	subscriptions map[string]domain.PushSubscription
	nextID        int64
}

func newFakeAccounts(users *fakeUsers) *fakeAccounts {
	return &fakeAccounts{
		users:         users,
		passwords:     map[int64]string{},
		telegram:      map[int64]int64{},
		sessions:      map[string]int64{},
		links:         map[string]int64{},
		states:        map[int64]domain.AccountState{},
		subscriptions: map[string]domain.PushSubscription{},
		nextID:        100,
	}
}

func duplicate() error {
	return &pgconn.PgError{Code: "23505"}
}

func (f *fakeAccounts) usernameTaken(username string, except int64) bool {
	for id, user := range f.users.users {
		if id != except && strings.EqualFold(user.Username, username) {
			return true
		}
	}
	return false
}

func (f *fakeAccounts) UserByTelegram(_ context.Context, telegramID int64) (domain.User, error) {
	id, ok := f.telegram[telegramID]
	if !ok {
		return domain.User{}, storage.ErrNotFound
	}
	return f.users.users[id], nil
}

func (f *fakeAccounts) CreateTelegramUser(
	_ context.Context,
	telegramID int64,
	username, firstName, photoURL string,
) (domain.User, error) {
	if _, exists := f.telegram[telegramID]; exists || f.usernameTaken(username, 0) {
		return domain.User{}, duplicate()
	}
	f.nextID++
	user, _ := f.users.Upsert(context.Background(), f.nextID, username, firstName, photoURL)
	f.telegram[telegramID] = user.ID
	f.states[user.ID] = domain.AccountState{
		TelegramLinked: true, TelegramNotificationsEnabled: true,
	}
	return user, nil
}

func (f *fakeAccounts) CreateLocalUser(
	_ context.Context,
	username, firstName, passwordHash string,
) (domain.User, error) {
	if f.usernameTaken(username, 0) {
		return domain.User{}, duplicate()
	}
	f.nextID++
	user, _ := f.users.Upsert(context.Background(), f.nextID, username, firstName, "")
	f.passwords[user.ID] = passwordHash
	f.states[user.ID] = domain.AccountState{
		WebLoginEnabled: true, TelegramNotificationsEnabled: true,
	}
	return user, nil
}

func (f *fakeAccounts) PasswordUser(
	_ context.Context,
	username string,
) (domain.User, string, error) {
	for id, user := range f.users.users {
		if strings.EqualFold(user.Username, username) {
			hash, ok := f.passwords[id]
			if !ok {
				break
			}
			return user, hash, nil
		}
	}
	return domain.User{}, "", storage.ErrNotFound
}

func (f *fakeAccounts) CredentialHash(_ context.Context, userID int64) (string, error) {
	hash, ok := f.passwords[userID]
	if !ok {
		return "", storage.ErrNotFound
	}
	return hash, nil
}

func (f *fakeAccounts) SetCredentials(
	_ context.Context,
	userID int64,
	username, passwordHash string,
) (domain.User, error) {
	if f.usernameTaken(username, userID) {
		return domain.User{}, duplicate()
	}
	user := f.users.users[userID]
	user.Username = username
	f.users.users[userID] = user
	f.passwords[userID] = passwordHash
	state := f.states[userID]
	state.WebLoginEnabled = true
	f.states[userID] = state
	clear(f.sessions)
	return user, nil
}

func (f *fakeAccounts) UpdateProfile(
	_ context.Context,
	userID int64,
	username, firstName string,
) (domain.User, error) {
	if f.usernameTaken(username, userID) {
		return domain.User{}, duplicate()
	}
	user := f.users.users[userID]
	user.Username, user.FirstName = username, firstName
	f.users.users[userID] = user
	return user, nil
}

func (f *fakeAccounts) State(_ context.Context, userID int64) (domain.AccountState, error) {
	return f.states[userID], nil
}

func (f *fakeAccounts) CreateSession(
	_ context.Context,
	userID int64,
	raw string,
	_ time.Time,
) error {
	f.sessions[raw] = userID
	return nil
}

func (f *fakeAccounts) RotateSession(
	_ context.Context,
	current, next string,
	_ time.Time,
) (domain.User, error) {
	id, ok := f.sessions[current]
	if !ok {
		return domain.User{}, storage.ErrNotFound
	}
	delete(f.sessions, current)
	f.sessions[next] = id
	return f.users.users[id], nil
}

func (f *fakeAccounts) RevokeSession(_ context.Context, raw string) error {
	delete(f.sessions, raw)
	return nil
}

func (f *fakeAccounts) CreateLinkToken(
	_ context.Context,
	userID int64,
	raw string,
	_ time.Time,
) error {
	f.links[raw] = userID
	return nil
}

func (f *fakeAccounts) LinkTelegram(
	_ context.Context,
	raw string,
	telegramID int64,
) (domain.User, error) {
	id, ok := f.links[raw]
	if !ok {
		return domain.User{}, storage.ErrNotFound
	}
	if _, exists := f.telegram[telegramID]; exists {
		return domain.User{}, duplicate()
	}
	delete(f.links, raw)
	f.telegram[telegramID] = id
	state := f.states[id]
	state.TelegramLinked = true
	state.TelegramNotificationsEnabled = true
	f.states[id] = state
	return f.users.users[id], nil
}

func (f *fakeAccounts) SetCustomAvatar(
	_ context.Context,
	userID int64,
	key string,
) (domain.User, string, error) {
	user := f.users.users[userID]
	old := user.AvatarKey
	user.AvatarKey, user.HasAvatar = key, true
	f.users.users[userID] = user
	return user, old, nil
}

func (f *fakeAccounts) ClearCustomAvatar(
	_ context.Context,
	userID int64,
) (domain.User, string, error) {
	user := f.users.users[userID]
	old := user.AvatarKey
	user.AvatarKey, user.HasAvatar = "", false
	f.users.users[userID] = user
	return user, old, nil
}

func (f *fakeAccounts) UpsertPushSubscription(
	_ context.Context,
	_ int64,
	endpoint, p256dh, secret string,
) (domain.PushSubscription, error) {
	sub := domain.PushSubscription{
		ID: uuid.NewString(), Endpoint: endpoint, P256DH: p256dh, Auth: secret,
	}
	f.subscriptions[sub.ID] = sub
	return sub, nil
}

func (f *fakeAccounts) DeletePushSubscription(
	_ context.Context,
	_ int64,
	id string,
) error {
	if _, ok := f.subscriptions[id]; !ok {
		return storage.ErrNotFound
	}
	delete(f.subscriptions, id)
	return nil
}

func (f *fakeAccounts) UpdateNotificationPreferences(
	_ context.Context,
	userID int64,
	push, telegram *bool,
) (domain.AccountState, error) {
	state := f.states[userID]
	if push != nil {
		state.PushEnabled = *push
	}
	if telegram != nil {
		state.TelegramNotificationsEnabled = *telegram
	}
	f.states[userID] = state
	return state, nil
}

func webEnv() (*env, *fakeAccounts) {
	var accounts *fakeAccounts
	e := newEnv(func(opts *httpapi.Options) {
		accounts = newFakeAccounts(opts.Users.(*fakeUsers))
		opts.Accounts = accounts
		opts.RefreshTTL = 30 * 24 * time.Hour
		opts.VAPIDPublicKey = "test-public-key"
	})
	return e, accounts
}

func TestWebRegisterLoginRefreshLogout(t *testing.T) {
	e, accounts := webEnv()
	body := httpapi.AuthPasswordRequest{
		Username: "Athlete_1", FirstName: "Атлет", Password: "a secure password",
	}
	rec := e.do(t, http.MethodPost, "/api/v1/auth/register", body, reqOpts{noAuth: true})
	if rec.Code != http.StatusOK {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	response := decode[httpapi.AuthTelegramResponse](t, rec)
	if response.Token == "" || response.User.Username != "athlete_1" {
		t.Fatalf("unexpected response: %+v", response)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("refresh cookie is not protected: %+v", cookies)
	}

	rec = e.do(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "athlete_1", "password": "wrong password here",
	}, reqOpts{noAuth: true})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", rec.Code)
	}

	rec = e.do(t, http.MethodPost, "/api/v1/auth/login", body, reqOpts{noAuth: true})
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	refresh := rec.Result().Cookies()[0]
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(refresh)
	refreshRec := httptest.NewRecorder()
	e.handler.ServeHTTP(refreshRec, req)
	if refreshRec.Code != http.StatusOK {
		t.Fatalf("refresh: %d %s", refreshRec.Code, refreshRec.Body.String())
	}
	if len(accounts.sessions) == 0 {
		t.Fatal("rotated session was not stored")
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutReq.AddCookie(refreshRec.Result().Cookies()[0])
	logoutRec := httptest.NewRecorder()
	e.handler.ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", logoutRec.Code)
	}
}

func TestWebAccountValidationAndPush(t *testing.T) {
	e, accounts := webEnv()
	rec := e.do(t, http.MethodPost, "/api/v1/auth/register", httpapi.AuthPasswordRequest{
		Username: "bad nick", FirstName: "Test", Password: "a secure password",
	}, reqOpts{noAuth: true})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid username: %d", rec.Code)
	}
	rec = e.do(t, http.MethodPost, "/api/v1/auth/register", httpapi.AuthPasswordRequest{
		Username: "too_short", FirstName: "Test", Password: "1234567",
	}, reqOpts{noAuth: true})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("seven-character password: %d", rec.Code)
	}
	rec = e.do(t, http.MethodPost, "/api/v1/auth/register", httpapi.AuthPasswordRequest{
		Username: "eight_ok", FirstName: "Test", Password: "12345678",
	}, reqOpts{noAuth: true})
	if rec.Code != http.StatusOK {
		t.Fatalf("eight-character password: %d %s", rec.Code, rec.Body.String())
	}

	user, _ := accounts.CreateLocalUser(context.Background(), "athlete", "Test", "hash")
	rec = e.do(t, http.MethodPost, "/api/v1/me/push-subscriptions", map[string]string{
		"endpoint": "http://invalid", "p256dh": "key", "auth": "secret",
	}, reqOpts{userID: user.ID})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid subscription: %d", rec.Code)
	}
	rec = e.do(t, http.MethodPost, "/api/v1/me/push-subscriptions", map[string]string{
		"endpoint": "https://push.example.test/sub", "p256dh": "key", "auth": "secret",
	}, reqOpts{userID: user.ID})
	if rec.Code != http.StatusOK {
		t.Fatalf("subscription: %d %s", rec.Code, rec.Body.String())
	}
	sub := decode[domain.PushSubscription](t, rec)
	rec = e.do(t, http.MethodDelete, "/api/v1/me/push-subscriptions/"+sub.ID, nil, reqOpts{userID: user.ID})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete subscription: %d", rec.Code)
	}
}

func TestWebCredentialsRequireCurrentPasswordAfterEnrollment(t *testing.T) {
	e, accounts := webEnv()
	user, _ := e.users.Upsert(context.Background(), 42, "telegram_user", "Test", "")
	accounts.states[user.ID] = domain.AccountState{
		TelegramLinked: true, TelegramNotificationsEnabled: true,
	}
	rec := e.do(t, http.MethodPatch, "/api/v1/me/credentials", map[string]string{
		"username": "telegram_user", "password": "first secure password",
	}, reqOpts{userID: user.ID})
	if rec.Code != http.StatusOK {
		t.Fatalf("enable credentials: %d %s", rec.Code, rec.Body.String())
	}
	response := decode[httpapi.AuthTelegramResponse](t, rec)
	if response.Token == "" {
		t.Fatal("new access token is missing")
	}

	rec = e.do(t, http.MethodPatch, "/api/v1/me/credentials", map[string]string{
		"username":         "telegram_user",
		"password":         "second secure password",
		"current_password": "wrong current password",
	}, reqOpts{userID: user.ID})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current password: %d", rec.Code)
	}

	rec = e.do(t, http.MethodPatch, "/api/v1/me/credentials", map[string]string{
		"username":         "telegram_user",
		"password":         "second secure password",
		"current_password": "first secure password",
	}, reqOpts{userID: user.ID})
	if rec.Code != http.StatusOK {
		t.Fatalf("change password: %d %s", rec.Code, rec.Body.String())
	}

	token, err := auth.IssueTokenWithMethod(
		jwtSecret,
		user.ID,
		auth.AuthMethodTelegram,
		time.Hour,
		time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]string{
		"username": "telegram_user",
		"password": "recovered secure password",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/me/credentials", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("telegram recovery: %d %s", rec.Code, rec.Body.String())
	}
}
