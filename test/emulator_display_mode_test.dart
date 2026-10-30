import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/app_layout.dart';

void main() {
  group('AppDevice.resolveTelevisionMode', () {
    test('auto mode treats an Android emulator as a TV test environment', () {
      expect(
        AppDevice.resolveTelevisionMode(
          displayMode: 'auto',
          television: false,
          emulator: true,
        ),
        isTrue,
      );
    });

    test('auto mode keeps a physical phone in standard mode', () {
      expect(
        AppDevice.resolveTelevisionMode(
          displayMode: 'auto',
          television: false,
          emulator: false,
        ),
        isFalse,
      );
    });

    test('explicit standard mode overrides emulator detection', () {
      expect(
        AppDevice.resolveTelevisionMode(
          displayMode: 'standard',
          television: false,
          emulator: true,
        ),
        isFalse,
      );
    });

    test('explicit television mode remains available on a physical device', () {
      expect(
        AppDevice.resolveTelevisionMode(
          displayMode: 'television',
          television: false,
          emulator: false,
        ),
        isTrue,
      );
    });
  });
}
