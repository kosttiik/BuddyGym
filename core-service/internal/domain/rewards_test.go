package domain

import (
	"slices"
	"testing"
	"time"
)

func TestEarnedAchievements(t *testing.T) {
	tests := []struct {
		name  string
		stats Stats
		want  []string
	}{
		{"nothing yet", Stats{}, nil},
		{"first workout", Stats{TotalWorkouts: 1}, []string{AchFirstCheckin}},
		{"just short of ten", Stats{TotalWorkouts: 9}, []string{AchFirstCheckin}},
		{"ten", Stats{TotalWorkouts: 10}, []string{AchFirstCheckin, AchWorkouts10}},
		{"a streak without workouts is impossible, but the fold must not invent one",
			Stats{BestStreak: 6}, nil},
		{"a week long streak",
			Stats{TotalWorkouts: 7, BestStreak: 7}, []string{AchFirstCheckin, AchStreak7}},
		{"every metric at once", Stats{
			TotalWorkouts: 250, BestStreak: 30, Rooms: 3, Buddies: 5, Comments: 10,
			EarlyWorkouts: 10, LateWorkouts: 10,
		}, []string{
			AchFirstCheckin, AchWorkouts10, AchWorkouts50, AchWorkouts100, AchWorkouts250,
			AchStreak7, AchStreak14, AchStreak30, AchRooms3, AchBuddies5, AchComments10,
			AchEarlyBird10, AchNightOwl10,
		}},
	}
	for _, tt := range tests {
		got := EarnedAchievements(tt.stats)
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s: EarnedAchievements = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestProgress(t *testing.T) {
	granted := []Achievement{{Key: AchFirstCheckin, GrantedAt: day("2026-07-01")}}
	got := Progress(Stats{TotalWorkouts: 4}, granted)

	if len(got) != len(Catalog) {
		t.Fatalf("got %d entries, want the whole catalog (%d)", len(got), len(Catalog))
	}

	byKey := map[string]AchievementProgress{}
	for _, p := range got {
		byKey[p.Key] = p
	}

	// an earned one reads as complete and carries its date
	first := byKey[AchFirstCheckin]
	if first.Current != 1 || first.Target != 1 || first.GrantedAt == nil {
		t.Errorf("first_checkin = %+v, want it complete and dated", first)
	}

	// a locked one carries real progress rather than a dead zero
	ten := byKey[AchWorkouts10]
	if ten.Current != 4 || ten.Target != 10 || ten.GrantedAt != nil {
		t.Errorf("workouts_10 = %+v, want 4/10 and no date", ten)
	}

	// progress never overshoots the target: the bar would run off the tile
	hundred := byKey[AchWorkouts100]
	if got := Progress(Stats{TotalWorkouts: 400}, nil); got[3].Current != got[3].Target {
		t.Errorf("workouts_100 = %+v, want it clamped to the target", hundred)
	}
}

func TestRankFor(t *testing.T) {
	tests := []struct {
		total int
		want  string
	}{
		{0, RankNovice}, {9, RankNovice},
		{10, RankRegular}, {49, RankRegular},
		{50, RankBeast}, {500, RankBeast},
	}
	for _, tt := range tests {
		if got := RankFor(tt.total); got != tt.want {
			t.Errorf("RankFor(%d) = %q, want %q", tt.total, got, tt.want)
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
