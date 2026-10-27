import 'dart:convert';

import 'package:hgtv/app_layout.dart';
import 'package:hgtv/app_theme.dart';
import 'package:hgtv/core_bridge.dart';
import 'package:hgtv/local_store.dart';
import 'package:hgtv/settings_screen.dart';
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

  test('theme accent persists, migrates old brand values, and validates backups', () async {
    final store = await localStore();
    addTearDown(store.dispose);
    expect(store.themeAccent, 'system');
    await store.setThemeAccent('teal');
    final backup = await store.exportBackup();
    await store.setThemeAccent('system');
    await store.importBackup(backup);
    expect(store.themeAccent, 'teal');

    final oldBackup = jsonDecode(backup) as Map<String, dynamic>;
    oldBackup['themeAccent'] = 'brand';
    await store.importBackup(jsonEncode(oldBackup));
    expect(store.themeAccent, 'system');

    oldBackup.remove('themeAccent');
    await store.setThemeAccent('ocean');
    await store.importBackup(jsonEncode(oldBackup));
    expect(store.themeAccent, 'ocean');

    oldBackup['themeAccent'] = 'not-a-theme';
    await expectLater(
      store.importBackup(jsonEncode(oldBackup)),
      throwsFormatException,
    );
    expect(store.themeAccent, 'ocean');
  });

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
    expect(find.byKey(const ValueKey('root-brightness')), findsOneWidget);

    await tester.tap(find.byKey(const ValueKey('open-settings')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('theme-setting')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('淺色').last);
    await tester.pumpAndSettle();

    expect(store.themeMode, 'light');
    // The settings route is on top of the root route; the root widget is not
    // visible to the finder while the route is still open.
    expect(find.byKey(const ValueKey('root-brightness')), findsNothing);
  });

  testWidgets('settings includes author information and theme navigation', (
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

    expect(find.byKey(const ValueKey('theme-setting')), findsOneWidget);
    expect(find.byKey(const ValueKey('theme-accent-setting')), findsOneWidget);
    expect(find.byKey(const ValueKey('playback-setting')), findsOneWidget);
    expect(find.text('片源管理'), findsNothing);
    await tester.drag(find.byType(ListView), const Offset(0, -900));
    await tester.pumpAndSettle();
    expect(find.byKey(const ValueKey('app-author')), findsOneWidget);
    expect(find.text('DickyR15'), findsOneWidget);
  });

  testWidgets('TV settings keeps author visible in a short viewport', (
    tester,
  ) async {
    final store = await localStore();
    addTearDown(store.dispose);
    await tester.binding.setSurfaceSize(const Size(960, 540));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    await tester.pumpWidget(
      AppLayout(
        television: true,
        child: MaterialApp(
          home: SettingsScreen(repository: NativeRepository(), store: store),
        ),
      ),
    );
    await tester.pumpAndSettle();
    final author = find.byKey(const ValueKey('app-author'));
    expect(author, findsOneWidget);
    final rect = tester.getRect(author);
    expect(rect.top, greaterThanOrEqualTo(0));
    expect(rect.bottom, lessThanOrEqualTo(540));
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
    // Simulate Android TV's platform Back action. Flutter's widget-test key
    // simulator has no physical-key mapping for the remote's Go Back key.
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(find.text('設定與備份'), findsNothing);
    expect(find.byKey(const ValueKey('open-settings-route')), findsOneWidget);
  });
}
