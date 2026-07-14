package avatar_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kosttiik/BuddyGym/core-service/internal/avatar"
)

type fakeUsers struct {
	key, source string
	calls       int
}

func (f *fakeUsers) SetAvatar(_ context.Context, _ int64, key, source string) error {
	f.key, f.source = key, source
	f.calls++
	return nil
}

type fakeFetcher struct {
	data  []byte
	err   error
	calls int
}

func (f *fakeFetcher) Fetch(context.Context, int64) ([]byte, error) {
	f.calls++
	return f.data, f.err
}

type fakeStore struct {
	objects map[string][]byte
}

func (f *fakeStore) Put(_ context.Context, key string, data []byte) error {
	f.objects[key] = data
	return nil
}

func newMirror(t *testing.T, fetcher *fakeFetcher) (*avatar.Mirror, *fakeUsers, *fakeStore) {
	t.Helper()
	users := &fakeUsers{}
	store := &fakeStore{objects: map[string][]byte{}}
	return avatar.NewMirror(users, fetcher, store, nil), users, store
}

// Re-authenticating with the same picture must not hammer the Bot API on every login.
func TestSyncSkipsWhenSourceUnchanged(t *testing.T) {
	fetcher := &fakeFetcher{data: []byte("jpeg")}
	mirror, users, _ := newMirror(t, fetcher)

	err := mirror.Sync(context.Background(), 7, "https://t.me/pic/a", "https://t.me/pic/a")
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if fetcher.calls != 0 || users.calls != 0 {
		t.Errorf("fetched %d times, wrote %d times, want 0 and 0", fetcher.calls, users.calls)
	}
}

func TestSyncMirrorsNewPicture(t *testing.T) {
	fetcher := &fakeFetcher{data: []byte("jpeg")}
	mirror, users, store := newMirror(t, fetcher)

	if err := mirror.Sync(context.Background(), 7, "https://t.me/pic/b", "https://t.me/pic/a"); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if got := string(store.objects["avatars/7"]); got != "jpeg" {
		t.Errorf("stored %q, want the fetched bytes", got)
	}
	if users.key != "avatars/7" || users.source != "https://t.me/pic/b" {
		t.Errorf("recorded key=%q source=%q", users.key, users.source)
	}
}

// A user who cleared their picture must lose the mirror too, not keep a stale face.
func TestSyncClearsWhenTelegramHasNoPicture(t *testing.T) {
	fetcher := &fakeFetcher{data: []byte("jpeg")}
	mirror, users, store := newMirror(t, fetcher)

	if err := mirror.Sync(context.Background(), 7, "", "https://t.me/pic/a"); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if fetcher.calls != 0 {
		t.Errorf("fetched %d times, want 0: there is nothing to fetch", fetcher.calls)
	}
	if users.key != "" || users.source != "" {
		t.Errorf("recorded key=%q source=%q, want both empty", users.key, users.source)
	}
	if len(store.objects) != 0 {
		t.Errorf("stored %v, want nothing", store.objects)
	}
}

// The bot cannot see the photo of a user who never started it. That is not an error:
// the source is recorded so we do not retry on every single login.
func TestSyncRecordsSourceWhenTelegramHasNoPhoto(t *testing.T) {
	fetcher := &fakeFetcher{err: avatar.ErrNoPhoto}
	mirror, users, store := newMirror(t, fetcher)

	if err := mirror.Sync(context.Background(), 7, "https://t.me/pic/b", ""); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if users.key != "" || users.source != "https://t.me/pic/b" {
		t.Errorf("recorded key=%q source=%q", users.key, users.source)
	}
	if len(store.objects) != 0 {
		t.Errorf("stored %v, want nothing", store.objects)
	}
}

func TestSyncPropagatesFetchFailure(t *testing.T) {
	fetcher := &fakeFetcher{err: errors.New("telegram is down")}
	mirror, users, _ := newMirror(t, fetcher)

	if err := mirror.Sync(context.Background(), 7, "https://t.me/pic/b", ""); err == nil {
		t.Fatal("Sync succeeded, want the fetch error")
	}
	// a failed mirror must not record the source, or it would never be retried
	if users.calls != 0 {
		t.Errorf("wrote %d times, want 0", users.calls)
	}
}

func TestTelegramFetchPicksTheLargestSize(t *testing.T) {
	var downloaded string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getUserProfilePhotos"):
			w.Write([]byte(`{"ok":true,"result":{"total_count":1,"photos":[[
				{"file_id":"small","width":160},
				{"file_id":"big","width":640}
			]]}}`))
		case strings.HasSuffix(r.URL.Path, "/getFile"):
			if got := r.URL.Query().Get("file_id"); got != "big" {
				t.Errorf("getFile asked for %q, want the largest size", got)
			}
			w.Write([]byte(`{"ok":true,"result":{"file_path":"photos/x.jpg"}}`))
		default:
			downloaded = r.URL.Path
			w.Write([]byte("bytes"))
		}
	}))
	defer srv.Close()

	tg := avatar.NewTelegramWithBase(srv.URL, "token", srv.Client())
	data, err := tg.Fetch(context.Background(), 7)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(data) != "bytes" {
		t.Errorf("got %q, want the downloaded bytes", data)
	}
	if !strings.HasSuffix(downloaded, "photos/x.jpg") {
		t.Errorf("downloaded %q, want the path getFile handed back", downloaded)
	}
}

func TestTelegramFetchWithoutPhotos(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"ok":true,"result":{"total_count":0,"photos":[]}}`))
	}))
	defer srv.Close()

	tg := avatar.NewTelegramWithBase(srv.URL, "token", srv.Client())
	if _, err := tg.Fetch(context.Background(), 7); !errors.Is(err, avatar.ErrNoPhoto) {
		t.Errorf("err = %v, want ErrNoPhoto", err)
	}
}
