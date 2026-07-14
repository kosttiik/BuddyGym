package domain

import (
	"slices"
	"testing"
	"time"
)

func TestEarnedAchievements(t *testing.T) {
	tests := []struct {
		total, streak int
		want          []string
	}{
		{0, 0, nil},
		{1, 0, []string{AchFirstCheckin}},
		{9, 0, []string{AchFirstCheckin}},
		{10, 0, []string{AchFirstCheckin, AchWorkouts10}},
		{50, 0, []string{AchFirstCheckin, AchWorkouts10, AchWorkouts50}},
		{100, 0, []string{AchFirstCheckin, AchWorkouts10, AchWorkouts50, AchWorkouts100}},
		{1, 7, []string{AchFirstCheckin, AchStreak7}},
		{0, 6, nil},
	}
	for _, tt := range tests {
		got := EarnedAchievements(tt.total, tt.streak)
		if !slices.Equal(got, tt.want) {
			t.Errorf("EarnedAchievements(%d, %d) = %v, want %v", tt.total, tt.streak, got, tt.want)
		}
	}
}

func TestStatusFor(t *testing.T) {
	tests := []struct {
		total int
		want  string
	}{
		{0, StatusNovice}, {9, StatusNovice},
		{10, StatusRegular}, {49, StatusRegular},
		{50, StatusBeast}, {500, StatusBeast},
	}
	for _, tt := range tests {
		if got := StatusFor(tt.total); got != tt.want {
			t.Errorf("StatusFor(%d) = %q, want %q", tt.total, got, tt.want)
		}
	}
}

func day(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return d
}

// joined 2026-07-01, goal 2 per 7 days: period 0 is Jul 1-7, period 1 Jul 8-14, period 2 Jul 15-21.
func TestRoomStreak(t *testing.T) {
	joined := day("2026-07-01")
	tests := []struct {
		name       string
		days       []time.Time
		now        time.Time
		goal       int
		periodDays int
		want       int
	}{
		{"no workouts", nil, day("2026-07-03"), 2, 7, 0},
		{"first period in progress, below goal, does not burn",
			[]time.Time{day("2026-07-02")}, day("2026-07-03"), 2, 7, 1},
		{"closed period met goal, carries into the next",
			[]time.Time{day("2026-07-02"), day("2026-07-04"), day("2026-07-09")},
			day("2026-07-10"), 2, 7, 3},
		{"closed period missed goal, burns to the current period only",
			[]time.Time{day("2026-07-02"), day("2026-07-09")},
			day("2026-07-10"), 2, 7, 1},
		{"closed period missed goal and nothing since",
			[]time.Time{day("2026-07-02")}, day("2026-07-10"), 2, 7, 0},
		{"gap period in the middle burns everything before it",
			[]time.Time{
				day("2026-07-02"), day("2026-07-04"), // period 0: met
				// period 1: nothing
				day("2026-07-16"), day("2026-07-18"), // period 2: current
			},
			day("2026-07-19"), 2, 7, 2},
		{"three periods all met",
			[]time.Time{
				day("2026-07-02"), day("2026-07-04"),
				day("2026-07-09"), day("2026-07-11"),
				day("2026-07-16"), day("2026-07-18"),
			},
			day("2026-07-19"), 2, 7, 6},
		{"goal 1 per day, unbroken",
			[]time.Time{day("2026-07-01"), day("2026-07-02"), day("2026-07-03")},
			day("2026-07-03"), 1, 1, 3},
		{"goal 1 per day, missed yesterday",
			[]time.Time{day("2026-07-01"), day("2026-07-03")},
			day("2026-07-03"), 1, 1, 1},
		{"days before joining are ignored",
			[]time.Time{day("2026-06-20"), day("2026-07-02")},
			day("2026-07-03"), 2, 7, 1},
		// a day just before the anchor is still inside period 0 by raw arithmetic;
		// negative durations truncate toward zero, so it must be filtered by date
		{"day just before joining is ignored",
			[]time.Time{day("2026-06-30"), day("2026-07-02")},
			day("2026-07-03"), 2, 7, 1},
		{"exceeding the goal still counts every day",
			[]time.Time{day("2026-07-02"), day("2026-07-03"), day("2026-07-04"), day("2026-07-09")},
			day("2026-07-10"), 2, 7, 4},
	}
	for _, tt := range tests {
		got := RoomStreak(tt.days, joined, tt.goal, tt.periodDays, tt.now)
		if got != tt.want {
			t.Errorf("%s: RoomStreak = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestNewInviteCode(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		code := NewInviteCode()
		if len(code) != InviteCodeLen {
			t.Fatalf("len(%q) = %d", code, len(code))
		}
		for _, c := range code {
			if !slices.Contains([]rune(inviteAlphabet), c) {
				t.Fatalf("char %q outside alphabet", c)
			}
		}
		if seen[code] {
			t.Fatalf("duplicate code %q in 100 draws", code)
		}
		seen[code] = true
	}
}
