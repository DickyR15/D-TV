import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:package_info_plus/package_info_plus.dart';
import 'package:path/path.dart' as path;
import 'package:path_provider/path_provider.dart';

class AppUpdateAsset {
  const AppUpdateAsset({
    required this.name,
    required this.downloadUrl,
    this.size = 0,
  });

  final String name;
  final String downloadUrl;
  final int size;
}

class AppUpdateInfo {
  const AppUpdateInfo({
    required this.version,
    required this.title,
    required this.releaseUrl,
    this.notes = '',
    this.publishedAt,
    this.assets = const [],
  });

  final String version;
  final String title;
  final String releaseUrl;
  final String notes;
  final DateTime? publishedAt;
  final List<AppUpdateAsset> assets;

  AppUpdateAsset? assetForPlatform(String operatingSystem) {
    final suffixes = switch (operatingSystem.toLowerCase()) {
      'android' => const ['-android.apk', '-android-universal.apk'],
      'windows' => const ['-windows-portable.zip'],
      'macos' => const ['-macos.dmg'],
      'ios' => const ['-ios.ipa'],
      _ => const <String>[],
    };
    for (final asset in assets) {
      final name = asset.name.toLowerCase();
      if (suffixes.any(name.endsWith)) return asset;
    }
    return null;
  }

  AppUpdateAsset? get currentPlatformAsset =>
      assetForPlatform(Platform.operatingSystem);
}

class AppUpdateService {
  const AppUpdateService();

  static const latestReleaseApi =
      'https://api.github.com/repos/DickyR15/D-TV/releases/latest';

  Future<AppUpdateInfo?> checkForUpdate({String? currentVersion}) async {
    final installedVersion =
        currentVersion ?? (await PackageInfo.fromPlatform()).version;
    final client = HttpClient()
      ..connectionTimeout = const Duration(seconds: 6)
      ..userAgent = 'D-TV/' + installedVersion;

    try {
      final request = await client
          .getUrl(Uri.parse(latestReleaseApi))
          .timeout(const Duration(seconds: 8));
      request.headers.set(
        HttpHeaders.acceptHeader,
        'application/vnd.github+json',
      );
      final response = await request.close().timeout(
        const Duration(seconds: 8),
      );
      if (response.statusCode != HttpStatus.ok) {
        await response.drain<void>();
        throw HttpException(
          'GitHub 更新服務回應 ' + response.statusCode.toString(),
          uri: Uri.parse(latestReleaseApi),
        );
      }
      final content = await response
          .transform(utf8.decoder)
          .join()
          .timeout(const Duration(seconds: 12));
      final decoded = jsonDecode(content);
      if (decoded is! Map) {
        throw const FormatException('GitHub 更新資訊格式無效');
      }
      return parseRelease(
        Map<String, dynamic>.from(decoded),
        currentVersion: installedVersion,
      );
    } finally {
      client.close(force: true);
    }
  }

  Future<File> downloadAsset(AppUpdateAsset asset) async {
    final uri = Uri.tryParse(asset.downloadUrl);
    if (uri == null ||
        uri.scheme != 'https' ||
        uri.host != 'github.com' ||
        !uri.path.contains('/releases/download/')) {
      throw const FormatException('更新檔下載網址無效');
    }
    final safeName = asset.name.replaceAll(RegExp(r'[^A-Za-z0-9._-]'), '_');
    if (safeName.isEmpty || safeName == '.' || safeName == '..') {
      throw const FormatException('更新檔名稱無效');
    }

    Directory baseDirectory;
    if (Platform.isAndroid) {
      baseDirectory = await getTemporaryDirectory();
    } else {
      try {
        baseDirectory =
            await getDownloadsDirectory() ?? await getTemporaryDirectory();
      } catch (_) {
        baseDirectory = await getTemporaryDirectory();
      }
    }
    final directory = Directory(path.join(baseDirectory.path, 'D TV Updates'));
    await directory.create(recursive: true);
    final target = File(path.join(directory.path, safeName));
    if (await target.exists()) await target.delete();

    final client = HttpClient()
      ..connectionTimeout = const Duration(seconds: 15)
      ..userAgent = 'D-TV-Updater';
    try {
      final request = await client.getUrl(uri).timeout(
        const Duration(seconds: 20),
      );
      request.headers.set(
        HttpHeaders.acceptHeader,
        'application/octet-stream',
      );
      final response = await request.close().timeout(
        const Duration(seconds: 30),
      );
      if (response.statusCode != HttpStatus.ok) {
        await response.drain<void>();
        throw HttpException(
          '更新檔下載失敗，伺服器回應 ' + response.statusCode.toString(),
          uri: uri,
        );
      }
      try {
        await response
            .pipe(target.openWrite())
            .timeout(const Duration(minutes: 5));
        final downloadedBytes = await target.length();
        if (downloadedBytes <= 0 ||
            (asset.size > 0 && downloadedBytes != asset.size)) {
          throw const FormatException('更新檔下載不完整，請重新檢查更新');
        }
      } catch (_) {
        if (await target.exists()) await target.delete();
        rethrow;
      }
      return target;
    } finally {
      client.close(force: true);
    }
  }

