import 'models.dart';

enum PlaybackRecoveryAction { alternative, refresh, recreatePlayer, stop }

class PlaybackRecovery {
  // A source can expose many URLs, but a single player should only cycle
  // through them once. When exhausted, recreate the MPV instance instead of
  // resolving the same list again inside a player that may be stuck.
  static const maxAlternativeAttempts = 12;
  int _alternativeAttempts = 0;

  void reset() {
    _alternativeAttempts = 0;
  }

  PlaybackRecoveryAction next(
    PlaybackPlan plan, {
    bool allowPlayerRecreate = true,
  }) {
    if (plan.hasAlternative &&
        _alternativeAttempts < maxAlternativeAttempts) {
      _alternativeAttempts++;
      return PlaybackRecoveryAction.alternative;
    }
    return allowPlayerRecreate
        ? PlaybackRecoveryAction.recreatePlayer
        : PlaybackRecoveryAction.stop;
  }
}

class PlaybackHealth {
  static const stallTimeout = Duration(seconds: 12);
  Duration? _position;
  DateTime? _lastProgress;

  void reset() {
    _position = null;
    _lastProgress = null;
  }

  bool stalled({
    required Duration position,
    required bool intendedToPlay,
    required bool foreground,
    required DateTime now,
  }) {
    if (!intendedToPlay || !foreground || position != _position) {
      _position = position;
      _lastProgress = now;
      return false;
    }
    _lastProgress ??= now;
    return now.difference(_lastProgress!) >= stallTimeout;
  }
}
