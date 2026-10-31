#!/usr/bin/env python3
"""Validate D TV branding and render the Windows ICO from the Android vector icon."""
from __future__ import annotations

import re
import xml.etree.ElementTree as ET
from pathlib import Path

try:
    from PIL import Image, ImageDraw
except ImportError as exc:
    raise SystemExit(
        "產生 Windows 圖示需要 Pillow；請先執行 python -m pip install Pillow。"
    ) from exc

root = Path(__file__).resolve().parents[1]
icon_vector = root / "android/app/src/main/res/drawable/dtv_icon.xml"
tv_banner = root / "android/app/src/main/res/drawable/tv_banner.xml"
windows_icon = root / "windows/runner/resources/app_icon.ico"

missing = [path for path in (icon_vector, tv_banner) if not path.is_file()]
if missing:
    raise SystemExit("找不到 D TV 品牌資源：" + ", ".join(str(path) for path in missing))

NUMBER_OR_COMMAND = re.compile(
    r"[A-Za-z]|[-+]?(?:\d*\.)?\d+(?:[eE][-+]?\d+)?"
)


def parse_path_data(data: str) -> tuple[list[tuple[float, float]], bool]:
    """Parse the M/L/H/V/C/Z commands used by the D TV VectorDrawable."""
    tokens = NUMBER_OR_COMMAND.findall(data)
    points: list[tuple[float, float]] = []
    current = (0.0, 0.0)
    start = current
    command: str | None = None
    closed = False
    index = 0

    def number() -> float:
        nonlocal index
        if index >= len(tokens) or tokens[index].isalpha():
            raise ValueError(f"Unexpected VectorDrawable path data: {data}")
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
            raise ValueError(f"Unsupported VectorDrawable path data: {data}")

        if command == "M":
            x, y = number(), number()
            current = (x, y)
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
            # Sample the cubic curve densely; rasterization happens at 512 px.
            for step in range(1, 33):
                t = step / 32
                u = 1 - t
                x = u**3 * x0 + 3*u*u*t*x1 + 3*u*t*t*x2 + t**3*x3
                y = u**3 * y0 + 3*u*u*t*y1 + 3*u*t*t*y2 + t**3*y3
                points.append((x, y))
            current = (x3, y3)
        else:
            raise ValueError(f"Unsupported VectorDrawable command {command!r}")

    return points, closed


def parse_color(value: str | None, opacity: str | None) -> tuple[int, int, int, int] | None:
    if not value or value.lower() in ("none", "#00000000"):
        return None
    value = value.lstrip("#")
    if len(value) == 8:
        alpha = int(value[:2], 16) / 255
        value = value[2:]
    else:
        alpha = 1.0
    if len(value) != 6:
        raise ValueError(f"Unsupported VectorDrawable color: #{value}")
    if opacity is not None:
        alpha *= float(opacity)
    return (
        int(value[0:2], 16),
        int(value[2:4], 16),
        int(value[4:6], 16),
        round(alpha * 255),
    )


def render_windows_icon() -> None:
    tree = ET.parse(icon_vector)
    vector = tree.getroot()
    namespace = "{http://schemas.android.com/apk/res/android}"
    width = float(vector.attrib[namespace + "viewportWidth"])
    height = float(vector.attrib[namespace + "viewportHeight"])
    canvas_size = 512
    sx, sy = canvas_size / width, canvas_size / height
    image = Image.new("RGBA", (canvas_size, canvas_size), (0, 0, 0, 0))

    for path in vector.findall(namespace + "path"):
        points, closed = parse_path_data(path.attrib[namespace + "pathData"])
        if not points:
            continue
        scaled = [(round(x * sx), round(y * sy)) for x, y in points]
        overlay = Image.new("RGBA", image.size, (0, 0, 0, 0))
        draw = ImageDraw.Draw(overlay)

        fill = parse_color(
            path.attrib.get(namespace + "fillColor"),
            path.attrib.get(namespace + "fillAlpha"),
        )
        if fill and closed and len(scaled) >= 3:
            draw.polygon(scaled, fill=fill)

        stroke = parse_color(
            path.attrib.get(namespace + "strokeColor"),
            path.attrib.get(namespace + "strokeAlpha"),
        )
        if stroke and len(scaled) >= 2:
            stroke_width = float(path.attrib.get(namespace + "strokeWidth", "1"))
            scaled_width = max(1, round(stroke_width * (sx + sy) / 2))
            draw.line(scaled, fill=stroke, width=scaled_width, joint="curve")

        image = Image.alpha_composite(image, overlay)

    windows_icon.parent.mkdir(parents=True, exist_ok=True)
    sizes = [(16, 16), (20, 20), (24, 24), (32, 32), (40, 40),
             (48, 48), (64, 64), (96, 96), (128, 128), (256, 256)]
    image.save(windows_icon, format="ICO", sizes=sizes)

    # Re-open the generated resource and verify that Explorer's common sizes exist.
    with Image.open(windows_icon) as generated:
        available = generated.ico.sizes()
        required_sizes = {(16, 16), (32, 32), (48, 48), (256, 256)}
        if not required_sizes.issubset(available):
            raise SystemExit(
                "Windows ICO 缺少必要尺寸：" +
                ", ".join(f"{w}x{h}" for w, h in sorted(required_sizes - available))
            )
    print(f"已從 D TV 共用向量圖產生 Windows 圖示：{windows_icon}")


if __name__ == "__main__":
    render_windows_icon()
    print("D TV Android 圖示、TV 橫幅與 Windows 圖示資源檢查完成。")
