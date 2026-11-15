import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/playback_lifecycle_policy.dart';

void main() {
  test('inactive remains visible for transient system UI interruptions', () {
    expect(playbackLifecycleVisible(AppLifecycleState.resumed), isTrue);
    expect(playbackLifecycleVisible(AppLifecycleState.inactive), isTrue);
    expect(playbackLifecycleShouldPause(AppLifecycleState.inactive), isFalse);
  });

  test('actual background and detached states pause playback', () {
    for (final state in [
      AppLifecycleState.paused,
      AppLifecycleState.hidden,
      AppLifecycleState.detached,
    ]) {
      expect(playbackLifecycleVisible(state), isFalse);
      expect(playbackLifecycleShouldPause(state), isTrue);
    }
  });
  test('playback Back bypasses disabled controls while loading or in error', () {
    expect(
      playbackBackShouldExitImmediately(loading: true, hasError: false),
      isTrue,
    );
    expect(
      playbackBackShouldExitImmediately(loading: false, hasError: true),
      isTrue,
    );
    expect(
      playbackBackShouldExitImmediately(loading: false, hasError: false),
      isFalse,
    );
  });
  test('desktop mini-player starts as a normal resizable window', () {
    expect(
      playbackWindowStartsFullscreen(desktopMiniPlayer: true),
      isFalse,
    );
    expect(
      playbackWindowStartsFullscreen(desktopMiniPlayer: false),
      isTrue,
    );
  });

  test('Android auto-PiP background transition retains active playback', () {
    expect(
      playbackLifecycleShouldRetainAndroidPictureInPicture(
        android: true,
        systemSupported: true,
        playing: true,
        enabled: true,
      ),
      isTrue,
    );
    expect(
      playbackLifecycleShouldRetainAndroidPictureInPicture(
        android: true,
        systemSupported: true,
        playing: false,
        enabled: true,
      ),
      isFalse,
    );
    expect(
      playbackLifecycleShouldRetainAndroidPictureInPicture(
        android: true,
        systemSupported: true,
        playing: true,
        enabled: false,
      ),
      isFalse,
    );
    expect(
      playbackLifecycleShouldRetainAndroidPictureInPicture(
        android: true,
        systemSupported: false,
        playing: true,
        enabled: true,
      ),
      isFalse,
    );
    expect(
      playbackLifecycleShouldRetainAndroidPictureInPicture(
        android: true,
        systemSupported: true,
        playing: true,
        enabled: true,
        television: true,
      ),
      isFalse,
    );
    expect(
      playbackLifecycleShouldRetainAndroidPictureInPicture(
        android: false,
        systemSupported: true,
        playing: true,
        enabled: true,
      ),
      isFalse,
    );
  });

}
