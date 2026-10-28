import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/playback_settings_screen.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('1.0.0 playback settings omit deferred portrait blur option', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    await tester.pumpWidget(
      MaterialApp(home: PlaybackSettingsScreen(store: store)),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('playback-default-speed')), findsOneWidget);
    expect(find.byKey(const ValueKey('playback-default-quality')), findsOneWidget);
    expect(
      find.byKey(const ValueKey('playback-portrait-blur-background')),
      findsNothing,
    );
    expect(find.text('直式影片模糊背景'), findsNothing);
  });
}
