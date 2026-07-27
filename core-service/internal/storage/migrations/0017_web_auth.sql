-- +goose Up
CREATE SEQUENCE users_id_seq;
SELECT setval('users_id_seq', GREATEST(COALESCE((SELECT max(id) FROM users), 0) + 1, 1), false);
ALTER TABLE users ALTER COLUMN id SET DEFAULT nextval('users_id_seq');
ALTER SEQUENCE users_id_seq OWNED BY users.id;

WITH duplicates AS (
    SELECT id, row_number() OVER (
        PARTITION BY lower(username)
        ORDER BY created_at DESC, id DESC
    ) AS position
    FROM users
    WHERE username <> ''
)
UPDATE users
SET username = ''
FROM duplicates
WHERE users.id = duplicates.id AND duplicates.position > 1;

CREATE UNIQUE INDEX users_username_key
    ON users (lower(username))
    WHERE username <> '';

CREATE TABLE auth_identities (
    provider TEXT NOT NULL CHECK (provider IN ('telegram')),
    subject TEXT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, subject),
    UNIQUE (provider, user_id)
);

INSERT INTO auth_identities (provider, subject, user_id)
SELECT 'telegram', id::text, id FROM users;

CREATE TABLE password_credentials (
    user_id BIGINT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    password_hash TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE auth_sessions (
    id UUID PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX auth_sessions_user_idx ON auth_sessions (user_id, expires_at DESC);

CREATE TABLE telegram_link_tokens (
    token_hash BYTEA PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE push_subscriptions (
    id UUID PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    endpoint TEXT NOT NULL UNIQUE,
    p256dh TEXT NOT NULL,
    auth TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX push_subscriptions_user_idx ON push_subscriptions (user_id);

CREATE TABLE notification_preferences (
    user_id BIGINT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    telegram_enabled BOOLEAN NOT NULL DEFAULT true,
    push_enabled BOOLEAN NOT NULL DEFAULT false
);

ALTER TABLE users ADD COLUMN avatar_custom BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE users DROP COLUMN avatar_custom;
DROP TABLE notification_preferences;
DROP TABLE push_subscriptions;
DROP TABLE telegram_link_tokens;
DROP TABLE auth_sessions;
DROP TABLE password_credentials;
DROP TABLE auth_identities;
DROP INDEX users_username_key;
ALTER TABLE users ALTER COLUMN id DROP DEFAULT;
DROP SEQUENCE users_id_seq;
