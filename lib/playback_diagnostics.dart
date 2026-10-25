import 'dart:convert';

import 'package:crypto/crypto.dart';

String playbackDiagnosticId(String value) =>
    sha256.convert(utf8.encode(value)).toString().substring(0, 12);

String playbackDiagnosticUrl(String value) {
  final uri = Uri.tryParse(value);
  if (uri == null || !uri.hasScheme) return '[invalid-url]';
  if (uri.scheme == 'file') return '[local-file]';
  final local = uri.host == '127.0.0.1' || uri.host == 'localhost';
  final segments = uri.pathSegments;
  final safePath = segments.indexed
      .map((entry) {
        final (index, segment) = entry;
        if ((local && index == 0) || segment.length > 48) {
          return '[id=${playbackDiagnosticId(segment)}]';
        }
        return Uri.encodeComponent(segment);
      })
      .join('/');
  return '${uri.scheme}://${uri.host}${uri.hasPort ? ':${uri.port}' : ''}/$safePath';
}

String playbackDiagnosticError(Object error) {
  var text = error.toString().replaceAllMapped(
    RegExp(r'https?://[^\s"<>]+'),
    (match) => playbackDiagnosticUrl(match.group(0)!),
  );
  text = text.replaceAll(
    RegExp(
      r'(authorization|cookie|token|secret)\s*[:=].*',
      caseSensitive: false,
    ),
    '[redacted]',
  );
  text = text.replaceAll(RegExp(r'[a-fA-F0-9]{32,}'), '[redacted-id]');
  return text.length > 512 ? text.substring(0, 512) : text;
}

String playbackDiagnosticLogText(String text) {
  final trimmed = text.trim();
  if (trimmed.isEmpty) return '';
  return playbackDiagnosticError(trimmed);
}

bool playbackDiagnosticLogRelevant({
  required String level,
  required String prefix,
  required String text,
}) {
  final value = '$level $prefix $text'.toLowerCase();
  if (RegExp(r'\b(error|fatal|warn)\b').hasMatch(value)) return true;
  return RegExp(
    r'http|https|tcp|tls|ssl|dns|hls|m3u8|demux|ffmpeg|ffmpeg/demuxer|lavf|stream|cache|protocol|whitelist|connection|connect|timeout|eof|127\.0\.0\.1|localhost',
  ).hasMatch(value);
}

/// Identifies MPV demuxer failures that mean the current URL cannot provide a
/// usable media stream. These arrive as log messages on some Android builds,
/// without an accompanying player.stream.error event, so the startup watchdog
/// would otherwise wait 20 seconds before trying the next route.
bool playbackDiagnosticLogRequiresRecovery({
  required String level,
  required String prefix,
  required String text,
}) {
  if (!RegExp(r'\b(error|fatal)\b').hasMatch(level.toLowerCase())) {
    return false;
  }
  final source = prefix.toLowerCase();
  if (!(source.contains('demuxer') ||
      source == 'lavf' ||
      source == 'stream')) {
    return false;
  }
  final message = text.toLowerCase();
  return RegExp(
    r'error reading header|failed to read the initialization vector|'
    r'hit eof while reading senc|avformat_open_input\(\) failed|'
    r'partial file|failed to open|moov atom not found|'
    r'invalid data found when processing input',
  ).hasMatch(message);
}
