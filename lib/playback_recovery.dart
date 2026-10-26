import 'models.dart';

enum PlaybackRecoveryAction { alternative, refresh, recreatePlayer, stop }

class PlaybackRecovery {
  PlaybackRecovery({DateTime Function()? now}) : _now = now ?? DateTime.now;

  // Try a bounded number of provider URLs, then force one fresh resolve.
  // Signed media URLs and CDN routes can expire or return incomplete MP4 data;
  // retrying the same session after exhaustion would only reuse stale URLs.
  static const maxAlternativeAttempts = 12;
  static const maxRefreshAttempts = 1;
  // A candidate can spend several seconds in DNS/connect/TLS and then wait
  // for response headers. Keep enough time to try the available route list,
  // while still guaranteeing that automatic recovery eventually terminates.
  static const maxRecoveryDuration = Duration(seconds: 180);
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

/// Tracks whether a recovered stream has played smoothly for long enough to
/// start a fresh recovery budget. A buffer event, pause/background transition,
/// or lack of position progress restarts the stability window.
class PlaybackRecoveryStability {
  static const stablePlaybackDuration = Duration(seconds: 30);
  static const maxNoProgress = Duration(seconds: 2);

  DateTime? _healthySince;
  DateTime? _lastProgressAt;
  Duration? _lastPosition;

  void reset() {
    _healthySince = null;
    _lastProgressAt = null;
    _lastPosition = null;
  }

  bool observe({
    required Duration position,
    required bool playing,
    required bool buffering,
    required bool intendedToPlay,
    required bool foreground,
    required bool completed,
    required DateTime now,
  }) {
    final healthy = position > Duration.zero &&
        playing &&
        !buffering &&
        intendedToPlay &&
        foreground &&
        !completed;
    if (!healthy) {
      _healthySince = null;
      _lastPosition = position;
      _lastProgressAt = now;
      return false;
    }

    if (_lastPosition == null || position != _lastPosition) {
      _lastProgressAt = now;
    }
    _lastPosition = position;
    final lastProgress = _lastProgressAt;
    if (lastProgress == null ||
        now.difference(lastProgress) > maxNoProgress) {
      _healthySince = null;
      return false;
    }

    _healthySince ??= now;
    return now.difference(_healthySince!) >= stablePlaybackDuration;
  }
}

class PlaybackHealth {
  // The proxy detects 12 seconds of read inactivity and makes one bounded
  // Range-resume attempt. Fail over after a shorter total stall window so a
  // dead CDN cannot leave playback spinning for nearly a minute.
  static const stallTimeout = Duration(seconds: 32);
  // The first route gets a little more time for DNS/TLS startup. Once it
  // has failed, alternates must prove useful quickly instead of each holding
  // the spinner for another 20 seconds.
  static const startupStallTimeout = Duration(seconds: 12);
  static const alternativeStartupStallTimeout = Duration(seconds: 8);

  static Duration startupTimeoutForRoute(int routeIndex) =>
      routeIndex <= 0 ? startupStallTimeout : alternativeStartupStallTimeout;

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
    int routeIndex = 0,
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
        ? startupTimeoutForRoute(routeIndex)
        : stallTimeout;
    return now.difference(_lastProgress!) >= timeout;
  }
}
