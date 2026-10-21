#!/usr/bin/env python3
"""One-shot migration helper: turns Xanerix/guoapp into HGTV."""
from pathlib import Path
import re
import shutil
from PIL import Image, ImageDraw, ImageFilter, ImageFont
from opencc import OpenCC

ROOT = Path(__file__).resolve().parents[1]
cc = OpenCC("s2twp")

# Convert app-facing source and documentation to Taiwan Traditional Chinese.
# Exclude lockfiles and native-provider protocol files to avoid changing
# checksums, dependency resolution, signatures, and upstream request constants.
CONVERT_SUFFIXES = {
    ".dart", ".kt", ".xml", ".swift", ".strings",
    ".md", ".html", ".json", ".yaml", ".yml", ".py", ".rc", ".txt",
}
SKIP_PARTS = {".git", ".dart_tool", "build", "dist", "Pods"}
SKIP_NAMES = {"pubspec.lock", "go.sum", "go.mod", "Podfile.lock"}

for path in ROOT.rglob("*"):
    if not path.is_file() or path.suffix.lower() not in CONVERT_SUFFIXES:
        continue
    if path.name in SKIP_NAMES or any(p in SKIP_PARTS for p in path.parts):
        continue
    try:
        original = path.read_text(encoding="utf-8")
    except (UnicodeDecodeError, OSError):
        continue
    converted = cc.convert(original)
    if converted != original:
        path.write_text(converted, encoding="utf-8")

# Replace the old edition names only in app/UI/docs/build files.
BRAND_ROOTS = [ROOT / "lib", ROOT / "android", ROOT / "ios", ROOT / "windows",
               ROOT / "scripts", ROOT / "README.md", ROOT / "CHANGELOG.md"]
BRAND_REPLACEMENTS = [
    ("紅果鑑", "HGTV"), ("真果鑑", "HGTV"),
    ("红果鉴", "HGTV"), ("真果鉴", "HGTV"),
    ("HongGuoJian", "HGTV"), ("ZhenGuoJian", "HGTV"),
]
for base in BRAND_ROOTS:
    candidates = [base] if base.is_file() else (base.rglob("*") if base.exists() else [])
    for path in candidates:
        if not path.is_file() or path.suffix.lower() not in CONVERT_SUFFIXES:
            continue
        try:
            data = path.read_text(encoding="utf-8")
        except (UnicodeDecodeError, OSError):
            continue
        new_data = data
        for old, new in BRAND_REPLACEMENTS:
            new_data = new_data.replace(old, new)
        if new_data != data:
            path.write_text(new_data, encoding="utf-8")

# App name and Android-TV icon/banner.
pubspec = ROOT / "pubspec.yaml"
text = pubspec.read_text(encoding="utf-8")
text = re.sub(r"^name:\s*.*$", "name: hgtv", text, flags=re.M)
text = re.sub(r"^description:\s*.*$", "description: HGTV 短劇播放器（繁體中文台灣版）", text, flags=re.M)
pubspec.write_text(text, encoding="utf-8")

gradle = ROOT / "android/app/build.gradle.kts"
text = gradle.read_text(encoding="utf-8")
text = text.replace('manifestPlaceholders["appLabel"] = if (allSources) "HGTV" else "HGTV"',
                    'manifestPlaceholders["appLabel"] = "HGTV"')
text = text.replace('manifestPlaceholders["appLabel"] = if (allSources) "真果鑑" else "紅果鑑"',
                    'manifestPlaceholders["appLabel"] = "HGTV"')
text = text.replace('manifestPlaceholders["appBanner"] = if (allSources) "@drawable/tv_banner_all_sources" else "@drawable/tv_banner"',
                    'manifestPlaceholders["appBanner"] = "@drawable/tv_banner"')
gradle.write_text(text, encoding="utf-8")

