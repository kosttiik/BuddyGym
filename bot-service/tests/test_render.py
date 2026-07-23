from io import BytesIO

import pytest
from PIL import Image

from src.notifications.texts import caption, card_for
from src.render.cards import placeholder_card, render

KINDS = [
    "comment",
    "reply",
    "vote_last_call",
    "member_left",
    "freeze_canceled",
    "streak_at_risk",
    "period_summary",
    "welcome",
    "vote_request",
    "approved",
    "rejected",
    "buddy_credited",
    "member_joined",
    "freeze_scheduled",
    "achievement",
    "reminder",
    "digest",
]

PAYLOAD = {
    "room_name": "Железные братья",
    "actor_name": "Марина",
    "body": "Красавчик, так держать!",
    "votes_approve": 1,
    "votes_required": 2,
    "done": 2,
    "goal": 3,
    "key": "streak_7",
    "starts_at": "2026-07-25",
    "ends_at": "2026-08-05",
    "lines": [["Железные братья", 1, 3]],
}


@pytest.mark.parametrize("kind", KINDS)
def test_every_card_renders_a_valid_png(kind: str):
    png = render(kind, card_for(kind, PAYLOAD))
    image = Image.open(BytesIO(png))

    assert image.format == "PNG"
    # cards crop to their content, so only the width is fixed
    assert image.width == 1080
    assert 420 <= image.height <= 720


@pytest.mark.parametrize("kind", KINDS)
def test_every_card_has_a_caption_without_placeholders(kind: str):
    text = caption(kind, PAYLOAD)

    assert text
    assert "{" not in text


def test_a_long_comment_is_ellipsed_rather_than_spilled():
    png = render("comment", card_for("comment", {**PAYLOAD, "body": "очень длинный текст " * 40}))

    assert Image.open(BytesIO(png)).width == 1080


def test_the_placeholder_is_a_card_of_its_own():
    assert Image.open(BytesIO(placeholder_card("Комната"))).width == 1080


def test_every_kind_carries_a_caption_in_both_languages():
    """A missing entry silently falls back to a bare "BuddyGym" line under the card."""
    for kind in KINDS:
        for language in ("ru", "en"):
            text = caption(kind, {**PAYLOAD, "language": language})
            assert text != "BuddyGym", f"{kind}/{language} has no caption"


def test_a_card_speaks_the_recipient_language():
    ru = card_for("member_joined", {**PAYLOAD, "language": "ru"})
    en = card_for("member_joined", {**PAYLOAD, "language": "en"})

    assert ru.title == "Новый участник"
    assert en.title == "New member"
    assert caption("member_joined", {**PAYLOAD, "language": "en"}).startswith("👋 New member")


def test_freeze_dates_follow_the_reader_locale():
    payload = {**PAYLOAD, "starts_at": "2026-07-25", "ends_at": "2026-08-05"}

    assert (
        card_for("freeze_scheduled", {**payload, "language": "ru"}).footer
        == "с 25 июля по 5 августа"
    )
    assert card_for("freeze_scheduled", {**payload, "language": "en"}).footer == "Jul 25 to Aug 5"


def test_a_reply_card_quotes_the_comment_it_answers():
    data = card_for(
        "reply",
        {
            **PAYLOAD,
            "body": "согласен",
            "reply_to_author": "Костя",
            "reply_to_body": "завтра в 8 утра",
        },
    )

    assert data.quote_author == "Костя"
    assert data.quote_body == "завтра в 8 утра"
    assert Image.open(BytesIO(render("reply", data))).width == 1080
