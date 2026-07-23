from dataclasses import dataclass, field

from PIL import Image, ImageDraw

from src.render.draw import (
    brand_header,
    card_background,
    circle_avatar,
    paste_rounded,
    progress_bar,
    rounded_mask,
    segmented_progress,
    star,
    text_lines,
    to_png,
)
from src.render.theme import (
    AMBER,
    CARD_HEIGHT,
    CARD_WIDTH,
    GREEN,
    GREEN_DEEP,
    INK,
    INK_SOFT,
    PADDING,
    PURPLE,
    RADIUS,
    RED,
    SURFACE_DIM,
    WHITE,
    bold,
    regular,
    semibold,
)

# the header owns the top strip, the payload starts well below it
HEADER_BOTTOM = PADDING + 64
TITLE_TOP = HEADER_BOTTOM + 70


@dataclass(slots=True)
class CardData:
    """Everything a card can draw. Fields the card does not use are simply ignored."""

    title: str
    subtitle: str = ""
    room_name: str = ""
    body: str = ""
    actor_name: str = ""
    actor_photo: Image.Image | None = None
    room_photo: Image.Image | None = None
    photo: Image.Image | None = None
    done: int = 0
    goal: int = 0
    votes_have: int = 0
    votes_need: int = 0
    accent: tuple[int, int, int] = GREEN_DEEP
    footer: str = ""
    badge: str = ""
    quote_author: str = ""
    quote_body: str = ""
    lines: list[tuple[str, int, int]] = field(default_factory=list)


def _finish(canvas: Image.Image, bottom: int) -> bytes:
    """Crop to the content: a fixed height leaves half the card empty on short notifications."""
    height = min(max(bottom + PADDING, 420), CARD_HEIGHT)
    return to_png(canvas.crop((0, 0, CARD_WIDTH, height)))


def _start(canvas: Image.Image, data: CardData) -> ImageDraw.ImageDraw:
    draw = ImageDraw.Draw(canvas)
    brand_header(canvas, draw, PADDING, PADDING, data.room_name, data.room_photo)
    return draw


def _panel(canvas: Image.Image, box: tuple[int, int, int, int]) -> ImageDraw.ImageDraw:
    panel = Image.new("RGB", (box[2] - box[0], box[3] - box[1]), WHITE)
    canvas.paste(panel, (box[0], box[1]), rounded_mask(panel.size, RADIUS))
    return ImageDraw.Draw(canvas)


def _badge(draw: ImageDraw.ImageDraw, y: int, label: str, color) -> int:
    width = int(semibold(28).getlength(label)) + 52
    draw.rounded_rectangle((PADDING, y, PADDING + width, y + 52), 26, fill=color)
    draw.text((PADDING + 26, y + 10), label, font=semibold(28), fill=WHITE)
    return y + 78


def _headline(draw: ImageDraw.ImageDraw, data: CardData, y: int = TITLE_TOP) -> int:
    if data.badge:
        y = _badge(draw, y - 8, data.badge, data.accent)
    for line in text_lines(data.title, bold(56), CARD_WIDTH - PADDING * 2, 2):
        draw.text((PADDING, y), line, font=bold(56), fill=INK)
        y += 68
    if data.subtitle:
        draw.text((PADDING, y + 4), data.subtitle, font=semibold(30), fill=INK_SOFT)
        y += 48
    return y


def _person_row(canvas: Image.Image, draw: ImageDraw.ImageDraw, y: int, data: CardData) -> None:
    """The white plate with the member's avatar, name and one line of context."""
    draw = _panel(canvas, (PADDING, y, CARD_WIDTH - PADDING, y + 148))
    avatar = circle_avatar(data.actor_photo, 88, data.actor_name, data.actor_name)
    canvas.paste(avatar, (PADDING + 32, y + 30), avatar)
    draw.text((PADDING + 148, y + 38), data.actor_name, font=bold(36), fill=INK)
    draw.text((PADDING + 148, y + 86), data.footer, font=semibold(28), fill=data.accent)


