import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/app_layout.dart';
import 'package:hgtv/settings_screen.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('restore settings backup explains replaced data without obsolete download claims', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    tester.view.physicalSize = const Size(1920, 1080);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      MaterialApp(
        home: AppLayout(
          television: false,
          formFactor: FormFactor.phone,
          child: SettingsScreen(
            repository: FixtureRepository(),
            store: store,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    final restoreTitle = find.text('還原設定備份');
    await tester.scrollUntilVisible(
      restoreTitle,
      240,
      scrollable: find.byType(Scrollable).first,
    );

    final tile = tester.widget<ListTile>(
      find.byKey(const ValueKey('restore-settings-backup')),
    );
    expect(
      tile.subtitle,
      isA<Text>()
          .having((value) => value.data, 'restore backup explanation', contains('收藏清單'))
          .having((value) => value.data, 'restore backup explanation', contains('觀看紀錄'))
          .having((value) => value.data, 'restore backup explanation', contains('偏好設定'))
          .having((value) => value.data, 'restore backup explanation', isNot(contains('已下載影片')))
          .having((value) => value.data, 'restore backup explanation', isNot(contains('影片檔案'))),
    );
  });
}
