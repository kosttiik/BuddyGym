from src.notifications.events import PushTarget
from src.notifications.sender import Outgoing
from src.notifications.web_push import WebPushSender

from .conftest import settings


async def test_web_push_fans_out_to_registered_browsers(monkeypatch):
    sender = WebPushSender(
        settings(
            VAPID_PRIVATE_KEY="private-key",
            VAPID_SUBJECT="mailto:test@buddygym.ru",
        )
    )
    sent = []

    def record(target, data):
        sent.append((target, data))
        return True

    monkeypatch.setattr(sender, "_send_one", record)
    delivered = await sender.send(
        Outgoing(
            id=1,
            chat_id=42,
            kind="member_joined",
            payload={"room_id": 7, "room_name": "Команда", "actor_name": "Костя"},
        ),
        [
            PushTarget("https://push.example.test/one", "key-1", "auth-1"),
            PushTarget("https://push.example.test/two", "key-2", "auth-2"),
        ],
    )

    assert delivered
    assert len(sent) == 2
    assert '"url": "https://buddygym.ru/rooms/7"' in sent[0][1]


async def test_web_push_distinguishes_no_targets_from_failed_delivery(monkeypatch):
    sender = WebPushSender(settings(VAPID_PRIVATE_KEY="private-key"))
    item = Outgoing(id=1, chat_id=42, kind="welcome", payload={})

    assert await sender.send(item, []) is None

    monkeypatch.setattr(sender, "_send_one", lambda target, data: False)
    assert not await sender.send(
        item,
        [PushTarget("https://push.example.test/one", "key", "auth")],
    )
