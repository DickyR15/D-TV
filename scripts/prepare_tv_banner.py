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

    # The baked-in slogan is a thin strip directly below the DTV lettering.
    # Rebuild only that strip from the nearby background pixels; do not cover
    # the DTV logo, the red underline, or the woman's portrait with a black bar.
    left = int(width * 0.155)
    right = int(width * 0.49)
    top = int(height * 0.605)
    bottom = int(height * 0.675)

    pixels = image.load()
    repaired = image.copy()
    repaired_pixels = repaired.load()
    sample_top = max(0, top - max(2, int(height * 0.008)))
    sample_bottom = min(height - 1, bottom + max(2, int(height * 0.008)))

    for x in range(left, right + 1):
        upper = pixels[x, sample_top]
        lower = pixels[x, sample_bottom]
        for y in range(top, bottom + 1):
            ratio = (y - sample_top) / (sample_bottom - sample_top)
            ratio = ratio * ratio * (3 - 2 * ratio)
            repaired_pixels[x, y] = tuple(
                round(upper[channel] * (1 - ratio) + lower[channel] * ratio)
                for channel in range(4)
            )

    # Feather the repair edge so it blends into the cinematic dark background.
    mask = Image.new("L", image.size, 0)
    draw = ImageDraw.Draw(mask)
    draw.rectangle((left, top, right, bottom), fill=255)
    mask = mask.filter(ImageFilter.GaussianBlur(max(1, width * 0.0028)))
    cleaned = Image.composite(repaired, image, mask).convert("RGB")

    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    cleaned.save(OUTPUT, "WEBP", quality=90, method=6)
    print(f"Prepared {OUTPUT.relative_to(ROOT)} ({OUTPUT.stat().st_size} bytes)")


if __name__ == "__main__":
    main()
