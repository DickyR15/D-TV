import 'package:flutter/painting.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/video_output_size.dart';

void main() {
  group('videoDisplaySizeForSource', () {
    test('prefers valid display dimensions over coded dimensions', () {
      expect(
        videoDisplaySizeForSource(
          codedSize: const Size(1920, 1080),
          displaySize: const Size(1280, 720),
        ),
        const Size(1280, 720),
      );
    });

    test('swaps dimensions for 90 and 270 degree rotations', () {
      expect(
        videoDisplaySizeForSource(
          codedSize: const Size(1920, 1080),
          rotation: 90,
        ),
        const Size(1080, 1920),
      );
      expect(
        videoDisplaySizeForSource(
          codedSize: const Size(1920, 1080),
          rotation: 270,
        ),
        const Size(1080, 1920),
      );
    });

    test('rejects missing or invalid source dimensions', () {
      expect(videoDisplaySizeForSource(codedSize: null), isNull);
      expect(videoDisplaySizeForSource(codedSize: Size.zero), isNull);
    });
  });

  group('videoOutputSizeForSource', () {
    const landscape = Size(1920, 1080);

    test('scales a small window down without changing aspect ratio', () {
      expect(
        videoOutputSizeForSource(landscape, const Size(960, 540)),
        const Size(960, 540),
      );
    });

    test('never renders above source dimensions when the window is large', () {
      expect(
        videoOutputSizeForSource(landscape, const Size(2560, 1440)),
        landscape,
      );
    });

    test('fits source inside a window with a different aspect ratio', () {
      expect(
        videoOutputSizeForSource(landscape, const Size(1200, 800)),
        const Size(1200, 675),
      );
      expect(
        videoOutputSizeForSource(landscape, const Size(320, 900)),
        const Size(320, 180),
      );
    });

    test('uses rotated dimensions for portrait streams', () {
      const portrait = Size(1080, 1920);
      expect(
        videoOutputSizeForSource(portrait, const Size(540, 960)),
        const Size(540, 960),
      );
    });

    test('rejects invalid and empty viewports', () {
      expect(videoOutputSizeForSource(landscape, Size.zero), isNull);
      expect(
        videoOutputSizeForSource(
          landscape,
          const Size(double.infinity, 10),
        ),
        isNull,
      );
    });
  });
}
