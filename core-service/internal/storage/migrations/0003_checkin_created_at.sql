-- +goose Up
-- applied_at is when voting finished, so bucketing days by it credits a late-night workout
-- to the next day. Existing rows are backfilled from applied_at: the two are within the 24h
-- voting window, and no better estimate exists.
ALTER TABLE checkin_results ADD COLUMN checkin_created_at TIMESTAMPTZ;

UPDATE checkin_results SET checkin_created_at = applied_at WHERE checkin_created_at IS NULL;

ALTER TABLE checkin_results ALTER COLUMN checkin_created_at SET NOT NULL;

-- +goose Down
ALTER TABLE checkin_results DROP COLUMN checkin_created_at;
