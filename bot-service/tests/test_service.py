import json
from datetime import UTC, datetime, timedelta

import pytest
from sqlalchemy import select, text
from sqlalchemy.ext.asyncio import async_sessionmaker, create_async_engine
from testcontainers.community.postgres import PostgresContainer

from src.core.config import Settings, asyncpg_dsn
from src.notifications.events import CoreReader
from src.notifications.models import Base, DeliveryStatus, Notification, Recipient
from src.notifications.sender import Outgoing, SendResult
from src.notifications.service import NotificationService

pytestmark = pytest.mark.integration

NOW = datetime(2026, 7, 22, 12, 0, tzinfo=UTC)

CORE_SCHEMA = """
CREATE TABLE users (
    id BIGINT PRIMARY KEY, username TEXT NOT NULL DEFAULT '',
    first_name TEXT NOT NULL DEFAULT '', avatar_key TEXT NOT NULL DEFAULT '',
    language TEXT NOT NULL DEFAULT 'ru', timezone TEXT NOT NULL DEFAULT 'UTC'
);
CREATE TABLE auth_identities (
    provider TEXT NOT NULL, subject TEXT NOT NULL, user_id BIGINT NOT NULL,
    PRIMARY KEY (provider, subject)
);
CREATE TABLE notification_preferences (
    user_id BIGINT PRIMARY KEY, telegram_enabled BOOLEAN NOT NULL DEFAULT true,
    push_enabled BOOLEAN NOT NULL DEFAULT false
);
CREATE TABLE push_subscriptions (
    id UUID PRIMARY KEY, user_id BIGINT NOT NULL, endpoint TEXT NOT NULL,
    p256dh TEXT NOT NULL, auth TEXT NOT NULL
);
CREATE TABLE rooms (
    id BIGINT PRIMARY KEY, name TEXT NOT NULL, goal_per_period INT NOT NULL,
    period_days INT NOT NULL, votes_required INT NOT NULL, deleted_at TIMESTAMPTZ,
    avatar_key TEXT NOT NULL DEFAULT '', calendar_week BOOLEAN NOT NULL DEFAULT false
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
INSERT INTO auth_identities (provider, subject, user_id)
    VALUES ('telegram', '1', 1), ('telegram', '2', 2), ('telegram', '3', 3);
INSERT INTO rooms (id, name, goal_per_period, period_days, votes_required)
    VALUES (7, 'Железные братья', 3, 7, 2);
INSERT INTO memberships (room_id, user_id) VALUES (7, 1), (7, 2), (7, 3);
"""


class RecordingSender:
    def __init__(self) -> None:
        self.sent: list[Outgoing] = []

    async def send(self, item: Outgoing) -> SendResult:
        if not item.payload.get("telegram_enabled", True):
            return SendResult(item.id, DeliveryStatus.SKIPPED)
        self.sent.append(item)
        return SendResult(item.id, DeliveryStatus.SENT, message_id=100 + item.id)


class RecordingPushSender:
    def __init__(self) -> None:
        self.sent: list[Outgoing] = []

    async def send(self, item: Outgoing, targets) -> bool | None:
        if not targets:
            return None
        self.sent.append(item)
        return True


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
    push_sender = RecordingPushSender()
    sessions = async_sessionmaker(engine, expire_on_commit=False)
    # the reminder query only picks members whose own clock reads this hour
    reader = CoreReader(engine, reminder_hour=datetime.now(UTC).hour)
    service = NotificationService(
        reader,
        sessions,
        sender,
        settings,
        push_sender=push_sender,
        now=lambda: NOW,
    )
    yield service, sender, push_sender, engine, sessions
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
    service, sender, _, engine, _ = env
    await add_event(engine, "checkin.created", 1, {"checkin_id": "c1", "status": "pending"})

    assert await service.ingest() == 2
    await service.deliver_pending()

    assert sorted(item.chat_id for item in sender.sent) == [2, 3]


async def test_the_same_event_is_never_delivered_twice(env):
    service, sender, _, engine, _ = env
    await add_event(engine, "checkin.created", 1, {"checkin_id": "c1", "status": "pending"})

    await service.ingest()
    await service.deliver_pending()
    # a restart replays the cursor read, but the unique key keeps the queue clean
    await service.ingest()
    await service.deliver_pending()

    assert len(sender.sent) == 2


