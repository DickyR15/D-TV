import 'dart:io';

import 'package:hgtv/diary_service.dart';
import 'package:hgtv/playback_diagnostics.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('localhost session, credentials and query never enter playback logs', () {
    const token = '0123456789abcdef0123456789abcdef0123456789abcdef';
    final safe = playbackDiagnosticUrl(
      'http://user:password@127.0.0.1:51037/$token/media.m3u8?token=secret#private',
    );
    expect(safe, contains('id=${playbackDiagnosticId(token)}'));
    expect(safe, contains('media.m3u8'));
    for (final secret in [token, 'user', 'password', 'secret', 'private']) {
      expect(safe, isNot(contains(secret)));
    }
    final error = playbackDiagnosticError(
      'failed https://user:password@cdn.test/media.ts?token=secret authorization: Bearer private',
    );
    expect(error, contains('cdn.test/media.ts'));
    expect(error, isNot(contains('secret')));
    expect(error, isNot(contains('private')));
    expect(playbackDiagnosticUrl('file:///private/local.mp4'), '[local-file]');
  });

  test('mpv logs are filtered and redacted for playback diagnosis', () {
    expect(
      playbackDiagnosticLogRelevant(
        level: 'info',
        prefix: 'ffmpeg/demuxer',
        text: 'Opening http://127.0.0.1:51037/media.m3u8',
      ),
      isTrue,
    );
    expect(
      playbackDiagnosticLogRelevant(
        level: 'info',
        prefix: 'statusline',
        text: 'AV: 00:00:01',
      ),
      isFalse,
    );
    final text = playbackDiagnosticLogText(
      'Failed http://127.0.0.1:51037/0123456789abcdef0123456789abcdef0123456789abcdef/media.m3u8?token=secret authorization: Bearer private',
    );
    expect(text, contains('127.0.0.1:51037/[id='));
    expect(text, contains('media.m3u8'));
    expect(text, isNot(contains('0123456789abcdef')));
    expect(text, isNot(contains('secret')));
    expect(text, isNot(contains('private')));
  });

  test('fatal MPV demuxer failures trigger fast route recovery', () {
    for (final sample in [
      (
        level: 'error',
        prefix: 'ffmpeg/demuxer',
        text: 'failed to read the initialization vector; Hit EOF while reading senc',
      ),
      (
        level: 'error',
        prefix: 'ffmpeg/demuxer',
        text: 'stream 1, offset 0x3277e: partial file',
      ),
      (
        level: 'error',
        prefix: 'stream',
        text: 'Failed to open http://127.0.0.1:35309/media.mp4',
      ),
      (
        level: 'error',
        prefix: 'lavf',
        text: 'avformat_open_input() failed',
      ),
    ]) {
      expect(
        playbackDiagnosticLogRequiresRecovery(
          level: sample.level,
          prefix: sample.prefix,
          text: sample.text,
        ),
        isTrue,
        reason: sample.text,
      );
    }
  });

  test('benign MPV property errors do not force route switching', () {
    expect(
      playbackDiagnosticLogRequiresRecovery(
        level: 'error',
        prefix: 'ffmpeg/demuxer',
        text: 'mov,mp4: error reading header',
      ),
      isFalse,
      reason: 'a generic header message can precede a successful first frame',
    );
    expect(
      playbackDiagnosticLogRequiresRecovery(
        level: 'error',
        prefix: 'lavf',
        text: 'avformat_open_input() failed',
      ),
      isTrue,
      reason: 'an explicit libavformat open failure should switch routes immediately',
    );
    expect(
      playbackDiagnosticLogRequiresRecovery(
        level: 'error',
        prefix: 'media_kit',
        text: 'property not found _setProperty(osc, 1)',
      ),
      isFalse,
    );
    expect(
      playbackDiagnosticLogRequiresRecovery(
        level: 'warn',
        prefix: 'ffmpeg/demuxer',
        text: 'error reading header',
      ),
      isFalse,
    );
    expect(
      playbackDiagnosticLogRequiresRecovery(
        level: 'error',
        prefix: 'vd',
        text: 'hardware decoder property rejected',
      ),
      isFalse,
    );
  });

  test('mpv warning and network keywords are relevant', () {
    expect(
      playbackDiagnosticLogRelevant(
        level: 'warn',
        prefix: 'cplayer',
        text: 'failed to open stream',
      ),
      isTrue,
    );
    expect(
      playbackDiagnosticLogRelevant(
        level: 'info',
        prefix: 'cache',
        text: 'EOF reached',
      ),
      isTrue,
    );
  });

  test(
    'diary copies bounded native stream events and skips partial records',
    () async {
      final directory = await Directory.systemTemp.createTemp('stream-diary-');
      addTearDown(() async {
        DiaryService.nativeLogPath = null;
        DiaryService.clear();
        await directory.delete(recursive: true);
      });
      final file = File('${directory.path}/app.log');
      DiaryService.nativeLogPath = file.path;
      await file.writeAsString(
        '{"event":"stream.session_cancel","message":"reason=release"}\n',
      );
      await file.rename('${file.path}.1');
      await file.writeAsString(
        '${'x' * (128 * 1024)}\n'
        '{"event":"catalog.other","message":"not a playback event"}\n'
        '{"event":"stream.local_response","httpStatus":200}\n'
        '{"event":"stream.incomplete"',
      );
      await DiaryService.refreshNativeLogs();
      expect(DiaryService.fullText, contains('stream.session_cancel'));
      expect(DiaryService.fullText, contains('stream.local_response'));
      expect(DiaryService.fullText, isNot(contains('catalog.other')));
      expect(DiaryService.fullText, isNot(contains('stream.incomplete')));
      expect(DiaryService.entries.last, contains('stream.local_response'));
    },
  );
}
