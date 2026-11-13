import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/app_layout.dart';
import 'package:hgtv/home_screen.dart';
import 'package:hgtv/detail_screen.dart';
import 'package:hgtv/models.dart';
import 'package:hgtv/app_navigation.dart';
import 'package:hgtv/remote_widgets.dart';

import 'fixtures.dart';

class _DeferredDetailRepository extends FixtureRepository {
  final Completer<DramaDetail> detailCompleter = Completer<DramaDetail>();

  @override
  Future<String> cover(Drama drama, {bool force = false}) async => '';

  @override
  Future<DramaDetail> detail(Drama drama) {
    detailCalls++;
    return detailCompleter.future;
  }
}

Future<void> _openSettingsFromMoreMenu(WidgetTester tester) async {
  await tester.tap(find.byKey(const ValueKey('home-more-menu')));
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 350));
  await tester.tap(find.text('設定與備份').first);
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 350));
}

Widget _tvAppWithNavigatorLayout({required Widget home}) => MaterialApp(
  navigatorObservers: [appNavigationObserver],
  builder: (context, child) => AppLayout(
    television: true,
    formFactor: FormFactor.television,
    child: child ?? const SizedBox.shrink(),
  ),
  home: home,
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets(
    'Android emulator auto detection starts on Red Fruit Hot with D-pad focus',
    (tester) async {
      SharedPreferences.setMockInitialValues({
        'sourceSwitches': '{"hongguo": false}',
      });
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);

      final formFactor = AppLayout.resolveFormFactor(
        mode: 'auto',
        detectedTelevision: false,
        detectedEmulator: true,
        screenSize: const Size(432, 768),
        desktopPlatform: false,
      );
      expect(formFactor, FormFactor.television);

      await tester.pumpWidget(
        MaterialApp(
          navigatorObservers: [appNavigationObserver],
          home: AppLayout(
            television: true,
            formFactor: formFactor,
            child: HomeScreen(
              repository: FixtureRepository(),
              store: store,
            ),
          ),
        ),
      );
      await tester.pump();

      final hotFinder = find.byKey(const ValueKey('side-nav-4'));
      final categoryFinder = find.byKey(const ValueKey('side-nav-8'));
      expect(tester.widget<RemoteTarget>(hotFinder).selected, isTrue);
      expect(tester.widget<RemoteTarget>(categoryFinder).selected, isFalse);
      expect(
        FocusManager.instance.primaryFocus?.debugLabel,
        'D TV navigation 4',
      );
    },
  );

  testWidgets(
    'switching from desktop detection to TV focuses Red Fruit Hot ranking',
    (tester) async {
      // The test concerns focus only; disable the synthetic catalog so its
      // intentionally missing remote poster fixtures are never requested.
      SharedPreferences.setMockInitialValues({
        'sourceSwitches': '{"hongguo": false}',
      });
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);
      final repository = FixtureRepository();

      Widget buildApp(FormFactor formFactor) => MaterialApp(
        home: AppLayout(
          television: formFactor == FormFactor.television,
          formFactor: formFactor,
          child: HomeScreen(repository: repository, store: store),
        ),
      );

      // Reproduce startup while Android TV device detection is still pending:
      // the desktop-style side navigation mounts before TV mode is resolved.
      await tester.pumpWidget(buildApp(FormFactor.window));
      await tester.pump();

      await tester.pumpWidget(buildApp(FormFactor.television));
      // The empty synthetic ranking intentionally keeps its loading indicator
      // visible, so pump the focus callback instead of waiting for all frames
      // to settle.
      await tester.pump();

      final redFruitNavigation = tester.widget<RemoteTarget>(
        find.byKey(const ValueKey('side-nav-4')),
      );
      expect(redFruitNavigation.selected, isTrue);
      expect(
        FocusManager.instance.primaryFocus?.debugLabel,
        'D TV navigation 4',
      );
    },
  );
  testWidgets(
    'startup reasserts Red Fruit Hot focus if the splash transition steals focus',
    (tester) async {
      SharedPreferences.setMockInitialValues({
        'sourceSwitches': '{"hongguo": false}',
      });
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);
      final repository = FixtureRepository();
      final transitionFocus = FocusNode(debugLabel: 'splash-transition-focus');
      addTearDown(transitionFocus.dispose);

      await tester.pumpWidget(
        MaterialApp(
          home: Focus(
            focusNode: transitionFocus,
            child: AppLayout(
              television: true,
              formFactor: FormFactor.television,
              child: HomeScreen(repository: repository, store: store),
            ),
          ),
        ),
      );
      await tester.pump();

      // Simulate the splash/route transition taking focus after HomeScreen's
      // first post-frame request. The guarded retry must restore the hot board.
      transitionFocus.requestFocus();
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 800));
      await tester.pump();

      expect(
        FocusManager.instance.primaryFocus?.debugLabel,
        'D TV navigation 4',
      );
    },
  );
  testWidgets(
    'transient category focus cannot replace the Red Fruit Hot startup page',
    (tester) async {
      SharedPreferences.setMockInitialValues({
        'sourceSwitches': '{"hongguo": false}',
      });
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);

      await tester.pumpWidget(
        _tvAppWithNavigatorLayout(
          home: HomeScreen(repository: FixtureRepository(), store: store),
        ),
      );
      await tester.pump();

      final hotFinder = find.byKey(const ValueKey('side-nav-4'));
      final categoryFinder = find.byKey(const ValueKey('side-nav-8'));
      final hot = tester.widget<RemoteTarget>(hotFinder);
      final category = tester.widget<RemoteTarget>(categoryFinder);

      expect(hot.selected, isTrue);
      category.focusNode!.requestFocus();
      await tester.pump();

      // A transient autofocus/focus-restoration event must not navigate away.
      expect(tester.widget<RemoteTarget>(hotFinder).selected, isTrue);
      expect(tester.widget<RemoteTarget>(categoryFinder).selected, isFalse);

      // The startup retry remains armed and returns focus to Red Fruit Hot.
      await tester.pump(const Duration(milliseconds: 800));
      await tester.pump();
      expect(
        FocusManager.instance.primaryFocus?.debugLabel,
        'D TV navigation 4',
      );
      expect(tester.widget<RemoteTarget>(hotFinder).selected, isTrue);
    },
  );

  testWidgets(
    'TV more menu opens with User Management focused and selectable',
    (tester) async {
      SharedPreferences.setMockInitialValues({
        'sourceSwitches': '{"hongguo": false}',
      });
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);
      final repository = FixtureRepository();

      await tester.pumpWidget(
        MaterialApp(
          navigatorObservers: [appNavigationObserver],
          home: AppLayout(
            television: true,
            formFactor: FormFactor.television,
            child: HomeScreen(repository: repository, store: store),
          ),
        ),
      );
      await tester.pump();

      await tester.tap(find.byKey(const ValueKey('home-more-menu')));
      // The home route has repeating TV animations; advance the popup
      // transition directly instead of waiting for the entire tree to settle.
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 350));

      expect(
        FocusManager.instance.primaryFocus?.debugLabel,
        'home-more-users',
      );

      await tester.sendKeyDownEvent(LogicalKeyboardKey.enter);
      await tester.sendKeyUpEvent(LogicalKeyboardKey.enter);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 350));

      expect(find.text('使用者管理'), findsOneWidget);
    },
  );


  testWidgets(
    'Settings is opened from the More menu, not a navigation destination',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);

      await tester.pumpWidget(
        _tvAppWithNavigatorLayout(
          home: HomeScreen(repository: FixtureRepository(), store: store),
        ),
      );
      await tester.pump(const Duration(milliseconds: 350));

      expect(find.byKey(const ValueKey('side-nav-9')), findsNothing);
      expect(find.text('設定與備份'), findsNothing);

      await tester.tap(find.byKey(const ValueKey('home-more-menu')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 350));
      await tester.tap(find.text('設定與備份').first);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 350));

      expect(find.byKey(const ValueKey('theme-setting')), findsOneWidget);
      expect(
        FocusManager.instance.primaryFocus?.debugLabel,
        'settings-device-interconnect',
      );
      // The Home route stays mounted below the pushed Settings route.
      expect(find.byKey(const ValueKey('home-more-menu')), findsOneWidget);

      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('theme-setting')), findsNothing);
      expect(find.byKey(const ValueKey('home-more-menu')), findsOneWidget);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    },
  );

  testWidgets(
    'TV Back from a theme chooser returns to Settings, not Home',
    (tester) async {
      SharedPreferences.setMockInitialValues({
        'sourceSwitches': '{"hongguo": false}',
      });
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);

      await tester.pumpWidget(
        _tvAppWithNavigatorLayout(
          home: HomeScreen(repository: FixtureRepository(), store: store),
        ),
      );
      await tester.pump(const Duration(milliseconds: 350));

      await _openSettingsFromMoreMenu(tester);
      expect(find.byKey(const ValueKey('side-nav-9')), findsNothing);
      expect(find.byKey(const ValueKey('theme-setting')), findsOneWidget);

      await tester.tap(find.byKey(const ValueKey('theme-setting')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 250));
      expect(find.byType(SimpleDialog), findsOneWidget);
      expect(appNavigationObserver.hasChildRoute, isTrue);

      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pump();
      expect(appNavigationObserver.hasChildRoute, isTrue);
      await tester.pump(const Duration(milliseconds: 400));

      expect(find.byType(SimpleDialog), findsNothing);
      expect(find.byKey(const ValueKey('theme-setting')), findsOneWidget);
      expect(find.text('設定與備份'), findsOneWidget);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    },
  );

  for (final setting in const <({String key, String label})>[
    (key: 'theme-accent-setting', label: '介面配色'),
    (key: 'display-mode-setting', label: '介面模式'),
  ]) {
    testWidgets(
      'TV Back from ${setting.label} chooser returns to Settings',
      (tester) async {
        SharedPreferences.setMockInitialValues({
          'sourceSwitches': '{"hongguo": false}',
        });
        final store = testStore(await SharedPreferences.getInstance());
        addTearDown(store.dispose);

        await tester.pumpWidget(
          _tvAppWithNavigatorLayout(
          home: HomeScreen(repository: FixtureRepository(), store: store),
        ),
        );
        await tester.pump(const Duration(milliseconds: 350));

        await _openSettingsFromMoreMenu(tester);
        expect(find.byKey(const ValueKey('side-nav-9')), findsNothing);

        final settingFinder = find.byKey(ValueKey(setting.key));
        expect(settingFinder, findsOneWidget);
        await tester.tap(settingFinder);
        await tester.pump();
        await tester.pump(const Duration(milliseconds: 250));
        expect(find.byType(SimpleDialog), findsOneWidget);
        expect(appNavigationObserver.hasChildRoute, isTrue);

        await tester.sendKeyEvent(LogicalKeyboardKey.escape);
        await tester.pump();
        expect(appNavigationObserver.hasChildRoute, isTrue);
        await tester.pump(const Duration(milliseconds: 400));

        expect(find.byType(SimpleDialog), findsNothing);
        expect(settingFinder, findsOneWidget);
        expect(find.text('設定與備份'), findsOneWidget);

        await tester.pumpWidget(const SizedBox.shrink());
        await tester.pump();
      },
    );
  }

  testWidgets(
    'TV Back duplicate after closing a theme chooser does not pop Settings page',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);

      await tester.pumpWidget(
        _tvAppWithNavigatorLayout(
          home: HomeScreen(repository: FixtureRepository(), store: store),
        ),
      );
      await tester.pump(const Duration(milliseconds: 350));

      await tester.tap(find.byKey(const ValueKey('home-more-menu')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 350));
      await tester.tap(find.text('設定與備份').first);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 350));
      expect(find.byKey(const ValueKey('theme-setting')), findsOneWidget);

      await tester.tap(find.byKey(const ValueKey('theme-setting')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 250));
      expect(find.byType(SimpleDialog), findsOneWidget);

      // Android's route pop closes the chooser. Some TV remotes then deliver
      // the same physical Back as a GoBack keyboard event.
      await tester.binding.handlePopRoute();
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 400));
      expect(find.byType(SimpleDialog), findsNothing);
      expect(find.byKey(const ValueKey('theme-setting')), findsOneWidget);

      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 150));

      expect(find.byKey(const ValueKey('theme-setting')), findsOneWidget);
      expect(find.text('設定與備份'), findsOneWidget);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    },
  );

  testWidgets(
    'TV remote selects the appearance theme from Settings',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);

      await tester.pumpWidget(
        _tvAppWithNavigatorLayout(
          home: HomeScreen(repository: FixtureRepository(), store: store),
        ),
      );
      await tester.pump(const Duration(milliseconds: 350));
      await _openSettingsFromMoreMenu(tester);

      await tester.tap(find.byKey(const ValueKey('theme-setting')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 250));
      expect(find.byType(SimpleDialog), findsOneWidget);
      expect(store.themeMode, 'system');

      // The current option receives initial focus; move to 深色 and confirm.
      await tester.sendKeyEvent(LogicalKeyboardKey.arrowUp);
      await tester.pump();
      await tester.sendKeyEvent(LogicalKeyboardKey.enter);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));

      expect(store.themeMode, 'dark');
      expect(find.byType(SimpleDialog), findsNothing);
      expect(find.byKey(const ValueKey('theme-setting')), findsOneWidget);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    },
  );

  testWidgets(
    'TV remote selects the interface accent color from Settings',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);

      await tester.pumpWidget(
        _tvAppWithNavigatorLayout(
          home: HomeScreen(repository: FixtureRepository(), store: store),
        ),
      );
      await tester.pump(const Duration(milliseconds: 350));
      await _openSettingsFromMoreMenu(tester);

      await tester.tap(find.byKey(const ValueKey('theme-accent-setting')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 250));
      expect(find.byType(SimpleDialog), findsOneWidget);
      expect(store.themeAccent, 'system');

      // The first option (跟隨系統色彩) is focused; select 暖橙.
      await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
      await tester.pump();
      await tester.sendKeyEvent(LogicalKeyboardKey.enter);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));

      expect(store.themeAccent, 'warmOrange');
      expect(find.byType(SimpleDialog), findsNothing);
      expect(find.byKey(const ValueKey('theme-accent-setting')), findsOneWidget);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    },
  );

  testWidgets(
    'TV Back from direct More menu display mode closes only the chooser',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);

      await tester.pumpWidget(
        _tvAppWithNavigatorLayout(
          home: HomeScreen(repository: FixtureRepository(), store: store),
        ),
      );
      await tester.pump(const Duration(milliseconds: 350));

      await tester.tap(find.byKey(const ValueKey('home-more-menu')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 350));
      await tester.tap(find.text('介面模式').last);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 250));
      expect(find.byType(SimpleDialog), findsOneWidget);

      await tester.binding.handlePopRoute();
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 350));

      expect(find.byType(SimpleDialog), findsNothing);
      expect(find.byKey(const ValueKey('home-more-menu')), findsOneWidget);
      expect(find.text('設定與備份'), findsNothing);
      expect(store.displayMode, 'auto');

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    },
  );

  testWidgets(
    'TV Back from a Settings subpage returns to Settings without popping Home',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);

      await tester.pumpWidget(
        _tvAppWithNavigatorLayout(
          home: HomeScreen(repository: FixtureRepository(), store: store),
        ),
      );
      await tester.pump(const Duration(milliseconds: 350));
      await _openSettingsFromMoreMenu(tester);

      await tester.tap(find.byKey(const ValueKey('playback-setting')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));
      expect(
        find.descendant(
          of: find.byType(AppBar).last,
          matching: find.text('播放'),
        ),
        findsOneWidget,
      );
      // The previous Settings route remains mounted underneath the player;
      // assert against the top-most AppBar rather than searching the whole tree.
      await tester.binding.handlePopRoute();
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 250));
      expect(find.byKey(const ValueKey('theme-setting')), findsOneWidget);
      expect(find.text('設定與備份'), findsOneWidget);

      // A second delivery from the same physical remote press must not close
      // the Settings route after the playback page has already been dismissed.
      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pump();
      expect(find.byKey(const ValueKey('theme-setting')), findsOneWidget);
      expect(find.text('設定與備份'), findsOneWidget);

      // A distinct system Back after the de-duplication window closes Settings.
      await tester.pump(const Duration(milliseconds: 1000));
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(find.text('設定與備份'), findsNothing);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    },
  );

  testWidgets(
    'TV remote can move focus and select a display mode from the More menu',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);

      await tester.pumpWidget(
        _tvAppWithNavigatorLayout(
          home: HomeScreen(repository: FixtureRepository(), store: store),
        ),
      );
      await tester.pump(const Duration(milliseconds: 350));

      await tester.tap(find.byKey(const ValueKey('home-more-menu')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 350));
      await tester.tap(find.text('介面模式').last);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 250));
      expect(find.byType(SimpleDialog), findsOneWidget);
      expect(store.displayMode, 'auto');

      await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
      await tester.pump();
      await tester.sendKeyEvent(LogicalKeyboardKey.enter);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));

      expect(store.displayMode, 'television');
      expect(find.byType(SimpleDialog), findsNothing);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    },
  );

  testWidgets(
    'rapid repeated TV poster activation opens one work details route',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);
      final repository = _DeferredDetailRepository();

      await tester.pumpWidget(
        MaterialApp(
          navigatorObservers: [appNavigationObserver],
          home: AppLayout(
            television: true,
            formFactor: FormFactor.television,
            child: HomeScreen(
              repository: repository,
              store: store,
            ),
          ),
        ),
      );
      await tester.pump(const Duration(milliseconds: 350));

      final categoryNavigation = tester.widget<RemoteTarget>(
        find.byKey(const ValueKey('side-nav-8')),
      );
      categoryNavigation.onPressed!.call();
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));

      final poster = find.ancestor(
        of: find.text('測試短劇'),
        matching: find.byType(RemoteTarget),
      );
      expect(poster, findsOneWidget);
      final target = tester.widget<RemoteTarget>(poster);

      // Simulate a fast double-select before the details route finishes
      // loading. Only one details page should be pushed.
      target.onPressed!.call();
      target.onPressed!.call();
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 80));

      expect(find.byType(DetailScreen), findsOneWidget);
      expect(find.byType(PlayerScreen), findsNothing);
      expect(repository.detailCalls, 1);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    },
  );


}
