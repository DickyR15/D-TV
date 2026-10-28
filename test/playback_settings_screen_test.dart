import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/playback_settings_screen.dart';
import 'package:hgtv/playback_preferences.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('portrait blur switch defaults off and auto-saves', (tester) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: PlaybackSettingsScreen(
          store: store,
          portraitBlurSupported: true,
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('playback-default-speed')), findsOneWidget);
    expect(find.byKey(const ValueKey('playback-default-quality')), findsOneWidget);
    expect(find.text('儲存'), findsNothing);
    final switchFinder = find.byKey(const ValueKey('playback-portrait-blur-background'));
    expect(switchFinder, findsOneWidget);
    expect(tester.widget<SwitchListTile>(switchFinder).value, isFalse);

    await tester.tap(switchFinder);
    await tester.pumpAndSettle();
    expect(store.playbackPreferences.portraitBlurBackground, isTrue);
    expect(
      PlaybackPreferences.fromJson(store.playbackPreferences.toJson()).portraitBlurBackground,
      isTrue,
    );

    await tester.tap(find.byKey(const ValueKey('playback-default-speed')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('1.25x').last);
    await tester.pumpAndSettle();

    expect(store.playbackPreferences.speed, 1.25);
    expect(store.playbackPreferences.portraitBlurBackground, isTrue);
    expect(
      PlaybackPreferences.fromJson(store.playbackPreferences.toJson()).speed,
      1.25,
    );
  });
}
