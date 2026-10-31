import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/app_layout.dart';
import 'package:hgtv/local_store.dart';
import 'package:hgtv/profiles_screen.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  for (final platform in [
    TargetPlatform.android,
    TargetPlatform.iOS,
    TargetPlatform.windows,
    TargetPlatform.macOS,
  ]) {
    testWidgets(
      'admin profile information row is inert on ${platform.name}',
      (tester) async {
        SharedPreferences.setMockInitialValues({});
        final store = LocalStore(await SharedPreferences.getInstance());
        await tester.pumpWidget(
          MaterialApp(
            theme: ThemeData(platform: platform),
            home: ProfilesScreen(store: store),
          ),
        );
        await tester.pumpAndSettle();

        final rowFinder = find.byKey(const ValueKey('profile-open-default'));
        final row = tester.widget<InkWell>(rowFinder);
        expect(row.canRequestFocus, isFalse);
        expect(row.onTap, isNull);

        await tester.tap(rowFinder);
        await tester.pumpAndSettle();

        expect(find.text('使用者管理'), findsOneWidget);
        expect(find.byKey(const ValueKey('profile-edit-default')), findsOneWidget);
        expect(store.profile.id, 'default');

        await tester.pumpWidget(const SizedBox.shrink());
        store.dispose();
      },
    );
  }


  testWidgets(
    'admin profile editor exposes editable fields and a focused save action',
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

      final openProfile = tester.widget<InkWell>(
        find.byKey(const ValueKey('profile-open-default')),
      );
      final editFinder = find.byKey(const ValueKey('profile-edit-default'));
      final editButton = tester.widget<IconButton>(editFinder);
      expect(openProfile.canRequestFocus, isFalse);
      expect(openProfile.onTap, isNull);
      expect(editButton.focusNode!.canRequestFocus, isTrue);
      editButton.focusNode!.requestFocus();
      await tester.pumpAndSettle();
      expect(editButton.focusNode!.hasPrimaryFocus, isTrue);
      await tester.sendKeyEvent(LogicalKeyboardKey.enter);
      await tester.pumpAndSettle();

      expect(find.byKey(const ValueKey('profile-editor-name')), findsOneWidget);
      expect(find.byKey(const ValueKey('profile-editor-pin')), findsOneWidget);
      expect(
        find.byKey(const ValueKey('profile-editor-confirm-pin')),
        findsOneWidget,
      );
      final save = find.byKey(const ValueKey('profile-editor-save'));
      expect(save, findsOneWidget);
      expect(tester.widget<FilledButton>(save).autofocus, isTrue);

      await tester.enterText(
        find.byKey(const ValueKey('profile-editor-name')),
        '管理員測試',
      );
      expect(
        tester.widget<TextField>(
          find.byKey(const ValueKey('profile-editor-name')),
        ).controller!.text,
        '管理員測試',
      );

      await tester.pumpWidget(const SizedBox.shrink());
      store.dispose();
    },
  );

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
