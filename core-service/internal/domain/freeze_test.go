package domain

import (
	"strings"
	"testing"
	"time"
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestFreezeWindow(t *testing.T) {
	f := Freeze{StartsAt: date(2026, 7, 10), EndsAt: date(2026, 7, 20)}
	if s, e, ok := f.Window(); !ok || !s.Equal(date(2026, 7, 10)) || !e.Equal(date(2026, 7, 20)) {
		t.Errorf("plain window = %v %v %v", s, e, ok)
	}

	c := date(2026, 7, 15)
	f.CanceledAt = &c
	if _, e, ok := f.Window(); !ok || !e.Equal(c) {
		t.Errorf("early unfreeze end = %v %v, want %v", e, ok, c)
	}

	c2 := date(2026, 7, 5)
	f.CanceledAt = &c2
	if _, _, ok := f.Window(); ok {
		t.Error("canceled before start must erase the window")
	}
}

func TestCanFreeze(t *testing.T) {
	now := date(2026, 7, 22)
	cases := []struct {
		name       string
		history    []Freeze
		start, end time.Time
		wantErr    string
	}{
		{"ok", nil, date(2026, 7, 25), date(2026, 8, 5), ""},
		{"ok today", nil, now, now.AddDate(0, 0, 7), ""},
		{"past", nil, date(2026, 7, 20), date(2026, 7, 30), "in the past"},
		{"too far", nil, date(2026, 10, 1), date(2026, 10, 5), "within 60 days"},
		{"too long", nil, date(2026, 7, 25), date(2026, 8, 30), "1..30 days"},
		{"zero days", nil, date(2026, 7, 25), date(2026, 7, 25), "1..30 days"},
		{"already scheduled",
			[]Freeze{{StartsAt: date(2026, 8, 1), EndsAt: date(2026, 8, 10)}},
			date(2026, 8, 20), date(2026, 8, 25), "already active or scheduled"},
		{"cooldown after long freeze",
			[]Freeze{{StartsAt: date(2026, 6, 20), EndsAt: date(2026, 7, 10)}},
			date(2026, 7, 25), date(2026, 7, 30), "cooldown until 2026-07-30"},
		{"cooldown expired",
			[]Freeze{{StartsAt: date(2026, 6, 1), EndsAt: date(2026, 6, 8)}},
			date(2026, 7, 25), date(2026, 7, 30), ""},
	}
	for _, tc := range cases {
		got := CanFreeze(tc.history, tc.start, tc.end, now)
		if tc.wantErr == "" && got != "" {
			t.Errorf("%s: unexpected error %q", tc.name, got)
		}
		if tc.wantErr != "" && !strings.Contains(got, tc.wantErr) {
			t.Errorf("%s: error %q, want containing %q", tc.name, got, tc.wantErr)
		}
	}
}

func TestFreezeCooldownGrowsWithLength(t *testing.T) {
	now := date(2026, 7, 22)
	short := []Freeze{{StartsAt: date(2026, 7, 10), EndsAt: date(2026, 7, 13)}}
	if got := FreezeCooldownUntil(short, now); !got.Equal(date(2026, 7, 20)) {
		t.Errorf("3-day freeze cooldown = %v, want min 7 days -> 2026-07-20", got)
	}
	long := []Freeze{{StartsAt: date(2026, 6, 20), EndsAt: date(2026, 7, 10)}}
	if got := FreezeCooldownUntil(long, now); !got.Equal(date(2026, 7, 30)) {
		t.Errorf("20-day freeze cooldown = %v, want 2026-07-30", got)
	}
}

func TestRoomStreakSkipsFrozenPeriods(t *testing.T) {
	joined := date(2026, 7, 1)
	now := date(2026, 7, 21)
	days := []time.Time{
		date(2026, 7, 1), date(2026, 7, 2), date(2026, 7, 3),
		date(2026, 7, 21),
	}
	if got := RoomStreak(days, joined, 3, 7, now); got != 1 {
		t.Fatalf("without freeze = %d, want 1 (failed middle period burns)", got)
	}
	frozen := Freeze{StartsAt: date(2026, 7, 9), EndsAt: date(2026, 7, 14)}
	if got := RoomStreak(days, joined, 3, 7, now, frozen); got != 4 {
		t.Errorf("with freeze = %d, want 4 (frozen period skipped, streak carries)", got)
	}
}

func TestPeriodJudgment(t *testing.T) {
	joined := date(2026, 7, 15)
	now := date(2026, 7, 18)
	if has, _ := PeriodJudgment(nil, joined, 3, 7, now); has {
		t.Error("no closed period yet must not be judged")
	}

	now = date(2026, 7, 24)
	if has, failed := PeriodJudgment(nil, joined, 3, 7, now); !has || !failed {
		t.Errorf("empty closed period = %v %v, want judged failed", has, failed)
	}
	met := []time.Time{date(2026, 7, 15), date(2026, 7, 16), date(2026, 7, 17)}
	if has, failed := PeriodJudgment(met, joined, 3, 7, now); !has || failed {
		t.Errorf("met closed period = %v %v, want judged passed", has, failed)
	}

	frozen := Freeze{StartsAt: date(2026, 7, 16), EndsAt: date(2026, 7, 20)}
	if has, _ := PeriodJudgment(nil, joined, 3, 7, now, frozen); has {
		t.Error("frozen closed period must not be judged")
	}

	laterFrozen := Freeze{StartsAt: date(2026, 7, 23), EndsAt: date(2026, 7, 27)}
	if has, failed := PeriodJudgment(met, joined, 3, 7, date(2026, 7, 31), laterFrozen); !has || failed {
		t.Errorf("judgment must fall through frozen period to the met one: %v %v", has, failed)
	}
}

func TestCurrentFreeze(t *testing.T) {
	now := date(2026, 7, 22)
	active := Freeze{ID: 1, StartsAt: date(2026, 7, 20), EndsAt: date(2026, 7, 25)}
	future := Freeze{ID: 2, StartsAt: date(2026, 8, 1), EndsAt: date(2026, 8, 5)}
	past := Freeze{ID: 3, StartsAt: date(2026, 6, 1), EndsAt: date(2026, 6, 5)}

	if got := CurrentFreeze([]Freeze{past, future, active}, now); got == nil || got.ID != 1 {
		t.Errorf("active freeze = %+v, want id 1", got)
	}
	if got := CurrentFreeze([]Freeze{past, future}, now); got == nil || got.ID != 2 {
		t.Errorf("scheduled freeze = %+v, want id 2", got)
	}
	if got := CurrentFreeze([]Freeze{past}, now); got != nil {
		t.Errorf("past only = %+v, want nil", got)
	}
}
