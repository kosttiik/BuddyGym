from dataclasses import dataclass, field
from datetime import datetime

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


@dataclass(slots=True)
class CardData:
    """Everything a card can draw. Fields the card does not use are simply ignored."""

    title: str
    subtitle: str = ""
    room_name: str = ""
    body: str = ""
    actor_name: str = ""
    actor_photo: Image.Image | None = None
    photo: Image.Image | None = None
    done: int = 0
    goal: int = 0
    votes_have: int = 0
    votes_need: int = 0
    accent: tuple[int, int, int] = GREEN_DEEP
    footer: str = ""
    lines: list[tuple[str, int, int]] = field(default_factory=list)
    deadline: datetime | None = None


def _panel(canvas: Image.Image, box: tuple[int, int, int, int]) -> ImageDraw.ImageDraw:
    panel = Image.new("RGB", (box[2] - box[0], box[3] - box[1]), WHITE)
    canvas.paste(panel, (box[0], box[1]), rounded_mask(panel.size, RADIUS))
    return ImageDraw.Draw(canvas)


def _headline(draw: ImageDraw.ImageDraw, data: CardData, y: int = 150) -> int:
    for line in text_lines(data.title, bold(58), CARD_WIDTH - PADDING * 2, 2):
        draw.text((PADDING, y), line, font=bold(58), fill=INK)
        y += 70
    if data.subtitle:
        draw.text((PADDING, y + 4), data.subtitle, font=semibold(30), fill=INK_SOFT)
        y += 48
    return y


def comment_card(data: CardData) -> bytes:
    """A comment landed on your photo: who wrote it, what they said, and the photo itself."""
    canvas = card_background()
    draw = ImageDraw.Draw(canvas)
    brand_header(draw, PADDING, PADDING, "BuddyGym", data.room_name)
    y = _headline(draw, data)

    panel_top = y + 24
    panel_box = (PADDING, panel_top, CARD_WIDTH - PADDING, CARD_HEIGHT - PADDING)
    draw = _panel(canvas, panel_box)

    inner = PADDING + 36
    if data.actor_photo is not None or data.actor_name:
        avatar = circle_avatar(data.actor_photo, 84, data.actor_name, data.actor_name)
        canvas.paste(avatar, (inner, panel_top + 36), avatar)
    draw.text((inner + 104, panel_top + 58), data.actor_name, font=bold(34), fill=INK)

    if data.photo is not None:
        right = CARD_WIDTH - PADDING - 36
        paste_rounded(canvas, data.photo, (right - 240, panel_top + 36, right, panel_top + 276), 28)

    # the body starts below the avatar row, never beside it: a long name would collide
    body_top = panel_top + 156
    body_width = CARD_WIDTH - PADDING * 2 - 72 - (280 if data.photo is not None else 0)
    for line in text_lines(data.body, regular(34), body_width, 3):
        draw.text((inner, body_top), line, font=regular(34), fill=INK_SOFT)
        body_top += 48

    return to_png(canvas)


def vote_card(data: CardData) -> bytes:
    """Someone logged a workout and needs votes: show the proof and the quorum so far."""
    canvas = card_background()
    draw = ImageDraw.Draw(canvas)
    brand_header(draw, PADDING, PADDING, "BuddyGym", data.room_name)
    y = _headline(draw, data)

    if data.photo is not None:
        paste_rounded(canvas, data.photo, (PADDING, y + 28, PADDING + 300, y + 328), RADIUS)
        text_left = PADDING + 340
    else:
        text_left = PADDING

    bar_top = y + 160
    draw.text((text_left, bar_top - 54), data.footer, font=semibold(30), fill=INK_SOFT)
    progress_bar(
        draw,
        (text_left, bar_top, CARD_WIDTH - PADDING, bar_top + 26),
        data.votes_have / max(data.votes_need, 1),
        fill=data.accent,
    )
    draw.text(
        (text_left, bar_top + 44),
        f"{data.votes_have} / {data.votes_need}",
        font=bold(40),
        fill=INK,
    )
    return to_png(canvas)


def verdict_card(data: CardData) -> bytes:
    """Approved or rejected, with the period progress it moved."""
    canvas = card_background()
    draw = ImageDraw.Draw(canvas)
    brand_header(draw, PADDING, PADDING, "BuddyGym", data.room_name)

    badge_top = 150
    label = data.subtitle or ""
    badge_width = int(semibold(30).getlength(label)) + 56
    draw.rounded_rectangle(
        (PADDING, badge_top, PADDING + badge_width, badge_top + 56), 28, fill=data.accent
    )
    draw.text((PADDING + 28, badge_top + 12), label, font=semibold(30), fill=WHITE)

    y = badge_top + 90
    for line in text_lines(data.title, bold(56), CARD_WIDTH - PADDING * 2, 2):
        draw.text((PADDING, y), line, font=bold(56), fill=INK)
        y += 68

    if data.goal:
        bar_top = CARD_HEIGHT - PADDING - 120
        draw.text(
            (PADDING, bar_top - 52),
            f"{data.done} / {data.goal}",
            font=bold(42),
            fill=INK,
        )
        segmented_progress(
            draw, (PADDING, bar_top, CARD_WIDTH - PADDING, bar_top + 34), data.done, data.goal
        )
        if data.footer:
            draw.text((PADDING, bar_top + 54), data.footer, font=semibold(28), fill=INK_SOFT)
    return to_png(canvas)


