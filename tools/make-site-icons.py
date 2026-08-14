"""Derive the site's favicon, touch icon and Open Graph card from the app icon.

Run from the repo root:  python tools/make-site-icons.py

build/appicon.png is the single source of truth for the mark - the same art as
site/logo.svg and frontend/src/lib/Logo.svelte. Pillow is the only dependency.
"""

from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

ROOT = Path(__file__).resolve().parent.parent
SITE = ROOT / "site"
FONTS = Path("C:/Windows/Fonts")

# Straight from site/index.html's dark theme.
GROUND = "#0b0f16"
LINE = "#26303f"
TEXT = "#e6edf3"
MUTED = "#8b97a7"
FAINT = "#5c6675"
ACCENT = "#5b8cff"
# The two stops of the logo gradient, used to fill the squircle's corners.
GRAD_TOP = (127, 178, 255)
GRAD_BOTTOM = (63, 95, 214)


def mark(size, opaque=False):
    """The app icon at `size` px. `opaque` extends the logo gradient into the
    transparent corners, which is what iOS wants - it applies its own mask."""
    src = Image.open(ROOT / "build" / "appicon.png").convert("RGBA")
    src = src.resize((size, size), Image.LANCZOS)
    if not opaque:
        return src
    bg = Image.new("RGB", (1, size))
    for y in range(size):
        t = y / max(1, size - 1)
        bg.putpixel((0, y), tuple(round(a + (b - a) * t) for a, b in zip(GRAD_TOP, GRAD_BOTTOM)))
    bg = bg.resize((size, size))
    bg.paste(src, (0, 0), src)
    return bg


def font(name, size):
    return ImageFont.truetype(str(FONTS / name), size)


def make_og():
    img = Image.new("RGB", (1200, 630), GROUND)
    d = ImageDraw.Draw(img)
    d.rectangle([36, 36, 1163, 593], outline=LINE, width=2)

    logo = mark(112)
    img.paste(logo, (96, 84), logo)

    d.text((96, 320), "fleet", font=font("CascadiaCode-Bold.ttf", 88), fill=TEXT, anchor="ls")
    d.rectangle([96, 352, 192, 357], fill=ACCENT)
    d.text(
        (96, 428),
        "Your repos at a glance.",
        font=font("seguisb.ttf", 46),
        fill=TEXT,
        anchor="ls",
    )
    d.text(
        (96, 478),
        "A desktop dashboard for every git repo under your project roots.",
        font=font("segoeui.ttf", 30),
        fill=MUTED,
        anchor="ls",
    )
    d.text((96, 556), "Free for noncommercial use.", font=font("segoeui.ttf", 26), fill=FAINT, anchor="ls")
    d.text(
        (1104, 556),
        "hoijunkim.github.io/fleet",
        font=font("CascadiaCode-Regular.ttf", 24),
        fill=FAINT,
        anchor="rs",
    )
    return img


def main():
    mark(64).save(SITE / "favicon.ico", sizes=[(16, 16), (32, 32), (48, 48)])
    mark(180, opaque=True).save(SITE / "apple-touch-icon.png")
    make_og().save(SITE / "og.png", optimize=True)
    print("wrote site/favicon.ico, site/apple-touch-icon.png, site/og.png")


if __name__ == "__main__":
    main()
