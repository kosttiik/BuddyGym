-- +goose Up
CREATE TABLE checkin_comments (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    checkin_id TEXT NOT NULL,
    room_id BIGINT NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    body TEXT NOT NULL CHECK (length(body) BETWEEN 1 AND 500),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX checkin_comments_checkin_idx ON checkin_comments (checkin_id, created_at);

-- +goose Down
DROP TABLE checkin_comments;
