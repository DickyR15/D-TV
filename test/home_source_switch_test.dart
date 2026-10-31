import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:hgtv/app_layout.dart';
import 'package:hgtv/home_screen.dart';
import 'package:hgtv/models.dart';

import 'fixtures.dart';

class _HomeSourceSwitchRepository extends FixtureRepository {
  @override
  Future<String> cover(Drama drama, {bool force = false}) async => '';
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  for (final factor in [
    FormFactor.phone,
    FormFactor.window,
    FormFactor.pad,
    FormFactor.television,
  ]) {
    testWidgets('home source selector is visible in ${factor.name} layout', (
      tester,
    ) async {
      SharedPreferences.setMockInitialValues({});
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);

      await tester.pumpWidget(
        MaterialApp(
          home: AppLayout(
            television: factor == FormFactor.television,
            formFactor: factor,
            child: HomeScreen(
              repository: _HomeSourceSwitchRepository(),
              store: store,
            ),
          ),
        ),
      );
      // Ranking/catalog initialization can include ongoing animations.
      await tester.pump(const Duration(milliseconds: 350));

      final key = switch (factor) {
        FormFactor.phone => const ValueKey('home-source-switch-mobile'),
        FormFactor.window || FormFactor.pad =>
          const ValueKey('home-source-switch-desktop'),
        FormFactor.television => const ValueKey('home-source-switch-tv'),
      };
      expect(find.byKey(key), findsOneWidget);
      if (factor == FormFactor.window) {
        expect(find.byIcon(Icons.public_rounded), findsOneWidget);
      }
    });
  }
  testWidgets('desktop sidebar is compact, aligned, and pins Settings to bottom', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await tester.binding.setSurfaceSize(const Size(1440, 900));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    await tester.pumpWidget(
      MaterialApp(
        home: AppLayout(
          television: false,
          formFactor: FormFactor.window,
          child: HomeScreen(
            repository: _HomeSourceSwitchRepository(),
            store: store,
          ),
        ),
      ),
    );
    await tester.pump(const Duration(milliseconds: 350));

    final tabs = [2, 1, 4, 5, 6, 7, 0, 9];
    final rects = [
      for (final tab in tabs)
        tester.getRect(find.byKey(ValueKey('side-nav-$tab'))),
    ];
    for (final rect in rects.skip(1)) {
      expect(rect.width, closeTo(rects.first.width, 0.1));
      expect(rect.height, closeTo(rects.first.height, 0.1));
    }
    expect(rects.first.width, lessThan(180));
    expect(rects.first.width, greaterThan(140));
    expect(rects.first.height, lessThan(70));
    for (var index = 1; index < 7; index++) {
      expect(rects[index].top, greaterThan(rects[index - 1].bottom));
      expect(rects[index].top - rects[index - 1].bottom, lessThan(20));
    }
    expect(rects.last.top, greaterThan(rects[6].bottom));
    expect(rects.last.bottom, greaterThan(840));
    expect(find.text('最近觀看'), findsOneWidget);
    expect(find.text('設定'), findsOneWidget);
  });

  testWidgets('desktop source menu opens source management directly', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: AppLayout(
          television: false,
          formFactor: FormFactor.window,
          child: HomeScreen(
            repository: _HomeSourceSwitchRepository(),
            store: store,
          ),
        ),
      ),
    );
    await tester.pump(const Duration(milliseconds: 350));

    final switcher = find.byKey(
      const ValueKey('home-source-switch-desktop'),
    );
    expect(switcher, findsOneWidget);
    await tester.tap(switcher);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 350));

    expect(find.text('片源管理'), findsOneWidget);
    await tester.tap(find.text('片源管理'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 350));

    expect(find.text('一般片源'), findsOneWidget);
    expect(find.text('進階片源'), findsNothing);
    expect(find.textContaining('七個進階片源'), findsNothing);
    expect(find.textContaining('忘記管理員密碼'), findsNothing);
  });

}
