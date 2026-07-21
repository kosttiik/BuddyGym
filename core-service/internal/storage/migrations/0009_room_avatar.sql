-- +goose Up
-- Room pictures live in the same private bucket as the mirrored user avatars. The key is
-- derived from the room id, so a replacement overwrites the old object instead of leaking it.
ALTER TABLE rooms ADD COLUMN avatar_key TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE rooms DROP COLUMN avatar_key;
