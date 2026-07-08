package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newLimiter(t *testing.T, limit int, window time.Duration) (*Limiter, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	return New(rdb, "test", limit, window, nil), mr
}

func TestAllowWithinLimit(t *testing.T) {
	l, _ := newLimiter(t, 3, time.Minute)
	ctx := context.Background()
	for i := range 3 {
		ok, err := l.Allow(ctx, "u1")
		if err != nil || !ok {
			t.Fatalf("request %d: ok=%v err=%v", i, ok, err)
		}
	}
	ok, err := l.Allow(ctx, "u1")
	if err != nil || ok {
		t.Fatalf("over limit allowed: ok=%v err=%v", ok, err)
	}
	// another key is unaffected
	if ok, _ := l.Allow(ctx, "u2"); !ok {
		t.Fatal("independent key throttled")
	}
}

func TestWindowReset(t *testing.T) {
	l, mr := newLimiter(t, 1, time.Minute)
	ctx := context.Background()
	if ok, _ := l.Allow(ctx, "u1"); !ok {
		t.Fatal("first request denied")
	}
	if ok, _ := l.Allow(ctx, "u1"); ok {
		t.Fatal("second request allowed")
	}
	mr.FastForward(61 * time.Second)
	if ok, _ := l.Allow(ctx, "u1"); !ok {
		t.Fatal("request after window denied")
	}
}

func TestFailOpen(t *testing.T) {
	l, mr := newLimiter(t, 1, time.Minute)
	mr.Close()
	if ok, err := l.Allow(context.Background(), "u1"); err != nil || !ok {
		t.Fatalf("must fail open: ok=%v err=%v", ok, err)
	}
}
