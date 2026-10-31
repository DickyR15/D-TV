import json
import os
import platform
import plistlib
import re
import shutil
import subprocess
import sys
from pathlib import Path

from app_build import BuildVariant

ROOT = Path(__file__).resolve().parents[1]
MINIMUM_MACOS = "10.15"
APP_BUNDLE_IDENTIFIER = "com.dickyr15.dtv"
APP_DISPLAY_NAME = "D TV"
APP_ICON_NAME = "AppIcon"
MACOS_ICON_SET = (
    ROOT / "macos" / "Runner" / "Assets.xcassets" / "AppIcon.appiconset"
)


def run(arguments, *, cwd=ROOT, env=None):
    subprocess.run(arguments, cwd=cwd, env=env, check=True)


def ensure_macos_project(flutter):
    project = ROOT / "macos"
    if not project.is_dir() or not (project / "Runner.xcodeproj").is_dir():
        run(
            [
                flutter,
                "create",
                "--platforms=macos",
                "--org",
                "com.dickyr15",
                "--project-name",
                "dtv",
                ".",
            ]
        )
    pbxproj = project / "Runner.xcodeproj" / "project.pbxproj"
    text = pbxproj.read_text(encoding="utf-8")
    text = re.sub(
        r"MACOSX_DEPLOYMENT_TARGET = [^;]+;",
        f"MACOSX_DEPLOYMENT_TARGET = {MINIMUM_MACOS};",
        text,
    )
    text = re.sub(
        r"PRODUCT_BUNDLE_IDENTIFIER = [^;]+;",
        "PRODUCT_BUNDLE_IDENTIFIER = " + APP_BUNDLE_IDENTIFIER + ";",
        text,
    )
    pbxproj.write_text(text, encoding="utf-8")

    info = project / "Runner" / "Info.plist"
    if info.is_file():
        with info.open("rb") as stream:
            values = plistlib.load(stream)
        values["CFBundleDisplayName"] = APP_DISPLAY_NAME
        values["CFBundleName"] = APP_DISPLAY_NAME
        values["CFBundleIconName"] = APP_ICON_NAME
        with info.open("wb") as stream:
            plistlib.dump(values, stream)

    for name in ["DebugProfile.entitlements", "Release.entitlements"]:
        entitlements = project / "Runner" / name
        if not entitlements.is_file():
            continue
        with entitlements.open("rb") as stream:
            values = plistlib.load(stream)
        values["com.apple.security.network.client"] = True
        values["com.apple.security.network.server"] = True
        values["com.apple.security.files.user-selected.read-write"] = True
        with entitlements.open("wb") as stream:
            plistlib.dump(values, stream)


def validate_macos_icon_assets():
    """Fail early if the shared D TV artwork was not generated for macOS."""
    manifest_path = MACOS_ICON_SET / "Contents.json"
    if not manifest_path.is_file():
        raise SystemExit("macOS 缺少 D TV AppIcon 資產清單。")
    try:
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise SystemExit("macOS AppIcon 資產清單無法讀取：" + str(error)) from error

    expected = {
        f"AppIcon_{size}x{size}{suffix}.png"
        for size in (16, 32, 128, 256, 512)
        for suffix in ("", "@2x")
    }
    configured = {
        entry.get("filename")
        for entry in manifest.get("images", [])
        if isinstance(entry, dict)
    }
    missing = sorted(expected - configured)
    if missing:
        raise SystemExit("macOS D TV AppIcon 清單缺少尺寸：" + ", ".join(missing))
    for filename in sorted(expected):
        asset = MACOS_ICON_SET / filename
        if not asset.is_file() or asset.stat().st_size == 0:
            raise SystemExit("macOS D TV AppIcon 圖檔缺少或為空：" + filename)


def validate_application_identity(application):
    """Validate the built bundle, not just the pre-build Xcode project."""
    if application.name != "D TV.app":
        raise SystemExit("macOS App 外層名稱不是 D TV.app。")

    info_path = application / "Contents" / "Info.plist"
    if not info_path.is_file():
        raise SystemExit("macOS App 缺少 Contents/Info.plist。")
    try:
        values = plistlib.loads(info_path.read_bytes())
    except (OSError, plistlib.InvalidFileException) as error:
        raise SystemExit("macOS App Info.plist 無法讀取：" + str(error)) from error

    expected = {
        "CFBundleIdentifier": APP_BUNDLE_IDENTIFIER,
        "CFBundleDisplayName": APP_DISPLAY_NAME,
        "CFBundleName": APP_DISPLAY_NAME,
        "CFBundleIconName": APP_ICON_NAME,
    }
    for key, wanted in expected.items():
        actual = values.get(key)
        if actual != wanted:
            raise SystemExit(
                f"macOS App 身分驗證失敗：{key} 應為 {wanted!r}，目前為 {actual!r}"
            )

    resources = application / "Contents" / "Resources"
    compiled_catalog = resources / "Assets.car"
    if not compiled_catalog.is_file() or compiled_catalog.stat().st_size == 0:
        raise SystemExit("macOS App 缺少已編譯的 AppIcon 資產（Contents/Resources/Assets.car）。")

    executable_name = values.get("CFBundleExecutable")
    if not isinstance(executable_name, str) or not executable_name.strip():
        raise SystemExit("macOS App Info.plist 缺少 CFBundleExecutable。")
    executable = application / "Contents" / "MacOS" / executable_name
    if not executable.is_file():
        raise SystemExit("macOS App 找不到主執行檔：" + executable_name)


