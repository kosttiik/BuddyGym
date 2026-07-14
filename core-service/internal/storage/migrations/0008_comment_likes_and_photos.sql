-- +goose Up
-- Comment photos live in core's own bucket. They are kept as long as the checkin photo is,
-- and the reaper clears them out on the same retention window.
ALTER TABLE checkin_comments ADD COLUMN photo_key TEXT NOT NULL DEFAULT '';

-- a meme needs no caption, so an empty body is fine as long as a photo carries the comment
ALTER TABLE checkin_comments DROP CONSTRAINT checkin_comments_body_check;
ALTER TABLE checkin_comments ADD CONSTRAINT checkin_comments_body_check
    CHECK (length(body) <= 500 AND (length(body) > 0 OR photo_key <> ''));

CREATE TABLE checkin_comment_likes (
    comment_id BIGINT NOT NULL REFERENCES checkin_comments (id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (comment_id, user_id)
);

-- +goose Down
DROP TABLE checkin_comment_likes;

DELETE FROM checkin_comments WHERE length(body) = 0;

ALTER TABLE checkin_comments DROP CONSTRAINT checkin_comments_body_check;
ALTER TABLE checkin_comments ADD CONSTRAINT checkin_comments_body_check
    CHECK (length(body) BETWEEN 1 AND 500);

ALTER TABLE checkin_comments DROP COLUMN photo_key;
