package rooms_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kosttiik/BuddyGym/core-service/internal/rooms"
)

type fakeStore struct {
	deleted map[int64]time.Time
	purged  []int64
}

func (f *fakeStore) ListDeletedBefore(_ context.Context, cutoff time.Time, limit int) ([]int64, error) {
	var ids []int64
	for id, at := range f.deleted {
		if !at.After(cutoff) && len(ids) < limit {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (f *fakeStore) Purge(_ context.Context, roomID int64) error {
	f.purged = append(f.purged, roomID)
	delete(f.deleted, roomID)
	return nil
}

type fakeCheckins struct {
	purged []int64
	err    error
}

func (f *fakeCheckins) PurgeRoom(_ context.Context, roomID int64) error {
	if f.err != nil {
		return f.err
	}
	f.purged = append(f.purged, roomID)
	return nil
}

func TestReaperKeepsRoomsInsideTheGracePeriod(t *testing.T) {
	now := time.Now()
	store := &fakeStore{deleted: map[int64]time.Time{1: now.Add(-2 * 24 * time.Hour)}}
	checkins := &fakeCheckins{}

	reaper := rooms.New(rooms.Options{
		Rooms: store, Checkins: checkins,
		Grace: 7 * 24 * time.Hour,
		Now:   func() time.Time { return now },
	})
	if err := reaper.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(store.purged) != 0 || len(checkins.purged) != 0 {
		t.Errorf("a room two days old must survive: rooms=%v checkins=%v", store.purged, checkins.purged)
	}
}

func TestReaperErasesRoomsPastTheGracePeriod(t *testing.T) {
	now := time.Now()
	store := &fakeStore{deleted: map[int64]time.Time{7: now.Add(-8 * 24 * time.Hour)}}
	checkins := &fakeCheckins{}

	reaper := rooms.New(rooms.Options{
		Rooms: store, Checkins: checkins,
		Grace: 7 * 24 * time.Hour,
		Now:   func() time.Time { return now },
	})
	if err := reaper.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(checkins.purged) != 1 || checkins.purged[0] != 7 {
		t.Errorf("checkins not purged: %v", checkins.purged)
	}
	if len(store.purged) != 1 || store.purged[0] != 7 {
		t.Errorf("room not purged: %v", store.purged)
	}
}

// Dropping the room row while its checkins are still there would strand the photos, so a
// failed checkin purge must leave the room for the next sweep.
func TestReaperKeepsTheRoomWhenCheckinsCannotBePurged(t *testing.T) {
	now := time.Now()
	store := &fakeStore{deleted: map[int64]time.Time{3: now.Add(-30 * 24 * time.Hour)}}
	checkins := &fakeCheckins{err: errors.New("checkin service down")}

	reaper := rooms.New(rooms.Options{
		Rooms: store, Checkins: checkins,
		Grace: 7 * 24 * time.Hour,
		Now:   func() time.Time { return now },
	})
	if err := reaper.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(store.purged) != 0 {
		t.Errorf("room erased despite the checkin purge failing: %v", store.purged)
	}
	if _, still := store.deleted[3]; !still {
		t.Error("room must stay marked for the next sweep")
	}
}
