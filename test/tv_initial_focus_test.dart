import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/app_layout.dart';
import 'package:hgtv/lan_controller.dart';
import 'package:hgtv/lan_screen.dart';
import 'package:hgtv/local_store.dart';
import 'package:hgtv/profiles_screen.dart';
import 'package:hgtv/resource_settings_screen.dart';
import 'package:hgtv/settings_screen.dart';
import 'package:hgtv/sources_screen.dart';

import 'fixtures.dart';

class _SourceManagementRepository extends FixtureRepository {
  @override
  bool get supportsSourceManagement => true;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Future<LocalStore> makeStore() async {
    SharedPreferences.setMockInitialValues({});
    return testStore(await SharedPreferences.getInstance());
  }

  Future<void> pumpTv(WidgetTester tester, Widget child) async {
    await tester.pumpWidget(
      AppLayout(television: true, child: MaterialApp(home: child)),
    );
    await tester.pumpAndSettle();
  }

  testWidgets('settings starts focus on device interconnect', (tester) async {
    final store = await makeStore();
    await pumpTv(
      tester,
      SettingsScreen(
        repository: FixtureRepository(),
        store: store,
        embedded: true,
      ),
    );
    expect(
      FocusManager.instance.primaryFocus?.debugLabel,
      'settings-device-interconnect',
    );
    await tester.pumpWidget(const SizedBox.shrink());
    store.dispose();
  });

  testWidgets('device interconnect screen starts focus on its switch tile', (
    tester,
  ) async {
    final store = await makeStore();
    final controller = LanController(
      FixtureRepository(),
      store,
      kind: 'tv',
    );
    await pumpTv(tester, LanSyncScreen(controller: controller));
    expect(
      FocusManager.instance.primaryFocus?.debugLabel,
      'lan-device-interconnect',
    );
    await tester.pumpWidget(const SizedBox.shrink());
    controller.dispose();
    store.dispose();
  });

  testWidgets('source diagnostics has no manual update or check actions', (
    tester,
  ) async {
    final store = await makeStore();
    await pumpTv(
      tester,
      SourcesScreen(
        repository: _SourceManagementRepository(),
        store: store,
      ),
    );
    expect(find.text('片源診斷'), findsOneWidget);
    expect(find.byKey(const ValueKey('update-hongguo')), findsNothing);
    expect(find.text('更新'), findsNothing);
    expect(find.text('檢測連線與播放'), findsNothing);
    await tester.pumpWidget(const SizedBox.shrink());
    store.dispose();
  });

  testWidgets('user management starts focus on force-login switch', (
    tester,
  ) async {
    final store = await makeStore();
    await pumpTv(tester, ProfilesScreen(store: store));
    expect(
      FocusManager.instance.primaryFocus?.debugLabel,
      'force-login-setting',
    );
    await tester.pumpWidget(const SizedBox.shrink());
    store.dispose();
  });

  testWidgets('network settings starts focus on connection mode', (
    tester,
  ) async {
    final store = await makeStore();
    await pumpTv(
      tester,
      ResourceSettingsScreen(repository: FixtureRepository(), store: store),
    );
    expect(
      FocusManager.instance.primaryFocus?.debugLabel,
      'resource-connection-mode',
    );
    await tester.pumpWidget(const SizedBox.shrink());
    store.dispose();
  });
}
