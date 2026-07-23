import json
from datetime import UTC, datetime, timedelta

import pytest
from sqlalchemy import select, text
from sqlalchemy.ext.asyncio import async_sessionmaker, create_async_engine
from testcontainers.postgres import PostgresContainer

from src.core.config import Settings, asyncpg_dsn
from src.notifications.events import CoreReader
from src.notifications.models import Base, DeliveryStatus, Notification
from src.notifications.sender import Outgoing, SendResult
from src.notifications.service import NotificationService

pytestmark = pytest.mark.integration

NOW = datetime(2026, 7, 22, 12, 0, tzinfo=UTC)

CORE_SCHEMA = """
CREATE TABLE users (
    id BIGINT PRIMARY KEY, username TEXT NOT NULL DEFAULT '',
    first_name TEXT NOT NULL DEFAULT '', avatar_key TEXT NOT NULL DEFAULT '',
    language TEXT NOT NULL DEFAULT 'ru'
);
CREATE TABLE rooms (
    id BIGINT PRIMARY KEY, name TEXT NOT NULL, goal_per_period INT NOT NULL,
    period_days INT NOT NULL, votes_required INT NOT NULL, deleted_at TIMESTAMPTZ,
    avatar_key TEXT NOT NULL DEFAULT ''
);
CREATE TABLE memberships (
    room_id BIGINT NOT NULL, user_id BIGINT NOT NULL, goal_per_period INT,
    sport_name TEXT NOT NULL DEFAULT '', sport_emoji TEXT NOT NULL DEFAULT '',
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY (room_id, user_id)
);
CREATE TABLE freezes (
    id BIGSERIAL PRIMARY KEY, room_id BIGINT NOT NULL, user_id BIGINT NOT NULL,
    starts_at DATE NOT NULL, ends_at DATE NOT NULL, canceled_at TIMESTAMPTZ
);
CREATE TABLE checkin_results (
    checkin_id TEXT PRIMARY KEY, room_id BIGINT NOT NULL, user_id BIGINT NOT NULL,
    status TEXT NOT NULL, checkin_created_at TIMESTAMPTZ
);
CREATE TABLE events (
    id BIGSERIAL PRIMARY KEY, type TEXT NOT NULL, room_id BIGINT NOT NULL,
    actor_id BIGINT NOT NULL DEFAULT 0, subject JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO users (id, first_name) VALUES (1, 'Костя'), (2, 'Марина'), (3, 'Дима');
INSERT INTO rooms (id, name, goal_per_period, period_days, votes_required)
    VALUES (7, 'Железные братья', 3, 7, 2);
INSERT INTO memberships (room_id, user_id) VALUES (7, 1), (7, 2), (7, 3);
"""


class RecordingSender:
    def __init__(self) -> None:
        self.sent: list[Outgoing] = []

    async def send(self, item: Outgoing) -> SendResult:
        self.sent.append(item)
        return SendResult(item.id, DeliveryStatus.SENT, message_id=100 + item.id)


@pytest.fixture(scope="module")
def postgres():
    with PostgresContainer("postgres:17-alpine") as container:
        yield container


@pytest.fixture
async def env(postgres):
    dsn = asyncpg_dsn(postgres.get_connection_url().replace("postgresql+psycopg2", "postgresql"))
    engine = create_async_engine(dsn)

    async with engine.begin() as conn:
        await conn.execute(text("DROP SCHEMA public CASCADE"))
        await conn.execute(text("CREATE SCHEMA public"))
        for statement in filter(None, (s.strip() for s in CORE_SCHEMA.split(";"))):
            await conn.execute(text(statement))
        await conn.run_sync(Base.metadata.create_all)

    settings = Settings(
        BOT_DB_DSN=postgres.get_connection_url().replace("postgresql+psycopg2", "postgresql"),
        CORE_DB_DSN=postgres.get_connection_url().replace("postgresql+psycopg2", "postgresql"),
    )
    sender = RecordingSender()
    sessions = async_sessionmaker(engine, expire_on_commit=False)
    service = NotificationService(CoreReader(engine), sessions, sender, settings, now=lambda: NOW)
    yield service, sender, engine, sessions
    await engine.dispose()


async def add_event(engine, type_: str, actor: int, subject: dict, created_at=NOW) -> None:
    async with engine.begin() as conn:
        await conn.execute(
            text(
                "INSERT INTO events (type, room_id, actor_id, subject, created_at)"
                " VALUES (:t, 7, :a, CAST(:s AS jsonb), :c)"
            ),
            {"t": type_, "a": actor, "s": json.dumps(subject), "c": created_at},
        )


async def test_a_pending_checkin_queues_a_card_for_every_other_member(env):
    service, sender, engine, _ = env
    await add_event(engine, "checkin.created", 1, {"checkin_id": "c1", "status": "pending"})

    assert await service.ingest() == 2
    await service.deliver_pending()

    assert sorted(item.chat_id for item in sender.sent) == [2, 3]


async def test_the_same_event_is_never_delivered_twice(env):
    service, sender, engine, _ = env
    await add_event(engine, "checkin.created", 1, {"checkin_id": "c1", "status": "pending"})

    await service.ingest()
    await service.deliver_pending()
    # a restart replays the cursor read, but the unique key keeps the queue clean
    await service.ingest()
    await service.deliver_pending()

    assert len(sender.sent) == 2


async def test_stale_events_are_folded_into_one_digest_per_chat(env):
    service, sender, engine, sessions = env
    old = NOW - timedelta(days=3)
    await add_event(engine, "checkin.created", 1, {"checkin_id": "c1", "status": "pending"}, old)
    await add_event(engine, "checkin.created", 1, {"checkin_id": "c2", "status": "pending"}, old)

    await service.ingest()
    assert await service.fold_backfill() == 2
    await service.deliver_pending()

    assert {item.kind for item in sender.sent} == {"digest"}
    assert len(sender.sent) == 2

    async with sessions() as session:
        rows = (await session.execute(select(Notification.status))).scalars().all()
    assert rows.count(DeliveryStatus.SKIPPED) == 4


async def test_a_comment_reaches_the_photo_author(env):
    service, sender, engine, _ = env
    await add_event(engine, "checkin.created", 3, {"checkin_id": "c9", "status": "pending"})
    await service.ingest()
    sender.sent.clear()

    await add_event(engine, "comment.created", 2, {"checkin_id": "c9", "body": "Красавчик"})
    await service.ingest()
    await service.deliver_pending()

    comments = [item for item in sender.sent if item.kind == "comment"]
    assert [item.chat_id for item in comments] == [3]
    assert comments[0].payload["actor_name"] == "Марина"


async def test_a_member_behind_the_goal_gets_a_reminder_unless_frozen(env):
    service, _, engine, sessions = env
    async with engine.begin() as conn:
        await conn.execute(
            text("UPDATE memberships SET joined_at = now() - interval '6 days' WHERE room_id = 7")
        )

    assert await service.queue_reminders() == 3

    async with sessions() as session:
        await session.execute(text("DELETE FROM notifications"))
        await session.commit()
    async with engine.begin() as conn:
        await conn.execute(
            text(
                "INSERT INTO freezes (room_id, user_id, starts_at, ends_at)"
                " VALUES (7, 1, current_date - 1, current_date + 5)"
            )
        )

    assert await service.queue_reminders() == 2
