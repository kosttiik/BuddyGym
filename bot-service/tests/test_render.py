from io import BytesIO

import pytest
from PIL import Image

from src.notifications.texts import caption, card_for
from src.render.cards import placeholder_card, render

KINDS = [
    "comment",
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
    assert image.size == (1080, 720)


@pytest.mark.parametrize("kind", KINDS)
def test_every_card_has_a_caption_without_placeholders(kind: str):
    text = caption(kind, PAYLOAD)

    assert text
    assert "{" not in text


def test_a_long_comment_is_ellipsed_rather_than_spilled():
    png = render("comment", card_for("comment", {**PAYLOAD, "body": "очень длинный текст " * 40}))

    assert Image.open(BytesIO(png)).size == (1080, 720)


def test_the_placeholder_is_a_card_of_its_own():
    assert Image.open(BytesIO(placeholder_card("Комната"))).size == (1080, 720)
