-- +goose Up
ALTER TABLE checkin_comments
    ADD COLUMN reply_to BIGINT REFERENCES checkin_comments (id) ON DELETE SET NULL;

CREATE INDEX checkin_comments_reply_idx ON checkin_comments (reply_to) WHERE reply_to IS NOT NULL;

-- +goose Down
DROP INDEX checkin_comments_reply_idx;
ALTER TABLE checkin_comments DROP COLUMN reply_to;
