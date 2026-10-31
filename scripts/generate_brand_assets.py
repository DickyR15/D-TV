#!/usr/bin/env python3
"""Generate Android, Windows, iOS and macOS icons from one D TV vector master."""
from __future__ import annotations

import json
import re
import xml.etree.ElementTree as ET
from pathlib import Path

from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parents[1]
VECTOR_ICON = ROOT / "android/app/src/main/res/drawable/dtv_icon.xml"
ANDROID_RES = ROOT / "android/app/src/main/res"
IOS_ICON_SET = ROOT / "ios/Runner/Assets.xcassets/AppIcon.appiconset"
MACOS_ICON_SET = ROOT / "macos/Runner/Assets.xcassets/AppIcon.appiconset"
WINDOWS_ICON = ROOT / "windows/runner/resources/app_icon.ico"
MASTER_SIZE = 1024

TOKEN = re.compile(r"[A-Za-z]|[-+]?(?:\d*\.)?\d+(?:[eE][-+]?\d+)?")


def parse_path_data(data: str) -> tuple[list[tuple[float, float]], bool]:
    """Parse the M/L/H/V/C/Z path commands used by dtv_icon.xml."""
    tokens = TOKEN.findall(data)
    points: list[tuple[float, float]] = []
    current = (0.0, 0.0)
    start = current
    command: str | None = None
    closed = False
    index = 0

    def number() -> float:
        nonlocal index
        if index >= len(tokens) or tokens[index].isalpha():
            raise ValueError(f"Unexpected vector path data: {data}")
        value = float(tokens[index])
        index += 1
        return value

    while index < len(tokens):
        if tokens[index].isalpha():
            command = tokens[index].upper()
            index += 1
            if command == "Z":
                closed = True
                current = start
                if points and points[-1] != start:
                    points.append(start)
                command = None
                continue

        if command is None:
            raise ValueError(f"Unsupported vector path data: {data}")

        if command == "M":
            current = (number(), number())
            start = current
            points.append(current)
            command = "L"
        elif command == "L":
            current = (number(), number())
            points.append(current)
        elif command == "H":
            current = (number(), current[1])
            points.append(current)
        elif command == "V":
            current = (current[0], number())
            points.append(current)
        elif command == "C":
            x0, y0 = current
            x1, y1, x2, y2, x3, y3 = (
                number(), number(), number(), number(), number(), number()
            )
            for step in range(1, 65):
                t = step / 64
                u = 1 - t
                x = u**3*x0 + 3*u*u*t*x1 + 3*u*t*t*x2 + t**3*x3
                y = u**3*y0 + 3*u*u*t*y1 + 3*u*t*t*y2 + t**3*y3
                points.append((x, y))
            current = (x3, y3)
        else:
            raise ValueError(f"Unsupported vector path command: {command}")
    return points, closed


def color(value: str | None, opacity: str | None) -> tuple[int, int, int, int] | None:
    if not value or value.lower() in ("none", "#00000000"):
        return None
    raw = value.lstrip("#")
    alpha = 1.0
    if len(raw) == 8:
        alpha = int(raw[:2], 16) / 255
        raw = raw[2:]
    if len(raw) != 6:
        raise ValueError(f"Unsupported vector color: {value}")
    if opacity is not None:
        alpha *= float(opacity)
    return (
        int(raw[0:2], 16),
        int(raw[2:4], 16),
        int(raw[4:6], 16),
        round(alpha * 255),
    )


def render_master() -> Image.Image:
    if not VECTOR_ICON.is_file():
        raise SystemExit(f"找不到 D TV 主圖示：{VECTOR_ICON}")
    tree = ET.parse(VECTOR_ICON)
    vector = tree.getroot()
    ns = "{http://schemas.android.com/apk/res/android}"
    width = float(vector.attrib[ns + "viewportWidth"])
    height = float(vector.attrib[ns + "viewportHeight"])
    sx, sy = MASTER_SIZE / width, MASTER_SIZE / height
    image = Image.new("RGBA", (MASTER_SIZE, MASTER_SIZE), (0, 0, 0, 0))

    for path in vector.findall("path"):
        points, closed = parse_path_data(path.attrib[ns + "pathData"])
        if not points:
            continue
        scaled = [(round(x * sx), round(y * sy)) for x, y in points]
        overlay = Image.new("RGBA", image.size, (0, 0, 0, 0))
        draw = ImageDraw.Draw(overlay)

        fill = color(
            path.attrib.get(ns + "fillColor"),
            path.attrib.get(ns + "fillAlpha"),
        )
        if fill and closed and len(scaled) >= 3:
            draw.polygon(scaled, fill=fill)

        stroke = color(
            path.attrib.get(ns + "strokeColor"),
            path.attrib.get(ns + "strokeAlpha"),
        )
        if stroke and len(scaled) >= 2:
            stroke_width = float(path.attrib.get(ns + "strokeWidth", "1"))
            width_px = max(1, round(stroke_width * (sx + sy) / 2))
            draw.line(scaled, fill=stroke, width=width_px, joint="curve")
        image = Image.alpha_composite(image, overlay)

    if image.getbbox() is None or image.getchannel("A").getextrema()[0] != 255:
        raise SystemExit("D TV 圖示未完整繪製或背景不是不透明，停止輸出。")
    return image


