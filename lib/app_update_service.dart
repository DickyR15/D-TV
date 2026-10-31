import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter/services.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:path_provider/path_provider.dart';

import 'app_build.dart';

class AppUpdateAsset {
  const AppUpdateAsset({
    required this.name,
    required this.downloadUrl,
    required this.size,
    this.digest,
  });

  final String name;
  final String downloadUrl;
  final int size;
  final String? digest;
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

  AppUpdateAsset? assetFor({
    required String platform,
    required String variant,
  }) {
    final candidates = switch (platform) {
      'android' => assets
          .where((asset) => asset.name.toLowerCase().endsWith('.apk'))
          .toList(),
      'windows' => assets
          .where((asset) {
            final name = asset.name.toLowerCase();
            return name.endsWith('.zip') && name.contains('windows');
          })
          .toList(),
      _ => <AppUpdateAsset>[],
    };
    if (candidates.isEmpty) return null;

    final variantMatches = candidates
        .where((asset) => asset.name.toLowerCase().contains(variant.toLowerCase()))
        .toList();
    var selected = variantMatches.isNotEmpty ? variantMatches : candidates;

    if (platform == 'android') {
      final v7a = selected
          .where((asset) => asset.name.toLowerCase().contains('armeabi-v7a'))
          .toList();
      if (v7a.isNotEmpty) selected = v7a;
    } else if (platform == 'windows') {
      final x64 = selected
          .where((asset) => asset.name.toLowerCase().contains('x64'))
          .toList();
      if (x64.isNotEmpty) selected = x64;
    }
    return selected.length == 1 ? selected.single : null;
  }
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
      ..userAgent = 'D-TV/$installedVersion';

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
        throw HttpException(
          'GitHub 更新服務回應 ${response.statusCode}',
          uri: Uri.parse(latestReleaseApi),
        );
      }
      final content = await response
          .transform(utf8.decoder)
          .join()
          .timeout(const Duration(seconds: 8));
      final decoded = jsonDecode(content);
      if (decoded is! Map) {
        throw const FormatException('GitHub 更新資料格式無效');
      }
      return parseRelease(
        Map<String, dynamic>.from(decoded),
        currentVersion: installedVersion,
      );
    } finally {
      client.close(force: true);
    }
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
      for (final row in rawAssets) {
        if (row is! Map) continue;
        final name = row['name'];
        final downloadUrl = Uri.tryParse(
          row['browser_download_url'] as String? ?? '',
        );
        final size = row['size'];
        if (name is! String ||
            name.trim().isEmpty ||
            downloadUrl == null ||
            downloadUrl.scheme != 'https' ||
            downloadUrl.host != 'github.com' ||
            !downloadUrl.path.contains('/releases/download/') ||
            size is! int ||
            size <= 0 ||
            size > 1024 * 1024 * 1024) {
          continue;
        }
        assets.add(
          AppUpdateAsset(
            name: name.trim(),
            downloadUrl: downloadUrl.toString(),
            size: size,
            digest: row['digest'] is String ? row['digest'] as String : null,
          ),
        );
      }
    }

    final published = DateTime.tryParse(
      release['published_at'] as String? ?? '',
    );
    return AppUpdateInfo(
      version: tag.trim(),
      title: title is String && title.trim().isNotEmpty ? title.trim() : tag.trim(),
      releaseUrl: url.toString(),
      notes: release['body'] is String ? release['body'] as String : '',
      publishedAt: published,
      assets: List.unmodifiable(assets),
    );
  }

  /// Manual checks must remain available even when startup checks are disabled
  /// or the current release was previously ignored.
  static bool shouldShowUpdate(
    AppUpdateInfo? update, {
    bool manual = false,
    bool autoCheckEnabled = true,
    String? ignoredVersion,
  }) {
    if (update == null) return false;
    if (manual) return true;
    if (!autoCheckEnabled) return false;
    return shouldNotify(update, ignoredVersion: ignoredVersion);
  }

  static bool shouldNotify(
    AppUpdateInfo? update, {
    String? ignoredVersion,
  }) {
    if (update == null) return false;
    final ignored = ignoredVersion?.trim() ?? '';
    return ignored.isEmpty || update.version.trim() != ignored;
  }

  Future<void> downloadAndApplyUpdate(
    AppUpdateInfo update, {
    required void Function(int receivedBytes, int? totalBytes) onProgress,
  }) async {
    final platform = Platform.isAndroid
        ? 'android'
        : Platform.isWindows
        ? 'windows'
        : '';
    final asset = update.assetFor(platform: platform, variant: appSlug);
    if (asset == null) {
      throw StateError('找不到適用於此裝置的 D TV 更新安裝檔');
    }

    final temporary = await getTemporaryDirectory();
    final updateDirectory = Directory(
      '${temporary.path}${Platform.pathSeparator}updates',
    );
    await updateDirectory.create(recursive: true);
    final fileName = platform == 'android'
        ? 'DTV-update.apk'
        : 'DTV-update.zip';
    final downloadFile = File(
      '${updateDirectory.path}${Platform.pathSeparator}$fileName',
    );

    final client = HttpClient()
      ..connectionTimeout = const Duration(seconds: 20)
      ..userAgent = 'D-TV-Updater';
    try {
      final request = await client
          .getUrl(Uri.parse(asset.downloadUrl))
          .timeout(const Duration(seconds: 30));
      request.headers.set(HttpHeaders.acceptHeader, 'application/octet-stream');
      final response = await request.close().timeout(
        const Duration(seconds: 30),
      );
      if (response.statusCode != HttpStatus.ok) {
        throw HttpException('下載更新檔失敗（HTTP ${response.statusCode}）');
      }

      final totalBytes = asset.size > 0
          ? asset.size
          : response.contentLength > 0
          ? response.contentLength
          : null;
      final sink = downloadFile.openWrite();
      var receivedBytes = 0;
      try {
        await for (final chunk in response.timeout(
          const Duration(seconds: 90),
        )) {
          sink.add(chunk);
          receivedBytes += chunk.length;
          onProgress(receivedBytes, totalBytes);
        }
        await sink.flush();
      } finally {
        await sink.close();
      }

      if (receivedBytes <= 0 ||
          (asset.size > 0 && receivedBytes != asset.size)) {
        throw const FormatException('更新檔下載不完整，請重新下載');
      }
      final expectedDigest = asset.digest;
      if (expectedDigest != null && expectedDigest.startsWith('sha256:')) {
        final actualDigest = await sha256.bind(downloadFile.openRead()).first;
        if (actualDigest.toString().toLowerCase() !=
            expectedDigest.substring('sha256:'.length).toLowerCase()) {
          throw const FormatException('更新檔完整性驗證失敗，已取消安裝');
        }
      }

      if (platform == 'android') {
        await const MethodChannel('duanju/device').invokeMethod<void>(
          'installApk',
          {'path': downloadFile.path},
        );
      } else if (platform == 'windows') {
        await _scheduleWindowsUpdate(downloadFile);
      } else {
        throw UnsupportedError('此平台尚未支援自動更新');
      }
    } catch (_) {
      if (await downloadFile.exists()) {
        await downloadFile.delete().catchError((Object _) => downloadFile);
      }
      rethrow;
    } finally {
      client.close(force: true);
    }
  }

  Future<void> _scheduleWindowsUpdate(File archive) async {
    final executable = File(Platform.resolvedExecutable);
    final targetDirectory = executable.parent;
    final probe = File(
      '${targetDirectory.path}${Platform.pathSeparator}.dtv-update-check-${pid}',
    );
    try {
      await probe.writeAsString('D TV update permission check');
      await probe.delete();
    } on FileSystemException {
      throw StateError(
        'D TV 安裝資料夾沒有寫入權限，請將免安裝版放在可寫入的資料夾後再更新',
      );
    }

    final scriptPath =
        '${Directory.systemTemp.path}${Platform.pathSeparator}'
        'dtv-update-${pid}-${DateTime.now().microsecondsSinceEpoch}.ps1';
    final script = File(scriptPath);
    await script.writeAsString(
      r'''
param(
  [Parameter(Mandatory=$true)][string]$Archive,
  [Parameter(Mandatory=$true)][string]$Target,
  [Parameter(Mandatory=$true)][string]$Executable,
  [Parameter(Mandatory=$true)][int]$ProcessId,
  [Parameter(Mandatory=$true)][string]$ScriptPath
)
$ErrorActionPreference = 'Stop'
$work = Join-Path $env:TEMP ('DTV-update-' + [guid]::NewGuid().ToString('N'))
$stage = Join-Path $work 'stage'
$backup = Join-Path $work 'backup'
$changed = New-Object System.Collections.Generic.List[string]
New-Item -ItemType Directory -Path $stage -Force | Out-Null
New-Item -ItemType Directory -Path $backup -Force | Out-Null
try {
  Wait-Process -Id $ProcessId -ErrorAction SilentlyContinue
  Expand-Archive -LiteralPath $Archive -DestinationPath $stage -Force
  $exeName = [IO.Path]::GetFileName($Executable)
  $exeCandidate = Get-ChildItem -LiteralPath $stage -Filter $exeName -File -Recurse |
    Select-Object -First 1
  if ($null -eq $exeCandidate) {
    throw ('更新壓縮檔缺少執行檔：' + $exeName)
  }
  $sourceRoot = $exeCandidate.Directory.FullName
  $files = Get-ChildItem -LiteralPath $sourceRoot -File -Recurse -Force
  foreach ($file in $files) {
    $relative = $file.FullName.Substring($sourceRoot.Length).TrimStart([char]92, [char]47)
    $destination = Join-Path $Target $relative
    $destinationDirectory = Split-Path -Parent $destination
    New-Item -ItemType Directory -Path $destinationDirectory -Force | Out-Null
    if (Test-Path -LiteralPath $destination -PathType Leaf) {
      $backupFile = Join-Path $backup $relative
      New-Item -ItemType Directory -Path (Split-Path -Parent $backupFile) -Force | Out-Null
      Copy-Item -LiteralPath $destination -Destination $backupFile -Force
      $changed.Add($relative)
    }
    Copy-Item -LiteralPath $file.FullName -Destination $destination -Force
  }
  Start-Process -FilePath $Executable -WorkingDirectory $Target
  Remove-Item -LiteralPath $Archive -Force -ErrorAction SilentlyContinue
} catch {
  for ($i = $changed.Count - 1; $i -ge 0; $i--) {
    $relative = $changed[$i]
    $backupFile = Join-Path $backup $relative
    $destination = Join-Path $Target $relative
    if (Test-Path -LiteralPath $backupFile -PathType Leaf) {
      Copy-Item -LiteralPath $backupFile -Destination $destination -Force
    }
  }
  $message = 'D TV 自動更新失敗，已嘗試還原已覆蓋的檔案。' + [Environment]::NewLine + $_.Exception.Message
  try {
    Add-Type -AssemblyName System.Windows.Forms
    [System.Windows.Forms.MessageBox]::Show($message, 'D TV 更新失敗') | Out-Null
  } catch {}
  try {
    Start-Process -FilePath $Executable -WorkingDirectory $Target
  } catch {}
} finally {
  Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
  Remove-Item -LiteralPath $ScriptPath -Force -ErrorAction SilentlyContinue
}
''',
      flush: true,
    );

    try {
      await Process.start(
        'powershell.exe',
        [
          '-NoLogo',
          '-NoProfile',
          '-NonInteractive',
          '-WindowStyle',
          'Hidden',
          '-ExecutionPolicy',
          'Bypass',
          '-File',
          scriptPath,
          '-Archive',
          archive.path,
          '-Target',
          targetDirectory.path,
          '-Executable',
          executable.path,
          '-ProcessId',
          pid.toString(),
          '-ScriptPath',
          scriptPath,
        ],
        mode: ProcessStartMode.detached,
      );
    } catch (_) {
      await script.delete().catchError((Object _) => script);
      rethrow;
    }

    // The detached updater waits for this process to exit, replaces the
    // portable bundle, and starts the updated executable.
    exit(0);
  }

  static bool isPlatformUpdateSupported() => Platform.isAndroid || Platform.isWindows;

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
