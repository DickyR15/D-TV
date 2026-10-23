#!/usr/bin/env python3
"""Prepare the Android TV launcher banner without Chinese tagline text.

The artwork source already contains only the English D TV wordmark and the
cinematic background. Do not mask or paint over the logo while preparing the
320:180 launcher banner asset.
"""
from __future__ import annotations

from pathlib import Path
from PIL import Image

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
        image = source.convert("RGB")

    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    image.save(OUTPUT, "WEBP", quality=88, method=6)
    print(
        f"Prepared {OUTPUT.relative_to(ROOT)} "
        f"({image.width}x{image.height}, {OUTPUT.stat().st_size} bytes)"
    )


if __name__ == "__main__":
    main()
