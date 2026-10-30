import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:hgtv/app_build.dart';
import 'package:hgtv/source_management_screen.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('advanced source toggles are hidden until admin password is verified', (
    tester,
  ) async {
    if (!allSourcesEnabled) return;

    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await store.saveProfile(
      id: 'default',
      name: '管理員',
      sources: const [],
      download: true,
      pin: 'source-pass-123',
    );

    await tester.pumpWidget(
      MaterialApp(home: SourceManagementScreen(store: store)),
    );
    expect(find.text('紅果'), findsOneWidget);
    expect(find.text('韓小圈'), findsOneWidget);
    expect(find.text('鬼片'), findsOneWidget);
    expect(find.text('青空'), findsOneWidget);
    expect(find.text('黃豆'), findsNothing);

    await tester.enterText(
      find.byKey(const ValueKey('advanced-source-password')),
      'source-pass-123',
    );
    await tester.tap(find.byKey(const ValueKey('unlock-advanced-sources')));
    await tester.pumpAndSettle();

    expect(store.advancedSourcesUnlocked, isTrue);
    for (final name in ['黃豆', '劇果', '野果', '帝果', '黃果影片', '黃果 AI', '黃果舊版']) {
      expect(find.text(name), findsOneWidget);
    }
    expect(store.sourceEnabled('huangdou'), isFalse);

    await tester.tap(find.byKey(const ValueKey('source-switch-huangdou')));
    await tester.pumpAndSettle();
    expect(store.sourceEnabled('huangdou'), isTrue);

    await tester.pumpWidget(const MaterialApp(home: Text('已離開片源管理')));
    await tester.pumpAndSettle();
    expect(store.advancedSourcesUnlocked, isFalse);
    await expectLater(
      store.setSourceEnabled('huangdou', false),
      throwsStateError,
    );
  });
}
