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

}