  Future<void> launchWindowsUpdater(File archive) async {
    if (!Platform.isWindows) {
      throw UnsupportedError('Windows 更新只支援 Windows');
    }
    if (!await archive.exists() ||
        !archive.path.toLowerCase().endsWith('.zip') ||
        await archive.length() <= 0) {
      throw const FormatException('Windows 更新 ZIP 檔案無效或為空');
    }

    final executable = File(Platform.resolvedExecutable);
    if (!await executable.exists()) {
      throw const FileSystemException('找不到目前執行中的 D TV.exe');
    }
    final installDirectory = executable.parent.path;
    final executableName = path.basename(executable.path);
    final workDirectory =
        await Directory.systemTemp.createTemp('dtv-update-');
    final script = File(path.join(workDirectory.path, 'apply-update.ps1'));
    final readyMarker = File(path.join(workDirectory.path, 'updater.ready'));
    final errorMarker = File(path.join(workDirectory.path, 'updater.error'));
    const logHint = r'%LOCALAPPDATA%\D TV\updater.log';

    await script.writeAsString(r'''
param(
  [int]$ProcessId,
  [string]$ArchivePath,
  [string]$TargetDirectory,
  [string]$ExecutableName,
  [string]$WorkDirectory
)
$ErrorActionPreference = 'Stop'
$logDirectory = Join-Path $env:LOCALAPPDATA 'D TV'
$logPath = Join-Path $logDirectory 'updater.log'
$readyPath = Join-Path $WorkDirectory 'updater.ready'
$errorPath = Join-Path $WorkDirectory 'updater.error'
$stage = Join-Path $WorkDirectory 'package'
$backupRoot = Join-Path $WorkDirectory 'backup'
$changedFiles = New-Object 'System.Collections.Generic.List[string]'
$createdFiles = New-Object 'System.Collections.Generic.List[string]'
$success = $false
$newProcess = $null

function Write-UpdateLog([string]$Message) {
  $stamp = Get-Date -Format 'yyyy-MM-dd HH:mm:ss'
  Add-Content -LiteralPath $logPath -Value "[$stamp] $Message" -Encoding UTF8
}

try {
  New-Item -ItemType Directory -Path $logDirectory -Force | Out-Null
  Write-UpdateLog 'Updater started.'
  if (!(Test-Path -LiteralPath $ArchivePath -PathType Leaf)) {
    throw '找不到已下載的更新 ZIP。'
  }
  if (!(Test-Path -LiteralPath $TargetDirectory -PathType Container)) {
    throw "找不到 D TV 安裝資料夾：$TargetDirectory"
  }

  New-Item -ItemType Directory -Path $stage -Force | Out-Null
  Expand-Archive -LiteralPath $ArchivePath -DestinationPath $stage -Force
  $stagedExecutable = Join-Path $stage $ExecutableName
  if (!(Test-Path -LiteralPath $stagedExecutable -PathType Leaf)) {
    throw '更新 ZIP 缺少 DTV.exe，已取消更新。'
  }
  foreach ($requiredFile in @('duanju_core.dll', 'flutter_windows.dll')) {
    if (!(Test-Path -LiteralPath (Join-Path $stage $requiredFile) -PathType Leaf)) {
      throw "更新 ZIP 缺少必要檔案：$requiredFile"
    }
  }
  $stagedFiles = @(Get-ChildItem -LiteralPath $stage -File -Recurse)
  if ($stagedFiles.Count -eq 0) {
    throw '更新 ZIP 沒有可安裝的檔案。'
  }

  # Signal readiness only after extraction and package validation have passed.
  [System.IO.File]::WriteAllText($readyPath, 'ready')
  Write-UpdateLog "Package validated; waiting for process $ProcessId to exit."

  $deadline = (Get-Date).AddMinutes(3)
  while ($null -ne (Get-Process -Id $ProcessId -ErrorAction SilentlyContinue)) {
    if ((Get-Date) -ge $deadline) {
      throw '等待 D TV 關閉逾時，尚未修改任何程式檔案。'
    }
    Start-Sleep -Milliseconds 300
  }

  foreach ($sourceFile in $stagedFiles) {
    $relative = $sourceFile.FullName.Substring($stage.Length).TrimStart([char[]]@('\','/'))
    $destination = Join-Path $TargetDirectory $relative
    $destinationDirectory = Split-Path -Parent $destination
    New-Item -ItemType Directory -Path $destinationDirectory -Force | Out-Null

    if (Test-Path -LiteralPath $destination -PathType Leaf) {
      $backupFile = Join-Path $backupRoot $relative
      $backupDirectory = Split-Path -Parent $backupFile
      New-Item -ItemType Directory -Path $backupDirectory -Force | Out-Null
      Copy-Item -LiteralPath $destination -Destination $backupFile -Force
      $changedFiles.Add($relative) | Out-Null
    } else {
      $createdFiles.Add($relative) | Out-Null
    }
    Copy-Item -LiteralPath $sourceFile.FullName -Destination $destination -Force
  }

  $installedExecutable = Join-Path $TargetDirectory $ExecutableName
  if (!(Test-Path -LiteralPath $installedExecutable -PathType Leaf)) {
    throw '更新後找不到 DTV.exe。'
  }
  if (!(Test-Path -LiteralPath (Join-Path $TargetDirectory 'duanju_core.dll') -PathType Leaf)) {
    throw '更新後找不到 duanju_core.dll。'
  }

  $newProcess = Start-Process -FilePath $installedExecutable -WorkingDirectory $TargetDirectory -PassThru
  Start-Sleep -Seconds 5
  $newProcess.Refresh()
  if ($newProcess.HasExited) {
    throw "新版 D TV 啟動後立即結束，結束代碼：$($newProcess.ExitCode)"
  }

  $success = $true
  Write-UpdateLog 'Update completed and the new D TV process is running.'
}
catch {
  $failure = $_.Exception.Message
  try {
    Write-UpdateLog "UPDATE FAILED: $failure"
  } catch {}

  if ($null -ne $newProcess) {
    try {
      $newProcess.Refresh()
      if (!$newProcess.HasExited) {
        Stop-Process -Id $newProcess.Id -Force -ErrorAction SilentlyContinue
      }
    } catch {}
  }

  foreach ($relative in $changedFiles) {
    try {
      $backupFile = Join-Path $backupRoot $relative
      $destination = Join-Path $TargetDirectory $relative
      if (Test-Path -LiteralPath $backupFile -PathType Leaf) {
        Copy-Item -LiteralPath $backupFile -Destination $destination -Force
      }
    } catch {
      try { Write-UpdateLog "ROLLBACK FAILED: $relative - $($_.Exception.Message)" } catch {}
    }
  }
  foreach ($relative in $createdFiles) {
    try {
      $destination = Join-Path $TargetDirectory $relative
      if (Test-Path -LiteralPath $destination -PathType Leaf) {
        Remove-Item -LiteralPath $destination -Force
      }
    } catch {
      try { Write-UpdateLog "CLEANUP FAILED: $relative - $($_.Exception.Message)" } catch {}
    }
  }

  $parentStillRunning = $null -ne (Get-Process -Id $ProcessId -ErrorAction SilentlyContinue)
  if (!$parentStillRunning) {
    try {
      $oldExecutable = Join-Path $TargetDirectory $ExecutableName
      if (Test-Path -LiteralPath $oldExecutable -PathType Leaf) {
        Start-Process -FilePath $oldExecutable -WorkingDirectory $TargetDirectory | Out-Null
      }
    } catch {
      try { Write-UpdateLog "OLD VERSION RESTART FAILED: $($_.Exception.Message)" } catch {}
    }
  }

  try {
    [System.IO.File]::WriteAllText($errorPath, $failure)
    if (!$parentStillRunning) {
      Add-Type -AssemblyName System.Windows.Forms
      [System.Windows.Forms.MessageBox]::Show(
        "D TV 更新失敗，已嘗試還原原版本。" + [Environment]::NewLine + [Environment]::NewLine + $failure + [Environment]::NewLine + [Environment]::NewLine + "詳細紀錄：" + $logPath,
        'D TV 更新',
        [System.Windows.Forms.MessageBoxButtons]::OK,
        [System.Windows.Forms.MessageBoxIcon]::Warning
      ) | Out-Null
    }
  } catch {}
}
finally {
  if ($success) {
    Start-Sleep -Seconds 2
    Remove-Item -LiteralPath $WorkDirectory -Recurse -Force -ErrorAction SilentlyContinue
  }
}
''', flush: true);

    await Process.start(
      'powershell.exe',
      [
        '-NoProfile',
        '-ExecutionPolicy',
        'Bypass',
        '-WindowStyle',
        'Hidden',
        '-File',
        script.path,
        '-ProcessId',
        pid.toString(),
        '-ArchivePath',
        archive.path,
        '-TargetDirectory',
        installDirectory,
        '-ExecutableName',
        executableName,
        '-WorkDirectory',
        workDirectory.path,
      ],
      mode: ProcessStartMode.detached,
    );

    // Keep D TV open until the detached updater has extracted and verified
    // the archive. A preparation failure never closes the current app.
    final deadline = DateTime.now().add(const Duration(seconds: 30));
    while (DateTime.now().isBefore(deadline)) {
      if (await readyMarker.exists()) return;
      if (await errorMarker.exists()) {
        final message = (await errorMarker.readAsString()).trim();
        throw StateError(
          message.isEmpty
              ? 'Windows 更新程序準備失敗。'
              : 'Windows 更新程序準備失敗：$message',
        );
      }
      await Future<void>.delayed(const Duration(milliseconds: 250));
    }
    throw TimeoutException(
      'Windows 更新程序未能在 30 秒內完成檔案驗證；D TV 尚未關閉。'
      '請檢查 $logHint。',
    );
  }

