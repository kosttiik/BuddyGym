package storage

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Events struct {
	pool *pgxpool.Pool
}

func NewEvents(pool *pgxpool.Pool) *Events {
	return &Events{pool: pool}
}

func (e *Events) Add(ctx context.Context, eventType string, roomID, actorID int64, subject map[string]any) error {
	if subject == nil {
		subject = map[string]any{}
	}
	raw, err := json.Marshal(subject)
	if err != nil {
		return err
	}
	_, err = e.pool.Exec(ctx,
		"INSERT INTO events (type, room_id, actor_id, subject) VALUES ($1, $2, $3, $4)",
		eventType, roomID, actorID, raw)
	return err
}
