import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/core_bridge.dart';
import 'package:hgtv/local_store.dart';
import 'package:hgtv/resource_settings.dart';
import 'package:hgtv/resource_settings_screen.dart';

import 'fixtures.dart';

class _ResourceSettingsRepository extends FixtureRepository {
  ResourceSettings current = const ResourceSettings();
  int saveCalls = 0;

  @override
  Future<ResourceSettings> resourceSettings() async => current;

  @override
  Future<ResourceSettings> saveResourceSettings(ResourceSettings settings) async {
    saveCalls++;
    current = settings;
    return settings;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('network and resource settings auto-save without a Save button', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final preferences = await SharedPreferences.getInstance();
    final store = testStore(preferences);
    final repository = _ResourceSettingsRepository();
    addTearDown(store.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: ResourceSettingsScreen(repository: repository, store: store),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('儲存'), findsNothing);
    expect(find.text('設定變更會自動儲存'), findsNothing);

    await tester.tap(find.byType(DropdownButtonFormField<String>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('直連').last);
    await tester.pumpAndSettle();
    await tester.pump(const Duration(milliseconds: 500));
    await tester.pumpAndSettle();

    expect(repository.current.proxyMode, 'direct');
    expect(repository.saveCalls, 1);
    expect(find.text('儲存'), findsNothing);
  });
}
