#!/usr/bin/env python3
"""Prepare the Android TV launcher banner without a tagline.

The source WebP is stored as base64 text so GitHub's text-only file API can
keep the image reproducible. During CI, this script writes the actual WebP
to both the shared brand asset and Android's drawable-nodpi launcher banner.
"""
from __future__ import annotations

import base64
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "tools" / "assets" / "dtv-tv-banner-no-tagline.webp.b64"
OUTPUTS = (
    ROOT / "assets" / "brand" / "dtv-tv-banner.webp",
    ROOT / "android" / "app" / "src" / "main" / "res" / "drawable-nodpi" / "dtv_tv_banner.webp",
)


def main() -> None:
    encoded = SOURCE.read_text(encoding="ascii").strip()
    image = base64.b64decode(encoded, validate=True)
    if not image.startswith(b"RIFF") or image[8:12] != b"WEBP":
        raise SystemExit("Launcher banner source is not a valid WebP image.")
    for output in OUTPUTS:
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_bytes(image)
        print(f"Prepared {output.relative_to(ROOT)} ({len(image)} bytes)")


if __name__ == "__main__":
    main()
