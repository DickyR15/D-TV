#!/usr/bin/env python3
"""Prepare the Android TV launcher banner with no tagline.

The launcher banner and the Flutter shared brand artwork were originally
exported with a small slogan underneath the D TV logo. For Android TV's
launcher banner we keep the logo and portrait but mask that text strip.
"""
from __future__ import annotations

from pathlib import Path

from PIL import Image, ImageDraw, ImageFilter

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "assets" / "brand" / "dtv-tv-banner.webp"
OUTPUT = (
    ROOT
    / "android"
    / "app"
    / "src"
    / "main"
    / "res"
    / "drawable-nodpi"
    / "dtv_tv_banner.webp"
)


def main() -> None:
    with Image.open(SOURCE) as source:
        image = source.convert("RGBA")

    width, height = image.size
    # Keep the patch confined below the logo and to the left of the portrait.
    # Full opacity over the letters removes the slogan and its underline;
    # a blurred mask softly blends the edges into the existing dark artwork.
    left = int(width * 0.125)
    right = int(width * 0.54)
    top = int(height * 0.585)
    bottom = int(height * 0.735)
    mask = Image.new("L", image.size, 0)
    draw = ImageDraw.Draw(mask)
    draw.rounded_rectangle(
        (left, top, right, bottom),
        radius=max(4, int(width * 0.012)),
        fill=255,
    )
    mask = mask.filter(ImageFilter.GaussianBlur(max(2, int(width * 0.008))))
    patch = Image.new("RGBA", image.size, (7, 3, 8, 255))
    cleaned = Image.composite(patch, image, mask).convert("RGB")

    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    cleaned.save(OUTPUT, "WEBP", quality=90, method=6)
    print(f"Prepared {OUTPUT.relative_to(ROOT)} ({OUTPUT.stat().st_size} bytes)")


if __name__ == "__main__":
    main()
