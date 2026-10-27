import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/app_layout.dart';
import 'package:hgtv/home_screen.dart';
import 'package:hgtv/local_store.dart';

import 'fixtures.dart';

class _SourceManagementRepository extends FixtureRepository {
  @override
  bool get supportsSourceManagement => true;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets(
    'top-right more menu omits source management and manual update on TV',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      final store = testStore(await SharedPreferences.getInstance());
      final repository = _SourceManagementRepository();

      await tester.pumpWidget(
        AppLayout(
          television: true,
          child: MaterialApp(
            home: HomeScreen(repository: repository, store: store),
          ),
        ),
      );
      // HomeScreen contains ongoing TV animations, so wait for the initial
      // layout instead of requiring the whole widget tree to become idle.
      await tester.pump(const Duration(milliseconds: 300));

      final moreButton = find.byTooltip('更多');
      expect(moreButton, findsOneWidget);
      await tester.tap(moreButton);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));

      expect(find.text('片源管理'), findsNothing);
      expect(find.text('更新劇庫'), findsNothing);
      expect(find.text('檢測連線與播放'), findsNothing);
      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pump();

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
      store.dispose();
    },
  );
}
