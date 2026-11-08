import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/app_layout.dart';

void main() {
  test('user-facing app version excludes the internal build number', () {
    expect(appVersion, '1.0.0');
    expect(formatAppVersion('1.0.0'), '1.0.0');
    expect(formatAppVersion(' 1.0.0 '), '1.0.0');
    expect(formatAppVersion(''), '1.0.0');
  });

  group('desktop poster density', () {
    test('keeps poster cards readable from HD to ultrawide widths', () {
      expect(desktopPosterColumnsForWidth(1366), 6);
      expect(desktopPosterColumnsForWidth(1920), 9);
      expect(desktopPosterColumnsForWidth(2560), 12);
      expect(desktopPosterColumnsForWidth(3440), 16);
    });

    test('caps extreme desktop widths and handles invalid constraints', () {
      expect(desktopPosterColumnsForWidth(5120), 16);
      expect(desktopPosterColumnsForWidth(double.infinity), 2);
      expect(desktopPosterColumnsForWidth(double.nan), 2);
      expect(desktopPosterColumnsForWidth(0), 2);
    });
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

    test('auto treats an Android emulator as the TV test layout', () {
      expect(
        AppLayout.resolveFormFactor(
          mode: 'auto',
          detectedTelevision: false,
          detectedEmulator: true,
          screenSize: const Size(432, 768),
          desktopPlatform: false,
        ),
        FormFactor.television,
      );
      // The selected form factor is also what HomeScreen uses to open the
      // Redfruit Hot page and request its initial D-pad focus.
    });

    test('auto keeps physical phones on the phone layout', () {
      expect(
        AppLayout.resolveFormFactor(
          mode: 'auto',
          detectedTelevision: false,
          detectedEmulator: false,
          screenSize: const Size(432, 768),
          desktopPlatform: false,
        ),
        FormFactor.phone,
      );
    });

    test('auto distinguishes phone, pad and desktop computer', () {
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

    test('computer mode falls back to pad off desktop; legacy standard suppresses TV', () {
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

    test('computer option is only shown on desktop platforms', () {
      final mobileOptions = AppLayout.availableDisplayModes(
        desktopPlatform: false,
      );
      expect(
        mobileOptions.keys,
        containsAll(['auto', 'television', 'phone', 'pad']),
      );
      expect(mobileOptions.containsKey('window'), isFalse);
      final desktopOptions = AppLayout.availableDisplayModes(desktopPlatform: true);
      expect(desktopOptions.containsKey('window'), isTrue);
      expect(desktopOptions['window'], '電腦');
      expect(
        AppLayout.displayModeLabel(
          'window',
          currentFactor: FormFactor.window,
          desktopPlatform: true,
        ),
        '電腦',
      );
    });
  });
  testWidgets('physical TV detection is retained when the user selects phone layout', (
    tester,
  ) async {
    var detectedTelevision = false;
    await tester.pumpWidget(
      MaterialApp(
        home: AppLayout(
          television: false,
          detectedTelevision: true,
          formFactor: FormFactor.phone,
          child: Builder(
            builder: (context) {
              detectedTelevision = AppLayout.isTelevisionDevice(context);
              return const SizedBox.shrink();
            },
          ),
        ),
      ),
    );
    expect(detectedTelevision, isTrue);
  });

}
