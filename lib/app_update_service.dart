import 'dart:convert';
import 'dart:io';

import 'package:package_info_plus/package_info_plus.dart';

class AppUpdateInfo {
  const AppUpdateInfo({
    required this.version,
    required this.title,
    required this.releaseUrl,
    this.notes = '',
    this.publishedAt,
  });

  final String version;
  final String title;
  final String releaseUrl;
  final String notes;
  final DateTime? publishedAt;
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
      if (response.statusCode != HttpStatus.ok) return null;
      final content = await response
          .transform(utf8.decoder)
          .join()
          .timeout(const Duration(seconds: 8));
      final decoded = jsonDecode(content);
      if (decoded is! Map) return null;
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
    final published = DateTime.tryParse(release['published_at'] as String? ?? '');
    return AppUpdateInfo(
      version: tag.trim(),
      title: title is String && title.trim().isNotEmpty ? title.trim() : tag.trim(),
      releaseUrl: url.toString(),
      notes: release['body'] is String ? release['body'] as String : '',
      publishedAt: published,
    );
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
