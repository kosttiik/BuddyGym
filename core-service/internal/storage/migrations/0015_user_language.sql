-- +goose Up
-- the notification bot writes in the language the member reads the app in
ALTER TABLE users ADD COLUMN language TEXT NOT NULL DEFAULT 'ru'
    CHECK (language IN ('ru', 'en'));

-- +goose Down
ALTER TABLE users DROP COLUMN language;
