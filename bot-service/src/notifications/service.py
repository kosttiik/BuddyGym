import json
import logging
from collections import Counter, defaultdict
from datetime import UTC, datetime, timedelta

from sqlalchemy import select, update
from sqlalchemy.dialects.postgresql import insert
from sqlalchemy.ext.asyncio import async_sessionmaker

from src.core.config import Settings
from src.notifications.events import CoreReader, Event
from src.notifications.fanout import Delivery, recipients_for
from src.notifications.models import Cursor, DeliveryStatus, Notification, Recipient
from src.notifications.sender import Outgoing, Sender, SendResult

CURSOR_NAME = "events"

DIGEST_LABELS = {
    "comment": "Комментарии к вашим фото",
    "vote_request": "Ждут вашего голоса",
    "approved": "Зачтённые тренировки",
    "rejected": "Отклонённые тренировки",
    "buddy_credited": "Совместные тренировки",
    "member_joined": "Новые участники",
    "freeze_scheduled": "Заморозки в комнатах",
}


class NotificationService:
    def __init__(
        self,
        core: CoreReader,
        sessions: async_sessionmaker,
        sender: Sender,
        settings: Settings,
        logger: logging.Logger | None = None,
        now=lambda: datetime.now(UTC),
    ) -> None:
        self._core = core
        self._sessions = sessions
        self._sender = sender
        self._settings = settings
        self._log = logger or logging.getLogger(__name__)
        self._now = now

    async def ingest(self) -> int:
        """Turn new outbox rows into pending notifications. Returns how many were queued."""
        async with self._sessions() as session:
            cursor = await session.get(Cursor, CURSOR_NAME)
            last_id = cursor.last_event_id if cursor else 0

        events = await self._core.events_after(last_id, self._settings.batch_size)
        if not events:
            return 0

        queued = 0
        for event in events:
            room = await self._core.room(event.room_id)
            if room is None:
                continue
            deliveries = recipients_for(event, room, await self._checkin_owner(event))
            if not deliveries:
                continue
            enriched = await self._enrich(event, deliveries)
            queued += await self._store(event, enriched)

        async with self._sessions() as session:
            await session.merge(
                Cursor(name=CURSOR_NAME, last_event_id=events[-1].id, updated_at=self._now())
            )
            await session.commit()
        return queued

    async def _checkin_owner(self, event: Event) -> int | None:
        checkin_id = event.subject.get("checkin_id")
        if event.type != "comment.created" or not checkin_id:
            return None
        return await self._core.checkin_owner(str(checkin_id))

    async def _enrich(self, event: Event, deliveries: list[Delivery]) -> list[Delivery]:
        users = await self._core.users([event.actor_id])
        actor = users.get(event.actor_id)
        if actor is None:
            return deliveries
        return [
            Delivery(
                d.chat_id,
                d.kind,
                {**d.payload, "actor_name": actor.first_name, "actor_avatar_key": actor.avatar_key},
            )
            for d in deliveries
        ]

    async def _store(self, event: Event, deliveries: list[Delivery]) -> int:
        async with self._sessions() as session:
            for delivery in deliveries:
                await session.execute(
                    insert(Notification)
                    .values(
                        event_id=event.id,
                        chat_id=delivery.chat_id,
                        kind=delivery.kind,
                        payload=json.dumps(delivery.payload, ensure_ascii=False, default=str),
                        status=DeliveryStatus.PENDING,
                        event_created_at=event.created_at,
                    )
                    .on_conflict_do_nothing()
                )
            await session.commit()
        return len(deliveries)

    async def fold_backfill(self) -> int:
        """Collapse everything older than the digest window into one card per chat.

        Without this the first run after a deploy would fire a burst of stale notifications.
        """
        cutoff = self._now() - timedelta(hours=self._settings.digest_after_hours)
        async with self._sessions() as session:
            rows = (
                (
                    await session.execute(
                        select(Notification)
                        .where(Notification.status == DeliveryStatus.PENDING)
                        .where(Notification.event_created_at < cutoff)
                    )
                )
                .scalars()
                .all()
            )

            if not rows:
                return 0

            per_chat: dict[int, Counter] = defaultdict(Counter)
            for row in rows:
                per_chat[row.chat_id][row.kind] += 1
                row.status = DeliveryStatus.SKIPPED

            for chat_id, counts in per_chat.items():
                lines = [
                    [DIGEST_LABELS.get(kind, kind), count, 0]
                    for kind, count in counts.most_common(5)
                ]
                session.add(
                    Notification(
                        event_id=0,
                        chat_id=chat_id,
                        kind="digest",
                        payload=json.dumps(
                            {"subtitle": "Собрали в одну сводку", "lines": lines},
                            ensure_ascii=False,
                        ),
                        status=DeliveryStatus.PENDING,
                        event_created_at=self._now(),
                    )
                )
            await session.commit()
            return len(per_chat)

    async def deliver_pending(self, limit: int = 50) -> list[SendResult]:
        async with self._sessions() as session:
            unreachable = set(
                (
                    await session.execute(
                        select(Recipient.user_id).where(Recipient.reachable.is_(False))
                    )
                )
                .scalars()
                .all()
            )
            rows = (
                (
                    await session.execute(
                        select(Notification)
                        .where(Notification.status == DeliveryStatus.PENDING)
                        .order_by(Notification.event_created_at)
                        .limit(limit)
                    )
                )
                .scalars()
                .all()
            )
            items = [
                Outgoing(row.id, row.chat_id, row.kind, json.loads(row.payload))
                for row in rows
                if row.chat_id not in unreachable
            ]

        results: list[SendResult] = []
        for item in items:
            result = await self._sender.send(item)
            results.append(result)
            await self._apply(result, item.chat_id)
        return results

    async def _apply(self, result: SendResult, chat_id: int) -> None:
        async with self._sessions() as session:
            values: dict = {"status": result.status, "error": result.error}
            if result.status == DeliveryStatus.SENT:
                values["sent_at"] = self._now()
                values["message_id"] = result.message_id
            if result.status == DeliveryStatus.PENDING:
                values.pop("status")
            await session.execute(
                update(Notification)
                .where(Notification.id == result.notification_id)
                .values(**values)
            )
            if result.status == DeliveryStatus.UNREACHABLE:
                await session.merge(
                    Recipient(user_id=chat_id, reachable=False, updated_at=self._now())
                )
            await session.commit()

    async def mark_reachable(self, user_id: int) -> None:
        """A /start (or a granted write access) reopens the chat; pending cards go out next tick."""
        async with self._sessions() as session:
            await session.merge(Recipient(user_id=user_id, reachable=True, updated_at=self._now()))
            await session.execute(
                update(Notification)
                .where(Notification.chat_id == user_id)
                .where(Notification.status == DeliveryStatus.UNREACHABLE)
                .values(status=DeliveryStatus.PENDING)
            )
            await session.commit()

    async def queue_reminders(self) -> int:
        """One card per member whose period closes soon with the goal still unmet."""
        behind = await self._core.members_behind_goal(self._settings.reminder_hours_before)
        if not behind:
            return 0

        per_user: dict[int, list[list]] = defaultdict(list)
        for row in behind:
            per_user[row.user_id].append([row.room_name, row.workouts_count, row.goal])

        async with self._sessions() as session:
            for user_id, lines in per_user.items():
                session.add(
                    Notification(
                        event_id=0,
                        chat_id=user_id,
                        kind="reminder",
                        payload=json.dumps(
                            {"subtitle": "Скоро конец периода", "lines": lines}, ensure_ascii=False
                        ),
                        status=DeliveryStatus.PENDING,
                        event_created_at=self._now(),
                    )
                )
            await session.commit()
        return len(per_user)
