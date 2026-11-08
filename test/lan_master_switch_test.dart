import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/lan_controller.dart';
import 'package:hgtv/local_store.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('legacy split settings cannot enable LAN when automatic sync is off', () async {
    SharedPreferences.setMockInitialValues({
      'lanSettings': jsonEncode({
        'enabled': true,
        'autoSync': false,
      }),
    });
    final store = LocalStore(await SharedPreferences.getInstance());
    final controller = LanController(
      FixtureRepository(),
      store,
      kind: 'computer',
    );
    addTearDown(controller.dispose);
    addTearDown(store.dispose);

    expect(controller.autoSync, isFalse);
    expect(controller.enabled, isFalse);
    expect(controller.receiving, isFalse);
  });

  test('disabling the master switch persists both legacy flags as false', () async {
    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    final controller = LanController(
      FixtureRepository(),
      store,
      kind: 'computer',
    );
    addTearDown(controller.dispose);
    addTearDown(store.dispose);

    await controller.setAutoSync(false);

    expect(store.lanSettings['enabled'], isFalse);
    expect(store.lanSettings['autoSync'], isFalse);
    expect(controller.enabled, isFalse);
    expect(controller.autoSync, isFalse);
    expect(controller.receiving, isFalse);
  });
}
