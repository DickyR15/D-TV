#!/usr/bin/env python3
"""Generate Android launcher assets from the approved D TV icon.

The square launcher icon is reused as an input only for icon resources; Android TV
uses the dedicated wide D TV banner at android/app/src/main/res/drawable/tv_banner.png.
"""
import argparse
from pathlib import Path

from PIL import Image

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description="套用 D TV Android 圖示。")
parser.add_argument("--output", type=Path, default=root)
args = parser.parse_args()

source = root / "assets/brand/dtv-icon.png"
if not source.is_file():
    raise SystemExit(f"找不到 D TV 圖示：{source}")

with Image.open(source) as image:
    icon = image.convert("RGBA")
    width, height = icon.size
    side = min(width, height)
    left = (width - side) // 2
    top = (height - side) // 2
    icon = icon.crop((left, top, left + side, top + side))
    icon = icon.resize((192, 192), Image.Resampling.LANCZOS)

destination = args.output / "android/app/src/main/res/drawable/app_icon.png"
destination.parent.mkdir(parents=True, exist_ok=True)
icon.save(destination, format="PNG", optimize=True)
print(f"已套用 D TV 圖示：{destination}")
