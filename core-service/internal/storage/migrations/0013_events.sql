-- +goose Up
CREATE TABLE events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    type TEXT NOT NULL,
    room_id BIGINT NOT NULL,
    actor_id BIGINT NOT NULL DEFAULT 0,
    subject JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- backfill so the notification bot can deliver what happened before it existed
INSERT INTO events (type, room_id, actor_id, subject, created_at)
SELECT 'comment.created', c.room_id, c.user_id,
       jsonb_build_object(
           'checkin_id', c.checkin_id,
           'comment_id', c.id,
           'body', left(c.body, 200),
           'has_photo', c.photo_key <> ''
       ),
       c.created_at
FROM checkin_comments c;

INSERT INTO events (type, room_id, actor_id, subject, created_at)
SELECT 'checkin.' || cr.status, cr.room_id, cr.user_id,
       jsonb_build_object('checkin_id', cr.checkin_id),
       COALESCE(cr.checkin_created_at, cr.applied_at)
FROM checkin_results cr;

INSERT INTO events (type, room_id, actor_id, subject, created_at)
SELECT 'member.joined', m.room_id, m.user_id, '{}', m.joined_at
FROM memberships m;

-- +goose Down
DROP TABLE events;
