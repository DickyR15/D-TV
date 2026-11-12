import 'package:hgtv/playback_controls_visibility.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('remote Back exits video when controls are hidden', () {
    expect(
      playbackControlsBackAction(controlsVisible: false),
      PlaybackControlsBackAction.exitPlayback,
    );
  });

  test('remote Back hides controls but keeps video open when controls are visible', () {
    expect(
      playbackControlsBackAction(controlsVisible: true),
      PlaybackControlsBackAction.hideControls,
    );
  });

  test('remote Back exits playback while controls are disabled during loading', () {
    expect(
      playbackControlsBackAction(
        controlsVisible: false,
        enabled: false,
      ),
      PlaybackControlsBackAction.exitPlayback,
    );
    expect(
      playbackControlsBackAction(
        controlsVisible: true,
        enabled: false,
      ),
      PlaybackControlsBackAction.exitPlayback,
    );
  });

  test('remote Back exits playback while controls are disabled during loading', () {
    expect(
      playbackControlsBackAction(
        controlsVisible: false,
        enabled: false,
      ),
      PlaybackControlsBackAction.exitPlayback,
    );
    expect(
      playbackControlsBackAction(
        controlsVisible: true,
        enabled: false,
      ),
      PlaybackControlsBackAction.exitPlayback,
    );
  });

  test('re-applies hidden-control policy on every recovery generation', () {
    final visibility = PlaybackControlsVisibility();

    visibility.synchronize(generation: 1, showOnPlaybackReady: false);
    expect(visibility.suppressAutomaticReveal, isTrue);

    // A transient playing event alone is not an explicit user action, so the
    // policy remains suppressed until revealFromUser is called.
    visibility.synchronize(generation: 1, showOnPlaybackReady: false);
    expect(visibility.suppressAutomaticReveal, isTrue);

    // The next automatic retry has the same display preference but a new
    // generation; it must restore suppression after earlier playback events.
    visibility.revealFromUser();
    expect(visibility.suppressAutomaticReveal, isFalse);
    visibility.synchronize(generation: 2, showOnPlaybackReady: false);
    expect(visibility.suppressAutomaticReveal, isTrue);
  });

  test('normal playback may show controls, while explicit recovery hides them', () {
    final visibility = PlaybackControlsVisibility();

    visibility.synchronize(generation: 1, showOnPlaybackReady: true);
    expect(visibility.suppressAutomaticReveal, isFalse);

    visibility.synchronize(generation: 2, showOnPlaybackReady: false);
    expect(visibility.suppressAutomaticReveal, isTrue);

    visibility.revealFromUser();
    expect(visibility.suppressAutomaticReveal, isFalse);
  });

  test('TV playback starts without controls even when readiness requests them', () {
    expect(
      playbackControlsVisibleOnReady(television: true, requested: true),
      isFalse,
    );
    expect(
      playbackControlsVisibleOnReady(television: true, requested: false),
      isFalse,
    );
  });

  test('non-TV playback keeps its requested startup control behavior', () {
    expect(
      playbackControlsVisibleOnReady(television: false, requested: true),
      isTrue,
    );
    expect(
      playbackControlsVisibleOnReady(television: false, requested: false),
      isFalse,
    );
  });


  test('auto-advance hides controls only after a completed episode with a next episode', () {
    expect(
      playbackControlsShouldHideForAutoAdvance(
        autoAdvanceAvailable: true,
        completed: true,
      ),
      isTrue,
    );
    expect(
      playbackControlsShouldHideForAutoAdvance(
        autoAdvanceAvailable: true,
        completed: false,
      ),
      isFalse,
    );
    expect(
      playbackControlsShouldHideForAutoAdvance(
        autoAdvanceAvailable: false,
        completed: true,
      ),
      isFalse,
    );
  });


  test('auto-next gate handles pause before completion and transient resume', () {
    final gate = PlaybackAutoAdvanceVisibility();
    gate.synchronize(generation: 1, enabled: true);
    gate.onPlayingChanged(false);
    expect(gate.pauseTransitionPending, isTrue);
    expect(gate.suppressOverlay, isTrue);

    gate.onPlayingChanged(true);
    expect(gate.pauseTransitionPending, isTrue);
    expect(gate.suppressOverlay, isTrue);

    gate.onCompletedChanged(true);
    expect(gate.completionObserved, isTrue);
    expect(gate.suppressOverlay, isTrue);
    gate.resolvePauseTransition();
    gate.revealFromUser();
    expect(gate.suppressOverlay, isTrue);
  });

  test('a genuine pause restores controls if completion never arrives', () {
    final gate = PlaybackAutoAdvanceVisibility();
    gate.synchronize(generation: 4, enabled: true);
    gate.onPlayingChanged(false);
    expect(gate.suppressOverlay, isTrue);
    gate.resolvePauseTransition();
    expect(gate.pauseTransitionPending, isFalse);
    expect(gate.suppressOverlay, isFalse);
  });

  test('auto-next gate resets when a new playback generation starts', () {
    final gate = PlaybackAutoAdvanceVisibility();
    gate.synchronize(generation: 7, enabled: true);
    gate.onCompletedChanged(true);
    expect(gate.suppressOverlay, isTrue);
    gate.synchronize(generation: 8, enabled: true);
    expect(gate.completionObserved, isFalse);
    expect(gate.pauseTransitionPending, isFalse);
    expect(gate.suppressOverlay, isFalse);
  });

  test('auto-next gate is inert when there is no next episode', () {
    final gate = PlaybackAutoAdvanceVisibility();
    gate.synchronize(generation: 1, enabled: false);
    gate.onPlayingChanged(false);
    gate.onCompletedChanged(true);
    expect(gate.suppressOverlay, isFalse);
  });

  test('overlay does not infer pause controls during automatic advance', () {
    expect(
      playbackControlsOverlayVisible(
        requested: true,
        playing: false,
        buffering: false,
        automaticRevealSuppressed: false,
        panelOpen: false,
        autoAdvanceSuppressed: true,
      ),
      isFalse,
    );
    expect(
      playbackControlsOverlayVisible(
        requested: false,
        playing: false,
        buffering: false,
        automaticRevealSuppressed: false,
        panelOpen: false,
        autoAdvanceSuppressed: false,
      ),
      isTrue,
    );
  });
  group('playback progress repaint policy', () {
    test('hidden progress controls skip position and buffer repaint', () {
      expect(playbackProgressRefreshAllowed(controlsVisible: false, buffering: false), isFalse);
    });
    test('visible controls continue to refresh progress', () {
      expect(playbackProgressRefreshAllowed(controlsVisible: true, buffering: false), isTrue);
    });
    test('buffering and transient seek hint remain refreshable while hidden', () {
      expect(playbackProgressRefreshAllowed(controlsVisible: false, buffering: true), isTrue);
      expect(playbackProgressRefreshAllowed(controlsVisible: false, buffering: false, transientSeekHint: true), isTrue);
    });
  });
}
