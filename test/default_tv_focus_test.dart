import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/app_layout.dart';
import 'package:hgtv/local_store.dart';
import 'package:hgtv/profiles_screen.dart';
import 'package:hgtv/resource_settings.dart';
import 'package:hgtv/resource_settings_screen.dart';
import 'package:hgtv/settings_screen.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

class _FocusTestRepository extends FixtureRepository {
  @override
  Future<ResourceSettings> resourceSettings() async =>
      const ResourceSettings();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Future<LocalStore> createStore() async {
    SharedPreferences.setMockInitialValues({});
    return testStore(await SharedPreferences.getInstance());
  }

  testWidgets('settings and backup focuses Device Interconnect on TV', (
    tester,
  ) async {
    final store = await createStore();
    await tester.pumpWidget(
      AppLayout(
        television: true,
        child: MaterialApp(
          home: SettingsScreen(
            repository: _FocusTestRepository(),
            store: store,
          ),
        ),
      ),
    );
    await tester.pump();

    final tile = tester.widget<ListTile>(
      find.byKey(const ValueKey('lan-settings')),
    );
    expect(tile.autofocus, isTrue);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump();
    store.dispose();
  });

  testWidgets('user management focuses Force Login on TV', (tester) async {
    final store = await createStore();
    await tester.pumpWidget(
      AppLayout(
        television: true,
        child: MaterialApp(home: ProfilesScreen(store: store)),
      ),
    );
    await tester.pump();

    final tile = tester.widget<SwitchListTile>(
      find.byKey(const ValueKey('force-login-setting')),
    );
    expect(tile.autofocus, isTrue);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump();
    store.dispose();
  });

  testWidgets('network settings focuses Connection Mode on TV', (
    tester,
  ) async {
    final store = await createStore();
    await tester.pumpWidget(
      AppLayout(
        television: true,
        child: MaterialApp(
          home: ResourceSettingsScreen(
            repository: _FocusTestRepository(),
            store: store,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    final mode = tester.widget<DropdownButton<String>>(
      find.byWidgetPredicate((widget) => widget is DropdownButton<String>),
    );
    expect(mode.autofocus, isTrue);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump();
    store.dispose();
  });
}
