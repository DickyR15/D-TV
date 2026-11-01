import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/app_layout.dart';
import 'package:hgtv/home_screen.dart';
import 'package:hgtv/remote_widgets.dart';
import 'package:hgtv/local_store.dart';

import 'fixtures.dart';

class _SourceManagementRepository extends FixtureRepository {
  @override
  bool get supportsSourceManagement => true;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('TV app startup defaults to Redfruit Hot and focuses it', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());

    await tester.pumpWidget(
      AppLayout(
        television: true,
        child: MaterialApp(
          home: HomeScreen(repository: FixtureRepository(), store: store),
        ),
      ),
    );
    // HomeScreen animations and ranking initialization can keep the tree busy.
    await tester.pump(const Duration(milliseconds: 300));

    final hotNavigation = find.byKey(const ValueKey('side-nav-4'));
    expect(hotNavigation, findsOneWidget);
    final target = tester.widget<RemoteTarget>(hotNavigation);
    expect(target.label, '紅果熱播榜');
    expect(target.selected, isTrue);
    expect(target.autofocus, isTrue);
    expect(
      FocusManager.instance.primaryFocus?.debugLabel,
      'D TV navigation 4',
    );
    expect(find.byKey(const ValueKey('tv-ranking-4')), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump();
    store.dispose();
  });

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

      expect(find.text('熱門榜單・收藏追劇・隨選即播'), findsOneWidget);

      final moreButton = find.byTooltip('更多');
      expect(moreButton, findsOneWidget);
      // find.byTooltip resolves to the internal RawTooltip rather than the
      // PopupMenuButton itself. Inspect the ancestor menu for its initial focus.
      final moreMenuFinder = find.ancestor(
        of: moreButton,
        matching: find.byType(PopupMenuButton<String>),
      );
      expect(moreMenuFinder, findsOneWidget);
      final moreMenu = tester.widget<PopupMenuButton<String>>(moreMenuFinder);
      expect(moreMenu.initialValue, 'users');
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
