"""notifications, cursor and recipients

Revision ID: 0001
"""

import sqlalchemy as sa

from alembic import op

revision = "0001"
down_revision = None
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "notifications",
        sa.Column("id", sa.BigInteger(), autoincrement=True, primary_key=True),
        sa.Column("event_id", sa.BigInteger(), nullable=False),
        sa.Column("chat_id", sa.BigInteger(), nullable=False),
        sa.Column("kind", sa.String(64), nullable=False),
        sa.Column("payload", sa.Text(), nullable=False, server_default="{}"),
        sa.Column("status", sa.String(16), nullable=False, server_default="pending"),
        sa.Column("message_id", sa.BigInteger()),
        sa.Column("attempts", sa.BigInteger(), nullable=False, server_default="0"),
        sa.Column("error", sa.Text()),
        sa.Column("event_created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column(
            "created_at", sa.DateTime(timezone=True), nullable=False, server_default=sa.func.now()
        ),
        sa.Column("sent_at", sa.DateTime(timezone=True)),
    )
    # one card per event and recipient: the constraint is what makes redelivery impossible
    op.create_index(
        "notifications_event_chat_key",
        "notifications",
        ["event_id", "chat_id", "kind"],
        unique=True,
    )
    op.create_index("notifications_pending_idx", "notifications", ["status", "created_at"])
    op.create_index("notifications_chat_idx", "notifications", ["chat_id"])

    op.create_table(
        "cursor",
        sa.Column("name", sa.String(32), primary_key=True),
        sa.Column("last_event_id", sa.BigInteger(), nullable=False, server_default="0"),
        sa.Column(
            "updated_at", sa.DateTime(timezone=True), nullable=False, server_default=sa.func.now()
        ),
    )

    op.create_table(
        "recipients",
        sa.Column("user_id", sa.BigInteger(), primary_key=True),
        sa.Column("reachable", sa.Boolean(), nullable=False, server_default=sa.true()),
        sa.Column(
            "updated_at", sa.DateTime(timezone=True), nullable=False, server_default=sa.func.now()
        ),
    )


def downgrade() -> None:
    op.drop_table("recipients")
    op.drop_table("cursor")
    op.drop_index("notifications_chat_idx", table_name="notifications")
    op.drop_index("notifications_pending_idx", table_name="notifications")
    op.drop_index("notifications_event_chat_key", table_name="notifications")
    op.drop_table("notifications")
