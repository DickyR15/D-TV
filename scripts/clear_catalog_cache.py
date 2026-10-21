#!/usr/bin/env python3
import argparse
import os
import shutil
import subprocess
import sys
from pathlib import Path

APPS = {
    "zhen": ("HGTV", "zhenguojian.exe"),
    "hong": ("HGTV", "hongguojian.exe"),
}


def running(executable: str) -> bool:
    output = subprocess.run(
        ["tasklist", "/FI", f"IMAGENAME eq {executable}", "/NH"],
        capture_output=True,
        text=True,
    ).stdout
    return executable.lower() in output.lower()


def remove(path: Path) -> None:
    if not path.exists():
        return
    if path.is_dir():
        shutil.rmtree(path)
    else:
        path.unlink()
    print(f"已刪除 {path}")


def main() -> int:
    parser = argparse.ArgumentParser(description="清空 Windows 版劇庫快取，模擬首次載入")
    parser.add_argument("--app", choices=APPS, default="zhen")
    parser.add_argument("--covers", action="store_true", help="同時清空海報快取")
    args = parser.parse_args()

    name, executable = APPS[args.app]
    if running(executable):
        print(f"{name} 正在執行，請先退出應用")
        return 1
    directory = Path(os.environ["APPDATA"]) / name / name
    if not directory.is_dir():
        print(f"找不到資料目錄 {directory}")
        return 1

    remove(directory / "catalogs.json")
    remove(directory / "sources.json")
    for temporary in directory.glob(".cache-write-*"):
        remove(temporary)
    if args.covers:
        remove(directory / "covers-v1")
    print("完成，追劇、收藏、設定和下載未改動")
    return 0


if __name__ == "__main__":
    sys.exit(main())
