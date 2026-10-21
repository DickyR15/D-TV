#!/usr/bin/env python3
import os
import shutil
import subprocess
import sys
from pathlib import Path

EXECUTABLES = ("zhenguojian.exe", "hongguojian.exe")


def running(executable: str) -> bool:
    output = subprocess.run(
        ["tasklist", "/FI", f"IMAGENAME eq {executable}", "/NH"],
        capture_output=True,
        text=True,
    ).stdout
    return executable.lower() in output.lower()


def main() -> int:
    for executable in EXECUTABLES:
        if running(executable):
            print(f"{executable} 正在執行，請先退出應用")
            return 1
    directory = Path(os.environ["APPDATA"]) / "HGTV"
    if not directory.exists():
        print(f"資料目錄 {directory} 不存在，無需清理")
        return 0
    shutil.rmtree(directory)
    print(f"已刪除 {directory}")
    print("完成，HGTV與HGTV共用此目錄，兩者的設定、追劇、收藏、快取、日誌和預設下載目錄均已清空")
    return 0


if __name__ == "__main__":
    sys.exit(main())
