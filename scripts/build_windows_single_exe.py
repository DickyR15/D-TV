import os
import re
import shutil
import subprocess
import tempfile
import zipfile
from pathlib import Path

root = Path(__file__).resolve().parents[1]
pubspec = (root / "pubspec.yaml").read_text(encoding="utf-8")
match = re.search(r"^version:\s*([\w.+-]+)\s*$", pubspec, re.MULTILINE)
if not match:
    raise SystemExit("pubspec.yaml 缺少合法版本號。")
version = match.group(1).split("+", 1)[0]
portable_zip = root / "dist" / "windows" / f"DTV-{version}-Windows-Portable.zip"
if not portable_zip.is_file() or portable_zip.stat().st_size == 0:
    raise SystemExit("找不到已驗證的 Windows Portable ZIP：" + str(portable_zip))
with zipfile.ZipFile(portable_zip, "r") as archive:
    corrupt = archive.testzip()
    if corrupt:
        raise SystemExit("Windows Portable ZIP 損壞：" + corrupt)

go, windres = shutil.which("go"), shutil.which("windres")
if not go:
    raise SystemExit("找不到 Go；無法建置 Windows 單檔啟動器。")
if not windres:
    raise SystemExit("找不到 windres；請先安裝 MSYS2 mingw-w64-x86_64-gcc。")
launcher_source = root / "scripts" / "windows_single_exe" / "launcher.go"
icon_source = root / "windows" / "runner" / "resources" / "app_icon.ico"
if not launcher_source.is_file() or not icon_source.is_file():
    raise SystemExit("缺少 Windows 單檔啟動器原始碼或 D TV 圖示。")

output_dir = root / "dist" / "windows"
output_dir.mkdir(parents=True, exist_ok=True)
output = output_dir / f"DTV-{version}-Windows.exe"
if output.exists():
    output.unlink()
with tempfile.TemporaryDirectory(prefix="dtv-single-exe-") as temporary:
    work = Path(temporary)
    shutil.copy2(launcher_source, work / "main.go")
    shutil.copy2(portable_zip, work / "payload.zip")
    shutil.copy2(icon_source, work / "dtv.ico")
    (work / "dtv.rc").write_text('IDI_ICON1 ICON "dtv.ico"\n', encoding="utf-8")
    subprocess.run([windres, "dtv.rc", "-o", "dtv_windows_amd64.syso"], cwd=work, check=True)
    env = os.environ.copy()
    env.update({"GO111MODULE": "off", "GOOS": "windows", "GOARCH": "amd64", "CGO_ENABLED": "0"})
    flags = f"-s -w -H=windowsgui -X main.appVersion={version}"
    subprocess.run([go, "build", "-trimpath", "-ldflags", flags, "-o", str(output), "."],
                   cwd=work, env=env, check=True)
if not output.is_file() or output.stat().st_size <= portable_zip.stat().st_size // 3:
    raise SystemExit("Windows 單檔 EXE 產出異常，請檢查內嵌封裝內容。")
print(f"Windows single-file launcher: {output} ({output.stat().st_size:,} bytes)")
