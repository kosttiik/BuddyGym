from functools import lru_cache
from io import BytesIO

from PIL import Image, ImageDraw, ImageFilter, ImageFont

from src.render.theme import (
    CARD_HEIGHT,
    CARD_WIDTH,
    FONT_DIR,
    GREEN,
    GREEN_DEEP,
    INK,
    SURFACE,
    SURFACE_DIM,
    WHITE,
)
from src.render.theme import font as _font


def vertical_gradient(
    size: tuple[int, int], top: tuple[int, int, int], bottom: tuple[int, int, int]
) -> Image.Image:
    """One-pixel-wide gradient stretched to size: cheaper than painting every row."""
    width, height = size
    strip = Image.new("RGB", (1, height))
    pixels = strip.load()
    for y in range(height):
        t = y / max(height - 1, 1)
        pixels[0, y] = tuple(round(a + (b - a) * t) for a, b in zip(top, bottom, strict=True))
    return strip.resize((width, height), Image.Resampling.BILINEAR)


def card_background() -> Image.Image:
    """The app's own backdrop: a pale surface with a green glow in the top corner."""
    base = vertical_gradient((CARD_WIDTH, CARD_HEIGHT), SURFACE, SURFACE_DIM)
    glow = Image.new("RGB", (CARD_WIDTH, CARD_HEIGHT), SURFACE)
    mask = Image.new("L", (CARD_WIDTH, CARD_HEIGHT), 0)
    ImageDraw.Draw(mask).ellipse((-260, -420, 640, 300), fill=70)
    mask = mask.filter(ImageFilter.GaussianBlur(120))
    glow.paste(Image.new("RGB", (CARD_WIDTH, CARD_HEIGHT), GREEN), (0, 0), mask)
    return Image.blend(base, glow, 0.35)


def rounded_mask(size: tuple[int, int], radius: int) -> Image.Image:
    mask = Image.new("L", size, 0)
    ImageDraw.Draw(mask).rounded_rectangle((0, 0, size[0] - 1, size[1] - 1), radius, fill=255)
    return mask


def paste_rounded(
    canvas: Image.Image, photo: Image.Image, box: tuple[int, int, int, int], radius: int
) -> None:
    """Cover-fit a photo into box and paste it with rounded corners."""
    left, top, right, bottom = box
    width, height = right - left, bottom - top
    scale = max(width / photo.width, height / photo.height)
    resized = photo.resize(
        (max(round(photo.width * scale), width), max(round(photo.height * scale), height)),
        Image.Resampling.LANCZOS,
    )
    offset_x = (resized.width - width) // 2
    offset_y = (resized.height - height) // 2
    cropped = resized.crop((offset_x, offset_y, offset_x + width, offset_y + height))
    canvas.paste(cropped.convert("RGB"), (left, top), rounded_mask((width, height), radius))