def comment_card(data: CardData) -> bytes:
    """A comment landed on your photo: who wrote it, what they said, and the photo itself."""
    canvas = card_background()
    draw = _start(canvas, data)
    y = _headline(draw, data)

    panel_top = y + 30
    inner = PADDING + 36
    body_width = CARD_WIDTH - PADDING * 2 - 72 - (220 if data.photo is not None else 0)
    lines = text_lines(data.body, regular(34), body_width, 3)
    quote_lines = (
        text_lines(data.quote_body, regular(28), body_width - 32, 2) if data.quote_author else []
    )

    quote_height = 40 + len(quote_lines) * 38 + 16 if data.quote_author else 0
    body_top = panel_top + 142 + quote_height
    text_bottom = body_top + len(lines) * 48
    photo_bottom = panel_top + 220 if data.photo is not None else 0
    panel_bottom = max(text_bottom, photo_bottom) + 24

    draw = _panel(canvas, (PADDING, panel_top, CARD_WIDTH - PADDING, panel_bottom))

    avatar = circle_avatar(data.actor_photo, 76, data.actor_name, data.actor_name)
    canvas.paste(avatar, (inner, panel_top + 34), avatar)
    draw.text((inner + 96, panel_top + 52), data.actor_name, font=bold(32), fill=INK)

    if data.photo is not None:
        right = CARD_WIDTH - PADDING - 32
        paste_rounded(canvas, data.photo, (right - 190, panel_top + 30, right, panel_top + 220), 26)

    if data.quote_author:
        # the quoted line wears the same left rail Telegram uses, so a reply reads as a reply
        quote_top = panel_top + 142
        quote_bottom = quote_top + 34 + len(quote_lines) * 38
        draw.rounded_rectangle((inner, quote_top, inner + 6, quote_bottom), 3, fill=GREEN)
        draw.text(
            (inner + 22, quote_top - 2), data.quote_author, font=semibold(26), fill=GREEN_DEEP
        )
        line_top = quote_top + 36
        for line in quote_lines:
            draw.text((inner + 22, line_top), line, font=regular(28), fill=INK_SOFT)
            line_top += 38

    for line in lines:
        draw.text((inner, body_top), line, font=regular(34), fill=INK_SOFT)
        body_top += 48
    return _finish(canvas, panel_bottom)


def vote_card(data: CardData) -> bytes:
    """Someone logged a workout and needs votes: show the proof and the quorum so far."""
    canvas = card_background()
    draw = _start(canvas, data)
    y = _headline(draw, data)

    top = y + 34
    if data.photo is not None:
        paste_rounded(canvas, data.photo, (PADDING, top, PADDING + 260, top + 260), RADIUS)
        left = PADDING + 300
    else:
        left = PADDING

    avatar = circle_avatar(data.actor_photo, 64, data.actor_name, data.actor_name)
    canvas.paste(avatar, (left, top), avatar)
    draw.text((left + 82, top + 16), data.actor_name, font=bold(34), fill=INK)

    bar_top = top + 150
    draw.text((left, bar_top - 46), data.footer, font=semibold(28), fill=INK_SOFT)
    progress_bar(
        draw,
        (left, bar_top, CARD_WIDTH - PADDING, bar_top + 24),
        data.votes_have / max(data.votes_need, 1),
        fill=data.accent,
    )
    draw.text(
        (left, bar_top + 42), f"{data.votes_have} / {data.votes_need}", font=bold(38), fill=INK
    )
    return _finish(canvas, max(bar_top + 90, top + 260 if data.photo is not None else 0))


def verdict_card(data: CardData) -> bytes:
    """Approved or rejected, with the period progress it moved."""
    canvas = card_background()
    draw = _start(canvas, data)
    y = _headline(draw, data)

    if not data.goal:
        return _finish(canvas, y)

    bar_top = y + 96
    draw.text((PADDING, bar_top - 60), f"{data.done} / {data.goal}", font=bold(44), fill=INK)
    segmented_progress(
        draw, (PADDING, bar_top, CARD_WIDTH - PADDING, bar_top + 32), data.done, data.goal
    )
    bottom = bar_top + 32
    if data.footer:
        draw.text((PADDING, bar_top + 52), data.footer, font=semibold(28), fill=INK_SOFT)
        bottom = bar_top + 96
    return _finish(canvas, bottom)


