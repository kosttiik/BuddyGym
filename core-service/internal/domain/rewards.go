package domain

import "time"

// Ranks are derived from the workout total. The member cannot set them; what they can set
// is the free-text status next to their name.
const (
	RankNovice  = "novice"
	RankRegular = "regular"
	RankBeast   = "beast"
)

func RankFor(totalWorkouts int) string {
	switch {
	case totalWorkouts >= 50:
		return RankBeast
	case totalWorkouts >= 10:
		return RankRegular
	default:
		return RankNovice
	}
}

// StreakInput is one membership with its approved workout days.
type StreakInput struct {
	RoomID     int64
	UserID     int64
	Goal       int
	PeriodDays int
	JoinedAt   time.Time
	Days       []time.Time
}

func (in StreakInput) Streak(now time.Time) int {
	return RoomStreak(in.Days, in.JoinedAt, in.Goal, in.PeriodDays, now)
}

// BestStreak is the highest streak the user holds across the rooms they belong to.
func BestStreak(inputs []StreakInput, now time.Time) int {
	best := 0
	for _, in := range inputs {
		best = max(best, in.Streak(now))
	}
	return best
}

// RoomStreak counts workout days since the last period the member failed to meet the
// room goal. A period is failed when it closed with fewer than goal workout days; the
// period in progress is never judged, so a streak cannot burn while there is still time
// to train. Days must be distinct UTC dates. The grid is anchored on joinedAt because
// memberships.period_start only moves when a checkin lands and would drift.
func RoomStreak(days []time.Time, joinedAt time.Time, goal, periodDays int, now time.Time) int {
	if goal <= 0 || periodDays <= 0 {
		return 0
	}
	anchor := joinedAt.UTC().Truncate(24 * time.Hour)
	period := time.Duration(periodDays) * 24 * time.Hour

	index := func(t time.Time) int {
		return int(t.UTC().Truncate(24*time.Hour).Sub(anchor) / period)
	}

	current := index(now)
	if current < 0 {
		return 0
	}

	counts := make(map[int]int, len(days))
	for _, d := range days {
		// a rejoin resets joined_at, so results from an earlier membership can predate the grid.
		// compare dates rather than the period index: negative durations truncate toward zero,
		// so a day inside the period before the anchor would otherwise index as 0.
		day := d.UTC().Truncate(24 * time.Hour)
		if day.Before(anchor) {
			continue
		}
		if k := index(day); k <= current {
			counts[k]++
		}
	}

	streak := counts[current]
	for k := current - 1; k >= 0; k-- {
		if counts[k] < goal {
			break
		}
		streak += counts[k]
	}
	return streak
}