async def test_stale_events_are_folded_into_one_digest_per_chat(env):
    service, sender, _, engine, sessions = env
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
    service, sender, _, engine, _ = env
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
    service, _, _, engine, sessions = env
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


async def test_the_reminder_waits_for_the_members_own_morning(env):
    """The scheduler ticks every hour; a member is picked up only when their local clock
    reads the reminder hour, so nobody is woken at 3am for living in the wrong zone."""
    service, _, _, engine, _ = env
    async with engine.begin() as conn:
        await conn.execute(
            text("UPDATE memberships SET joined_at = now() - interval '6 days' WHERE room_id = 7")
        )
        # eight hours ahead of the zone the reader is tuned to
        await conn.execute(text("UPDATE users SET timezone = 'Asia/Shanghai' WHERE id = 1"))

    assert await service.queue_reminders() == 2


async def test_a_calendar_week_room_reminds_before_sunday_midnight(env):
    service, _, _, engine, _ = env
    async with engine.begin() as conn:
        await conn.execute(text("UPDATE rooms SET calendar_week = true WHERE id = 7"))
        await conn.execute(
            text("UPDATE memberships SET joined_at = now() - interval '30 days' WHERE room_id = 7")
        )

    queued = await service.queue_reminders()
    # only on a Sunday is the end of the week within the 24 hour window
    expected = 3 if datetime.now(UTC).weekday() == 6 else 0
    assert queued == expected


async def test_reminders_do_not_crash_on_a_second_daily_run(env):
    """The unique (event_id, chat_id, kind) key used to collide on the second cron run and
    crash the whole batch. A per-day event id makes the repeat a no-op instead."""
    service, _, _, engine, sessions = env
    async with engine.begin() as conn:
        await conn.execute(
            text("UPDATE memberships SET joined_at = now() - interval '6 days' WHERE room_id = 7")
        )

    assert await service.queue_reminders() == 3
    # same day again: no new rows, and no IntegrityError
    assert await service.queue_reminders() == 3

    async with sessions() as session:
        count = (
            await session.execute(
                text("SELECT count(*) FROM notifications WHERE kind = 'reminder'")
            )
        ).scalar_one()
    assert count == 3


async def test_a_welcome_card_is_queued_once_per_user(env):
    service, sender, _, _, sessions = env

    await service.queue_welcome(1, "ru")
    await service.queue_welcome(1, "ru")
    await service.deliver_pending()

    welcomes = [item for item in sender.sent if item.kind == "welcome"]
    assert [item.chat_id for item in welcomes] == [1]


async def test_web_push_delivers_when_telegram_is_unreachable(env):
    service, sender, push_sender, engine, sessions = env
    async with engine.begin() as conn:
        await conn.execute(
            text("INSERT INTO notification_preferences (user_id, push_enabled) VALUES (2, true)")
        )
        await conn.execute(
            text(
                "INSERT INTO push_subscriptions (id, user_id, endpoint, p256dh, auth)"
                " VALUES ('00000000-0000-0000-0000-000000000001', 2,"
                " 'https://push.example.test/2', 'key', 'auth')"
            )
        )
    async with sessions() as session:
        session.add(Recipient(user_id=2, reachable=False, updated_at=NOW))
        await session.commit()

    await add_event(engine, "checkin.created", 1, {"checkin_id": "c1", "status": "pending"})
    await service.ingest()
    await service.deliver_pending()

    assert [item.chat_id for item in push_sender.sent] == [2]
    assert [item.chat_id for item in sender.sent] == [3]


async def test_disabled_telegram_keeps_web_push_enabled(env):
    service, sender, push_sender, engine, _ = env
    async with engine.begin() as conn:
        await conn.execute(
            text(
                "INSERT INTO notification_preferences"
                " (user_id, telegram_enabled, push_enabled) VALUES (2, false, true)"
            )
        )
        await conn.execute(
            text(
                "INSERT INTO push_subscriptions (id, user_id, endpoint, p256dh, auth)"
                " VALUES ('00000000-0000-0000-0000-000000000001', 2,"
                " 'https://push.example.test/2', 'key', 'auth')"
            )
        )

    await add_event(engine, "checkin.created", 1, {"checkin_id": "c1", "status": "pending"})
    await service.ingest()
    await service.deliver_pending()

    assert [item.chat_id for item in push_sender.sent] == [2]
    assert [item.chat_id for item in sender.sent] == [3]
