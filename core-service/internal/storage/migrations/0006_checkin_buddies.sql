-- +goose Up
-- Who trained with the author. The credit is applied when the checkin is approved, so a
-- rejected photo hands out nothing. room_id carries the cascade: purging a room takes the
-- tags with it.
CREATE TABLE checkin_buddies (
    checkin_id TEXT NOT NULL,
    room_id BIGINT NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    author_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (checkin_id, user_id)
);

CREATE INDEX checkin_buddies_room_idx ON checkin_buddies (room_id);

-- +goose Down
DROP TABLE checkin_buddies;
