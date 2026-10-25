import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/app_layout.dart';
import 'package:hgtv/local_store.dart';
import 'package:hgtv/profiles_screen.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets(
    'force-login switch keeps TV focus when enabled and disabled',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      final store = LocalStore(await SharedPreferences.getInstance());
      await tester.pumpWidget(
        AppLayout(
          television: true,
          child: MaterialApp(home: ProfilesScreen(store: store)),
        ),
      );
      await tester.pumpAndSettle();

      final switchFinder = find.byKey(
        const ValueKey('force-login-setting'),
      );
      expect(switchFinder, findsOneWidget);
      expect(store.forceLogin, isFalse);

      await tester.tap(switchFinder);
      await tester.pumpAndSettle();
      expect(store.forceLogin, isTrue);
      expect(
        tester.widget<SwitchListTile>(switchFinder).focusNode!.hasFocus,
        isTrue,
      );

      await tester.tap(switchFinder);
      await tester.pumpAndSettle();
      expect(store.forceLogin, isFalse);
      expect(
        tester.widget<SwitchListTile>(switchFinder).focusNode!.hasFocus,
        isTrue,
      );

      await tester.pumpWidget(const SizedBox.shrink());
      store.dispose();
    },
  );
}
