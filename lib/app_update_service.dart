import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:path/path.dart' as path;
import 'package:path_provider/path_provider.dart';

class AppUpdateAsset {
  const AppUpdateAsset({
    required this.name,
    required this.downloadUrl,
    this.size = 0,
    this.sha256,
  });

  final String name;
  final String downloadUrl;
  final int size;
  final String? sha256;
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

  AppUpdateAsset? get windowsUpdaterAsset {
    for (final asset in assets) {
      if (asset.name.toLowerCase() == 'dtv-updater.exe') return asset;
    }
    return null;
  }

  AppUpdateAsset? get currentPlatformAsset =>
      assetForPlatform(Platform.operatingSystem);
}

class _RetryableUpdateException implements Exception {
  const _RetryableUpdateException(this.message);
  final String message;

  @override
  String toString() => message;
}

class AppUpdateService {
  const AppUpdateService();

  /// Only retries HttpExceptions that indicate a response body ended early.
  /// Ordinary HTTP status failures remain terminal and are not retried.
  static bool isRetryableResponseBodyError(Object error) {
    if (error is! HttpException) return false;
    final message = error.message.toLowerCase();
    return message.contains('connection closed') ||
        message.contains('premature') ||
        message.contains('unexpected eof') ||
        message.contains('unexpected end of') ||
        message.contains('end of file');
  }

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
    final partial = File(path.join(directory.path, '${safeName}.part'));
    final finalTarget = File(path.join(directory.path, safeName));
    if (await finalTarget.exists()) await finalTarget.delete();
    if (await partial.exists()) await partial.delete();

