import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/app_layout.dart';
import 'package:hgtv/playback_settings_screen.dart';
import 'package:hgtv/playback_preferences.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('landscape devices enable blur by default and preserve manual choice', (tester) async {
    SharedPreferences.setMockInitialValues({
      'playback': '{"speed":1.0,"portraitBlurBackground":false}',
    });
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    tester.view.physicalSize = const Size(1920, 1080);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

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
    expect(
      tester.widget<SwitchListTile>(blurFinder).value,
      isTrue,
      reason: 'landscape devices migrate to blur enabled by default',
    );
    expect(store.playbackPreferences.portraitBlurBackground, isTrue);

    await tester.tap(blurFinder);
    await tester.pumpAndSettle();
    expect(store.playbackPreferences.portraitBlurBackground, isFalse);

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
      isFalse,
    );
  });

  testWidgets('portrait devices keep blur off by default until rotated landscape', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    tester.view.physicalSize = const Size(600, 1000);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      MaterialApp(
        home: AppLayout(
          television: false,
          formFactor: FormFactor.phone,
          child: PlaybackSettingsScreen(
            store: store,
            portraitBlurSupported: true,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    final blur = tester.widget<SwitchListTile>(
      find.byKey(const ValueKey('playback-portrait-blur-background')),
    );
    expect(blur.value, isFalse);
    expect(store.needsPortraitBlurLandscapeDefaultMigration, isTrue);
    await tester.pumpWidget(const SizedBox.shrink());
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

  testWidgets('TV playback settings exposes the normal portrait blur toggle', (
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
          child: PlaybackSettingsScreen(
            store: store,
            portraitBlurSupported: true,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    final blur = tester.widget<SwitchListTile>(
      find.byKey(const ValueKey('playback-portrait-blur-background')),
    );
    expect(blur.value, isTrue);
    expect(blur.onChanged, isNotNull);
    expect((blur.title as Text).data, '直式影片模糊背景');
    expect(find.textContaining('試驗'), findsNothing);
    expect(
      blur.subtitle,
      isA<Text>().having(
        (value) => value.data,
        'TV blur explanation',
        allOf(contains('橫式畫面播放直式影片'), contains('不另開解碼器')),
      ),
    );
  });

}
