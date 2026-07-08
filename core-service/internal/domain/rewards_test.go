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

func TestStreak(t *testing.T) {
	now := day("2026-07-08").Add(15 * time.Hour)
	tests := []struct {
		name string
		days []time.Time
		want int
	}{
		{"empty", nil, 0},
		{"today only", []time.Time{day("2026-07-08")}, 1},
		{"yesterday only", []time.Time{day("2026-07-07")}, 1},
		{"broken two days ago", []time.Time{day("2026-07-06")}, 0},
		{"three in a row", []time.Time{day("2026-07-08"), day("2026-07-07"), day("2026-07-06")}, 3},
		{"gap stops count", []time.Time{day("2026-07-08"), day("2026-07-07"), day("2026-07-05")}, 2},
		{"week", []time.Time{
			day("2026-07-08"), day("2026-07-07"), day("2026-07-06"), day("2026-07-05"),
			day("2026-07-04"), day("2026-07-03"), day("2026-07-02"),
		}, 7},
	}
	for _, tt := range tests {
		if got := Streak(tt.days, now); got != tt.want {
			t.Errorf("%s: Streak = %d, want %d", tt.name, got, tt.want)
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
