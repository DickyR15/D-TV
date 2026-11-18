import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/app_layout.dart';
import 'package:hgtv/core_bridge.dart';
import 'package:hgtv/local_store.dart';
import 'package:hgtv/resource_settings.dart';
import 'package:hgtv/resource_settings_screen.dart';

import 'fixtures.dart';

class _ResourceSettingsRepository extends FixtureRepository {
  ResourceSettings current = const ResourceSettings();
  final writes = <ResourceSettings>[];
  int cacheClears = 0;

  @override
  Future<ResourceSettings> resourceSettings() async => current;

  @override
  Future<void> clearSourceCache() async {
    cacheClears++;
  }

  @override
  Future<ResourceSettings> saveResourceSettings(ResourceSettings settings) async {
    writes.add(settings);
    current = settings;
    return settings;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();


  testWidgets('source cache clearing is confirmed and keeps user data', (tester) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    final repository = _ResourceSettingsRepository();

    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => Scaffold(
            body: Center(
              child: FilledButton(
                key: const ValueKey('open-resource-settings-test'),
                onPressed: () => Navigator.of(context).push<bool>(
                  MaterialPageRoute<bool>(
                    builder: (_) => ResourceSettingsScreen(
                      repository: repository,
                      store: store,
                    ),
                  ),
                ),
                child: const Text('開啟網路設定'),
              ),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.byKey(const ValueKey('open-resource-settings-test')));
    await tester.pumpAndSettle();
    final clearButton = find.byKey(const ValueKey('resource-clear-source-cache'));
    await tester.ensureVisible(clearButton);
    await tester.pumpAndSettle();
    await tester.tap(clearButton);
    await tester.pumpAndSettle();

    expect(find.text('清除片源快取？'), findsOneWidget);
    expect(
      find.textContaining('收藏、追劇清單、最近觀看、播放進度、使用者帳號及其他設定都會保留。'),
      findsOneWidget,
    );
    await tester.tap(find.text('取消'));
    await tester.pumpAndSettle();
    expect(repository.cacheClears, 0);

    await tester.ensureVisible(clearButton);
    await tester.pumpAndSettle();
    await tester.tap(clearButton);
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('resource-confirm-clear-cache')));
    await tester.pumpAndSettle();

    expect(repository.cacheClears, 1);
    expect(find.text('開啟網路設定'), findsOneWidget);
  });

  testWidgets('resource settings autosave without a Save button', (tester) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    final repository = _ResourceSettingsRepository();

    await tester.pumpWidget(
      AppLayout(
        television: false,
        child: MaterialApp(
          home: ResourceSettingsScreen(repository: repository, store: store),
        ),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 100));

    expect(find.text('網路與資源'), findsOneWidget);
    expect(find.text('儲存'), findsNothing);

    await tester.tap(find.byType(DropdownButtonFormField<String>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('手動代理').last);
    await tester.pumpAndSettle();
    await tester.pump(const Duration(milliseconds: 500));
    await tester.pump();
    expect(repository.writes, isNotEmpty);
    expect(repository.current.proxyMode, 'manual');

    final proxy = find.byType(TextField);
    expect(proxy, findsOneWidget);
    await tester.enterText(proxy, 'http://127.0.0.1:7890');
    await tester.pump(const Duration(milliseconds: 500));
    await tester.pump();
    expect(repository.current.proxyUrl, 'http://127.0.0.1:7890');

    await tester.pumpWidget(const SizedBox.shrink());
    store.dispose();
  });
}
