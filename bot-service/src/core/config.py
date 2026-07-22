from enum import StrEnum
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit

from pydantic import Field, PostgresDsn
from pydantic_settings import BaseSettings, SettingsConfigDict


class BotMode(StrEnum):
    LIVE = "live"
    DRY_RUN = "dry-run"


def asyncpg_dsn(dsn: str) -> str:
    parsed = urlsplit(dsn)
    query = [
        (key, value)
        for key, value in parse_qsl(parsed.query, keep_blank_values=True)
        if not (key == "sslmode" and value == "disable")
    ]
    scheme = "postgresql+asyncpg" if parsed.scheme in {"postgres", "postgresql"} else parsed.scheme
    return urlunsplit((scheme, parsed.netloc, parsed.path, urlencode(query), parsed.fragment))


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", env_ignore_empty=True, extra="ignore")

    bot_token: str = Field(default="", validation_alias="BOT_TOKEN")
    bot_username: str = Field(default="buddygym_bot", validation_alias="BOT_USERNAME")
    mini_app_short_name: str = Field(default="app", validation_alias="MINI_APP_SHORT_NAME")
    # dry-run renders every card to disk and sends nothing: the way to review a change
    # before it reaches real chats
    mode: BotMode = Field(default=BotMode.DRY_RUN, validation_alias="BOT_MODE")
    dry_run_dir: str = Field(default="out", validation_alias="BOT_DRY_RUN_DIR")

    bot_db_dsn: PostgresDsn = Field(validation_alias="BOT_DB_DSN")
    core_db_dsn: PostgresDsn = Field(validation_alias="CORE_DB_DSN")
    checkin_grpc_addr: str = Field(default="checkin:9091", validation_alias="CHECKIN_GRPC_ADDR")

    s3_endpoint: str = Field(default="", validation_alias="S3_ENDPOINT")
    s3_access_key: str = Field(default="", validation_alias="S3_ACCESS_KEY")
    s3_secret_key: str = Field(default="", validation_alias="S3_SECRET_KEY")
    s3_avatar_bucket: str = Field(default="avatars", validation_alias="S3_AVATAR_BUCKET")
    s3_comment_bucket: str = Field(default="comment-photos", validation_alias="S3_COMMENT_BUCKET")

    poll_interval_seconds: float = Field(default=5, validation_alias="POLL_INTERVAL_SECONDS", gt=0)
    batch_size: int = Field(default=200, validation_alias="EVENT_BATCH_SIZE", ge=1, le=1000)
    send_workers: int = Field(default=4, validation_alias="SEND_WORKERS", ge=1, le=32)
    # Telegram allows 30 messages a second overall and one a second per chat
    global_rate: int = Field(default=25, validation_alias="GLOBAL_RATE", ge=1, le=30)
    # events older than this are folded into one digest instead of a burst of messages
    digest_after_hours: int = Field(default=24, validation_alias="DIGEST_AFTER_HOURS", ge=1)
    reminder_hours_before: int = Field(default=24, validation_alias="REMINDER_HOURS_BEFORE", ge=1)

    @property
    def bot_sqlalchemy_dsn(self) -> str:
        return asyncpg_dsn(str(self.bot_db_dsn))

    @property
    def core_sqlalchemy_dsn(self) -> str:
        return asyncpg_dsn(str(self.core_db_dsn))

    @property
    def mini_app_url(self) -> str:
        return f"https://t.me/{self.bot_username}/{self.mini_app_short_name}"
