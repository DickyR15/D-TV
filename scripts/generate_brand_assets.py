#!/usr/bin/env python3
"""Validate D TV's vector-based Android branding resources.

The app icon and TV banner are Android VectorDrawable resources. This avoids
regenerating or accidentally restoring the previous HGTV bitmap artwork.
"""
from pathlib import Path

root = Path(__file__).resolve().parents[1]
required = [
    root / "android/app/src/main/res/drawable/dtv_icon.xml",
    root / "android/app/src/main/res/drawable/tv_banner.xml",
]
missing = [path for path in required if not path.is_file()]
if missing:
    raise SystemExit("找不到 D TV 品牌資源：" + ", ".join(str(path) for path in missing))
print("D TV Android 圖示與 TV 橫幅資源檢查完成。")
