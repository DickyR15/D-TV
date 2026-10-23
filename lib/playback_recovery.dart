import 'models.dart';

enum PlaybackRecoveryAction { alternative, refresh, stop }

class PlaybackRecovery {
  // A media source may expose more than three mirrors/quality URLs.
  // Exhaust those routes before forcing a fresh resolution, while keeping
  // retries bounded if a provider returns an unusually large candidate list.
  static const maxAlternativeAttempts = 12;
  int _alternativeAttempts = 0;
  bool _refreshed = false;

  void reset() {
    _alternativeAttempts = 0;
    _refreshed = false;
  }

  PlaybackRecoveryAction next(PlaybackPlan plan) {
    if (plan.hasAlternative &&
        _alternativeAttempts < maxAlternativeAttempts) {
      _alternativeAttempts++;
      return PlaybackRecoveryAction.alternative;
    }
    if (!_refreshed) {
      _refreshed = true;
      // A refreshed resolution can return new signed URLs, so allow the new
      // route set one bounded pass too.
      _alternativeAttempts = 0;
      return PlaybackRecoveryAction.refresh;
    }
    return PlaybackRecoveryAction.stop;
  }
}

class PlaybackHealth {
  static const stallTimeout = Duration(seconds: 20);
  Duration? _position;
  DateTime? _lastProgress;

  void reset() {
    _position = null;
    _lastProgress = null;
  }

  bool stalled({
    required Duration position,
    required bool playing,
    required bool foreground,
    required DateTime now,
  }) {
    if (!playing || !foreground || position != _position) {
      _position = position;
      _lastProgress = now;
      return false;
    }
    _lastProgress ??= now;
    return now.difference(_lastProgress!) >= stallTimeout;
  }
}
