-- +goose Up
-- A room with no members left is dead. It is marked first and erased later, so a
-- mistaken leave can still be undone and the checkin side has time to purge its photos.
ALTER TABLE rooms ADD COLUMN deleted_at TIMESTAMPTZ;

CREATE INDEX idx_rooms_deleted_at ON rooms (deleted_at) WHERE deleted_at IS NOT NULL;

-- +goose Down
DROP INDEX idx_rooms_deleted_at;

ALTER TABLE rooms DROP COLUMN deleted_at;
