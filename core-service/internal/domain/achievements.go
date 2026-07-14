package domain

import "time"

// Stats is everything the achievement catalog can be measured against.
type Stats struct {
	TotalWorkouts int `json:"total_workouts"`
	BestStreak    int `json:"best_streak"`
	Rooms         int `json:"rooms"`
	Buddies       int `json:"buddies"`
	Comments      int `json:"comments"`
	EarlyWorkouts int `json:"early_workouts"`
	LateWorkouts  int `json:"late_workouts"`
}

type Metric string

const (
	MetricWorkouts      Metric = "total_workouts"
	MetricBestStreak    Metric = "best_streak"
	MetricRooms         Metric = "rooms"
	MetricBuddies       Metric = "buddies"
	MetricComments      Metric = "comments"
	MetricEarlyWorkouts Metric = "early_workouts"
	MetricLateWorkouts  Metric = "late_workouts"
)

func (s Stats) value(m Metric) int {
	switch m {
	case MetricWorkouts:
		return s.TotalWorkouts
	case MetricBestStreak:
		return s.BestStreak
	case MetricRooms:
		return s.Rooms
	case MetricBuddies:
		return s.Buddies
	case MetricComments:
		return s.Comments
	case MetricEarlyWorkouts:
		return s.EarlyWorkouts
	case MetricLateWorkouts:
		return s.LateWorkouts
	}
	return 0
}

const (
	AchFirstCheckin = "first_checkin"
	AchWorkouts10   = "workouts_10"
	AchWorkouts50   = "workouts_50"
	AchWorkouts100  = "workouts_100"
	AchWorkouts250  = "workouts_250"
	AchStreak7      = "streak_7"
	AchStreak14     = "streak_14"
	AchStreak30     = "streak_30"
	AchRooms3       = "rooms_3"
	AchBuddies5     = "buddies_5"
	AchComments10   = "comments_10"
	AchEarlyBird10  = "early_bird_10"
	AchNightOwl10   = "night_owl_10"
)

type AchievementSpec struct {
	Key    string
	Metric Metric
	Target int
}

// Catalog is the whole achievement set. Adding one is a line here: the grant path folds over
// this list and never needs touching again.
//
// ponytail: a "finished first in a room period" achievement needs a period-close snapshot job,
// so it is deliberately not here yet.
var Catalog = []AchievementSpec{
	{AchFirstCheckin, MetricWorkouts, 1},
	{AchWorkouts10, MetricWorkouts, 10},
	{AchWorkouts50, MetricWorkouts, 50},
	{AchWorkouts100, MetricWorkouts, 100},
	{AchWorkouts250, MetricWorkouts, 250},
	{AchStreak7, MetricBestStreak, 7},
	{AchStreak14, MetricBestStreak, 14},
	{AchStreak30, MetricBestStreak, 30},
	{AchRooms3, MetricRooms, 3},
	{AchBuddies5, MetricBuddies, 5},
	{AchComments10, MetricComments, 10},
	{AchEarlyBird10, MetricEarlyWorkouts, 10},
	{AchNightOwl10, MetricLateWorkouts, 10},
}

// EarnedAchievements returns every achievement the stats qualify for. Already granted ones are
// filtered out by the storage layer on insert.
func EarnedAchievements(stats Stats) []string {
	var keys []string
	for _, spec := range Catalog {
		if stats.value(spec.Metric) >= spec.Target {
			keys = append(keys, spec.Key)
		}
	}
	return keys
}

// AchievementProgress is one catalog entry as the profile shows it: how far along, and when it
// was earned if it was.
type AchievementProgress struct {
	Key       string     `json:"key"`
	Current   int        `json:"current"`
	Target    int        `json:"target"`
	GrantedAt *time.Time `json:"granted_at,omitempty"`
}

// Progress folds the catalog against the stats. Every key comes back, earned or not: a locked
// tile showing real progress beats a dead grey square.
func Progress(stats Stats, granted []Achievement) []AchievementProgress {
	grantedAt := make(map[string]time.Time, len(granted))
	for _, a := range granted {
		grantedAt[a.Key] = a.GrantedAt
	}

	out := make([]AchievementProgress, 0, len(Catalog))
	for _, spec := range Catalog {
		p := AchievementProgress{
			Key:     spec.Key,
			Current: min(stats.value(spec.Metric), spec.Target),
			Target:  spec.Target,
		}
		if at, ok := grantedAt[spec.Key]; ok {
			p.GrantedAt = &at
			// a granted one is complete even if the metric later moved
			p.Current = spec.Target
		}
		out = append(out, p)
	}
	return out
}
