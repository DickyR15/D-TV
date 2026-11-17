import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/playback_startup_visibility.dart';

void main() {
  test('dismiss startup overlay after visible video begins playing', () {
    expect(
      playbackStartupVideoIsVisible(
        playing: true,
        buffering: false,
        playIntent: true,
        foreground: true,
        width: 1920,
        height: 1080,
      ),
      isTrue,
    );
  });

  test('keep startup overlay while the video is still buffering', () {
    expect(
      playbackStartupVideoIsVisible(
        playing: true,
        buffering: true,
        playIntent: true,
        foreground: true,
        width: 1920,
        height: 1080,
      ),
      isFalse,
    );
  });

  test('do not dismiss before video dimensions are available', () {
    expect(
      playbackStartupVideoIsVisible(
        playing: true,
        buffering: false,
        playIntent: true,
        foreground: true,
        width: null,
        height: null,
      ),
      isFalse,
    );
  });

  test('do not dismiss when playback is paused by the user', () {
    expect(
      playbackStartupVideoIsVisible(
        playing: true,
        buffering: false,
        playIntent: false,
        foreground: true,
        width: 1920,
        height: 1080,
      ),
      isFalse,
    );
  });

  test('do not dismiss while app is not in the foreground', () {
    expect(
      playbackStartupVideoIsVisible(
        playing: true,
        buffering: false,
        playIntent: true,
        foreground: false,
        width: 1920,
        height: 1080,
      ),
      isFalse,
    );
  });
}
