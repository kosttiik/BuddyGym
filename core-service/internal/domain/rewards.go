package domain

import "time"

const (
	AchFirstCheckin = "first_checkin"
	AchWorkouts10   = "workouts_10"
	AchWorkouts50   = "workouts_50"
	AchWorkouts100  = "workouts_100"
	AchStreak7      = "streak_7"
)

const (
	StatusNovice  = "novice"
	StatusRegular = "regular"
	StatusBeast   = "beast"
)

// EarnedAchievements returns every achievement the totals qualify for.
// Already granted ones are filtered out by the storage layer on insert.
func EarnedAchievements(totalWorkouts, streakDays int) []string {
	var keys []string
	if totalWorkouts >= 1 {
		keys = append(keys, AchFirstCheckin)
	}
	if totalWorkouts >= 10 {
		keys = append(keys, AchWorkouts10)
	}
	if totalWorkouts >= 50 {
		keys = append(keys, AchWorkouts50)
	}
	if totalWorkouts >= 100 {
		keys = append(keys, AchWorkouts100)
	}
	if streakDays >= 7 {
		keys = append(keys, AchStreak7)
	}
	return keys
}

func StatusFor(totalWorkouts int) string {
	switch {
	case totalWorkouts >= 50:
		return StatusBeast
	case totalWorkouts >= 10:
		return StatusRegular
	default:
		return StatusNovice
	}
}

// Streak counts consecutive days with at least one workout, ending today
// or yesterday. Days must be distinct UTC dates sorted descending.
func Streak(days []time.Time, now time.Time) int {
	if len(days) == 0 {
		return 0
	}
	today := now.UTC().Truncate(24 * time.Hour)
	head := days[0].UTC().Truncate(24 * time.Hour)
	if today.Sub(head) > 24*time.Hour {
		return 0
	}
	streak := 1
	prev := head
	for _, d := range days[1:] {
		d = d.UTC().Truncate(24 * time.Hour)
		if prev.Sub(d) != 24*time.Hour {
			break
		}
		streak++
		prev = d
	}
	return streak
}
