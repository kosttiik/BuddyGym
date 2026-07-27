import asyncio
import json
import logging
import re

from pywebpush import WebPushException, webpush

from src.core.config import Settings
from src.notifications.events import PushTarget
from src.notifications.sender import Outgoing
from src.notifications.texts import caption


class WebPushSender:
    def __init__(self, settings: Settings, logger: logging.Logger | None = None) -> None:
        self._private_key = settings.vapid_private_key
        self._subject = settings.vapid_subject
        self._base_url = settings.web_app_url.rstrip("/")
        self._log = logger or logging.getLogger(__name__)

    async def send(self, item: Outgoing, targets: list[PushTarget]) -> bool | None:
        if not targets:
            return None
        room_id = item.payload.get("room_id")
        url = f"{self._base_url}/rooms/{room_id}" if room_id else self._base_url
        body = re.sub(r"<[^>]+>", "", caption(item.kind, item.payload))
        data = json.dumps(
            {
                "title": "BuddyGym",
                "body": body[:180],
                "url": url,
                "icon": "/icon-192.png",
            },
            ensure_ascii=False,
        )
        results = await asyncio.gather(
            *(asyncio.to_thread(self._send_one, target, data) for target in targets)
        )
        return any(results)

    def _send_one(self, target: PushTarget, data: str) -> bool:
        try:
            webpush(
                subscription_info={
                    "endpoint": target.endpoint,
                    "keys": {"p256dh": target.p256dh, "auth": target.auth},
                },
                data=data,
                vapid_private_key=self._private_key,
                vapid_claims={"sub": self._subject},
                timeout=10,
            )
            return True
        except WebPushException as error:
            status = error.response.status_code if error.response is not None else "unknown"
            self._log.warning("web push failed status=%s", status)
            return False
