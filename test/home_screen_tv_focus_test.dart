import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/app_layout.dart';
import 'package:hgtv/home_screen.dart';
import 'package:hgtv/remote_widgets.dart';

import 'fixtures.dart';

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
        MaterialApp(
          home: AppLayout(
            television: true,
            formFactor: FormFactor.television,
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


}
