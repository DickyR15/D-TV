import 'package:flutter/painting.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/video_output_size.dart';
import 'package:media_kit_video/media_kit_video.dart';

void main() {
  group('videoDisplaySize', () {
    test('uses display dimensions when provided', () {
      expect(
        videoDisplaySize(VideoParams(w: 1920, h: 1080, dw: 1280, dh: 720)),
        const Size(1280, 720),
      );
    });

    test('swaps display dimensions for 90 and 270 degree rotation', () {
      expect(
        videoDisplaySize(VideoParams(w: 1920, h: 1080, rotate: 90)),
        const Size(1080, 1920),
      );
      expect(
        videoDisplaySize(VideoParams(w: 1920, h: 1080, rotate: 270)),
        const Size(1080, 1920),
      );
    });

    test('rejects missing source dimensions', () {
      expect(videoDisplaySize(const VideoParams()), isNull);
    });
  });

  group('videoOutputSizeForViewport', () {
    const landscape = VideoParams(w: 1920, h: 1080, dw: 1920, dh: 1080);

    test('scales a small window down without changing aspect ratio', () {
      expect(
        videoOutputSizeForViewport(landscape, const Size(960, 540)),
        const Size(960, 540),
      );
    });

    test('never renders above the source dimensions when the window is large', () {
      expect(
        videoOutputSizeForViewport(landscape, const Size(2560, 1440)),
        const Size(1920, 1080),
      );
    });

    test('fits the source inside a window with a different aspect ratio', () {
      expect(
        videoOutputSizeForViewport(landscape, const Size(1200, 800)),
        const Size(1200, 675),
      );
      expect(
        videoOutputSizeForViewport(landscape, const Size(320, 900)),
        const Size(320, 180),
      );
    });

    test('uses rotated display dimensions for portrait streams', () {
      const portrait = VideoParams(w: 1920, h: 1080, rotate: 90);
      expect(
        videoOutputSizeForViewport(portrait, const Size(540, 960)),
        const Size(540, 960),
      );
    });

    test('rejects invalid and empty viewports', () {
      expect(videoOutputSizeForViewport(landscape, Size.zero), isNull);
      expect(videoOutputSizeForViewport(landscape, const Size(double.infinity, 10)), isNull);
    });
  });
}
