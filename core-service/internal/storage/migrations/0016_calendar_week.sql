-- +goose Up
-- periods used to be counted from joined_at in UTC, so every member had a private week
-- and it reset at 00:00 UTC regardless of where they live
ALTER TABLE users ADD COLUMN timezone TEXT NOT NULL DEFAULT 'UTC';

ALTER TABLE rooms ADD COLUMN calendar_week BOOLEAN NOT NULL DEFAULT false;
UPDATE rooms SET calendar_week = true WHERE period_days = 7;
ALTER TABLE rooms ADD CONSTRAINT rooms_calendar_week_is_seven_days
    CHECK (NOT calendar_week OR period_days = 7);

-- +goose Down
ALTER TABLE rooms DROP CONSTRAINT rooms_calendar_week_is_seven_days;
ALTER TABLE rooms DROP COLUMN calendar_week;
ALTER TABLE users DROP COLUMN timezone;
