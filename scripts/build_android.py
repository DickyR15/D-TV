import argparse
import os
import platform
import re
import shutil
import subprocess
import sys
from pathlib import Path

from app_build import BuildVariant, add_variant_argument

root = Path(__file__).resolve().parents[1]
ALL_ABIS = ["arm64-v8a", "armeabi-v7a", "x86_64"]
TARGET_PLATFORMS = {
    "arm64-v8a": "android-arm64",
    "armeabi-v7a": "android-arm",
    "x86_64": "android-x64",
}

parser = argparse.ArgumentParser(
    description="建置 D-TV Android APK；可選擇分 ABI 或合併為單一 Universal APK。"
)
parser.add_argument(
    "--abi",
    action="append",
    choices=ALL_ABIS,
    help="分 ABI 建置時指定目標，可重複使用。",
)
parser.add_argument(
    "--universal",
    action="store_true",
    help="產生同時包含 ARM64、ARMv7、x86_64 的單一 APK。",
)
parser.add_argument(
    "--player-engine",
    choices=["mpv", "exo", "mpv_embed"],
    default="mpv_embed",
    help="Android 播放引擎；預設使用 MPV mediacodec_embed。",
)
parser.add_argument("--build-name", help="覆寫 Android app versionName。")
parser.add_argument("--build-number", help="覆寫 Android app versionCode。")
add_variant_argument(parser)
options = parser.parse_args()

if options.universal and options.abi:
    parser.error("--universal 會固定建置三種 ABI，不可同時指定 --abi。")

variant = BuildVariant(options.all_sources)
abis = ALL_ABIS if options.universal else (options.abi or ALL_ABIS)
env = os.environ.copy()
if platform.system() == "Darwin":
    env["LANG"] = "en_US.UTF-8"
    env["LC_ALL"] = "en_US.UTF-8"
env.setdefault("GOPROXY", "https://goproxy.cn,direct")
env.setdefault("GOSUMDB", "off")

flutter = shutil.which("flutter")
if not flutter:
    raise SystemExit("請先將 Flutter SDK 的 bin 目錄加入 PATH。")

abi_args = [item for abi in abis for item in ["--abi", abi]]
subprocess.run(
    [
        sys.executable,
        str(root / "scripts" / "build_native.py"),
        "--platform",
        "android",
        *abi_args,
        *variant.arguments,
    ],
    cwd=root,
    env=env,
    check=True,
)
subprocess.run([flutter, "pub", "get"], cwd=root, env=env, check=True)

build_args = [
    flutter,
    "build",
    "apk",
    "--release",
    "--no-pub",
    *variant.flutter_arguments,
    "--dart-define=DTV_ANDROID_PLAYER=" + options.player_engine,
]
if not options.universal:
    build_args.append("--split-per-abi")
build_args += [
    "--target-platform",
    ",".join(TARGET_PLATFORMS[abi] for abi in abis),
]
if options.build_name:
    build_args += ["--build-name", options.build_name]
if options.build_number:
    build_args += ["--build-number", options.build_number]

subprocess.run(build_args, cwd=root, env=env, check=True)

package_args = [
    sys.executable,
    str(root / "scripts" / "package_release.py"),
    "--platform",
    "android",
    *variant.arguments,
]
if options.universal:
    package_args.append("--universal")
else:
    package_args += abi_args
subprocess.run(package_args, cwd=root, env=env, check=True)

if options.universal and options.build_number:
    # package_release.py names the validated intermediate APK with the full
    # pubspec version (including its existing +build metadata). Flutter's
    # --build-name/-number overrides only the final public delivery filename.
    pubspec = (root / "pubspec.yaml").read_text(encoding="utf-8")
    match = re.search(r"^version:\\s*([\\w.+-]+)\\s*$", pubspec, re.MULTILINE)
    if not match:
        raise SystemExit("pubspec.yaml 缺少合法版本號。")
    package_version = match.group(1)
    version = options.build_name or package_version.split("+", 1)[0]
    source = root / "dist" / "android" / (
        f"{variant.slug}-{package_version}-universal.apk"
    )
    target = root / "dist" / "android" / (
        f"D TV-Android-Universal_v{version}+{options.build_number}_MPV.apk"
    )
    if not source.is_file():
        raise SystemExit("找不到已驗證的 Universal APK：" + str(source))
    shutil.copy2(source, target)
    print(target)
