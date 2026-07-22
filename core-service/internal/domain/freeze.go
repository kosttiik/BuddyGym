package domain

import (
	"fmt"
	"time"
)

type Freeze struct {
	ID         int64      `json:"id"`
	RoomID     int64      `json:"room_id"`
	UserID     int64      `json:"user_id"`
	StartsAt   time.Time  `json:"starts_at"`
	EndsAt     time.Time  `json:"ends_at"`
	CanceledAt *time.Time `json:"canceled_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

const (
	FreezeMinDays         = 1
	FreezeMaxDays         = 30
	FreezeMaxAheadDays    = 60
	FreezeMinCooldownDays = 7
)

const oneDay = 24 * time.Hour

func (f Freeze) Window() (start, end time.Time, ok bool) {
	start, end = f.StartsAt.UTC().Truncate(oneDay), f.EndsAt.UTC().Truncate(oneDay)
	if f.CanceledAt != nil {
		c := f.CanceledAt.UTC().Truncate(oneDay)
		if !c.After(start) {
			return start, end, false
		}
		if c.Before(end) {
			end = c
		}
	}
	return start, end, true
}

func (f Freeze) Active(now time.Time) bool {
	start, end, ok := f.Window()
	d := now.UTC().Truncate(oneDay)
	return ok && !d.Before(start) && d.Before(end)
}

func CurrentFreeze(freezes []Freeze, now time.Time) *Freeze {
	d := now.UTC().Truncate(oneDay)
	var next *Freeze
	for i := range freezes {
		start, end, ok := freezes[i].Window()
		if !ok || !end.After(d) {
			continue
		}
		if !start.After(d) {
			return &freezes[i]
		}
		if next == nil || start.Before(next.StartsAt.UTC().Truncate(oneDay)) {
			next = &freezes[i]
		}
	}
	return next
}

func FreezeCooldownUntil(freezes []Freeze, now time.Time) time.Time {
	d := now.UTC().Truncate(oneDay)
	var until time.Time
	for _, f := range freezes {
		start, end, ok := f.Window()
		if !ok || end.After(d) {
			continue
		}
		used := int(end.Sub(start) / oneDay)
		u := end.Add(time.Duration(max(FreezeMinCooldownDays, used)) * oneDay)
		if u.After(until) {
			until = u
		}
	}
	return until
}

func CanFreeze(history []Freeze, startsAt, endsAt, now time.Time) string {
	d := now.UTC().Truncate(oneDay)
	start, end := startsAt.UTC().Truncate(oneDay), endsAt.UTC().Truncate(oneDay)
	dur := int(end.Sub(start) / oneDay)
	switch {
	case start.Before(d):
		return "starts_at cannot be in the past"
	case start.After(d.Add(FreezeMaxAheadDays * oneDay)):
		return fmt.Sprintf("starts_at must be within %d days", FreezeMaxAheadDays)
	case dur < FreezeMinDays || dur > FreezeMaxDays:
		return fmt.Sprintf("freeze must be %d..%d days", FreezeMinDays, FreezeMaxDays)
	}
	for _, f := range history {
		_, e, ok := f.Window()
		if ok && e.After(d) {
			return "another freeze is already active or scheduled"
		}
	}
	if until := FreezeCooldownUntil(history, now); start.Before(until) {
		return "freeze cooldown until " + until.Format("2006-01-02")
	}
	return ""
}
