-- +goose Up
-- Telegram serves avatars from t.me and telegram-cdn, both unreachable for our users, so the
-- photo_url from initData renders as a broken image. We mirror the bytes into object storage
-- instead. avatar_source keeps the photo_url the mirror was taken from: when Telegram hands us
-- a different one the user changed their picture and the mirror is refreshed.
ALTER TABLE users
    ADD COLUMN avatar_key TEXT NOT NULL DEFAULT '',
    ADD COLUMN avatar_source TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE users DROP COLUMN avatar_source, DROP COLUMN avatar_key;
