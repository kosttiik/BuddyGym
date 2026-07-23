from functools import lru_cache
from pathlib import Path

from PIL import ImageFont

FONT_DIR = Path(__file__).resolve().parents[2] / "assets" / "fonts"

CARD_WIDTH = 1080
CARD_HEIGHT = 720
PADDING = 64
RADIUS = 44

# mirrors the mini app tokens: the same green gradient, the same near-black ink
GREEN = (34, 200, 119)
GREEN_DEEP = (14, 165, 92)
GREEN_DARK = (12, 82, 50)
INK = (11, 26, 18)
INK_SOFT = (86, 106, 95)
SURFACE = (244, 248, 245)
SURFACE_DIM = (228, 236, 230)
WHITE = (255, 255, 255)
RED = (207, 62, 54)
RED_TINT = (250, 232, 231)
AMBER = (214, 150, 26)
PURPLE = (124, 92, 214)


@lru_cache(maxsize=32)
def font(weight: str, size: int) -> ImageFont.FreeTypeFont:
    return ImageFont.truetype(str(FONT_DIR / f"Onest-{weight}.ttf"), size)


def bold(size: int) -> ImageFont.FreeTypeFont:
    return font("Bold", size)


def semibold(size: int) -> ImageFont.FreeTypeFont:
    return font("SemiBold", size)


def regular(size: int) -> ImageFont.FreeTypeFont:
    return font("Regular", size)