def circle_avatar(photo: Image.Image | None, size: int, seed: str, initial: str) -> Image.Image:
    """The member's picture as a circle, or the same lettered fallback the mini app draws."""
    if photo is not None:
        square = min(photo.width, photo.height)
        left = (photo.width - square) // 2
        top = (photo.height - square) // 2
        cropped = photo.crop((left, top, left + square, top + square)).resize(
            (size, size), Image.Resampling.LANCZOS
        )
        out = Image.new("RGBA", (size, size), (0, 0, 0, 0))
        out.paste(cropped.convert("RGB"), (0, 0), rounded_mask((size, size), size // 2))
        return out

    palette = [(228, 106, 118), (86, 132, 235), (166, 176, 184), (238, 155, 84), (124, 92, 214)]
    color = palette[sum(map(ord, seed or "?")) % len(palette)]
    out = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(out)
    draw.ellipse((0, 0, size - 1, size - 1), fill=color)
    draw.text(
        (size / 2, size / 2),
        (initial or "?")[:1].upper(),
        font=_font("Bold", round(size * 0.44)),
        fill=WHITE,
        anchor="mm",
    )
    return out


def progress_bar(
    draw: ImageDraw.ImageDraw,
    box: tuple[int, int, int, int],
    ratio: float,
    fill: tuple[int, int, int] = GREEN,
    track: tuple[int, int, int] = (215, 226, 219),
) -> None:
    left, top, right, bottom = box
    height = bottom - top
    draw.rounded_rectangle(box, height // 2, fill=track)
    filled = left + round((right - left) * max(0.0, min(ratio, 1.0)))
    if filled > left + height // 2:
        draw.rounded_rectangle((left, top, filled, bottom), height // 2, fill=fill)


def segmented_progress(
    draw: ImageDraw.ImageDraw,
    box: tuple[int, int, int, int],
    done: int,
    goal: int,
    gap: int = 10,
) -> None:
    """One segment per workout, the way the room card shows a small goal."""
    left, top, right, bottom = box
    goal = max(goal, 1)
    if goal > 12:
        progress_bar(draw, box, done / goal)
        return
    width = (right - left - gap * (goal - 1)) / goal
    height = bottom - top
    for i in range(goal):
        x0 = left + i * (width + gap)
        color = GREEN if i < done else (215, 226, 219)
        draw.rounded_rectangle((x0, top, x0 + width, bottom), height // 2, fill=color)


def text_lines(text: str, fnt: ImageFont.FreeTypeFont, max_width: int, max_lines: int) -> list[str]:
    """Greedy wrap, ellipsing the last line instead of spilling out of the card."""
    words = text.split()
    lines: list[str] = []
    current = ""
    for word in words:
        candidate = f"{current} {word}".strip()
        if fnt.getlength(candidate) <= max_width or not current:
            current = candidate
            continue
        lines.append(current)
        current = word
        if len(lines) == max_lines:
            break
    if current and len(lines) < max_lines:
        lines.append(current)
    if not lines:
        return []
    while len(lines) == max_lines and fnt.getlength(lines[-1]) > max_width and len(lines[-1]) > 1:
        lines[-1] = lines[-1][:-1]
    if len(words) > sum(len(line.split()) for line in lines):
        lines[-1] = lines[-1].rstrip(" .,") + "…"
    return lines


def logo(draw: ImageDraw.ImageDraw, x: int, y: int, size: int = 56) -> None:
    """The app's own dumbbell mark, traced from IconDumbbell on a 24x24 grid."""
    draw.rounded_rectangle((x, y, x + size, y + size), size * 0.3, fill=GREEN_DEEP)

    # the icon is inset inside the tile the way the app draws it
    scale = size * 0.62 / 24
    ox = x + size / 2 - 12 * scale
    oy = y + size / 2 - 12 * scale
    bars = [
        (1.5, 9, 3.2, 6, 1.3),
        (5.3, 7, 3.2, 10, 1.3),
        (15.5, 7, 3.2, 10, 1.3),
        (19.3, 9, 3.2, 6, 1.3),
        (9, 10.6, 6, 2.8, 1.2),
    ]
    for bx, by, bw, bh, radius in bars:
        draw.rounded_rectangle(
            (ox + bx * scale, oy + by * scale, ox + (bx + bw) * scale, oy + (by + bh) * scale),
            radius * scale,
            fill=WHITE,
        )


EMOJI_STRIKE = 109


@lru_cache(maxsize=64)
def emoji_image(char: str, size: int) -> Image.Image | None:
    """Color emoji as an image.

    The bundled text font has no emoji glyphs, and NotoColorEmoji is a bitmap font with one
    fixed strike, so it is drawn at that size and scaled down.
    """
    path = FONT_DIR / "NotoColorEmoji.ttf"
    if not path.exists():
        return None
    try:
        font = ImageFont.truetype(str(path), EMOJI_STRIKE)
    except OSError:
        return None
    canvas = Image.new("RGBA", (EMOJI_STRIKE + 40, EMOJI_STRIKE + 40), (0, 0, 0, 0))
    ImageDraw.Draw(canvas).text((10, 10), char, font=font, embedded_color=True)
    box = canvas.getbbox()
    if box is None:
        return None
    return canvas.crop(box).resize((size, size), Image.Resampling.LANCZOS)


def draw_text_with_emoji(
    canvas: Image.Image,
    draw: ImageDraw.ImageDraw,
    xy: tuple[int, int],
    text: str,
    font: ImageFont.FreeTypeFont,
    fill,
) -> int:
    """Draws a line where a leading emoji is painted as an image. Returns the end x."""
    x, y = xy
    parts = text.split(" ", 1)
    lead = parts[0]
    if lead and max(map(ord, lead)) > 0x2000:
        size = round(font.size * 1.1)
        image = emoji_image(lead, size)
        if image is not None:
            canvas.paste(image, (x, y - 2), image)
            x += size + 10
            text = parts[1] if len(parts) > 1 else ""
        else:
            text = parts[1] if len(parts) > 1 else ""
    if text:
        draw.text((x, y), text, font=font, fill=fill)
        x += round(font.getlength(text))
    return x


def star(draw: ImageDraw.ImageDraw, cx: int, cy: int, radius: int, fill) -> None:
    """Drawn, not typed: the bundled font carries no star glyph."""
    import math

    points = []
    for i in range(10):
        angle = math.pi / 2 + i * math.pi / 5
        r = radius if i % 2 == 0 else radius * 0.44
        points.append((cx + r * math.cos(angle), cy - r * math.sin(angle)))
    draw.polygon(points, fill=fill)


def square_avatar(photo: Image.Image | None, size: int, seed: str, initial: str) -> Image.Image:
    """A room picture as the rounded square the app uses, or its lettered fallback."""
    out = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    radius = round(size * 0.32)
    if photo is not None:
        square = min(photo.width, photo.height)
        left = (photo.width - square) // 2
        top = (photo.height - square) // 2
        cropped = photo.crop((left, top, left + square, top + square)).resize(
            (size, size), Image.Resampling.LANCZOS
        )
        out.paste(cropped.convert("RGB"), (0, 0), rounded_mask((size, size), radius))
        return out

    palette = [(228, 106, 118), (86, 132, 235), (166, 176, 184), (238, 155, 84), (124, 92, 214)]
    color = palette[sum(map(ord, seed or "?")) % len(palette)]
    draw = ImageDraw.Draw(out)
    draw.rounded_rectangle((0, 0, size - 1, size - 1), radius, fill=color)
    draw.text(
        (size / 2, size / 2),
        (initial or "?")[:1].upper(),
        font=_font("Bold", round(size * 0.44)),
        fill=WHITE,
        anchor="mm",
    )
    return out


def brand_header(
    canvas: Image.Image,
    draw: ImageDraw.ImageDraw,
    x: int,
    y: int,
    room_name: str,
    room_photo: Image.Image | None = None,
) -> None:
    """The room wears its own picture; the brand mark sits opposite so both stay readable."""
    from src.render.theme import CARD_WIDTH, INK_SOFT, bold, semibold

    if room_name:
        avatar = square_avatar(room_photo, 64, room_name, room_name)
        canvas.paste(avatar, (x, y), avatar)
        draw.text((x + 84, y + 6), room_name, font=bold(32), fill=INK)
        draw.text((x + 84, y + 44), "BuddyGym", font=semibold(22), fill=INK_SOFT)
    else:
        logo(draw, x, y, 64)
        draw.text((x + 84, y + 16), "BuddyGym", font=bold(32), fill=INK)
        return

    logo(draw, CARD_WIDTH - x - 44, y + 10, 44)


def to_png(image: Image.Image) -> bytes:
    buffer = BytesIO()
    image.convert("RGB").save(buffer, format="PNG", optimize=True)
    return buffer.getvalue()