def build_universal_core(variant):
    go = shutil.which("go")
    clang = shutil.which("clang")
    lipo = shutil.which("lipo") or "/usr/bin/lipo"
    if not go or not clang or not Path(lipo).is_file():
        raise SystemExit("macOS Universal 核心需要 Go、Clang 與 lipo。")

    output_root = ROOT / "native" / "build" / "macos"
    output_root.mkdir(parents=True, exist_ok=True)
    libraries = []
    for go_arch, clang_arch in [("arm64", "arm64"), ("amd64", "x86_64")]:
        directory = output_root / go_arch
        directory.mkdir(parents=True, exist_ok=True)
        output = directory / "libduanju_core.dylib"
        environment = os.environ.copy()
        environment.update(
            {
                "GOOS": "darwin",
                "GOARCH": go_arch,
                "CGO_ENABLED": "1",
                "CC": clang,
                "MACOSX_DEPLOYMENT_TARGET": MINIMUM_MACOS,
                "CGO_CFLAGS": f"-arch {clang_arch} -mmacosx-version-min={MINIMUM_MACOS}",
                "CGO_LDFLAGS": f"-arch {clang_arch} -mmacosx-version-min={MINIMUM_MACOS}",
            }
        )
        environment.setdefault("GOPROXY", "https://goproxy.cn,direct")
        environment.setdefault("GOSUMDB", "off")
        run(
            [
                go,
                "build",
                "-trimpath",
                "-buildmode=c-shared",
                "-ldflags=" + variant.linker_flags,
                "-o",
                str(output),
                "./bridge",
            ],
            cwd=ROOT / "native",
            env=environment,
        )
        libraries.append(output)

    universal = output_root / "libduanju_core.dylib"
    run([lipo, "-create", *(str(item) for item in libraries), "-output", str(universal)])
    architectures = subprocess.check_output(
        [lipo, "-archs", str(universal)], text=True
    ).split()
    if not {"arm64", "x86_64"}.issubset(set(architectures)):
        raise SystemExit(
            "Go 共用核心不是雙架構："
            + " ".join(architectures)
        )
    symbols = subprocess.check_output(["nm", "-gU", str(universal)], text=True)
    for symbol in ["_DuanjuRequest", "_DuanjuFree"]:
        if symbol not in symbols:
            raise SystemExit("macOS Go 核心缺少 FFI 入口：" + symbol)
    return universal


def locate_application():
    products = ROOT / "build" / "macos" / "Build" / "Products" / "Release"
    apps = sorted(products.glob("*.app"))
    if not apps:
        raise SystemExit("flutter build macos 沒有產生 .app。")
    # Flutter's generated project currently uses one Runner app target.
    return apps[0]


def main():
    import argparse
    from app_build import add_variant_argument

    parser = argparse.ArgumentParser(description="建立 macOS arm64 + x86_64 Universal DMG。")
    add_variant_argument(parser)
    options = parser.parse_args()
    variant = BuildVariant(options.all_sources)

    if platform.system() != "Darwin":
        raise SystemExit("macOS Universal 建置必須在 macOS + Xcode 環境執行。")
    flutter = shutil.which("flutter")
    if not flutter:
        raise SystemExit("請先安裝 Flutter SDK。")
    if not shutil.which("xcodebuild"):
        raise SystemExit("找不到 Xcode Command Line Tools。")

    ensure_macos_project(flutter)
    run([sys.executable, str(ROOT / "scripts" / "generate_brand_assets.py")])
    validate_macos_icon_assets()
    core = build_universal_core(variant)
    run([flutter, "pub", "get"])
    pod = shutil.which("pod")
    if pod and (ROOT / "macos" / "Podfile").is_file():
        run([pod, "install"], cwd=ROOT / "macos")

    environment = os.environ.copy()
    environment["MACOSX_DEPLOYMENT_TARGET"] = MINIMUM_MACOS
    environment["ARCHS"] = "arm64 x86_64"
    environment["ONLY_ACTIVE_ARCH"] = "NO"
    run(
        [
            flutter,
            "build",
            "macos",
            "--release",
            "--no-pub",
            *variant.flutter_arguments,
        ],
        env=environment,
    )

    application = locate_application()
    if application.name != "D TV.app":
        branded_application = application.with_name("D TV.app")
        if branded_application.exists():
            shutil.rmtree(branded_application)
        application.rename(branded_application)
        application = branded_application
    validate_application_identity(application)
    contents = application / "Contents"
    executable_name = plistlib.loads(
        (contents / "Info.plist").read_bytes()
    ).get("CFBundleExecutable")
    executable = contents / "MacOS" / executable_name

    app_archs = subprocess.check_output(["lipo", "-archs", str(executable)], text=True).split()
    if not {"arm64", "x86_64"}.issubset(set(app_archs)):
        raise SystemExit(
            "Flutter macOS 主程式不是 Universal："
            + " ".join(app_archs)
            + "。請檢查 Xcode ARCHS_STANDARD 與 ONLY_ACTIVE_ARCH 設定。"
        )

    frameworks = contents / "Frameworks"
    frameworks.mkdir(parents=True, exist_ok=True)
    bundled_core = frameworks / "libduanju_core.dylib"
    shutil.copy2(core, bundled_core)
    run(["install_name_tool", "-id", "@rpath/libduanju_core.dylib", str(bundled_core)])
    run(["codesign", "--force", "--sign", "-", str(bundled_core)])
    run(["codesign", "--force", "--deep", "--sign", "-", str(application)])
    run(["codesign", "--verify", "--deep", "--strict", str(application)])

    package_args = [
        sys.executable,
        str(ROOT / "scripts" / "package_release.py"),
        "--platform",
        "macos",
        *variant.arguments,
    ]
    run(package_args)
    print("macOS Universal .app:", application)
    print("macOS Universal Go core:", bundled_core)


if __name__ == "__main__":
    main()
