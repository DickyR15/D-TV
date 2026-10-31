import argparse
import re
import shutil
import zipfile
from pathlib import Path

from app_build import BuildVariant, add_variant_argument

root = Path(__file__).resolve().parents[1]
ALL_ABIS = ["arm64-v8a", "armeabi-v7a", "x86_64"]

parser = argparse.ArgumentParser(description="驗證並封裝 D TV 發行產物。")
parser.add_argument("--platform", choices=["android", "windows", "macos"], required=True)
parser.add_argument("--abi", action="append", choices=ALL_ABIS)
parser.add_argument("--universal", action="store_true", help="驗證包含三種 ABI 的單一 Android APK。")
add_variant_argument(parser)
options = parser.parse_args()

if options.universal and (options.platform != "android" or options.abi):
    parser.error("--universal 僅適用 Android，且不可與 --abi 同時使用。")

variant = BuildVariant(options.all_sources)
match = re.search(
    r"^version:\s*([\w.+-]+)\s*$",
    (root / "pubspec.yaml").read_text(encoding="utf-8"),
    re.MULTILINE,
)
if not match:
    raise SystemExit("pubspec.yaml 缺少合法版本號。")
version = match.group(1).split("+", 1)[0]
output = root / "dist" / options.platform
output.mkdir(parents=True, exist_ok=True)
artifacts = []

if options.platform == "android":
    if options.universal:
        source = root / "build" / "app" / "outputs" / "flutter-apk" / "app-release.apk"
        if not source.is_file():
            raise SystemExit("缺少 Android Universal APK：" + str(source))
        required_libraries = [
            "libduanju_core.so",
            "libflutter.so",
            "libapp.so",
            "libmpv.so",
            "libffmpegkit.so",
        ]
        with zipfile.ZipFile(source) as archive:
            names = set(archive.namelist())
            missing = [
                f"lib/{abi}/{library}"
                for abi in ALL_ABIS
                for library in required_libraries
                if f"lib/{abi}/{library}" not in names
            ]
            if missing:
                raise SystemExit(
                    "Universal APK 缺少原生庫：" + ", ".join(missing)
                )
            corrupt = archive.testzip()
            if corrupt:
                raise SystemExit("Universal APK 壓縮資料損壞：" + corrupt)
        target = output / f"DTV-{version}-Android-Universal.apk"
        shutil.copy2(source, target)
        artifacts.append(target)
    else:
        for abi in options.abi or ALL_ABIS:
            source = (
                root
                / "build"
                / "app"
                / "outputs"
                / "flutter-apk"
                / f"app-{abi}-release.apk"
            )
            if not source.is_file():
                raise SystemExit("缺少 APK：" + str(source))
            with zipfile.ZipFile(source) as archive:
                names = set(archive.namelist())
                required = [
                    f"lib/{abi}/{library}"
                    for library in [
                        "libduanju_core.so",
                        "libflutter.so",
                        "libapp.so",
                        "libmpv.so",
                        "libffmpegkit.so",
                    ]
                ]
                missing = set(required) - names
                if missing:
                    raise SystemExit("APK 缺少原生庫：" + ", ".join(sorted(missing)))
                corrupt = archive.testzip()
                if corrupt:
                    raise SystemExit("APK 壓縮資料損壞：" + corrupt)
            target = output / f"DTV-{version}-Android-{abi}.apk"
            shutil.copy2(source, target)
            artifacts.append(target)

elif options.platform == "windows":
    bundle = root / "build" / "windows" / "x64" / "runner" / "Release"
    required = [
        "DTV.exe",
        "duanju_core.dll",
        "flutter_windows.dll",
        "libffmpegkit.dll",
        "libmpv-2.dll",
        "msvcp140.dll",
        "vcruntime140.dll",
        "data/icudtl.dat",
        "data/app.so",
    ]
    missing = [name for name in required if not (bundle / name).is_file()]
    if missing:
        raise SystemExit("Windows 免安裝版缺少檔案：" + ", ".join(missing))
    target = output / f"DTV-{version}-Windows-Portable.zip"
    with zipfile.ZipFile(target, "w", zipfile.ZIP_DEFLATED) as archive:
        for source in sorted(bundle.rglob("*")):
            if source.is_file():
                relative = source.relative_to(bundle).as_posix()
                archive.write(source, relative)

    # Reopen the closed archive for CRC validation. Checking it while the ZIP
    # writer is still open can report false corruption on large Windows AOT
    # snapshots such as data/app.so.
    with zipfile.ZipFile(target, "r") as archive:
        corrupt = archive.testzip()
        if corrupt:
            raise SystemExit("Windows ZIP 壓縮資料損壞：" + corrupt)
    artifacts.append(target)

else:
    products = root / "build" / "macos" / "Build" / "Products" / "Release"
    apps = sorted(products.glob("*.app"))
    if not apps:
        raise SystemExit("找不到 macOS Universal .app：" + str(products))
    app = apps[0]
    contents = app / "Contents"
    info = __import__("plistlib").loads((contents / "Info.plist").read_bytes())
    executable = contents / "MacOS" / info.get("CFBundleExecutable", "")
    dylib = contents / "Frameworks" / "libduanju_core.dylib"
    if not executable.is_file() or not dylib.is_file():
        raise SystemExit("macOS App 缺少主程式或共用 Go 核心。")
    for binary in [executable, dylib]:
        architectures = set(
            __import__("subprocess").check_output(
                ["lipo", "-archs", str(binary)], text=True
            ).split()
        )
        if not {"arm64", "x86_64"}.issubset(architectures):
            raise SystemExit(
                f"macOS 檔案不是 Universal：{binary.name} ({' '.join(sorted(architectures))})"
            )
    target = output / f"DTV-{version}-macOS.dmg"
    if target.exists():
        target.unlink()
    __import__("subprocess").run(
        [
            "hdiutil",
            "create",
            "-volname",
            "D TV",
            "-srcfolder",
            str(app),
            "-ov",
            "-format",
            "UDZO",
            str(target),
        ],
        check=True,
    )
    if not target.is_file() or target.stat().st_size == 0:
        raise SystemExit("macOS DMG 未成功產生。")
    artifacts.append(target)

for artifact in artifacts:
    print(artifact)
