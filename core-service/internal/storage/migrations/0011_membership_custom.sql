-- +goose Up
ALTER TABLE memberships
    ADD COLUMN sport_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN sport_emoji TEXT NOT NULL DEFAULT '',
    ADD COLUMN goal_per_period INT CHECK (goal_per_period BETWEEN 1 AND 100);

-- +goose Down
ALTER TABLE memberships
    DROP COLUMN sport_name,
    DROP COLUMN sport_emoji,
    DROP COLUMN goal_per_period;
