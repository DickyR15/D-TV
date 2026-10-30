import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/app_layout.dart';
import 'package:hgtv/playback_settings_screen.dart';
import 'package:hgtv/playback_preferences.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('blur background is opt-in and auto-saves with playback settings', (tester) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: AppLayout(
          television: true,
          formFactor: FormFactor.television,
          child: PlaybackSettingsScreen(
            store: store,
            portraitBlurSupported: true,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('playback-default-speed')), findsOneWidget);
    expect(find.byKey(const ValueKey('playback-default-quality')), findsOneWidget);
    expect(find.byKey(const ValueKey('playback-decoder')), findsOneWidget);
    expect(find.byKey(const ValueKey('playback-preopen-next-episode')), findsOneWidget);
    expect(
      tester
          .widget<SwitchListTile>(
            find.byKey(const ValueKey('playback-preopen-next-episode')),
          )
          .value,
      isTrue,
      reason: 'fresh TV installs should enable next-episode preloading',
    );
    expect(find.byKey(const ValueKey('playback-auto-advance')), findsOneWidget);
    final blurFinder =
        find.byKey(const ValueKey('playback-portrait-blur-background'));
    expect(blurFinder, findsOneWidget);
    expect(tester.widget<SwitchListTile>(blurFinder).value, isFalse);

    await tester.tap(blurFinder);
    await tester.pumpAndSettle();
    expect(store.playbackPreferences.portraitBlurBackground, isTrue);

    await tester.tap(find.byKey(const ValueKey('playback-default-speed')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('1.25x').last);
    await tester.pumpAndSettle();

    expect(store.playbackPreferences.speed, 1.25);
    expect(
      PlaybackPreferences.fromJson(store.playbackPreferences.toJson()).speed,
      1.25,
    );
    expect(
      store.playbackPreferences.toJson()['portraitBlurBackground'],
      isTrue,
    );
  });

  testWidgets('decoder setting is visible in phone, tablet and window layouts', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    for (final factor in [
      FormFactor.phone,
      FormFactor.pad,
      FormFactor.window,
    ]) {
      await tester.pumpWidget(
        MaterialApp(
          home: AppLayout(
            television: false,
            formFactor: factor,
            child: PlaybackSettingsScreen(
              store: store,
              portraitBlurSupported: true,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(
        find.byKey(const ValueKey('playback-decoder')),
        findsOneWidget,
        reason: 'decoder setting should appear in ${factor.name} layout',
      );
    }

    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('TV playback settings keeps portrait blur disabled for sync stability', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({
      'playback': '{"portraitBlurBackground": true}',
    });
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: AppLayout(
          television: true,
          formFactor: FormFactor.television,
          child: PlaybackSettingsScreen(store: store),
        ),
      ),
    );
    await tester.pumpAndSettle();

    final blur = tester.widget<SwitchListTile>(
      find.byKey(const ValueKey('playback-portrait-blur-background')),
    );
    expect(blur.value, isFalse);
    expect(blur.onChanged, isNull);
    expect(
      blur.subtitle,
      isA<Text>().having(
        (value) => value.data,
        'TV blur pause reason',
        contains('電視端暫停此功能'),
      ),
    );
  });

}
