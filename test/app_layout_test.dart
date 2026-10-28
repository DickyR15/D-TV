import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/app_layout.dart';

void main() {
  test('fallback app version matches the 1.0.0 candidate build', () {
    expect(appVersion, '1.0.0+339');
  });

  group('interface mode resolution', () {
    test('auto selects TV before screen-size rules', () {
      expect(
        AppLayout.resolveFormFactor(
          mode: 'auto',
          detectedTelevision: true,
          screenSize: const Size(1920, 1080),
          desktopPlatform: false,
        ),
        FormFactor.television,
      );
    });

    test('auto distinguishes phone, pad and desktop window', () {
      expect(
        AppLayout.resolveFormFactor(
          mode: 'auto',
          detectedTelevision: false,
          screenSize: const Size(390, 844),
          desktopPlatform: false,
        ),
        FormFactor.phone,
      );
      expect(
        AppLayout.resolveFormFactor(
          mode: 'auto',
          detectedTelevision: false,
          screenSize: const Size(800, 1280),
          desktopPlatform: false,
        ),
        FormFactor.pad,
      );
      expect(
        AppLayout.resolveFormFactor(
          mode: 'auto',
          detectedTelevision: false,
          screenSize: const Size(1440, 900),
          desktopPlatform: true,
        ),
        FormFactor.window,
      );
    });

    test('explicit modes override auto detection', () {
      const size = Size(1920, 1080);
      const modes = <String, FormFactor>{
        'phone': FormFactor.phone,
        'pad': FormFactor.pad,
        'television': FormFactor.television,
        'window': FormFactor.window,
      };
      for (final mode in modes.entries) {
        expect(
          AppLayout.resolveFormFactor(
            mode: mode.key,
            detectedTelevision: true,
            screenSize: size,
            desktopPlatform: true,
          ),
          mode.value,
          reason: 'mode=${mode.key}',
        );
      }
    });

    test('window falls back to pad off desktop; legacy standard suppresses TV', () {
      expect(
        AppLayout.resolveFormFactor(
          mode: 'window',
          detectedTelevision: false,
          screenSize: const Size(400, 800),
          desktopPlatform: false,
        ),
        FormFactor.pad,
      );
      expect(
        AppLayout.resolveFormFactor(
          mode: 'standard',
          detectedTelevision: true,
          screenSize: const Size(800, 1280),
          desktopPlatform: false,
        ),
        FormFactor.pad,
      );
    });

    test('window option is only shown on desktop platforms', () {
      final mobileOptions = AppLayout.availableDisplayModes(
        desktopPlatform: false,
      );
      expect(
        mobileOptions.keys,
        containsAll(['auto', 'television', 'phone', 'pad']),
      );
      expect(mobileOptions.containsKey('window'), isFalse);
      expect(
        AppLayout.availableDisplayModes(desktopPlatform: true).containsKey('window'),
        isTrue,
      );
    });
  });
}