def progress_card(data: CardData) -> bytes:
    """One row per room: what is done, what is left, worst first."""
    canvas = card_background()
    draw = _start(canvas, data)
    y = _headline(draw, data)

    row_top = y + 36
    for name, done, goal in data.lines[:3]:
        draw.text((PADDING, row_top), name, font=semibold(32), fill=INK)
        draw.text(
            (CARD_WIDTH - PADDING, row_top),
            f"{done}/{goal}",
            font=bold(32),
            fill=AMBER if done < goal else GREEN_DEEP,
            anchor="ra",
        )
        segmented_progress(
            draw,
            (CARD_WIDTH - PADDING - 380, row_top + 46, CARD_WIDTH - PADDING, row_top + 72),
            done,
            goal,
        )
        row_top += 106

    bottom = row_top
    if data.footer:
        draw.text((PADDING, row_top + 6), data.footer, font=semibold(28), fill=INK_SOFT)
        bottom = row_top + 50
    return _finish(canvas, bottom)


def person_card(data: CardData) -> bytes:
    """A card about somebody: joins, leaves, freezes."""
    canvas = card_background()
    draw = _start(canvas, data)
    y = _headline(draw, data)
    _person_row(canvas, draw, y + 40, data)
    return _finish(canvas, y + 188)


def achievement_card(data: CardData) -> bytes:
    """A medal moment: the badge, its name, and what earned it."""
    canvas = card_background()
    draw = _start(canvas, data)

    center_x = CARD_WIDTH // 2
    medal_y = TITLE_TOP + 110
    draw.ellipse((center_x - 92, medal_y - 92, center_x + 92, medal_y + 92), fill=data.accent)
    draw.ellipse((center_x - 64, medal_y - 64, center_x + 64, medal_y + 64), fill=WHITE)
    star(draw, center_x, medal_y, 46, data.accent)

    y = medal_y + 132
    for line in text_lines(data.title, bold(54), CARD_WIDTH - PADDING * 2, 2):
        draw.text((center_x, y), line, font=bold(54), fill=INK, anchor="ma")
        y += 66
    if data.subtitle:
        draw.text((center_x, y + 6), data.subtitle, font=semibold(30), fill=INK_SOFT, anchor="ma")
        y += 50
    return _finish(canvas, y)


def digest_card(data: CardData) -> bytes:
    """What piled up while the bot was away, folded into one card instead of a burst."""
    canvas = card_background()
    draw = _start(canvas, data)
    y = _headline(draw, data)

    row_top = y + 34
    for label, count, _ in data.lines[:5]:
        draw.ellipse((PADDING, row_top + 14, PADDING + 16, row_top + 30), fill=GREEN)
        draw.text((PADDING + 38, row_top), label, font=semibold(32), fill=INK)
        if count:
            draw.text(
                (CARD_WIDTH - PADDING, row_top),
                str(count),
                font=bold(32),
                fill=INK_SOFT,
                anchor="ra",
            )
        row_top += 64
    return _finish(canvas, row_top)


def placeholder_card(room_name: str = "") -> bytes:
    """Sent within the first second so the message exists; the real card edits over it."""
    canvas = card_background()
    draw = ImageDraw.Draw(canvas)
    brand_header(canvas, draw, PADDING, PADDING, room_name)
    draw.rounded_rectangle(
        (PADDING, TITLE_TOP, CARD_WIDTH - PADDING, TITLE_TOP + 74), 37, fill=SURFACE_DIM
    )
    draw.rounded_rectangle(
        (PADDING, TITLE_TOP + 110, CARD_WIDTH - 320, TITLE_TOP + 168), 29, fill=SURFACE_DIM
    )
    draw.rounded_rectangle(
        (PADDING, TITLE_TOP + 200, CARD_WIDTH - 520, TITLE_TOP + 248), 24, fill=SURFACE_DIM
    )
    return _finish(canvas, TITLE_TOP + 248)


RENDERERS = {
    "comment": comment_card,
    "reply": comment_card,
    "vote_request": vote_card,
    "vote_last_call": vote_card,
    "approved": verdict_card,
    "rejected": verdict_card,
    "buddy_credited": verdict_card,
    "member_joined": person_card,
    "member_left": person_card,
    "freeze_scheduled": person_card,
    "freeze_canceled": person_card,
    "achievement": achievement_card,
    "reminder": progress_card,
    "streak_at_risk": progress_card,
    "period_summary": progress_card,
    "digest": digest_card,
    "welcome": digest_card,
}


def render(kind: str, data: CardData) -> bytes:
    renderer = RENDERERS.get(kind)
    if renderer is None:
        raise ValueError(f"no renderer for {kind}")
    return renderer(data)


__all__ = ["CardData", "render", "placeholder_card", "RED", "PURPLE", "RADIUS"]
