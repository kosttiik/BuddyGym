import asyncio
import json
import logging
from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import Path
from typing import Any, Protocol

from aiogram import Bot
from aiogram.exceptions import TelegramForbiddenError, TelegramRetryAfter
from aiogram.types import (
    BufferedInputFile,
    InlineKeyboardButton,
    InlineKeyboardMarkup,
    InputMediaPhoto,
)
from aiolimiter import AsyncLimiter

from src.core.config import BotMode, Settings
from src.notifications.texts import caption, card_for
from src.render.cards import placeholder_card, render


class Photos(Protocol):
    async def comment_photo(self, key: str) -> bytes | None: ...
    async def avatar(self, key: str) -> bytes | None: ...
    async def checkin_photo(self, checkin_id: str) -> bytes | None: ...


@dataclass(slots=True)
class Outgoing:
    id: int
    chat_id: int
    kind: str
    payload: dict[str, Any]
    attempts: int = 0


@dataclass(slots=True)
class SendResult:
    notification_id: int
    status: str
    message_id: int | None = None
    error: str | None = None
    attempts: int = 0


class Sender:
    """Sends one card per notification.

    The message is posted within a second as a branded placeholder, then edited to the
    finished card: rendering and photo fetching happen while the user already sees something.
    """

    def __init__(
        self,
        bot: Bot | None,
        settings: Settings,
        photos: Photos | None,
        logger: logging.Logger | None = None,
    ) -> None:
        self._bot = bot
        self._settings = settings
        self._photos = photos
        self._log = logger or logging.getLogger(__name__)
        self._global = AsyncLimiter(settings.global_rate, 1)
        self._chat_locks: dict[int, asyncio.Lock] = {}
        self._placeholder = placeholder_card()

    def _chat_lock(self, chat_id: int) -> asyncio.Lock:
        return self._chat_locks.setdefault(chat_id, asyncio.Lock())

    def _keyboard(self, payload: dict[str, Any]) -> InlineKeyboardMarkup:
        room_id = payload.get("room_id")
        url = self._settings.mini_app_url
        if room_id:
            url = f"{url}?startapp=room_{room_id}"
        return InlineKeyboardMarkup(
            inline_keyboard=[[InlineKeyboardButton(text="Открыть BuddyGym", url=url)]]
        )

    async def _card_bytes(self, item: Outgoing) -> bytes:
        data = card_for(item.kind, item.payload)
        if self._photos is not None:
            from PIL import Image

            def load(raw: bytes | None) -> Image.Image | None:
                if not raw:
                    return None
                from io import BytesIO

                try:
                    return Image.open(BytesIO(raw)).convert("RGB")
                except Exception:
                    return None

            if avatar_key := item.payload.get("actor_avatar_key"):
                data.actor_photo = load(await self._photos.avatar(avatar_key))
            if room_key := item.payload.get("room_avatar_key"):
                data.room_photo = load(await self._photos.avatar(room_key))
            if item.kind == "comment" and item.payload.get("comment_photo_key"):
                data.photo = load(
                    await self._photos.comment_photo(item.payload["comment_photo_key"])
                )
            elif item.kind in {"vote_request", "comment"} and item.payload.get("checkin_id"):
                data.photo = load(await self._photos.checkin_photo(item.payload["checkin_id"]))
        return render(item.kind, data)

    async def send(self, item: Outgoing) -> SendResult:
        if self._settings.mode is BotMode.DRY_RUN or self._bot is None:
            return await self._dry_run(item)

        text = caption(item.kind, item.payload)
        keyboard = self._keyboard(item.payload)

        async with self._chat_lock(item.chat_id):
            try:
                async with self._global:
                    await self._bot.send_chat_action(item.chat_id, "upload_photo")
                    message = await self._bot.send_photo(
                        item.chat_id,
                        BufferedInputFile(self._placeholder, "buddygym.png"),
                        caption=text,
                        reply_markup=keyboard,
                    )
            except TelegramForbiddenError as error:
                return SendResult(item.id, "unreachable", error=str(error))
            except TelegramRetryAfter as error:
                await asyncio.sleep(error.retry_after)
                return SendResult(item.id, "pending", error=str(error), attempts=item.attempts)
            except Exception as error:
                self._log.exception("send placeholder failed", exc_info=error)
                return SendResult(item.id, "failed", error=str(error))

            try:
                card = await self._card_bytes(item)
                async with self._global:
                    await self._bot.edit_message_media(
                        chat_id=item.chat_id,
                        message_id=message.message_id,
                        media=InputMediaPhoto(
                            media=BufferedInputFile(card, "buddygym.png"), caption=text
                        ),
                        reply_markup=keyboard,
                    )
            except Exception as error:
                # the placeholder is already delivered, so a failed edit is not a lost message
                self._log.warning("card edit failed: %s", error)

            # Telegram allows one message a second per chat
            await asyncio.sleep(1)
            return SendResult(item.id, "sent", message_id=message.message_id)

    async def _dry_run(self, item: Outgoing) -> SendResult:
        directory = Path(self._settings.dry_run_dir)
        directory.mkdir(parents=True, exist_ok=True)
        stamp = datetime.now(UTC).strftime("%H%M%S")
        name = f"{item.id:06d}-{item.kind}-{item.chat_id}-{stamp}"
        (directory / f"{name}.png").write_bytes(await self._card_bytes(item))
        (directory / f"{name}.json").write_text(
            json.dumps(
                {
                    "chat_id": item.chat_id,
                    "kind": item.kind,
                    "caption": caption(item.kind, item.payload),
                    "payload": item.payload,
                },
                ensure_ascii=False,
                indent=2,
            ),
            encoding="utf-8",
        )
        return SendResult(item.id, "sent")
