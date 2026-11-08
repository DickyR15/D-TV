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
