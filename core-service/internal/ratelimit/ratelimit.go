package ratelimit

import (
	"context"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter is a fixed-window counter backed by Redis.
type Limiter struct {
	rdb    *redis.Client
	prefix string
	limit  int64
	window time.Duration
	log    *slog.Logger
}

func New(rdb *redis.Client, prefix string, limit int, window time.Duration, log *slog.Logger) *Limiter {
	if log == nil {
		log = slog.Default()
	}
	return &Limiter{rdb: rdb, prefix: prefix, limit: int64(limit), window: window, log: log}
}

// Allow fails open: a Redis outage must not take the API down.
func (l *Limiter) Allow(ctx context.Context, key string) (bool, error) {
	full := "rl:" + l.prefix + ":" + key
	pipe := l.rdb.TxPipeline()
	incr := pipe.Incr(ctx, full)
	pipe.ExpireNX(ctx, full, l.window)
	if _, err := pipe.Exec(ctx); err != nil {
		l.log.Error("rate limiter unavailable", "err", err)
		return true, nil
	}
	return incr.Val() <= l.limit, nil
}
