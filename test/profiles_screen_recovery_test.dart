import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/app_layout.dart';
import 'package:hgtv/local_snapshot.dart';
import 'package:hgtv/profiles_screen.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('corrupt local snapshot exposes TV recovery actions without replacing raw data', (
    tester,
  ) async {
    const corruptSnapshot = '{broken-local-snapshot';
    SharedPreferences.setMockInitialValues({
      LocalSnapshot.storageKey: corruptSnapshot,
    });
    final preferences = await SharedPreferences.getInstance();
    final store = testStore(preferences);
    expect(store.configurationError, isNotNull);
    expect(store.locked, isTrue);

    await tester.pumpWidget(
      AppLayout(
        television: true,
        formFactor: FormFactor.television,
        child: MaterialApp(
          home: ProfilesScreen(store: store, locked: true),
        ),
      ),
    );
    await tester.pump();

    expect(find.text('設定復原'), findsOneWidget);
    expect(find.text('需要還原本機設定'), findsOneWidget);
    expect(find.byKey(const ValueKey('profiles-recovery-restore')), findsOneWidget);
    expect(find.text('匯出原始設定'), findsOneWidget);
    expect(find.text('重新讀取設定'), findsOneWidget);
    expect(
      FocusManager.instance.primaryFocus?.debugLabel,
      'recovery-settings-focus',
    );

    expect(preferences.getString(LocalSnapshot.storageKey), corruptSnapshot);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump();
    store.dispose();
  });
}
