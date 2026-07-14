-- +goose Up
-- users.status held the derived rank (novice/regular/beast). The user-facing "status" is now a
-- free-text line the member writes themselves, so the derived one takes its real name. A rename
-- keeps every existing rank intact: nothing is recomputed and no row is touched.
ALTER TABLE users RENAME COLUMN status TO rank;

ALTER TABLE users
    ADD COLUMN status_emoji TEXT NOT NULL DEFAULT '',
    ADD COLUMN status_text TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE users DROP COLUMN status_text, DROP COLUMN status_emoji;

ALTER TABLE users RENAME COLUMN rank TO status;
