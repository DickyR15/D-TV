import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/desktop_mini_window_policy.dart';

void main() {
  test('desktop mini-player is disabled on Windows and secondary players', () {
    expect(
      supportsDesktopMiniPlayer(
        isWindows: true,
        isMacOS: false,
        secondaryPlayer: false,
      ),
      isFalse,
    );
    expect(
      supportsDesktopMiniPlayer(
        isWindows: true,
        isMacOS: true,
        secondaryPlayer: false,
      ),
      isFalse,
    );
    expect(
      supportsDesktopMiniPlayer(
        isWindows: false,
        isMacOS: true,
        secondaryPlayer: false,
      ),
      isTrue,
    );
    expect(
      supportsDesktopMiniPlayer(
        isWindows: false,
        isMacOS: true,
        secondaryPlayer: true,
      ),
      isFalse,
    );
  });

  test('mini window preserves landscape video aspect ratio', () {
    expect(clampDesktopMiniAspectRatio(16 / 9), closeTo(16 / 9, 0.0001));
    expect(
      desktopMiniWindowSize(16 / 9, 480).width,
      closeTo(480, 0.001),
    );
    expect(
      desktopMiniWindowSize(16 / 9, 480).height,
      closeTo(270, 0.001),
    );
  });

  test('mini window preserves portrait video aspect ratio', () {
    final size = desktopMiniWindowSize(9 / 16, 480);
    expect(size.height, closeTo(480, 0.001));
    expect(size.width, closeTo(270, 0.001));
  });

  test('invalid aspect ratios use a safe landscape default', () {
    for (final value in [0.0, -1.0, double.nan, double.infinity]) {
      expect(clampDesktopMiniAspectRatio(value), closeTo(16 / 9, 0.0001));
    }
  });

  test('extreme aspect ratios are clamped to supported bounds', () {
    expect(
      clampDesktopMiniAspectRatio(0.01),
      closeTo(1 / 2.39, 0.0001),
    );
    expect(clampDesktopMiniAspectRatio(10), closeTo(2.39, 0.0001));
  });

  test('invalid extent uses the default mini-window size', () {
    expect(
      desktopMiniWindowSize(16 / 9, double.nan).width,
      closeTo(480, 0.001),
    );
  });
}
