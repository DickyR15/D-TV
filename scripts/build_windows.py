import argparse
import os
import shutil
import subprocess
import sys
from pathlib import Path

from app_build import BuildVariant, add_variant_argument

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
add_variant_argument(parser)
options = parser.parse_args()
variant = BuildVariant(options.all_sources)
env = os.environ.copy()
env.setdefault('GOPROXY', 'https://goproxy.cn,direct')
env.setdefault('GOSUMDB', 'off')
flutter = shutil.which('flutter')
if not flutter:
    raise SystemExit('請先將 Flutter SDK 的 bin 目錄加入 PATH。')

# Ensure the executable embeds the same D TV vector artwork used by Android.
subprocess.run(
    [sys.executable, str(root / 'scripts' / 'generate_brand_assets.py')],
    cwd=root, env=env, check=True,
)
subprocess.run([sys.executable, str(root / 'scripts' / 'build_native.py'), '--platform', 'windows', *variant.arguments],
               cwd=root, env=env, check=True)
subprocess.run([flutter, 'pub', 'get'], cwd=root, env=env, check=True)
subprocess.run([flutter, 'build', 'windows', '--release', '--no-pub', *variant.flutter_arguments], cwd=root, env=env, check=True)

# Reuse the exact multi-size ICO generated for the main D TV executable.
updater_icon = root / 'windows' / 'runner' / 'resources' / 'app_icon.ico'
if not updater_icon.is_file() or updater_icon.stat().st_size < 1024:
    raise SystemExit('DTV-Updater.exe 缺少主程式共用圖示 app_icon.ico。')

# Publish a self-contained helper so the app can update its own Windows bundle.
dotnet = shutil.which('dotnet')
if not dotnet:
    raise SystemExit('建置 Windows 獨立更新器需要 .NET 10 SDK，請先安裝並加入 PATH。')
updater_output = root / 'dist' / 'windows-updater'
if updater_output.exists():
    shutil.rmtree(updater_output)
subprocess.run(
    [
        dotnet, 'publish', str(root / 'windows_updater' / 'DTVUpdater.csproj'),
        '--configuration', 'Release',
        '--runtime', 'win-x64',
        '--self-contained', 'true',
        '-p:PublishSingleFile=true',
        '-p:IncludeNativeLibrariesForSelfExtract=true',
        '--output', str(updater_output),
    ],
    cwd=root, env=env, check=True,
)
updater = updater_output / 'DTV-Updater.exe'
if not updater.is_file() or updater.stat().st_size < 1024:
    raise SystemExit('DTV-Updater.exe 未成功產生或檔案不完整。')
bundle = root / 'build' / 'windows' / 'x64' / 'runner' / 'Release'
shutil.copy2(updater, bundle / 'DTV-Updater.exe')
subprocess.run([str(bundle / 'DTV-Updater.exe'), '--self-test'], cwd=root, env=env, check=True)

subprocess.run([sys.executable, str(root / 'scripts' / 'package_release.py'), '--platform', 'windows', *variant.arguments],
               cwd=root, env=env, check=True)
