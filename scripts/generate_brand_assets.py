#!/usr/bin/env python3
"""將已確認的 HGTV 圖示套用到 Android 啟動資源。

Android TV 橫幅改由 res/drawable/tv_banner.xml 提供，避免重新生成另一套
與 HGTV 品牌不一致的圖示或 PNG 橫幅。
"""
import argparse
from pathlib import Path

from PIL import Image

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description="套用既有 HGTV Android 圖示。")
parser.add_argument("--output", type=Path, default=root)
args = parser.parse_args()

source = root / "assets/brand/hgtv-icon.png"
if not source.is_file():
    raise SystemExit(f"找不到既有 HGTV 圖示：{source}")

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
print(f"已套用既有 HGTV 圖示：{destination}")
