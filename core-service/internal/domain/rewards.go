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
	RoomID     int64
	UserID     int64
	Goal       int
	PeriodDays int
	JoinedAt   time.Time
	Days       []time.Time
	Freezes    []Freeze
}

func (in StreakInput) Streak(now time.Time) int {
	return RoomStreak(in.Days, in.JoinedAt, in.Goal, in.PeriodDays, now, in.Freezes...)
}

func (in StreakInput) Judgment(now time.Time) (hasClosed, lastFailed bool) {
	return PeriodJudgment(in.Days, in.JoinedAt, in.Goal, in.PeriodDays, now, in.Freezes...)
}

func BestStreak(inputs []StreakInput, now time.Time) int {
	best := 0
	for _, in := range inputs {
		best = max(best, in.Streak(now))
	}
	return best
}

func RoomStreak(days []time.Time, joinedAt time.Time, goal, periodDays int, now time.Time, freezes ...Freeze) int {
	g, ok := newPeriodGrid(days, joinedAt, goal, periodDays, now, freezes)
	if !ok {
		return 0
	}
	streak := g.counts[g.current]
	for k := g.current - 1; k >= 0; k-- {
		if g.exempt(k) {
			streak += g.counts[k]
			continue
		}
		if g.counts[k] < goal {
			break
		}
		streak += g.counts[k]
	}
	return streak
}

func PeriodJudgment(days []time.Time, joinedAt time.Time, goal, periodDays int, now time.Time, freezes ...Freeze) (hasClosed, lastFailed bool) {
	g, ok := newPeriodGrid(days, joinedAt, goal, periodDays, now, freezes)
	if !ok {
		return false, false
	}
	for k := g.current - 1; k >= 0; k-- {
		if g.exempt(k) {
			continue
		}
		return true, g.counts[k] < goal
	}
	return false, false
}

type periodGrid struct {
	anchor  time.Time
	period  time.Duration
	current int
	counts  map[int]int
	windows [][2]time.Time
}

func newPeriodGrid(days []time.Time, joinedAt time.Time, goal, periodDays int, now time.Time, freezes []Freeze) (periodGrid, bool) {
	if goal <= 0 || periodDays <= 0 {
		return periodGrid{}, false
	}
	g := periodGrid{
		anchor: joinedAt.UTC().Truncate(oneDay),
		period: time.Duration(periodDays) * oneDay,
	}
	index := func(t time.Time) int {
		return int(t.UTC().Truncate(oneDay).Sub(g.anchor) / g.period)
	}
	g.current = index(now)
	if g.current < 0 {
		return periodGrid{}, false
	}
	g.counts = make(map[int]int, len(days))
	for _, d := range days {
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
	for _, f := range freezes {
		if start, end, ok := f.Window(); ok {
			g.windows = append(g.windows, [2]time.Time{start, end})
		}
	}
	return g, true
}

func (g periodGrid) exempt(k int) bool {
	start := g.anchor.Add(time.Duration(k) * g.period)
	end := start.Add(g.period)
	for _, w := range g.windows {
		if w[0].Before(end) && w[1].After(start) {
			return true
		}
	}
	return false
}
