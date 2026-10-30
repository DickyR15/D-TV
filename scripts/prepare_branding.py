#!/usr/bin/env python3
"""Generate every platform app icon from D TV's own Android vector drawable."""

import json
import re
import sys
import xml.etree.ElementTree as ET
from pathlib import Path

try:
    import aggdraw
    from PIL import Image
except ImportError as error:
    raise SystemExit(
        "D TV 圖示產生需要 Pillow 與 aggdraw；請執行："
        "python -m pip install Pillow aggdraw"
    ) from error

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
if hasattr(sys.stderr, "reconfigure"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")

ROOT = Path(__file__).resolve().parents[1]
ANDROID = "{http://schemas.android.com/apk/res/android}"
TOKEN = re.compile(r"[A-Za-z]|[-+]?(?:\d*\.\d+|\d+\.?\d*)(?:[eE][-+]?\d+)?")


def parse_path_data(value: str, scale: float, offset_x: float, offset_y: float):
    tokens = TOKEN.findall(value)
    path = aggdraw.Path()
    index = 0
    command = None
    current_x = 0.0
    current_y = 0.0
    moved = False

    def number():
        nonlocal index
        if index >= len(tokens) or tokens[index].isalpha():
            raise ValueError(f"不完整的 D TV 向量路徑：{value}")
        result = float(tokens[index])
        index += 1
        return result

    def point(x, y):
        return x * scale + offset_x, y * scale + offset_y

    while index < len(tokens):
        if tokens[index].isalpha():
            command = tokens[index]
            index += 1
        if command is None:
            raise ValueError(f"無效的 D TV 向量路徑：{value}")

        if command == "Z":
            path.close()
            command = None
            current_x = current_y = 0.0
            continue
        if command == "M":
            x, y = number(), number()
            current_x, current_y = x, y
            path.moveto(*point(x, y))
            moved = True
            command = "L"
            continue
        if command == "L":
            x, y = number(), number()
            current_x, current_y = x, y
            path.lineto(*point(x, y))
            continue
        if command == "H":
            x = number()
            current_x = x
            path.lineto(*point(current_x, current_y))
            continue
        if command == "V":
            y = number()
            current_y = y
            path.lineto(*point(current_x, current_y))
            continue
        if command == "C":
            x1, y1, x2, y2, x, y = (number() for _ in range(6))
            path.curveto(*point(x1, y1), *point(x2, y2), *point(x, y))
            current_x, current_y = x, y
            continue
        raise ValueError(f"尚未支援的 D TV 向量指令：{command}")

    if not moved:
        raise ValueError("D TV 圖示沒有有效的向量路徑")
    return path


def parse_color(value: str, opacity: float):
    if not value or value.lower() == "#00000000":
        return None
    raw = value.lstrip("#")
    if len(raw) == 8:  # Android VectorDrawable uses #AARRGGBB.
        alpha = int(raw[0:2], 16) / 255.0
        red, green, blue = (
            int(raw[2:4], 16),
            int(raw[4:6], 16),
            int(raw[6:8], 16),
        )
    elif len(raw) == 6:
        alpha = 1.0
        red, green, blue = (
            int(raw[0:2], 16),
            int(raw[2:4], 16),
            int(raw[4:6], 16),
        )
    else:
        raise ValueError(f"不支援的 D TV 圖示色彩：{value}")
    return (red, green, blue), max(0, min(255, round(alpha * opacity * 255)))


def render_master_icon(size: int = 1024):
    source = ROOT / "android/app/src/main/res/drawable/dtv_icon.xml"
    root = ET.parse(source).getroot()
    width = float(root.attrib[ANDROID + "viewportWidth"])
    height = float(root.attrib[ANDROID + "viewportHeight"])
    scale = min(size / width, size / height)
    offset_x = (size - width * scale) / 2
    offset_y = (size - height * scale) / 2
    # Draw at 2x and downsample for smooth edges in all icon sizes.
    oversample = 2
    canvas_size = size * oversample
    factor = scale * oversample
    image = Image.new("RGBA", (canvas_size, canvas_size), (0, 0, 0, 0))
    draw = aggdraw.Draw(image)

    for element in root:
        path_data = element.attrib.get(ANDROID + "pathData")
        if not path_data:
            continue
        path = parse_path_data(
            path_data,
            factor,
            offset_x * oversample,
            offset_y * oversample,
        )
        fill_value = parse_color(
            element.attrib.get(ANDROID + "fillColor", ""),
            float(element.attrib.get(ANDROID + "fillAlpha", "1")),
        )
        stroke_value = parse_color(
            element.attrib.get(ANDROID + "strokeColor", ""),
            float(element.attrib.get(ANDROID + "strokeAlpha", "1")),
        )
        fill = (
            aggdraw.Brush(fill_value[0], fill_value[1])
            if fill_value and fill_value[1] > 0
            else None
        )
        stroke_width = float(element.attrib.get(ANDROID + "strokeWidth", "0")) * factor
        pen = (
            aggdraw.Pen(stroke_value[0], stroke_width, stroke_value[1])
            if stroke_value and stroke_value[1] > 0 and stroke_width > 0
            else None
        )
        if fill or pen:
            draw.path(path, pen, fill)
    draw.flush()
    return image.resize((size, size), Image.Resampling.LANCZOS)


def save_resized(master, destination: Path, size: int, format_name="PNG"):
    destination.parent.mkdir(parents=True, exist_ok=True)
    icon = master.resize((size, size), Image.Resampling.LANCZOS)
    if format_name == "WEBP":
        icon.save(destination, "WEBP", quality=100, method=6)
    else:
        icon.save(destination, "PNG", optimize=True)


def build_android(master):
    densities = {
        "mdpi": 48,
        "hdpi": 72,
        "xhdpi": 96,
        "xxhdpi": 144,
        "xxxhdpi": 192,
    }
    for density, size in densities.items():
        folder = ROOT / "android/app/src/main/res" / f"mipmap-{density}"
        save_resized(master, folder / "ic_dtv.webp", size, "WEBP")
        # Keep the old launcher resource name aligned as well; manifests and
        # third-party launchers must never fall back to the previous author icon.
        save_resized(master, folder / "ic_launcher.png", size)
    save_resized(
        master,
        ROOT / "android/app/src/main/res/drawable/app_icon.png",
        192,
    )


def build_apple_icon_set(master, directory: Path):
    contents_path = directory / "Contents.json"
    if not contents_path.is_file():
        raise SystemExit(f"找不到 Apple AppIcon 設定：{contents_path}")
    contents = json.loads(contents_path.read_text(encoding="utf-8"))
    for item in contents.get("images", []):
        filename = item.get("filename")
        if not filename:
            continue
        base_size = float(item["size"].split("x", 1)[0])
        scale = float(item.get("scale", "1x").removesuffix("x"))
        pixel_size = round(base_size * scale)
        save_resized(master, directory / filename, pixel_size)


def build_macos_icon_set(master):
    directory = ROOT / "macos/Runner/Assets.xcassets/AppIcon.appiconset"
    directory.mkdir(parents=True, exist_ok=True)
    images = []
    for base in [16, 32, 128, 256, 512]:
        for scale in [1, 2]:
            pixels = base * scale
            filename = f"dtv-{pixels}.png"
            save_resized(master, directory / filename, pixels)
            images.append({
                "size": f"{base}x{base}",
                "idiom": "mac",
                "filename": filename,
                "scale": f"{scale}x",
            })
    (directory / "Contents.json").write_text(
        json.dumps(
            {"images": images, "info": {"version": 1, "author": "xcode"}},
            ensure_ascii=False,
            indent=2,
        ) + "\n",
        encoding="utf-8",
    )


def build_windows(master):
    target = ROOT / "windows/runner/resources/app_icon.ico"
    target.parent.mkdir(parents=True, exist_ok=True)
    master.save(
        target,
        format="ICO",
        sizes=[(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)],
    )


def main():
    master = render_master_icon()
    save_resized(master, ROOT / "assets/brand/dtv-app-icon.png", 1024)
    build_android(master)
    build_apple_icon_set(
        master,
        ROOT / "ios/Runner/Assets.xcassets/AppIcon.appiconset",
    )
    if (ROOT / "macos/Runner.xcodeproj").is_dir():
        build_macos_icon_set(master)
    build_windows(master)
    print("已從 D TV 自有藍白向量圖產生 Android、iOS、macOS、Windows 圖示。")


if __name__ == "__main__":
    main()
