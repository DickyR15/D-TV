import 'package:flutter/painting.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/player_fullscreen_layout_policy.dart';

void main() {
  group('playerTopSafeAreaEnabled', () {
    test('removes the top inset only for iOS fullscreen playback', () {
      expect(
        playerTopSafeAreaEnabled(
          ios: true,
          television: false,
          fullscreen: true,
          mobile: true,
        ),
        isFalse,
      );
    });

    test('keeps the top inset for regular iOS playback', () {
      expect(
        playerTopSafeAreaEnabled(
          ios: true,
          television: false,
          fullscreen: false,
          mobile: true,
        ),
        isTrue,
      );
    });

    test('does not change Android fullscreen safe-area behavior', () {
      expect(
        playerTopSafeAreaEnabled(
          ios: false,
          television: false,
          fullscreen: true,
          mobile: true,
        ),
        isTrue,
      );
    });
  });

  group('playerVideoFit', () {
    test('fills iOS fullscreen portrait video to the screen edges', () {
      expect(
        playerVideoFit(ios: true, fullscreen: true, aspectRatio: 9 / 16),
        BoxFit.cover,
      );
    });

    test('fills iOS fullscreen landscape video to the screen edges', () {
      expect(
        playerVideoFit(ios: true, fullscreen: true, aspectRatio: 16 / 9),
        BoxFit.cover,
      );
    });

    test('does not change other platforms', () {
      expect(
        playerVideoFit(ios: false, fullscreen: true, aspectRatio: 9 / 16),
        BoxFit.contain,
      );
    });
  });

  group('playerVideoOverscan', () {
    test('adds slight overscan only to iOS landscape fullscreen', () {
      expect(
        playerVideoOverscan(ios: true, fullscreen: true, aspectRatio: 16 / 9),
        1.06,
      );
    });

    test('does not crop portrait video or other platforms', () {
      expect(
        playerVideoOverscan(ios: true, fullscreen: true, aspectRatio: 9 / 16),
        1.0,
      );
      expect(
        playerVideoOverscan(ios: false, fullscreen: true, aspectRatio: 16 / 9),
        1.0,
      );
    });
  });
}