  Future<void> openMacDmg(File image) async {
    if (!Platform.isMacOS) {
      throw UnsupportedError('macOS 更新只支援 macOS');
    }
    if (!await image.exists() || !image.path.toLowerCase().endsWith('.dmg')) {
      throw const FormatException('macOS DMG 檔案無效');
    }
    await Process.start(
      'open',
      [image.path],
      mode: ProcessStartMode.detached,
    );
  }

  static AppUpdateInfo? parseRelease(
    Map<String, dynamic> release, {
    required String currentVersion,
  }) {
    if (release['draft'] == true || release['prerelease'] == true) return null;
    final tag = release['tag_name'];
    final title = release['name'];
    final url = Uri.tryParse(release['html_url'] as String? ?? '');
    if (tag is! String ||
        tag.trim().isEmpty ||
        url == null ||
        url.scheme != 'https' ||
        url.host != 'github.com' ||
        !url.path.contains('/releases/tag/')) {
      return null;
    }
    if (!isNewerVersion(currentVersion, tag)) return null;

    final assets = <AppUpdateAsset>[];
    final rawAssets = release['assets'];
    if (rawAssets is List) {
      for (final rawAsset in rawAssets) {
        if (rawAsset is! Map) continue;
        final name = rawAsset['name'];
        final downloadUrl = rawAsset['browser_download_url'];
        final assetUri = Uri.tryParse(
          downloadUrl is String ? downloadUrl : '',
        );
        if (name is! String ||
            name.trim().isEmpty ||
            name.contains('/') ||
            assetUri == null ||
            assetUri.scheme != 'https' ||
            assetUri.host != 'github.com' ||
            !assetUri.path.contains('/releases/download/')) {
          continue;
        }
        final sizeValue = rawAsset['size'];
        assets.add(
          AppUpdateAsset(
            name: name.trim(),
            downloadUrl: assetUri.toString(),
            size: sizeValue is int && sizeValue > 0 ? sizeValue : 0,
          ),
        );
      }
    }

    final published = DateTime.tryParse(release['published_at'] as String? ?? '');
    return AppUpdateInfo(
      version: tag.trim(),
      title: title is String && title.trim().isNotEmpty ? title.trim() : tag.trim(),
      releaseUrl: url.toString(),
      notes: release['body'] is String ? release['body'] as String : '',
      publishedAt: published,
      assets: List.unmodifiable(assets),
    );
  }

  static bool shouldNotify(
    AppUpdateInfo? update, {
    String? ignoredVersion,
  }) {
    if (update == null) return false;
    final ignored = ignoredVersion?.trim() ?? '';
    return ignored.isEmpty || update.version.trim() != ignored;
  }

  static bool isNewerVersion(String installed, String available) {
    final current = _versionParts(installed);
    final latest = _versionParts(available);
    if (current == null || latest == null) return false;
    for (var i = 0; i < 3; i++) {
      if (latest[i] != current[i]) return latest[i] > current[i];
    }
    return false;
  }

  static List<int>? _versionParts(String value) {
    final match = RegExp(r'(\d+)\.(\d+)(?:\.(\d+))?').firstMatch(value);
    if (match == null) return null;
    return [
      int.parse(match.group(1)!),
      int.parse(match.group(2)!),
      int.parse(match.group(3) ?? '0'),
    ];
  }
}
