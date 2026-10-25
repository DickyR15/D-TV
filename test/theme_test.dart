import 'dart:convert';

import 'package:duanju_app/app_layout.dart';
import 'package:duanju_app/app_theme.dart';
import 'package:duanju_app/core_bridge.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/settings_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Future<LocalStore> localStore() async {
    SharedPreferences.setMockInitialValues({});
    return LocalStore(await SharedPreferences.getInstance());
  }

  test(
    'theme survives restart and backup, with compatible old backups',
    () async {
      final store = await localStore();
      expect(store.themeMode, 'system');
      await store.setThemeMode('light');
      final restored = LocalStore(store.preferences);
      expect(restored.themeMode, 'light');
      final backup = await store.exportBackup();
      await store.setThemeMode('system');
      await store.importBackup(backup);
      expect(store.themeMode, 'light');
      final old = jsonDecode(backup) as Map<String, dynamic>;
      old.remove('themeMode');
      await store.setThemeMode('system');
      await store.importBackup(jsonEncode(old));
      expect(store.themeMode, 'system');
      old['themeMode'] = 'invalid';
      await expectLater(
        store.importBackup(jsonEncode(old)),
        throwsFormatException,
      );
      expect(store.themeMode, 'system');
      store.dispose();
      restored.dispose();
    },
  );

  testWidgets('settings theme choice immediately updates the running app', (
    tester,
  ) async {
    final store = await localStore();
    addTearDown(store.dispose);
    final repository = NativeRepository();

    await tester.pumpWidget(
      AnimatedBuilder(
        animation: store,
        builder: (_, _) => MaterialApp(
          theme: AppTheme.light,
          darkTheme: AppTheme.dark,
          themeMode: AppTheme.mode(store.themeMode),
          home: Builder(
            builder: (context) => Scaffold(
              body: Column(
                children: [
                  Text(
                    'root-${Theme.of(context).brightness}',
                    key: const ValueKey('root-brightness'),
                  ),
                  TextButton(
                    key: const ValueKey('open-settings'),
                    onPressed: () => Navigator.of(context).push<void>(
                      MaterialPageRoute<void>(
                        builder: (_) => SettingsScreen(
                          repository: repository,
                          store: store,
                        ),
                      ),
                    ),
                    child: const Text('開啟設定'),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
    expect(find.text('root-Brightness.dark'), findsOneWidget);

    await tester.tap(find.byKey(const ValueKey('open-settings')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('theme-setting')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('淺色').last);
    await tester.pumpAndSettle();

    expect(store.themeMode, 'light');
    expect(find.text('root-Brightness.light'), findsOneWidget);
  });

  testWidgets('settings exposes and saves an explicit TV/phone operation mode', (
    tester,
  ) async {
    final store = await localStore();
    addTearDown(store.dispose);
    await tester.pumpWidget(
      MaterialApp(
        home: SettingsScreen(
          repository: NativeRepository(),
          store: store,
        ),
      ),
    );

    await tester.tap(find.byKey(const ValueKey('display-mode-setting')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('電視／遙控器模式').last);
    await tester.pumpAndSettle();

    expect(store.displayMode, 'television');
    expect(find.text('電視／遙控器模式').first, findsOneWidget);
  });

  testWidgets('TV settings handles remote Back on the settings route', (
    tester,
  ) async {
    final store = await localStore();
    addTearDown(store.dispose);
    await tester.pumpWidget(
      AppLayout(
        television: true,
        child: MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: TextButton(
                key: const ValueKey('open-settings-route'),
                onPressed: () => Navigator.of(context).push<void>(
                  MaterialPageRoute<void>(
                    builder: (_) => SettingsScreen(
                      repository: NativeRepository(),
                      store: store,
                    ),
                  ),
                ),
                child: const Text('設定'),
              ),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.byKey(const ValueKey('open-settings-route')));
    await tester.pumpAndSettle();
    expect(find.text('設定與備份'), findsOneWidget);
    await tester.sendKeyEvent(LogicalKeyboardKey.goBack);
    await tester.pumpAndSettle();
    expect(find.text('設定與備份'), findsNothing);
    expect(find.byKey(const ValueKey('open-settings-route')), findsOneWidget);
  });
}
