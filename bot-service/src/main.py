import asyncio
import logging

from aiogram import Bot, Dispatcher, F
from aiogram.filters import CommandStart
from aiogram.types import Message
from apscheduler.schedulers.asyncio import AsyncIOScheduler
from sqlalchemy.ext.asyncio import async_sessionmaker, create_async_engine

from src.core.config import BotMode, Settings
from src.notifications.events import CoreReader
from src.notifications.photos import PhotoSource
from src.notifications.sender import Sender
from src.notifications.service import NotificationService

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s")
log = logging.getLogger("buddygym.bot")


def build(settings: Settings) -> tuple[NotificationService, Bot | None, PhotoSource]:
    core_engine = create_async_engine(settings.core_sqlalchemy_dsn, pool_pre_ping=True)
    bot_engine = create_async_engine(settings.bot_sqlalchemy_dsn, pool_pre_ping=True)
    sessions = async_sessionmaker(bot_engine, expire_on_commit=False)

    bot = Bot(settings.bot_token) if settings.mode is BotMode.LIVE and settings.bot_token else None
    photos = PhotoSource(settings)
    sender = Sender(bot, settings, photos, log)
    return (
        NotificationService(CoreReader(core_engine), sessions, sender, settings, log),
        bot,
        photos,
    )


async def poll_forever(service: NotificationService, settings: Settings) -> None:
    while True:
        try:
            queued = await service.ingest()
            folded = await service.fold_backfill()
            results = await service.deliver_pending()
            if queued or folded or results:
                log.info("queued=%s digests=%s delivered=%s", queued, folded, len(results))
        except Exception as error:
            log.exception("poll cycle failed", exc_info=error)
        await asyncio.sleep(settings.poll_interval_seconds)


async def main() -> None:
    settings = Settings()
    service, bot, photos = build(settings)

    scheduler = AsyncIOScheduler(timezone="UTC")
    scheduler.add_job(service.queue_reminders, "cron", hour=9, minute=0)
    scheduler.start()

    tasks = [asyncio.create_task(poll_forever(service, settings))]

    if bot is not None:
        dispatcher = Dispatcher()

        @dispatcher.message(CommandStart())
        async def on_start(message: Message) -> None:
            # a /start is the moment the chat becomes writable: flush what was held back
            await service.mark_reachable(message.chat.id)
            await message.answer(
                "BuddyGym на связи. Буду присылать комментарии к вашим фото, "
                "результаты голосований, достижения и напоминания перед концом периода."
            )

        @dispatcher.message(F.text)
        async def on_any(message: Message) -> None:
            await service.mark_reachable(message.chat.id)

        log.info("bot polling as live")
        tasks.append(asyncio.create_task(dispatcher.start_polling(bot)))
    else:
        log.info("bot running in %s mode, cards go to %s", settings.mode, settings.dry_run_dir)

    try:
        await asyncio.gather(*tasks)
    finally:
        scheduler.shutdown(wait=False)
        await photos.close()
        if bot is not None:
            await bot.session.close()


if __name__ == "__main__":
    asyncio.run(main())
