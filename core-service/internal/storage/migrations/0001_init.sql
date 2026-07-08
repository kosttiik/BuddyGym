-- +goose Up
CREATE TABLE users (
    id BIGINT PRIMARY KEY,
    username TEXT NOT NULL DEFAULT '',
    first_name TEXT NOT NULL DEFAULT '',
    photo_url TEXT NOT NULL DEFAULT '',
    theme TEXT NOT NULL DEFAULT 'default',
    status TEXT NOT NULL DEFAULT 'novice',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE rooms (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('open', 'invite')),
    invite_code TEXT NOT NULL UNIQUE,
    goal_per_period INT NOT NULL CHECK (goal_per_period > 0),
    period_days INT NOT NULL CHECK (period_days > 0),
    votes_required INT NOT NULL CHECK (votes_required > 0),
    creator_id BIGINT NOT NULL REFERENCES users (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE memberships (
    room_id BIGINT NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    workouts_count INT NOT NULL DEFAULT 0,
    period_start TIMESTAMPTZ NOT NULL DEFAULT now(),
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (room_id, user_id)
);

CREATE TABLE achievements (
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, key)
);

-- also serves as the idempotency guard for ApplyCheckinResult
CREATE TABLE checkin_results (
    checkin_id TEXT PRIMARY KEY,
    room_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    status TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX checkin_results_user_idx ON checkin_results (user_id, applied_at DESC);

-- +goose Down
DROP TABLE checkin_results;
DROP TABLE achievements;
DROP TABLE memberships;
DROP TABLE rooms;
DROP TABLE users;
