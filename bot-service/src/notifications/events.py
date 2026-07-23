import json
from dataclasses import dataclass
from datetime import datetime
from typing import Any

from sqlalchemy import text
from sqlalchemy.ext.asyncio import AsyncEngine

EVENT_KINDS = (
    "comment.created",
    "checkin.created",
    "checkin.approved",
    "checkin.rejected",
    "member.joined",
    "freeze.scheduled",
    "freeze.canceled",
    "buddy.credited",
)


@dataclass(frozen=True, slots=True)
class Event:
    id: int
    type: str
    room_id: int
    actor_id: int
    subject: dict[str, Any]
    created_at: datetime


@dataclass(frozen=True, slots=True)
class RoomContext:
    room_id: int
    room_name: str
    goal_per_period: int
    period_days: int
    votes_required: int
    member_ids: tuple[int, ...]


@dataclass(frozen=True, slots=True)
class UserContext:
    id: int
    first_name: str
    username: str
    avatar_key: str


@dataclass(frozen=True, slots=True)
class MemberProgress:
    user_id: int
    room_name: str
    first_name: str
    workouts_count: int
    goal: int
    period_ends_at: datetime
    frozen: bool


class CoreReader:
    """Read-only view of core_db. The bot never writes there."""

    def __init__(self, engine: AsyncEngine) -> None:
        self._engine = engine

    async def events_after(self, last_id: int, limit: int) -> list[Event]:
        query = text(
            """
            SELECT id, type, room_id, actor_id, subject, created_at
            FROM events
            WHERE id > :last_id
            ORDER BY id
            LIMIT :limit
            """
        )
        async with self._engine.connect() as conn:
            rows = await conn.execute(query, {"last_id": last_id, "limit": limit})
            return [
                Event(
                    id=row.id,
                    type=row.type,
                    room_id=row.room_id,
                    actor_id=row.actor_id,
                    subject=row.subject
                    if isinstance(row.subject, dict)
                    else json.loads(row.subject),
                    created_at=row.created_at,
                )
                for row in rows
            ]

    async def max_event_id(self) -> int:
        async with self._engine.connect() as conn:
            result = await conn.execute(text("SELECT COALESCE(max(id), 0) FROM events"))
            return int(result.scalar_one())

    async def room(self, room_id: int) -> RoomContext | None:
        query = text(
            """
            SELECT r.id, r.name, r.goal_per_period, r.period_days, r.votes_required,
                   COALESCE(array_agg(m.user_id ORDER BY m.joined_at)
                            FILTER (WHERE m.user_id IS NOT NULL), '{}') AS member_ids
            FROM rooms r
            LEFT JOIN memberships m ON m.room_id = r.id
            WHERE r.id = :room_id AND r.deleted_at IS NULL
            GROUP BY r.id
            """
        )
        async with self._engine.connect() as conn:
            row = (await conn.execute(query, {"room_id": room_id})).first()
        if row is None:
            return None
        return RoomContext(
            room_id=row.id,
            room_name=row.name,
            goal_per_period=row.goal_per_period,
            period_days=row.period_days,
            votes_required=row.votes_required,
            member_ids=tuple(row.member_ids),
        )

    async def users(self, user_ids: list[int]) -> dict[int, UserContext]:
        if not user_ids:
            return {}
        query = text(
            """
            SELECT id, first_name, username, avatar_key
            FROM users WHERE id = ANY(:ids)
            """
        )
        async with self._engine.connect() as conn:
            rows = await conn.execute(query, {"ids": user_ids})
            return {
                row.id: UserContext(
                    id=row.id,
                    first_name=row.first_name,
                    username=row.username,
                    avatar_key=row.avatar_key or "",
                )
                for row in rows
            }

    async def checkin_owner(self, checkin_id: str) -> int | None:
        """The outbox carries checkin ids only, so the author is looked up by its create event."""
        query = text(
            "SELECT actor_id FROM events WHERE type = 'checkin.created'"
            " AND subject->>'checkin_id' = :id ORDER BY id LIMIT 1"
        )
        async with self._engine.connect() as conn:
            row = (await conn.execute(query, {"id": checkin_id})).first()
        return int(row.actor_id) if row else None

    async def members_behind_goal(self, hours_before: int) -> list[MemberProgress]:
        """Members whose period closes within the window and who have not met their goal yet.

        A member with an active freeze is skipped: the period is not judged for them.
        """
        query = text(
            """
            WITH grid AS (
                SELECT m.room_id, r.name AS room_name, m.user_id, u.first_name,
                       COALESCE(m.goal_per_period, r.goal_per_period) AS goal,
                       ((m.joined_at AT TIME ZONE 'UTC')::date + (floor(
                           (((now() AT TIME ZONE 'UTC')::date
                             - (m.joined_at AT TIME ZONE 'UTC')::date))::numeric / r.period_days
                       )::int * r.period_days)) AS period_start,
                       r.period_days,
                       EXISTS (
                           SELECT 1 FROM freezes f
                           WHERE f.room_id = m.room_id AND f.user_id = m.user_id
                             AND f.canceled_at IS NULL
                             AND f.starts_at <= (now() AT TIME ZONE 'UTC')::date
                             AND f.ends_at > (now() AT TIME ZONE 'UTC')::date
                       ) AS frozen
                FROM memberships m
                JOIN rooms r ON r.id = m.room_id
                JOIN users u ON u.id = m.user_id
                WHERE r.deleted_at IS NULL
            )
            SELECT g.room_id, g.room_name, g.user_id, g.first_name, g.goal,
                   (g.period_start + g.period_days)::timestamptz AS period_ends_at,
                   g.frozen,
                   (
                       SELECT count(DISTINCT (cr.checkin_created_at AT TIME ZONE 'UTC')::date)::int
                       FROM checkin_results cr
                       WHERE cr.room_id = g.room_id AND cr.user_id = g.user_id
                         AND cr.status = 'approved'
                         AND (cr.checkin_created_at AT TIME ZONE 'UTC')::date >= g.period_start
                   ) AS workouts_count
            FROM grid g
            WHERE NOT g.frozen
              AND (g.period_start + g.period_days)::timestamptz
                  BETWEEN now() AND now() + make_interval(hours => :hours)
            """
        )
        async with self._engine.connect() as conn:
            rows = await conn.execute(query, {"hours": hours_before})
            return [
                MemberProgress(
                    user_id=row.user_id,
                    room_name=row.room_name,
                    first_name=row.first_name,
                    workouts_count=row.workouts_count,
                    goal=row.goal,
                    period_ends_at=row.period_ends_at,
                    frozen=row.frozen,
                )
                for row in rows
                if row.workouts_count < row.goal
            ]
