import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:hgtv/app_build.dart';
import 'package:hgtv/app_layout.dart';
import 'package:hgtv/models.dart';
import 'package:hgtv/source_management_screen.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('source management removes the Dart Bad state prefix', () {
    expect(
      sourceManagementErrorMessage(StateError('密碼不正確')),
      '密碼不正確',
    );
    expect(
      sourceManagementErrorMessage(Exception('連線失敗')),
      contains('連線失敗'),
    );
    expect(
      sourceManagementErrorMessage(StateError('Bad state: 密碼不正確')),
      '密碼不正確',
    );
  });

  testWidgets('wrong admin password never displays the Dart Bad state prefix', (tester) async {
    if (!allSourcesEnabled) return;
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await store.saveProfile(
      id: 'default',
      name: '管理員',
      sources: const [],
      download: true,
      pin: 'source-pass-123',
    );

    await tester.pumpWidget(
      MaterialApp(home: SourceManagementScreen(store: store)),
    );
    await tester.enterText(
      find.byKey(const ValueKey('advanced-source-password')),
      'wrong-password',
    );
    final unlockButton = find.byKey(const ValueKey('unlock-advanced-sources'));
    await tester.ensureVisible(unlockButton);
    await tester.pumpAndSettle();
    await tester.tap(unlockButton);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 100));
    await tester.pumpAndSettle();

    expect(store.advancedSourcesUnlocked, isFalse);
    expect(find.text('密碼不正確'), findsOneWidget);
    expect(find.textContaining('Bad state:'), findsNothing);
  });

  testWidgets('three primary sources and guidance are visible in every edition', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await store.saveProfile(
      id: 'default',
      name: '管理員',
      sources: const [],
      download: true,
      pin: 'source-pass-123',
    );

    await tester.pumpWidget(
      MaterialApp(home: SourceManagementScreen(store: store)),
    );
    expect(find.text('紅果'), findsOneWidget);
    expect(find.text('韓小圈'), findsOneWidget);
    expect(find.text('多多追劇'), findsOneWidget);
    expect(find.text('電影 · 電視劇 · 動漫 · 綜藝'), findsOneWidget);
    expect(
      find.textContaining(
        '預設開啟 3 個一般片源：紅果、韓小圈、多多追劇。',
      ),
      findsOneWidget,
    );
    expect(store.sourceEnabled('hongguo'), isTrue);
    expect(store.sourceEnabled('hanxiaoquan'), isTrue);
    expect(store.sourceEnabled('duoduo'), isTrue);
  });

  testWidgets(
    'TV standard edition starts on the first available general source control',
    (tester) async {
      if (allSourcesEnabled) return;
      SharedPreferences.setMockInitialValues({});
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);

      await tester.pumpWidget(
        AppLayout(
          television: true,
          formFactor: FormFactor.television,
          child: MaterialApp(home: SourceManagementScreen(store: store)),
        ),
      );
      await tester.pump();

      final firstGeneral = tester.widget<SwitchListTile>(
        find.byKey(const ValueKey('source-switch-hongguo')),
      );
      expect(firstGeneral.focusNode!.hasPrimaryFocus, isTrue);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    },
  );

  testWidgets(
    'TV source management focuses setup-password action before password setup',
    (tester) async {
      if (!allSourcesEnabled) return;
      SharedPreferences.setMockInitialValues({});
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);

      await tester.pumpWidget(
        AppLayout(
          television: true,
          formFactor: FormFactor.television,
          child: MaterialApp(home: SourceManagementScreen(store: store)),
        ),
      );
      await tester.pump();

      final setup = tester.widget<OutlinedButton>(
        find.byKey(const ValueKey('configure-admin-source-password')),
      );
      expect(setup.focusNode!.hasPrimaryFocus, isTrue);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    },
  );

  testWidgets(
    'TV source management moves focus from password gate to first advanced source after unlock',
    (tester) async {
      if (!allSourcesEnabled) return;
      SharedPreferences.setMockInitialValues({});
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);
      await store.saveProfile(
        id: 'default',
        name: '管理員',
        sources: const [],
        download: true,
        pin: 'source-pass-123',
      );

      await tester.pumpWidget(
        AppLayout(
          television: true,
          formFactor: FormFactor.television,
          child: MaterialApp(home: SourceManagementScreen(store: store)),
        ),
      );
      await tester.pump();

      final password = tester.widget<TextField>(
        find.byKey(const ValueKey('advanced-source-password')),
      );
      expect(password.focusNode!.hasPrimaryFocus, isTrue);
      await tester.enterText(
        find.byKey(const ValueKey('advanced-source-password')),
        'source-pass-123',
      );
      await tester.testTextInput.receiveAction(TextInputAction.done);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 100));

      final firstAdvanced = tester.widget<SwitchListTile>(
        find.byKey(const ValueKey('source-switch-huangdou')),
      );
      expect(firstAdvanced.focusNode!.hasPrimaryFocus, isTrue);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    },
  );

  testWidgets('advanced source toggles are hidden until admin password is verified', (
    tester,
  ) async {
    if (!allSourcesEnabled) return;

    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await store.saveProfile(
      id: 'default',
      name: '管理員',
      sources: const [],
      download: true,
      pin: 'source-pass-123',
    );

    await tester.pumpWidget(
      MaterialApp(home: SourceManagementScreen(store: store)),
    );
    expect(find.text('紅果'), findsOneWidget);
    expect(find.text('韓小圈'), findsOneWidget);
    expect(find.text('多多追劇'), findsOneWidget);
    expect(find.text('暴風'), findsNothing);
    expect(find.text('極速'), findsNothing);
    expect(find.text('360'), findsNothing);
    expect(find.text('虎牙'), findsNothing);

    expect(find.text('西瓜影視'), findsNothing);
    expect(find.text('管理功能已上鎖'), findsOneWidget);
    expect(find.text('進階片源'), findsNothing);
    expect(
      find.textContaining(
        '預設開啟 3 個一般片源：紅果、韓小圈、多多追劇。',
      ),
      findsOneWidget,
    );
    expect(find.textContaining('七個進階片源'), findsNothing);
    expect(find.textContaining('忘記管理員密碼'), findsNothing);
    expect(find.textContaining('shared_preferences'), findsNothing);
    expect(find.text('黃豆'), findsNothing);

    await tester.enterText(
      find.byKey(const ValueKey('advanced-source-password')),
      'source-pass-123',
    );
    final unlockButton = find.byKey(const ValueKey('unlock-advanced-sources'));
    await tester.ensureVisible(unlockButton);
    await tester.pumpAndSettle();
    await tester.tap(unlockButton);
    await tester.pumpAndSettle();

    expect(store.advancedSourcesUnlocked, isTrue);
    expect(find.text('進階片源'), findsOneWidget);
    // This test covers the password gate and the first advanced switch.
    // The complete source registry and all source permissions are covered by
    // source_switches_test.dart; do not assert lazy ListView children here.
    final huangdouSwitch = find.byKey(const ValueKey('source-switch-huangdou'));
    await tester.ensureVisible(huangdouSwitch);
    await tester.pumpAndSettle();
    expect(huangdouSwitch, findsOneWidget);
    expect(store.sourceEnabled('huangdou'), isFalse);

    await tester.tap(huangdouSwitch);
    await tester.pumpAndSettle();
    expect(store.sourceEnabled('huangdou'), isTrue);


    await tester.pumpWidget(const MaterialApp(home: Text('已離開片源管理')));
    await tester.pumpAndSettle();
    expect(store.advancedSourcesUnlocked, isFalse);
    await expectLater(
      store.setSourceEnabled('huangdou', false),
      throwsStateError,
    );
  });

  testWidgets('iPhone-sized source list keeps other switches active during one pending write', (tester) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    final pendingWrite = Completer<void>();
    addTearDown(() {
      if (!pendingWrite.isCompleted) pendingWrite.complete();
    });

    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      AppLayout(
        television: false,
        formFactor: FormFactor.phone,
        child: MaterialApp(
          theme: ThemeData(platform: TargetPlatform.iOS),
          home: SourceManagementScreen(
            store: store,
            updateSourceEnabled: (sourceId, enabled) async {
              if (sourceId == 'hongguo') await pendingWrite.future;
              await store.setSourceEnabled(sourceId, enabled);
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    final first = find.byKey(const ValueKey('source-switch-hongguo'));
    final other = find.byKey(const ValueKey('source-switch-hanxiaoquan'));
    await tester.ensureVisible(first);
    await tester.pumpAndSettle();
    final otherElement = tester.element(other);

    await tester.tap(first);
    await tester.pump();

    expect(tester.widget<SwitchListTile>(first).onChanged, isNull);
    expect(tester.widget<SwitchListTile>(other).onChanged, isNotNull);
    expect(tester.widget<SwitchListTile>(other).value, isTrue);
    expect(identical(tester.element(other), otherElement), isTrue);
    expect(store.sourceEnabled('hongguo'), isTrue);

    pendingWrite.complete();
    await tester.pumpAndSettle();
    expect(store.sourceEnabled('hongguo'), isFalse);
    expect(tester.widget<SwitchListTile>(other).onChanged, isNotNull);
    expect(tester.takeException(), isNull);
  });

  test('advanced source order persists and controls permitted source ordering', () async {
    if (!allSourcesEnabled) return;
    SharedPreferences.setMockInitialValues({});
    final preferences = await SharedPreferences.getInstance();
    final store = testStore(preferences);
    addTearDown(store.dispose);
    await store.saveProfile(
      id: 'default',
      name: '管理員',
      sources: const [],
      download: true,
      pin: 'source-pass-123',
    );
    await store.unlockAdvancedSources('source-pass-123');

    final order = store.advancedSourceOrder.reversed.toList();
    await store.setAdvancedSourceOrder(order);
    expect(store.advancedSourceOrder, order);
    expect(
      store.permittedSources
          .where((source) => SourceSite.isRestricted(source.id))
          .map((source) => source.id)
          .toList(),
      order,
    );

    final restored = testStore(preferences);
    addTearDown(restored.dispose);
    expect(restored.advancedSourceOrder, order);
    await restored.switchProfile('default', pin: 'source-pass-123');
    expect(
      restored.permittedSources
          .where((source) => SourceSite.isRestricted(source.id))
          .map((source) => source.id)
          .toList(),
      order,
    );
  });

  testWidgets('TV advanced sources use hold-to-select, D-pad reorder and short-press toggle', (
    tester,
  ) async {
    if (!allSourcesEnabled) return;
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await store.saveProfile(
      id: 'default',
      name: '管理員',
      sources: const [],
      download: true,
      pin: 'source-pass-123',
    );

    await tester.pumpWidget(
      AppLayout(
        television: true,
        formFactor: FormFactor.television,
        child: MaterialApp(home: SourceManagementScreen(store: store)),
      ),
    );
    await tester.pump();
    await tester.enterText(
      find.byKey(const ValueKey('advanced-source-password')),
      'source-pass-123',
    );
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 100));
    await tester.pumpAndSettle();

    final initial = store.advancedSourceOrder;
    final firstSource = initial.first;
    final firstTile = find.byKey(ValueKey('source-switch-$firstSource'));
    await tester.ensureVisible(firstTile);
    await tester.pumpAndSettle();
    final firstFocus = tester.widget<SwitchListTile>(firstTile).focusNode!;
    firstFocus.requestFocus();
    await tester.pump();

    await tester.sendKeyDownEvent(LogicalKeyboardKey.select);
    await tester.pump(const Duration(milliseconds: 600));
    await tester.sendKeyUpEvent(LogicalKeyboardKey.select);
    await tester.pump();
    expect(find.text('已選取：使用遙控器方向鍵移動，按確認完成'), findsOneWidget);

    await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
    await tester.pumpAndSettle();
    expect(
      store.advancedSourceOrder.take(2).toList(),
      [initial[1], initial[0]],
    );
    expect(
      find.byKey(ValueKey('source-move-down-$firstSource')),
      findsNothing,
    );

    await tester.sendKeyDownEvent(LogicalKeyboardKey.select);
    await tester.sendKeyUpEvent(LogicalKeyboardKey.select);
    await tester.pumpAndSettle();
    expect(find.text('已選取：使用遙控器方向鍵移動，按確認完成'), findsNothing);

    final shortPressSource = store.advancedSourceOrder.first;
    expect(store.sourceEnabled(shortPressSource), isFalse);
    final shortPressTile = find.byKey(
      ValueKey('source-switch-$shortPressSource'),
    );
    await tester.ensureVisible(shortPressTile);
    final shortPressFocus =
        tester.widget<SwitchListTile>(shortPressTile).focusNode!;
    shortPressFocus.requestFocus();
    await tester.pump();
    await tester.sendKeyDownEvent(LogicalKeyboardKey.select);
    await tester.pump(const Duration(milliseconds: 100));
    await tester.sendKeyUpEvent(LogicalKeyboardKey.select);
    await tester.pumpAndSettle();
    expect(store.sourceEnabled(shortPressSource), isTrue);
    expect(tester.takeException(), isNull);
  });

  testWidgets('advanced source rows can be dragged by holding the title area', (
    tester,
  ) async {
    if (!allSourcesEnabled) return;
    tester.view.physicalSize = const Size(1280, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await store.saveProfile(
      id: 'default',
      name: '管理員',
      sources: const [],
      download: true,
      pin: 'source-pass-123',
    );

    await tester.pumpWidget(
      AppLayout(
        television: false,
        formFactor: FormFactor.phone,
        child: MaterialApp(
          home: SourceManagementScreen(store: store),
        ),
      ),
    );
    await tester.enterText(
      find.byKey(const ValueKey('advanced-source-password')),
      'source-pass-123',
    );
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pumpAndSettle();
    expect(store.advancedSourcesUnlocked, isTrue);

    final initial = store.advancedSourceOrder;
    final firstId = initial[0];
    final secondId = initial[1];
    final firstTile = find.byKey(ValueKey('source-switch-$firstId'));
    final secondTile = find.byKey(ValueKey('source-switch-$secondId'));
    final title = find.descendant(
      of: firstTile,
      matching: find.text(SourceSite.byId(firstId).name),
    );
    await tester.pumpAndSettle();
    await tester.ensureVisible(firstTile);
    await tester.ensureVisible(secondTile);
    await tester.pumpAndSettle();
    expect(
      find.byKey(ValueKey('reorderable-source-$firstId')),
      findsOneWidget,
    );

    final gesture = await tester.startGesture(tester.getCenter(title));
    await tester.pump(const Duration(milliseconds: 600));
    final secondTileSize = tester.getSize(secondTile);
    await gesture.moveTo(
      tester.getCenter(secondTile) + Offset(0, secondTileSize.height * 0.4),
    );
    await tester.pump(const Duration(milliseconds: 700));
    await gesture.up();
    await tester.pumpAndSettle();

    expect(
      store.advancedSourceOrder.take(2).toList(),
      [secondId, firstId],
    );
    expect(tester.takeException(), isNull);
  });

}
