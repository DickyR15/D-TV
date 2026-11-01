import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/app_orientation.dart';

void main() {
  group('orientation policy follows the selected interface mode', () {
    test('TV is always landscape', () {
      expect(
        AppOrientationController.orientations(television: true),
        const [
          DeviceOrientation.landscapeLeft,
          DeviceOrientation.landscapeRight,
        ],
      );
    });

    test('phone home is portrait but landscape video may rotate', () {
      expect(
        AppOrientationController.orientations(
          television: false,
          phone: true,
        ),
        const [DeviceOrientation.portraitUp],
      );
      expect(
        AppOrientationController.orientations(
          television: false,
          phone: true,
          aspectRatio: 16 / 9,
        ),
        const [
          DeviceOrientation.landscapeLeft,
          DeviceOrientation.landscapeRight,
        ],
      );
    });

    test('fullscreen follows video aspect ratio for phone and pad', () {
      expect(
        AppOrientationController.orientations(
          television: false,
          phone: true,
          fullscreen: true,
          aspectRatio: 16 / 9,
        ),
        const [
          DeviceOrientation.landscapeLeft,
          DeviceOrientation.landscapeRight,
        ],
      );
      expect(
        AppOrientationController.orientations(
          television: false,
          phone: true,
          fullscreen: true,
          aspectRatio: 9 / 16,
        ),
        const [
          DeviceOrientation.portraitUp,
          DeviceOrientation.portraitDown,
        ],
      );
      expect(
        AppOrientationController.orientations(
          television: false,
          phone: false,
        ),
        DeviceOrientation.values,
      );
    });
  });
}
