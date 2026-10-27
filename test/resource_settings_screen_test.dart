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

  @override
  Future<ResourceSettings> resourceSettings() async => current;

  @override
  Future<ResourceSettings> saveResourceSettings(ResourceSettings settings) async {
    writes.add(settings);
    current = settings;
    return settings;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

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
    expect(repository.writes, isNotEmpty);
    expect(repository.current.proxyMode, 'manual');

    final proxy = find.byType(TextField);
    expect(proxy, findsOneWidget);
    await tester.enterText(proxy, 'http://127.0.0.1:7890');
    await tester.pump(const Duration(milliseconds: 500));
    expect(repository.current.proxyUrl, 'http://127.0.0.1:7890');

    await tester.pumpWidget(const SizedBox.shrink());
    store.dispose();
  });
}
