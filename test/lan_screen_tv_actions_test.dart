import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/app_layout.dart';
import 'package:hgtv/lan_controller.dart';
import 'package:hgtv/lan_screen.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets(
    'TV sync actions remain visible after opening interconnect from a large-screen route',
    (tester) async {
      tester.view.physicalSize = const Size(1280, 720);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);

      SharedPreferences.setMockInitialValues({});
      final store = testStore(await SharedPreferences.getInstance());
      final repository = FixtureRepository();
      final controller = LanController(repository, store, kind: 'tv');
      LanController.current = controller;

      await tester.pumpWidget(
        MaterialApp(
          // Match production: AppLayout is installed above the Navigator.
          // The device kind remains authoritative for TV-only sync actions.
          builder: (context, child) => AppLayout(
            television: false,
            formFactor: FormFactor.pad,
            child: child!,
          ),
          home: Builder(
            builder: (context) => Scaffold(
              body: Center(
                child: FilledButton(
                  onPressed: () => openLanSync(context),
                  child: const Text('開啟裝置互聯'),
                ),
              ),
            ),
          ),
        ),
      );

      await tester.tap(find.text('開啟裝置互聯'));
      await tester.pumpAndSettle();

      final syncNow = find.byKey(const ValueKey('lan-tv-sync-now'));
      final manualSync = find.byKey(const ValueKey('lan-tv-manual-sync'));
      expect(syncNow, findsOneWidget);
      expect(manualSync, findsOneWidget);
      expect(syncNow.hitTestable(), findsOneWidget);
      expect(manualSync.hitTestable(), findsOneWidget);
      expect(find.text('立即同步'), findsOneWidget);
      expect(find.text('手動同步'), findsOneWidget);

      // TV gets the pinned action buttons, not the mobile scroll-only copies.
      expect(find.byKey(const ValueKey('lan-sync-now')), findsNothing);
      expect(find.byKey(const ValueKey('lan-manual-sync')), findsNothing);
      expect(
        FocusManager.instance.primaryFocus?.debugLabel,
        'lan-device-interconnect',
      );

      final syncRect = tester.getRect(syncNow);
      final manualRect = tester.getRect(manualSync);
      expect(syncRect.top, greaterThanOrEqualTo(0));
      expect(manualRect.bottom, lessThanOrEqualTo(720));

      await tester.pumpWidget(const SizedBox.shrink());
      LanController.current = null;
      controller.dispose();
      store.dispose();
      await tester.pump();
    },
  );
}