def font(size, bold=True):
    choices = [
        "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf" if bold else "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
        "/usr/share/fonts/truetype/liberation2/LiberationSans-Bold.ttf" if bold else "/usr/share/fonts/truetype/liberation2/LiberationSans-Regular.ttf",
    ]
    for candidate in choices:
        if Path(candidate).exists():
            return ImageFont.truetype(candidate, size)
    return ImageFont.load_default()

def gradient_bg(width, height):
    im = Image.new("RGB", (width, height))
    p = im.load()
    for y in range(height):
        for x in range(width):
            t = (x * 0.62 + y * 0.38) / (width + height)
            stops = [(0.0, (11, 18, 48)), (0.48, (37, 45, 110)), (1.0, (0, 151, 164))]
            for i in range(len(stops)-1):
                if stops[i][0] <= t <= stops[i+1][0]:
                    p0,c0=stops[i]; p1,c1=stops[i+1]
                    u=(t-p0)/(p1-p0)
                    p[x,y]=tuple(int(c0[k]*(1-u)+c1[k]*u) for k in range(3))
                    break
    return im

def make_icon(size=1024):
    im = gradient_bg(size, size).convert("RGBA")
    overlay = Image.new("RGBA", im.size, (0,0,0,0))
    d = ImageDraw.Draw(overlay)
    margin=int(size*.065)
    d.rounded_rectangle((margin,margin,size-margin,size-margin), radius=int(size*.20),
                        fill=(255,255,255,9), outline=(255,255,255,64), width=max(2,size//220))
    cx, cy = size//2, int(size*.39)
    glow=Image.new("RGBA", im.size, (0,0,0,0))
    gd=ImageDraw.Draw(glow)
    r=int(size*.19)
    gd.ellipse((cx-r,cy-r,cx+r,cy+r), fill=(31,232,222,70))
    glow=glow.filter(ImageFilter.GaussianBlur(size*.035))
    overlay=Image.alpha_composite(overlay,glow)
    d=ImageDraw.Draw(overlay)
    d.rounded_rectangle((int(size*.29),int(size*.20),int(size*.71),int(size*.54)),
                        radius=int(size*.055), fill=(245,250,255,248))
    d.rounded_rectangle((int(size*.315),int(size*.225),int(size*.685),int(size*.515)),
                        radius=int(size*.035), fill=(17,27,67,255))
    d.polygon([(int(size*.445),int(size*.285)),(int(size*.445),int(size*.455)),(int(size*.585),int(size*.370))],
              fill=(42,230,215,255))
    d.line([(int(size*.43),int(size*.17)),(int(size*.50),int(size*.115)),(int(size*.57),int(size*.17))],
           fill=(255,255,255,210), width=max(5,size//100), joint="curve")
    label="HGTV"
    f=font(int(size*.17))
    box=d.textbbox((0,0),label,font=f)
    tw=box[2]-box[0]
    d.text(((size-tw)//2,int(size*.62)-box[1]),label,font=f,fill=(255,255,255,255))
    d.rounded_rectangle((int(size*.34),int(size*.825),int(size*.66),int(size*.842)),
                        radius=int(size*.01), fill=(48,231,218,255))
    return Image.alpha_composite(im,overlay).convert("RGBA")

res = ROOT / "android/app/src/main/res"
drawable = res / "drawable"
drawable.mkdir(parents=True, exist_ok=True)
old_vector = drawable / "app_icon.xml"
if old_vector.exists():
    old_vector.unlink()
make_icon().save(drawable / "app_icon.png", optimize=True)

# Android TV banner in matching style.
banner = gradient_bg(1280,720).convert("RGBA")
icon = make_icon(640).resize((520,520), Image.Resampling.LANCZOS)
banner.alpha_composite(icon, (50,100))
d=ImageDraw.Draw(banner)
d.text((610,220),"HGTV",font=font(138),fill=(255,255,255,255))
d.text((620,382),"短劇・影視",font=font(56,False),fill=(151,244,238,255))
d.rounded_rectangle((625,485,960,500),radius=7,fill=(48,231,218,255))
banner_path = res / "drawable-xhdpi/tv_banner.png"
banner_path.parent.mkdir(parents=True,exist_ok=True)
banner.convert("RGB").save(banner_path, optimize=True)
all_banner=res / "drawable-xhdpi/tv_banner_all_sources.png"
if all_banner.exists():
    shutil.copy2(banner_path, all_banner)

icon1024=make_icon(1024)
for density, side in [("mdpi",48),("hdpi",72),("xhdpi",96),("xxhdpi",144),("xxxhdpi",192)]:
    folder=res/f"mipmap-{density}"
    folder.mkdir(parents=True,exist_ok=True)
    icon1024.resize((side,side),Image.Resampling.LANCZOS).save(folder/"ic_launcher.png", optimize=True)

readme=ROOT/"README.md"
if readme.exists():
    t=readme.read_text(encoding="utf-8")
    t=re.sub(r"^#.*$", "# HGTV", t, count=1, flags=re.M)
    t=t.replace("红果鉴 / 真果鉴", "HGTV").replace("紅果鑑 / 真果鑑", "HGTV")
    t=t.replace("红果鉴", "HGTV").replace("真果鉴", "HGTV")
    t=t.replace("紅果鑑", "HGTV").replace("真果鑑", "HGTV")
    t += "\n\n## HGTV 移植說明\n\n此儲存庫基於 [Xanerix/guoapp](https://github.com/Xanerix/guoapp) 移植，保留原始來源與第三方元件授權資訊。應用顯示名稱為 HGTV，介面文字採繁體中文（台灣用語）。\n"
    readme.write_text(cc.convert(t), encoding="utf-8")

# Replace upstream multi-platform release workflow with a focused v7a APK build.
workflow = ROOT / ".github/workflows/build.yml"
workflow.parent.mkdir(parents=True, exist_ok=True)
workflow.write_text("""name: Build HGTV Android v7a

on:
  workflow_dispatch:

permissions:
  contents: read

jobs:
  build-android-v7a:
    runs-on: ubuntu-latest
    timeout-minutes: 60
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-python@v5
        with:
          python-version: '3.12'
      - uses: actions/setup-java@v4
        with:
          distribution: temurin
          java-version: '17'
      - uses: actions/setup-go@v5
        with:
          go-version-file: native/go.mod
          cache-dependency-path: native/go.sum
      - uses: subosito/flutter-action@v2
        with:
          flutter-version: '3.47.4'
          channel: stable
          cache: true
      - uses: android-actions/setup-android@v3
        with:
          packages: 'platforms;android-36'
          accept-android-sdk-licenses: true
      - name: Install Android NDK
        run: sdkmanager 'ndk;28.2.13676358'
      - name: Build APK (armeabi-v7a)
        run: python scripts/build_android.py --abi armeabi-v7a
      - name: Rename APK
        run: |
          set -euo pipefail
          apk=$(find dist/android -maxdepth 1 -type f -name '*armeabi-v7a.apk' | head -n1)
          test -n "$apk"
          mv "$apk" dist/android/HGTV-armeabi-v7a.apk
      - uses: actions/upload-artifact@v4
        with:
          name: HGTV-Android-v7a
          path: dist/android/HGTV-armeabi-v7a.apk
          if-no-files-found: error
          retention-days: 14
""", encoding="utf-8")

notice = ROOT / "UPSTREAM-NOTICE.md"
notice.write_text("""# 上游來源與聲明

HGTV 基於上游專案 [Xanerix/guoapp](https://github.com/Xanerix/guoapp) 移植。
請以原專案內的授權文件及第三方元件授權條款為準；若上游未提供明確授權，請在重新散布或公開發佈前取得必要許可。
HGTV 的名稱、繁體中文台灣用語轉換及圖示為此移植版本的修改。
""", encoding="utf-8")
print("HGTV migration complete")
