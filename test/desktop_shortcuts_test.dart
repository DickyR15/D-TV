import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/app_layout.dart';
import 'package:hgtv/home_screen.dart';
import 'package:hgtv/remote_widgets.dart';
import 'package:hgtv/search_input.dart';

import 'fixtures.dart';

class _EmptyDesktopRepository extends FixtureRepository {
  @override
  Future<CatalogPage> cached(String source, {String category = ''}) async =>
      CatalogPage([]);

  @override
  Future<CatalogPage> catalog(
    String source, {
    int page = 1,
    String query = '',
    String category = '',
    bool force = false,
  }) async => CatalogPage([], page: page, hasMore: false);
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('Windows desktop shortcuts navigate and focus search', (tester) async {
    debugDefaultTargetPlatformOverride = TargetPlatform.windows;
    addTearDown(() => debugDefaultTargetPlatformOverride = null);
    tester.view.physicalSize = const Size(1440, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    await tester.pumpWidget(
      AppLayout(
        television: false,
        formFactor: FormFactor.window,
        child: MaterialApp(
          home: HomeScreen(repository: _EmptyDesktopRepository(), store: store),
        ),
      ),
    );
    await tester.pump(const Duration(milliseconds: 300));

    Future<void> pressControl(LogicalKeyboardKey key) async {
      await tester.sendKeyDownEvent(LogicalKeyboardKey.controlLeft);
      await tester.sendKeyDownEvent(key);
      await tester.sendKeyUpEvent(key);
      await tester.sendKeyUpEvent(LogicalKeyboardKey.controlLeft);
      await tester.pump(const Duration(milliseconds: 80));
    }

    await pressControl(LogicalKeyboardKey.digit2);
    final favorites = tester.widget<RemoteTarget>(
      find.byKey(const ValueKey('side-nav-1')),
    );
    expect(favorites.selected, isTrue);

    await pressControl(LogicalKeyboardKey.keyF);
    await tester.pump(const Duration(milliseconds: 200));
    expect(find.byType(SearchInput), findsOneWidget);
    expect(
      FocusManager.instance.primaryFocus?.debugLabel,
      'desktop-search',
    );

    await tester.sendKeyEvent(LogicalKeyboardKey.f1);
    await tester.pump(const Duration(milliseconds: 300));
    expect(find.text('桌面快捷鍵'), findsOneWidget);
    expect(find.text('Ctrl + 8'), findsOneWidget);
    expect(find.text('Space'), findsOneWidget);

    await tester.tap(find.text('關閉'));
    await tester.pump(const Duration(milliseconds: 200));
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump();
  });
}
