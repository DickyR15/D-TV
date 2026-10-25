import 'models.dart';

enum PlaybackRecoveryAction { alternative, refresh, recreatePlayer, stop }

class PlaybackRecovery {
  PlaybackRecovery({DateTime Function()? now}) : _now = now ?? DateTime.now;

  // Try a bounded number of provider URLs, then force one fresh resolve.
  // Signed media URLs and CDN routes can expire or return incomplete MP4 data;
  // retrying the same session after exhaustion would only reuse stale URLs.
  static const maxAlternativeAttempts = 12;
  static const maxRefreshAttempts = 1;
  static const maxRecoveryDuration = Duration(seconds: 45);
  final DateTime Function() _now;
  DateTime? _startedAt;
  int _alternativeAttempts = 0;
  int _refreshAttempts = 0;

  void reset() {
    _alternativeAttempts = 0;
    _refreshAttempts = 0;
    _startedAt = null;
  }

  PlaybackRecoveryAction next(
    PlaybackPlan plan, {
    bool allowPlayerRecreate = true,
  }) {
    final now = _now();
    _startedAt ??= now;
    // A route may take up to the player-open timeout to fail. Bound total
    // automatic recovery as well as individual requests so the spinner is
    // eventually replaced by a retryable error instead of cycling forever.
    if (now.difference(_startedAt!) >= maxRecoveryDuration) {
      return PlaybackRecoveryAction.stop;
    }
    if (plan.hasAlternative &&
        _alternativeAttempts < maxAlternativeAttempts) {
      _alternativeAttempts++;
      return PlaybackRecoveryAction.alternative;
    }
    if (_refreshAttempts < maxRefreshAttempts) {
      _refreshAttempts++;
      _alternativeAttempts = 0;
      return PlaybackRecoveryAction.refresh;
    }
    return allowPlayerRecreate
        ? PlaybackRecoveryAction.recreatePlayer
        : PlaybackRecoveryAction.stop;
  }
}

class PlaybackHealth {
  static const stallTimeout = Duration(seconds: 10);
  // First-frame startup can take longer than an ordinary mid-playback stall,
  // especially when a CDN delays MP4 headers or the initial byte range.
  static const startupStallTimeout = Duration(seconds: 15);
  Duration? _position;
  Duration? _duration;
  Duration? _buffer;
  DateTime? _lastProgress;

  void reset() {
    _position = null;
    _duration = null;
    _buffer = null;
    _lastProgress = null;
  }

  bool stalled({
    required Duration position,
    Duration duration = Duration.zero,
    Duration buffer = Duration.zero,
    required bool intendedToPlay,
    required bool foreground,
    required DateTime now,
  }) {
    if (!intendedToPlay || !foreground) {
      _position = position;
      _duration = duration;
      _buffer = buffer;
      _lastProgress = now;
      return false;
    }

    // MPV can still be opening the demuxer while position remains zero.
    // A changing buffer or discovered duration is meaningful progress and
    // should not cause a healthy (but slow) stream to be discarded.
    if (position != _position ||
        duration != _duration ||
        buffer != _buffer) {
      _position = position;
      _duration = duration;
      _buffer = buffer;
      _lastProgress = now;
      return false;
    }

    _lastProgress ??= now;
    final waitingForInitialMedia =
        position == Duration.zero && duration == Duration.zero;
    final timeout = waitingForInitialMedia
        ? startupStallTimeout
        : stallTimeout;
    return now.difference(_lastProgress!) >= timeout;
  }
}
