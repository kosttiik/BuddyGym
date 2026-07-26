package domain

import "time"

// Location resolves an IANA timezone name, falling back to UTC. Members carry their own
// zone because a period must close at their local midnight, not at 00:00 UTC.
func Location(name string) *time.Location {
	if name == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

func ValidTimezone(name string) bool {
	_, err := time.LoadLocation(name)
	return err == nil
}

// LocalDate is the calendar day t falls on in loc, carried as a UTC midnight so that dates
// coming back from Postgres as `(ts AT TIME ZONE tz)::date` compare with it directly.
func LocalDate(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// WeekStart is the Monday of the week a date belongs to.
func WeekStart(date time.Time) time.Time {
	return date.AddDate(0, 0, -((int(date.Weekday()) + 6) % 7))
}
