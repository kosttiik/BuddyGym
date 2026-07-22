from datetime import UTC, datetime

from src.core.config import BotMode, Settings


def settings(**overrides) -> Settings:
    values = {
        "BOT_DB_DSN": "postgresql://bot:bot@localhost:5432/bot_db",
        "CORE_DB_DSN": "postgresql://core:core@localhost:5432/core_db",
        "BOT_MODE": BotMode.DRY_RUN,
    }
    values.update(overrides)
    return Settings(**{k: v for k, v in values.items()})


def now() -> datetime:
    return datetime(2026, 7, 22, 12, 0, tzinfo=UTC)