def reminder_card(data: CardData) -> bytes:
    """The period is closing and the goal is not met: one row per room, worst first."""
    canvas = card_background()
    draw = ImageDraw.Draw(canvas)
    brand_header(draw, PADDING, PADDING, "BuddyGym", data.subtitle)
    y = _headline(draw, data)

    row_top = y + 30
    for name, done, goal in data.lines[:4]:
        draw.text((PADDING, row_top), name, font=semibold(32), fill=INK)
        left = CARD_WIDTH - PADDING - 360
        draw.text(
            (CARD_WIDTH - PADDING, row_top),
            f"{done}/{goal}",
            font=bold(32),
            fill=AMBER if done < goal else GREEN_DEEP,
            anchor="ra",
        )
        segmented_progress(
            draw, (left, row_top + 48, CARD_WIDTH - PADDING, row_top + 74), done, goal
        )
        row_top += 108

    if data.footer:
        draw.text(
            (PADDING, CARD_HEIGHT - PADDING - 32), data.footer, font=semibold(28), fill=INK_SOFT
        )
    return to_png(canvas)


def freeze_card(data: CardData) -> bytes:
    canvas = card_background()
    draw = ImageDraw.Draw(canvas)
    brand_header(draw, PADDING, PADDING, "BuddyGym", data.room_name)
    _headline(draw, data)

    box_top = 300
    draw.rounded_rectangle(
        (PADDING, box_top, CARD_WIDTH - PADDING, box_top + 140), RADIUS, fill=WHITE
    )
    if data.actor_photo is not None or data.actor_name:
        avatar = circle_avatar(data.actor_photo, 88, data.actor_name, data.actor_name)
        canvas.paste(avatar, (PADDING + 32, box_top + 26), avatar)
    draw.text((PADDING + 148, box_top + 34), data.actor_name, font=bold(36), fill=INK)
    draw.text((PADDING + 148, box_top + 82), data.footer, font=semibold(28), fill=PURPLE)
    return to_png(canvas)


def achievement_card(data: CardData) -> bytes:
    """A medal moment: the badge, its name, and what earned it."""
    canvas = card_background()
    draw = ImageDraw.Draw(canvas)
    brand_header(draw, PADDING, PADDING, "BuddyGym", data.room_name)

    center_x = CARD_WIDTH // 2
    medal_y = 250
    draw.ellipse((center_x - 90, medal_y - 90, center_x + 90, medal_y + 90), fill=data.accent)
    draw.ellipse((center_x - 62, medal_y - 62, center_x + 62, medal_y + 62), fill=WHITE)
    star(draw, center_x, medal_y, 46, data.accent)

    y = medal_y + 130
    for line in text_lines(data.title, bold(56), CARD_WIDTH - PADDING * 2, 2):
        draw.text((center_x, y), line, font=bold(56), fill=INK, anchor="ma")
        y += 68
    if data.subtitle:
        draw.text((center_x, y + 8), data.subtitle, font=semibold(30), fill=INK_SOFT, anchor="ma")
    return to_png(canvas)


def digest_card(data: CardData) -> bytes:
    """What piled up while the bot was away, folded into one card instead of a burst."""
    canvas = card_background()
    draw = ImageDraw.Draw(canvas)
    brand_header(draw, PADDING, PADDING, "BuddyGym", data.subtitle)
    y = _headline(draw, data)

    row_top = y + 24
    for label, count, _ in data.lines[:5]:
        draw.ellipse((PADDING, row_top + 12, PADDING + 16, row_top + 28), fill=GREEN)
        draw.text((PADDING + 36, row_top), label, font=semibold(32), fill=INK)
        draw.text(
            (CARD_WIDTH - PADDING, row_top), str(count), font=bold(32), fill=INK_SOFT, anchor="ra"
        )
        row_top += 62
    return to_png(canvas)


def placeholder_card(room_name: str = "") -> bytes:
    """Sent within the first second so the message exists; the real card edits over it."""
    canvas = card_background()
    draw = ImageDraw.Draw(canvas)
    brand_header(draw, PADDING, PADDING, "BuddyGym", room_name)
    draw.rounded_rectangle((PADDING, 220, CARD_WIDTH - PADDING, 300), 40, fill=SURFACE_DIM)
    draw.rounded_rectangle((PADDING, 340, CARD_WIDTH - 320, 400), 30, fill=SURFACE_DIM)
    draw.rounded_rectangle((PADDING, 440, CARD_WIDTH - 520, 490), 25, fill=SURFACE_DIM)
    return to_png(canvas)


RENDERERS = {
    "comment": comment_card,
    "vote_request": vote_card,
    "approved": verdict_card,
    "rejected": verdict_card,
    "buddy_credited": verdict_card,
    "member_joined": freeze_card,
    "freeze_scheduled": freeze_card,
    "achievement": achievement_card,
    "reminder": reminder_card,
    "digest": digest_card,
}


def render(kind: str, data: CardData) -> bytes:
    renderer = RENDERERS.get(kind)
    if renderer is None:
        raise ValueError(f"no renderer for {kind}")
    return renderer(data)


__all__ = ["CardData", "render", "placeholder_card", "RED", "RADIUS"]