def square(image: Image.Image, pixels: int) -> Image.Image:
    return image.resize((pixels, pixels), Image.Resampling.LANCZOS)


def write_android(master: Image.Image) -> int:
    density_sizes = {
        "mipmap-mdpi": 48,
        "mipmap-hdpi": 72,
        "mipmap-xhdpi": 96,
        "mipmap-xxhdpi": 144,
        "mipmap-xxxhdpi": 192,
    }
    count = 0
    for folder, pixels in density_sizes.items():
        destination = ANDROID_RES / folder
        destination.mkdir(parents=True, exist_ok=True)
        icon = square(master, pixels)
        icon.save(destination / "ic_dtv.webp", format="WEBP", lossless=True, method=6)
        icon.save(destination / "ic_launcher.png", format="PNG", optimize=True)
        count += 2

    drawable_icon = ANDROID_RES / "drawable" / "app_icon.png"
    drawable_icon.parent.mkdir(parents=True, exist_ok=True)
    square(master, 512).save(drawable_icon, format="PNG", optimize=True)
    return count + 1


def write_windows(master: Image.Image) -> int:
    WINDOWS_ICON.parent.mkdir(parents=True, exist_ok=True)
    master.save(
        WINDOWS_ICON,
        format="ICO",
        sizes=[(16, 16), (20, 20), (24, 24), (32, 32), (40, 40),
               (48, 48), (64, 64), (96, 96), (128, 128), (256, 256)],
    )
    with Image.open(WINDOWS_ICON) as generated:
        required = {(16, 16), (32, 32), (48, 48), (256, 256)}
        if not required.issubset(generated.ico.sizes()):
            raise SystemExit("Windows ICO 缺少必要尺寸。")
    return 1


def write_ios(master: Image.Image) -> int:
    manifest_path = IOS_ICON_SET / "Contents.json"
    if not manifest_path.is_file():
        raise SystemExit("找不到 iOS AppIcon.appiconset/Contents.json。")
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    IOS_ICON_SET.mkdir(parents=True, exist_ok=True)
    written = 0
    for entry in manifest.get("images", []):
        filename = entry.get("filename")
        if not filename:
            continue
        logical_size = float(entry["size"].split("x")[0])
        scale = float(entry["scale"].removesuffix("x"))
        pixels = round(logical_size * scale)
        target = IOS_ICON_SET / filename
        target.parent.mkdir(parents=True, exist_ok=True)
        square(master, pixels).convert("RGB").save(
            target, format="PNG", optimize=True
        )
        written += 1
    if written == 0:
        raise SystemExit("iOS AppIcon 資產清單沒有任何圖示檔案。")
    return written


def write_macos(master: Image.Image) -> int:
    # The macOS project is generated by Flutter during the macOS build.
    # Do not fail iOS/Android/Windows builds when the macOS project is absent.
    if not (ROOT / "macos" / "Runner.xcodeproj").is_dir():
        return 0

    MACOS_ICON_SET.mkdir(parents=True, exist_ok=True)
    images = []
    count = 0
    for logical in (16, 32, 128, 256, 512):
        for scale, suffix in ((1, ""), (2, "@2x")):
            pixels = logical * scale
            filename = f"AppIcon_{logical}x{logical}{suffix}.png"
            square(master, pixels).convert("RGB").save(
                MACOS_ICON_SET / filename, format="PNG", optimize=True
            )
            images.append({
                "size": f"{logical}x{logical}",
                "idiom": "mac",
                "filename": filename,
                "scale": f"{scale}x",
            })
            count += 1
    manifest = {
        "images": images,
        "info": {"author": "xcode", "version": 1},
    }
    (MACOS_ICON_SET / "Contents.json").write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )
    return count


def main() -> None:
    master = render_master()
    counts = {
        "Android": write_android(master),
        "Windows": write_windows(master),
        "iOS": write_ios(master),
        "macOS": write_macos(master),
    }
    print("D TV 共用圖示已產生：" + "、".join(
        f"{platform} {count} 個資產" for platform, count in counts.items()
    ))


if __name__ == "__main__":
    main()
