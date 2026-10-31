import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:hgtv/app_layout.dart';
import 'package:hgtv/home_screen.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  for (final factor in [
    FormFactor.phone,
    FormFactor.window,
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
              repository: FixtureRepository(),
              store: store,
            ),
          ),
        ),
      );
      // Ranking/catalog initialization can include ongoing animations.
      await tester.pump(const Duration(milliseconds: 350));

      final key = switch (factor) {
        FormFactor.phone => const ValueKey('home-source-switch-mobile'),
        FormFactor.window => const ValueKey('home-source-switch-wide'),
        FormFactor.television => const ValueKey('home-source-switch-tv'),
        FormFactor.pad => const ValueKey('unused'),
      };
      expect(find.byKey(key), findsOneWidget);
      expect(find.text('紅果'), findsWidgets);
    });
  }
}
