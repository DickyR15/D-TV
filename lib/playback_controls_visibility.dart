/// What Back should do based on the current playback-control visibility.
enum PlaybackControlsBackAction { exitPlayback, hideControls }

PlaybackControlsBackAction playbackControlsBackAction({
  required bool controlsVisible,
}) => controlsVisible
    ? PlaybackControlsBackAction.hideControls
    : PlaybackControlsBackAction.exitPlayback;

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
