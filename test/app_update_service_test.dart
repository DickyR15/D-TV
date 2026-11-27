import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/app_update_service.dart';

void main() {
  test('Windows TLS fallback only handles certificate verification failure', () {
    const error = HandshakeException('CERTIFICATE_VERIFY_FAILED');
    expect(AppUpdateService.shouldUseWindowsTLSFallback(error, windows: true), isTrue);
    expect(AppUpdateService.shouldUseWindowsTLSFallback(error, windows: false), isFalse);
    expect(AppUpdateService.shouldUseWindowsTLSFallback(const HandshakeException('connection reset'), windows: true), isFalse);
  });

  test('system curl preserves HTTPS and certificate validation', () {
    final args = AppUpdateService.windowsTLSArguments(
      Uri.parse(AppUpdateService.latestReleaseApi),
    );
    expect(args.first, '--disable');
    expect(args[args.indexOf('--proto') + 1], '=https');
    expect(args[args.indexOf('--proto-redir') + 1], '=https');
    expect(args, isNot(contains('--insecure')));
    expect(args, isNot(contains('-k')));
    expect(() => AppUpdateService.windowsTLSArguments(Uri.parse('http://github.com/')), throwsFormatException);
    expect(() => AppUpdateService.windowsTLSArguments(Uri.parse('https://example.com/')), throwsFormatException);
  });

  test('only premature response-body closures are retryable', () {
    expect(
      AppUpdateService.isRetryableResponseBodyError(
        const HttpException('Connection closed while receiving data'),
      ),
      isTrue,
    );
    expect(
      AppUpdateService.isRetryableResponseBodyError(
        const HttpException('Unexpected end of file'),
      ),
      isTrue,
    );
    expect(
      AppUpdateService.isRetryableResponseBodyError(
        const HttpException('更新檔下載失敗，伺服器回應 404'),
      ),
      isFalse,
    );
    expect(
      AppUpdateService.isRetryableResponseBodyError(
        const FormatException('檔案格式錯誤'),
      ),
      isFalse,
    );
  });

  test('version comparison ignores build metadata and recognizes newer releases', () {
    expect(AppUpdateService.isNewerVersion('1.0.0+340', 'v1.0.1'), isTrue);
    expect(AppUpdateService.isNewerVersion('1.0.0', '1.1.0'), isTrue);
    expect(AppUpdateService.isNewerVersion('1.0.0', '2.0.0'), isTrue);
    expect(AppUpdateService.isNewerVersion('1.0.1', '1.0.1'), isFalse);
    expect(AppUpdateService.isNewerVersion('1.0.1', '1.0.0'), isFalse);
    expect(AppUpdateService.isNewerVersion('unknown', '1.0.1'), isFalse);
  });

  test('latest public release is offered only when newer and points to GitHub', () {
    final info = AppUpdateService.parseRelease(
      const {
        'tag_name': 'v1.0.1',
        'name': 'D TV 1.0.1',
        'html_url': 'https://github.com/DickyR15/D-TV/releases/tag/v1.0.1',
        'body': '修正播放與同步問題',
        'published_at': '2026-10-30T00:00:00Z',
        'draft': false,
        'prerelease': false,
        'assets': [
          {
            'name': 'DTV-1.0.1-Android.apk',
            'browser_download_url':
                'https://github.com/DickyR15/D-TV/releases/download/v1.0.1/DTV-1.0.1-Android.apk',
            'size': 1024,
          },
          {
            'name': 'DTV-1.0.1-Windows-Portable.zip',
            'browser_download_url':
                'https://github.com/DickyR15/D-TV/releases/download/v1.0.1/DTV-1.0.1-Windows-Portable.zip',
            'size': 2048,
            'digest': 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
          },
          {
            'name': 'DTV-Updater.exe',
            'browser_download_url':
                'https://github.com/DickyR15/D-TV/releases/download/v1.0.1/DTV-Updater.exe',
            'size': 1048576,
            'digest': 'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
          },
          {
            'name': 'DTV-1.0.1-macOS.dmg',
            'browser_download_url':
                'https://github.com/DickyR15/D-TV/releases/download/v1.0.1/DTV-1.0.1-macOS.dmg',
            'size': 4096,
          },
          {
            'name': 'DTV-1.0.1-iOS.ipa',
            'browser_download_url':
                'https://github.com/DickyR15/D-TV/releases/download/v1.0.1/DTV-1.0.1-iOS.ipa',
            'size': 3072,
          },
        ],
      },
      currentVersion: '1.0.0+340',
    );

    expect(info, isNotNull);
    expect(info!.version, 'v1.0.1');
    expect(info.title, 'D TV 1.0.1');
    expect(info.notes, '修正播放與同步問題');
    expect(info.releaseUrl, contains('/releases/tag/v1.0.1'));
    expect(info.publishedAt, DateTime.utc(2026, 10, 30));
    expect(info.assetForPlatform('android')?.name, 'DTV-1.0.1-Android.apk');
    expect(info.assetForPlatform('windows')?.name, 'DTV-1.0.1-Windows-Portable.zip');
    expect(info.assetForPlatform('windows')?.sha256, 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa');
    expect(info.windowsUpdaterAsset?.name, 'DTV-Updater.exe');
    expect(info.windowsUpdaterAsset?.sha256, 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb');
    expect(info.assetForPlatform('macos')?.name, 'DTV-1.0.1-macOS.dmg');
    expect(info.assetForPlatform('ios')?.name, 'DTV-1.0.1-iOS.ipa');
  });

  test('same version, prerelease, draft or external URL never trigger notice', () {
    const base = {
      'tag_name': 'v1.0.1',
      'name': 'D TV 1.0.1',
      'html_url': 'https://github.com/DickyR15/D-TV/releases/tag/v1.0.1',
      'draft': false,
      'prerelease': false,
    };

    expect(
      AppUpdateService.parseRelease(base, currentVersion: '1.0.1'),
      isNull,
    );
    expect(
      AppUpdateService.parseRelease(
        {...base, 'prerelease': true},
        currentVersion: '1.0.0',
      ),
      isNull,
    );
    expect(
      AppUpdateService.parseRelease(
        {...base, 'draft': true},
        currentVersion: '1.0.0',
      ),
      isNull,
    );
    expect(
      AppUpdateService.parseRelease(
        {...base, 'html_url': 'https://example.com/update'},
        currentVersion: '1.0.0',
      ),
      isNull,
    );
  });  test('an explicitly ignored stable version is not notified again', () {
    const ignored = AppUpdateInfo(
      version: 'v1.0.1',
      title: 'D TV 1.0.1',
      releaseUrl: 'https://github.com/DickyR15/D-TV/releases/tag/v1.0.1',
    );
    const newer = AppUpdateInfo(
      version: 'v1.0.2',
      title: 'D TV 1.0.2',
      releaseUrl: 'https://github.com/DickyR15/D-TV/releases/tag/v1.0.2',
    );

    expect(
      AppUpdateService.shouldNotify(ignored, ignoredVersion: 'v1.0.1'),
      isFalse,
    );
    expect(
      AppUpdateService.shouldNotify(newer, ignoredVersion: 'v1.0.1'),
      isTrue,
    );
    expect(AppUpdateService.shouldNotify(null), isFalse);
    expect(
      AppUpdateService.shouldNotify(ignored, ignoredVersion: '  '),
      isTrue,
    );
  });

}
