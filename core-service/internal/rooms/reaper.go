// Package rooms holds background upkeep for rooms.
package rooms

import (
	"context"
	"log/slog"
	"time"
)

const (
	DefaultGrace = 7 * 24 * time.Hour
	batchSize    = 50
)

type Store interface {
	ListDeletedBefore(ctx context.Context, cutoff time.Time, limit int) ([]int64, error)
	Purge(ctx context.Context, roomID int64) error
}

type CheckinPurger interface {
	PurgeRoom(ctx context.Context, roomID int64) error
}

type Reaper struct {
	rooms    Store
	checkins CheckinPurger
	grace    time.Duration
	interval time.Duration
	log      *slog.Logger
	now      func() time.Time
}

type Options struct {
	Rooms    Store
	Checkins CheckinPurger
	Grace    time.Duration
	Interval time.Duration
	Log      *slog.Logger
	Now      func() time.Time
}

func New(opts Options) *Reaper {
	if opts.Grace == 0 {
		opts.Grace = DefaultGrace
	}
	if opts.Interval == 0 {
		opts.Interval = time.Hour
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	return &Reaper{
		rooms:    opts.Rooms,
		checkins: opts.Checkins,
		grace:    opts.Grace,
		interval: opts.Interval,
		log:      opts.Log,
		now:      opts.Now,
	}
}

func (r *Reaper) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		if err := r.Sweep(ctx); err != nil && ctx.Err() == nil {
			r.log.Error("room reaper sweep failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Reaper) Sweep(ctx context.Context) error {
	ids, err := r.rooms.ListDeletedBefore(ctx, r.now().Add(-r.grace), batchSize)
	if err != nil {
		return err
	}

	for _, id := range ids {
		if err := r.checkins.PurgeRoom(ctx, id); err != nil {
			r.log.Error("purging checkins failed, room kept for the next sweep",
				"room_id", id, "err", err)
			continue
		}
		if err := r.rooms.Purge(ctx, id); err != nil {
			r.log.Error("purging room failed", "room_id", id, "err", err)
			continue
		}
		r.log.Info("room purged", "room_id", id)
	}
	return nil
}
