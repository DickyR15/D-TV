import 'dart:async';
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
  // libavformat's open-input failure is a definitive failure to initialize
  // the current media input. Switch routes immediately rather than waiting for
  // the startup watchdog. Keep generic MP4 header errors on the bounded grace
  // period because some MPV builds emit them transiently during stream probing.
  if (source == 'lavf' && message.contains('avformat_open_input() failed')) {
    return true;
  }
  return RegExp(
    r'failed to read the initialization vector|'
    r'hit eof while reading senc|partial file|failed to open|'
    r'moov atom not found|invalid data found when processing input',
  ).hasMatch(message);
}


/// Formats the change in a cumulative MPV frame counter between two samples.
/// A missing first sample is a baseline; a decreasing value means MPV reset it.
String playbackDiagnosticCounterDelta(String currentValue, int? previousValue) {
  final current = int.tryParse(currentValue.trim());
  if (current == null) return 'n/a';
  if (previousValue == null) return 'baseline';
  if (current < previousValue) return 'reset';
  return '+${current - previousValue}';
}

/// Select display-clock synchronization where the supplied logs show cadence judder.
String playbackVideoSyncModeFor({
  required bool windows,
  required bool android,
  required bool television,
}) => windows || (android && television) ? 'display-resample' : 'audio';

/// Fast, read-only MPV properties sampled during native playback.
/// The two-second cadence is diagnostic only and does not alter playback.
const playbackFramePacingSampleInterval = Duration(seconds: 2);

const playbackFramePacingProperties = <String>[
  'time-pos',
  'estimated-vf-fps',
  'display-fps',
  'estimated-display-fps',
  'display-sync-active',
  'vsync-ratio',
  'vo-delayed-frame-count',
  'frame-drop-count',
  'decoder-frame-drop-count',
  'avsync',
  'demuxer-cache-duration',
  'paused-for-cache',
];

const playbackFramePacingCounterProperties = <String>{
  'vo-delayed-frame-count',
  'frame-drop-count',
  'decoder-frame-drop-count',
};


/// Debounces persisted volume changes while keeping the latest requested value
/// available immediately for episode replacement. Zero is an intentional mute.
class PlaybackVolumeMemory {
  PlaybackVolumeMemory({
    required this.save,
    double initial = 100,
    this.debounce = const Duration(milliseconds: 500),
  }) : _value = initial.isFinite
           ? initial.clamp(0.0, 100.0).toDouble()
           : 100.0;

  final Future<void> Function(double value) save;
  final Duration debounce;

  Timer? _timer;
  double _value;
  double? _pending;
  bool _disposed = false;

  double get value => _value;

  void remember(double value) {
    if (_disposed || !value.isFinite) return;
    _value = value.clamp(0.0, 100.0).toDouble();
    _pending = _value;
    _timer?.cancel();
    _timer = Timer(debounce, flush);
  }

  void flush() {
    _timer?.cancel();
    _timer = null;
    final pending = _pending;
    _pending = null;
    if (pending == null) return;
    try {
      unawaited(save(pending).catchError((Object _) {}));
    } catch (_) {}
  }

  void dispose() {
    if (_disposed) return;
    flush();
    _disposed = true;
  }
}

/// Buffered media time ahead of the current position, used by ExoPlayer
/// diagnostics to separate network starvation from decode/frame pacing.
int? playbackBufferedAheadMilliseconds({
  required Duration position,
  required Duration? bufferedEnd,
}) {
  if (bufferedEnd == null) return null;
  return (bufferedEnd - position).inMilliseconds;
}
