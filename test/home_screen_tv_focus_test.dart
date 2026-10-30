import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/app_layout.dart';
import 'package:hgtv/home_screen.dart';
import 'package:hgtv/remote_widgets.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets(
    'switching from desktop detection to TV focuses Red Fruit Hot ranking',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
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
      await tester.pumpAndSettle();

      await tester.pumpWidget(buildApp(FormFactor.television));
      await tester.pumpAndSettle();

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
}
