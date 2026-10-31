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
    if (!await archive.exists() || !archive.path.toLowerCase().endsWith('.zip')) {
      throw const FormatException('Windows 更新 ZIP 檔案無效');
    }

    final executable = File(Platform.resolvedExecutable);
    final installDirectory = executable.parent.path;
    final workDirectory = await Directory.systemTemp.createTemp('dtv-update-');
    final script = File(path.join(workDirectory.path, 'apply-update.ps1'));
    await script.writeAsString(r'''
param(
  [int]$ProcessId,
  [string]$ArchivePath,
  [string]$TargetDirectory,
  [string]$ExecutableName,
  [string]$WorkDirectory
)
$ErrorActionPreference = 'Stop'
try {
  $deadline = (Get-Date).AddMinutes(3)
  while ((Get-Process -Id $ProcessId -ErrorAction SilentlyContinue) -and (Get-Date) -lt $deadline) {
    Start-Sleep -Milliseconds 400
  }
  $stage = Join-Path $WorkDirectory 'package'
  New-Item -ItemType Directory -Path $stage -Force | Out-Null
  Expand-Archive -LiteralPath $ArchivePath -DestinationPath $stage -Force
  Copy-Item -Path (Join-Path $stage '*') -Destination $TargetDirectory -Recurse -Force
  $application = Join-Path $TargetDirectory $ExecutableName
  if (!(Test-Path -LiteralPath $application)) {
    throw '更新 ZIP 缺少 DTV.exe。'
  }
  Start-Process -FilePath $application -WorkingDirectory $TargetDirectory
}
catch {
  Add-Content -LiteralPath (Join-Path $WorkDirectory 'update-error.log') -Value $_.ToString()
}
finally {
  Start-Sleep -Seconds 2
  Remove-Item -LiteralPath $WorkDirectory -Recurse -Force -ErrorAction SilentlyContinue
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
        executable.uri.pathSegments.last,
        '-WorkDirectory',
        workDirectory.path,
      ],
      mode: ProcessStartMode.detached,
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
