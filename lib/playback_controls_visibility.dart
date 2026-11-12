/// TV video surfaces stay clean on entry even if a caller requests controls
/// on readiness. Touch-first surfaces may retain their existing behavior.
bool playbackControlsVisibleOnReady({
  required bool television,
  required bool requested,
}) => !television && requested;

/// What Back should do based on the current playback-control visibility.
enum PlaybackControlsBackAction { exitPlayback, hideControls }

PlaybackControlsBackAction playbackControlsBackAction({
  required bool controlsVisible,
  bool enabled = true,
}) {
  // Loading/error states disable the on-screen controls, but must never
  // disable the system/remote Back action that exits the playback route.
  if (!enabled) return PlaybackControlsBackAction.exitPlayback;
  return controlsVisible
      ? PlaybackControlsBackAction.hideControls
      : PlaybackControlsBackAction.exitPlayback;
}

/// Controls whether playback events may automatically reveal the control overlay.
///
/// A new playback generation must reapply the visibility policy even when
/// [showOnPlaybackReady] stays false across consecutive recovery attempts.
class PlaybackControlsVisibility {
  int? _generation;
  bool? _showOnPlaybackReady;
  bool _suppressAutomaticReveal = false;

  bool get suppressAutomaticReveal => _suppressAutomaticReveal;

  void synchronize({
    required int generation,
    required bool showOnPlaybackReady,
  }) {
    if (_generation == generation &&
        _showOnPlaybackReady == showOnPlaybackReady) {
      return;
    }
    _generation = generation;
    _showOnPlaybackReady = showOnPlaybackReady;
    _suppressAutomaticReveal = !showOnPlaybackReady;
  }

  /// A real user action is allowed to reveal controls during recovery.
  void revealFromUser() {
    _suppressAutomaticReveal = false;
  }
}

/// Keeps the desktop control overlay hidden at the end of an episode when
/// automatic advance is about to replace the media. The next episode receives
/// its own playback generation and normal visibility policy.
bool playbackControlsShouldHideForAutoAdvance({
  required bool autoAdvanceAvailable,
  required bool completed,
}) => autoAdvanceAvailable && completed;


/// Coordinates MPV's short playing=false transition with a later completed
/// event during Windows automatic episode advance. A new playback generation
/// always starts with a clean transition state.
class PlaybackAutoAdvanceVisibility {
  int? _generation;
  bool _enabled = false;
  bool _completed = false;
  bool _pausePending = false;

  bool get suppressOverlay => _enabled && (_completed || _pausePending);
  bool get completionObserved => _completed;
  bool get pauseTransitionPending => _pausePending;

  void synchronize({required int generation, required bool enabled}) {
    if (_generation == generation && _enabled == enabled) return;
    _generation = generation;
    _enabled = enabled;
    _completed = false;
    _pausePending = false;
  }

  void onPlayingChanged(bool playing) {
    if (_enabled && !_completed && !playing) _pausePending = true;
  }

  void onCompletedChanged(bool completed) {
    if (_enabled && completed) {
      _completed = true;
      _pausePending = false;
    }
  }

  void resolvePauseTransition() {
    if (!_completed) _pausePending = false;
  }

  void revealFromUser() {
    if (!_completed) _pausePending = false;
  }
}

bool playbackControlsOverlayVisible({
  required bool requested,
  required bool playing,
  required bool buffering,
  required bool automaticRevealSuppressed,
  required bool panelOpen,
  required bool autoAdvanceSuppressed,
}) {
  if (autoAdvanceSuppressed) return false;
  return requested ||
      (!playing && !automaticRevealSuppressed) ||
      (buffering && !automaticRevealSuppressed) ||
      panelOpen;
}


/// Hidden playback controls should not rebuild their progress UI for every
/// position/buffer event. Buffering and transient seek hints remain visible.
bool playbackProgressRefreshAllowed({
  required bool controlsVisible,
  required bool buffering,
  bool transientSeekHint = false,
}) => controlsVisible || buffering || transientSeekHint;