    Object? lastError;
    for (var attempt = 1; attempt <= 3; attempt++) {
      final client = HttpClient()
        ..connectionTimeout = const Duration(seconds: 20)
        ..idleTimeout = const Duration(seconds: 45)
        ..userAgent = 'D-TV-Updater';
      IOSink? sink;
      var readingResponseBody = false;
      try {
        final request = await client.getUrl(uri).timeout(
          const Duration(seconds: 30),
        );
        request.headers.set(
          HttpHeaders.acceptHeader,
          'application/octet-stream',
        );
        final response = await request.close().timeout(
          const Duration(seconds: 45),
        );
        if (response.statusCode != HttpStatus.ok) {
          await response.drain<void>();
          final message =
              '更新檔下載失敗，伺服器回應 ${response.statusCode}';
          if (response.statusCode == 408 ||
              response.statusCode == 429 ||
              response.statusCode >= 500) {
            throw _RetryableUpdateException(message);
          }
          throw HttpException(message, uri: uri);
        }

        sink = partial.openWrite();
        var receivedBytes = 0;
        readingResponseBody = true;
        await for (final chunk in response.timeout(
          const Duration(seconds: 45),
        )) {
          sink.add(chunk);
          receivedBytes += chunk.length;
        }
        readingResponseBody = false;
        await sink.flush();
        await sink.close();
        sink = null;

        final downloadedBytes = await partial.length();
        if (receivedBytes <= 0 ||
            downloadedBytes != receivedBytes ||
            (asset.size > 0 && downloadedBytes != asset.size)) {
          throw const FormatException('更新檔下載不完整，請重新檢查更新');
        }
        final expectedDigest = asset.sha256;
        if (expectedDigest != null) {
          final actualDigest = await sha256.bind(partial.openRead()).first;
          if (actualDigest.toString().toLowerCase() != expectedDigest) {
            throw const FormatException(
              '更新檔 SHA-256 校驗失敗，為保護程式完整性，已取消更新',
            );
          }
        }
        return await partial.rename(finalTarget.path);
      } catch (error) {
        lastError = error;
        try {
          await sink?.close();
        } catch (_) {}
        if (await partial.exists()) {
          try {
            await partial.delete();
          } catch (_) {}
        }
        final retryable = error is SocketException ||
            error is TimeoutException ||
            error is _RetryableUpdateException ||
            error is FormatException ||
            (readingResponseBody && isRetryableResponseBodyError(error));
        if (!retryable || attempt == 3) {
          throw StateError(
            '下載更新檔失敗（已嘗試 ${attempt}/3 次）：${error.toString()}',
          );
        }
        await Future<void>.delayed(Duration(seconds: attempt));
      } finally {
        client.close(force: true);
      }
    }
    throw StateError('下載更新檔失敗：${lastError ?? '未知錯誤'}');
  }

  Future<void> launchWindowsUpdater(
    File archive, {
    File? updaterFile,
    required String expectedVersion,
    required String releaseUrl,
  }) async {
    if (!Platform.isWindows) {
      throw UnsupportedError('Windows 更新只支援 Windows');
    }
    if (!await archive.exists() ||
        !archive.path.toLowerCase().endsWith('.zip') ||
        await archive.length() <= 0) {
      throw const FormatException('Windows 更新 ZIP 檔案無效或為空');
    }

    final releaseUri = Uri.tryParse(releaseUrl);
    if (releaseUri == null ||
        releaseUri.scheme != 'https' ||
        releaseUri.host != 'github.com' ||
        !releaseUri.path.startsWith('/DickyR15/D-TV/releases/tag/')) {
      throw const FormatException('Windows 更新頁網址無效');
    }

    final targetVersion = expectedVersion.trim().replaceFirst(
      RegExp(r'^[vV]'),
      '',
    );
    if (!RegExp(r'^\\d+\\.\\d+\\.\\d+$').hasMatch(targetVersion)) {
      throw const FormatException('Windows 更新版本號格式無效');
    }

    final executable = File(Platform.resolvedExecutable);
    if (!await executable.exists()) {
      throw const FileSystemException('找不到目前執行中的 DTV.exe');
    }
    final installDirectory = executable.parent.path;
    final executableName = path.basename(executable.path);
    if (executableName.toLowerCase() != 'dtv.exe') {
      throw const FormatException('目前執行檔不是受支援的 DTV.exe');
    }

    // Prefer the updater already shipped in the portable bundle. Older
    // installations (for example 1.0.7) bootstrap it from the Release asset.
    final bundledUpdater = File(
      path.join(installDirectory, 'DTV-Updater.exe'),
    );
    final File updaterSource;
    if (await bundledUpdater.exists()) {
      updaterSource = bundledUpdater;
    } else if (updaterFile != null && await updaterFile.exists()) {
      updaterSource = updaterFile;
    } else {
      throw const FileSystemException(
        '找不到 DTV-Updater.exe；請確認此版本 Release 已附上獨立更新器。',
      );
    }

    if (await updaterSource.length() < 1024) {
      throw const FormatException('DTV-Updater.exe 檔案無效或不完整');
    }

    final localAppData = Platform.environment['LOCALAPPDATA'];
    if (localAppData == null || localAppData.trim().isEmpty) {
      throw const FileSystemException('無法取得 Windows 使用者的 LOCALAPPDATA 路徑');
    }
    final updaterDirectory = Directory(
      path.join(localAppData, 'D TV', 'Updater'),
    );
    await updaterDirectory.create(recursive: true);
    final updaterTarget = File(
      path.join(updaterDirectory.path, 'DTV-Updater.exe'),
    );
    final updaterStaged = File(
      path.join(updaterDirectory.path, 'DTV-Updater.exe.new'),
    );

    if (await updaterStaged.exists()) await updaterStaged.delete();
    await updaterSource.copy(updaterStaged.path);
    if (await updaterTarget.exists()) await updaterTarget.delete();
    await updaterStaged.rename(updaterTarget.path);

    final archiveDigest = await sha256.bind(archive.openRead()).first;
    final digest = archiveDigest.toString().toLowerCase();
    final arguments = [
      '--process-id=' + pid.toString(),
      '--archive-path=' + archive.absolute.path,
      '--install-directory=' + executable.parent.absolute.path,
      '--executable-name=' + executableName,
      '--expected-version=' + targetVersion,
      '--archive-sha256=' + digest,
      '--release-url=' + releaseUri.toString(),
    ];

    // The helper is cached outside the install directory so it can replace
    // the bundled DTV-Updater.exe without attempting to overwrite itself.
    await Process.start(
      updaterTarget.path,
      arguments,
      mode: ProcessStartMode.detached,
    );

    if (updaterFile != null &&
        updaterFile.path != updaterSource.path &&
        await updaterFile.exists()) {
      try {
        await updaterFile.delete();
      } catch (_) {
        // The updater will clean the archive directory when the ZIP succeeds.
      }
    }
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
            sha256: _parseSha256(rawAsset['digest']),
          ),
        );
      }
    }

    final published =
        DateTime.tryParse(release['published_at'] as String? ?? '');
    return AppUpdateInfo(
      version: tag.trim(),
      title: title is String && title.trim().isNotEmpty ? title.trim() : tag.trim(),
      releaseUrl: url.toString(),
      notes: release['body'] is String ? release['body'] as String : '',
      publishedAt: published,
      assets: List.unmodifiable(assets),
    );
  }

  static String? _parseSha256(Object? value) {
    if (value is! String) return null;
    final text = value.trim();
    if (!text.toLowerCase().startsWith('sha256:')) return null;
    final digest = text.substring(7);
    if (digest.length != 64) return null;
    final isHex = digest.codeUnits.every(
      (unit) =>
          (unit >= 48 && unit <= 57) ||
          (unit >= 65 && unit <= 70) ||
          (unit >= 97 && unit <= 102),
    );
    return isHex ? digest.toLowerCase() : null;
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
