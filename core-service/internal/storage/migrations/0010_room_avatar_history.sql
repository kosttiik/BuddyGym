-- +goose Up
-- Room pictures are a gallery, not a single slot: any member may add one, the newest is the
-- face of the room and the older ones stay browsable. rooms.avatar_key caches the current
-- object so room listings need no join.
CREATE TABLE room_avatars (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    room_id BIGINT NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    object_key TEXT NOT NULL,
    uploaded_by BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX room_avatars_room_idx ON room_avatars (room_id, created_at DESC);

-- +goose Down
DROP TABLE room_avatars;
