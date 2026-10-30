import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/playback_settings_screen.dart';
import 'package:hgtv/playback_preferences.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('1.0.0 hides deferred blur control and keeps auto-save', (tester) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    await tester.pumpWidget(
      MaterialApp(home: PlaybackSettingsScreen(store: store)),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('playback-default-speed')), findsOneWidget);
    expect(find.byKey(const ValueKey('playback-default-quality')), findsOneWidget);
    expect(find.byKey(const ValueKey('playback-preopen-next-episode')), findsOneWidget);
    expect(find.byKey(const ValueKey('playback-auto-advance')), findsOneWidget);
    expect(find.byKey(const ValueKey('playback-portrait-blur-background')), findsNothing);
    expect(find.text('直式影片模糊背景'), findsNothing);

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
      store.playbackPreferences.toJson().containsKey('portraitBlurBackground'),
      isFalse,
    );
  });
}
