package avatar

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"
)

const mirrorTimeout = 20 * time.Second

type UsersRepo interface {
	SetAvatar(ctx context.Context, id int64, key, source string) error
}

type Fetcher interface {
	Fetch(ctx context.Context, userID int64) ([]byte, error)
}

type Uploader interface {
	Put(ctx context.Context, key string, data []byte) error
}

func Key(userID int64) string {
	return "avatars/" + strconv.FormatInt(userID, 10)
}

type Mirror struct {
	users    UsersRepo
	telegram Fetcher
	store    Uploader
	log      *slog.Logger
}

func NewMirror(users UsersRepo, telegram Fetcher, store Uploader, log *slog.Logger) *Mirror {
	if log == nil {
		log = slog.Default()
	}
	return &Mirror{users: users, telegram: telegram, store: store, log: log}
}

func (m *Mirror) Sync(ctx context.Context, userID int64, photoURL, mirroredFrom string) error {
	if photoURL == mirroredFrom {
		return nil
	}
	if photoURL == "" {
		return m.users.SetAvatar(ctx, userID, "", "")
	}

	data, err := m.telegram.Fetch(ctx, userID)
	if errors.Is(err, ErrNoPhoto) {
		return m.users.SetAvatar(ctx, userID, "", photoURL)
	}
	if err != nil {
		return err
	}

	key := Key(userID)
	if err := m.store.Put(ctx, key, data); err != nil {
		return err
	}
	return m.users.SetAvatar(ctx, userID, key, photoURL)
}

func (m *Mirror) SyncInBackground(userID int64, photoURL, mirroredFrom string) {
	if photoURL == mirroredFrom {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(context.Background()), mirrorTimeout)
		defer cancel()
		if err := m.Sync(ctx, userID, photoURL, mirroredFrom); err != nil {
			m.log.Error("mirror avatar", "err", err, "user_id", userID)
		}
	}()
}
