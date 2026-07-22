-- +goose Up
CREATE TABLE freezes (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    room_id BIGINT NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    starts_at DATE NOT NULL,
    ends_at DATE NOT NULL CHECK (ends_at > starts_at),
    canceled_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX freezes_member_idx ON freezes (room_id, user_id);

-- +goose Down
DROP TABLE freezes;
