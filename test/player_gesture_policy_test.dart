import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/player_gesture_policy.dart';

void main() {
  group('player touch gesture support', () {
    test('enables touch gestures on iOS phones and tablets', () {
      expect(
        supportsPlayerTouchGestures(
          android: false,
          ios: true,
          television: false,
        ),
        isTrue,
      );
    });

    test('enables touch gestures on Android mobile devices', () {
      expect(
        supportsPlayerTouchGestures(
          android: true,
          ios: false,
          television: false,
        ),
        isTrue,
      );
    });

    test('does not enable touch gestures on televisions', () {
      expect(
        supportsPlayerTouchGestures(
          android: true,
          ios: false,
          television: true,
        ),
        isFalse,
      );
      expect(
        supportsPlayerTouchGestures(
          android: false,
          ios: true,
          television: true,
        ),
        isFalse,
      );
    });
  });

  group('three-lane vertical swipe controls', () {
    test('left adjusts brightness, centre changes episode, right adjusts volume', () {
      // 10% left brightness / 80% centre episode / 10% right volume.
      expect(
        playerSwipeZoneForPosition(x: 39, width: 400),
        PlayerSwipeZone.brightness,
      );
      expect(
        playerSwipeZoneForPosition(x: 40, width: 400),
        PlayerSwipeZone.episode,
      );
      expect(
        playerSwipeZoneForPosition(x: 359, width: 400),
        PlayerSwipeZone.episode,
      );
      expect(
        playerSwipeZoneForPosition(x: 360, width: 400),
        PlayerSwipeZone.volume,
      );
      expect(
        playerSwipeZoneForPosition(x: 399, width: 400),
        PlayerSwipeZone.volume,
      );
    });

    test('invalid touch coordinates do not trigger a playback action', () {
      expect(
        playerSwipeZoneForPosition(x: -1, width: 400),
        PlayerSwipeZone.none,
      );
      expect(
        playerSwipeZoneForPosition(x: 401, width: 400),
        PlayerSwipeZone.none,
      );
      expect(
        playerSwipeZoneForPosition(x: 100, width: 0),
        PlayerSwipeZone.none,
      );
    });
  });

  group('vertical volume and brightness gesture policy', () {
    test('swiping up increases brightness or volume', () {
      expect(
        playerVerticalSwipeValue(
          initialValue: 0.5,
          originY: 300,
          currentY: 150,
          height: 600,
          maximumValue: 1,
        ),
        closeTo(0.9167, 0.001),
      );
      expect(
        playerVerticalSwipeValue(
          initialValue: 50,
          originY: 300,
          currentY: 150,
          height: 600,
          maximumValue: 100,
        ),
        closeTo(91.667, 0.01),
      );
    });

    test('swiping down decreases the level and clamps at the limits', () {
      expect(
        playerVerticalSwipeValue(
          initialValue: 50,
          originY: 150,
          currentY: 500,
          height: 600,
          maximumValue: 100,
        ),
        0,
      );
      expect(
        playerVerticalSwipeValue(
          initialValue: 90,
          originY: 500,
          currentY: 50,
          height: 600,
          maximumValue: 100,
        ),
        100,
      );
    });

    test('invalid viewport height keeps the current level', () {
      expect(
        playerVerticalSwipeValue(
          initialValue: 42,
          originY: 100,
          currentY: 0,
          height: 0,
          maximumValue: 100,
        ),
        42,
      );
    });
  });

  group('two finger pinch zoom policy', () {
    test('pinching apart zooms in proportionally', () {
      expect(
        playerPinchZoomScale(
          initialScale: 1,
          initialDistance: 100,
          currentDistance: 150,
        ),
        1.5,
      );
      expect(
        playerPinchZoomScale(
          initialScale: 1.5,
          initialDistance: 100,
          currentDistance: 200,
        ),
        3,
      );
    });

    test('pinching together zooms back towards original size', () {
      expect(
        playerPinchZoomScale(
          initialScale: 2,
          initialDistance: 200,
          currentDistance: 100,
        ),
        1,
      );
    });

    test('zoom remains within 1x to 3x and ignores invalid distances', () {
      expect(
        playerPinchZoomScale(
          initialScale: 2.5,
          initialDistance: 100,
          currentDistance: 500,
        ),
        3,
      );
      expect(
        playerPinchZoomScale(
          initialScale: 2,
          initialDistance: 0,
          currentDistance: 120,
        ),
        2,
      );
    });
  });
}
