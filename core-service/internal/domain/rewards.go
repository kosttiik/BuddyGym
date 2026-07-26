package domain

import "time"

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

type StreakInput struct {
	RoomID int64
	UserID int64
	Goal   int
	// PeriodDays is 7 whenever CalendarWeek is set: the grid is then Monday to Sunday
	// instead of a rolling window from JoinedAt.
	PeriodDays   int
	CalendarWeek bool
	// Timezone is the member's IANA zone: every boundary is their local midnight.
	Timezone string
	JoinedAt time.Time
	// Days are calendar dates already in the member's zone, as UTC midnights.
	Days    []time.Time
	Freezes []Freeze
}

func (in StreakInput) Streak(now time.Time) int {
	g, ok := in.grid(now)
	if !ok {
		return 0
	}
	streak := g.counts[g.current]
	for k := g.current - 1; k >= 0; k-- {
		if g.exempt(k) {
			streak += g.counts[k]
			continue
		}
		if g.counts[k] < in.Goal {
			break
		}
		streak += g.counts[k]
	}
	return streak
}

func (in StreakInput) Judgment(now time.Time) (hasClosed, lastFailed bool) {
	g, ok := in.grid(now)
	if !ok {
		return false, false
	}
	for k := g.current - 1; k >= 0; k-- {
		if g.exempt(k) {
			continue
		}
		return true, g.counts[k] < in.Goal
	}
	return false, false
}

// CurrentCount is how many days of the running period already carry an approved workout.
func (in StreakInput) CurrentCount(now time.Time) int {
	g, ok := in.grid(now)
	if !ok {
		return 0
	}
	return g.counts[g.current]
}

// PeriodEndsAt is the instant the current period closes, in the member's zone.
func (in StreakInput) PeriodEndsAt(now time.Time) time.Time {
	g, ok := in.grid(now)
	if !ok {
		return time.Time{}
	}
	loc := Location(in.Timezone)
	end := g.anchor.Add(time.Duration(g.current+1) * g.period)
	return time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, loc)
}

func BestStreak(inputs []StreakInput, now time.Time) int {
	best := 0
	for _, in := range inputs {
		best = max(best, in.Streak(now))
	}
	return best
}

type periodGrid struct {
	anchor time.Time
	period time.Duration
	// partial marks period 0 as shorter than the rest: a calendar-week room anchors on the
	// Monday before the member joined, and nobody is judged on a week they were not in yet.
	partial bool
	current int
	counts  map[int]int
	windows [][2]time.Time
}

func (in StreakInput) grid(now time.Time) (periodGrid, bool) {
	if in.Goal <= 0 || in.PeriodDays <= 0 {
		return periodGrid{}, false
	}
	loc := Location(in.Timezone)
	joined := LocalDate(in.JoinedAt, loc)
	g := periodGrid{anchor: joined, period: time.Duration(in.PeriodDays) * oneDay}
	if in.CalendarWeek {
		g.anchor = WeekStart(joined)
		g.partial = !g.anchor.Equal(joined)
	}
	index := func(t time.Time) int {
		return int(t.Sub(g.anchor) / g.period)
	}
	g.current = index(LocalDate(now, loc))
	if g.current < 0 {
		return periodGrid{}, false
	}
	g.counts = make(map[int]int, len(in.Days))
	for _, d := range in.Days {
		// a rejoin resets joined_at, so results from an earlier membership can predate the grid.
		// compare dates rather than the period index: negative durations truncate toward zero,
		// so a day inside the period before the anchor would otherwise index as 0.
		dd := d.UTC().Truncate(oneDay)
		if dd.Before(g.anchor) {
			continue
		}
		if k := index(dd); k <= g.current {
			g.counts[k]++
		}
	}
	for _, f := range in.Freezes {
		if start, end, ok := f.Window(); ok {
			g.windows = append(g.windows, [2]time.Time{start, end})
		}
	}
	return g, true
}

func (g periodGrid) exempt(k int) bool {
	if k == 0 && g.partial {
		return true
	}
	start := g.anchor.Add(time.Duration(k) * g.period)
	end := start.Add(g.period)
	for _, w := range g.windows {
		if w[0].Before(end) && w[1].After(start) {
			return true
		}
	}
	return false
}
