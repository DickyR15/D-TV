import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/settings_screen.dart';
import 'package:hgtv/sources_screen.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('source diagnostics is directly accessible from Settings', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: SettingsScreen(
          repository: FixtureRepository(),
          store: store,
        ),
      ),
    );
    await tester.pumpAndSettle();

    final diagnostics = find.byKey(
      const ValueKey('source-diagnostics-setting'),
    );
    expect(diagnostics, findsOneWidget);
    expect(find.text('片源診斷'), findsOneWidget);

    await tester.tap(diagnostics);
    await tester.pumpAndSettle();

    expect(find.byType(SourcesScreen), findsOneWidget);
    expect(
      find.text('顯示目前可用片源的狀態與最近一次自動檢查紀錄。'),
      findsOneWidget,
    );
  });
}
