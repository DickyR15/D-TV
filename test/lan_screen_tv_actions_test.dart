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
    'Android TV keeps both original sync actions in the scrollable interconnect page',
    (tester) async {
      tester.view.physicalSize = const Size(1280, 720);
      tester.view.devicePixelRatio = 1;
      tester.view.viewPadding = const FakeViewPadding(bottom: 48);
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      addTearDown(tester.view.resetViewPadding);

      SharedPreferences.setMockInitialValues({});
      final store = testStore(await SharedPreferences.getInstance());
      final repository = FixtureRepository();
      final controller = LanController(repository, store, kind: 'tv');
      controller.connection = LanConnection(
        peer: const LanPeer(
          id: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
          name: '測試裝置',
          pin: 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
          kind: 'tv',
          port: 43210,
        ),
        address: '192.168.1.20',
        token: 'test-token',
        account: 'cccccccccccccccccccccccccccccccc',
        user: '測試使用者',
        sources: const {'hongguo'},
        autoSync: true,
      );

      await tester.pumpWidget(
        AppLayout(
          television: true,
          formFactor: FormFactor.television,
          child: MaterialApp(
            home: LanSyncScreen(controller: controller),
          ),
        ),
      );
      await tester.pump();

      final syncNow = find.byKey(const ValueKey('lan-sync-now'));
      final manualSync = find.byKey(const ValueKey('lan-manual-sync'));
      expect(syncNow, findsOneWidget);
      expect(manualSync, findsOneWidget);
      expect(find.text('立即同步'), findsOneWidget);
      expect(find.text('手動同步'), findsOneWidget);

      // The actions remain in the original list rather than a new fixed footer.
      expect(find.byKey(const ValueKey('lan-tv-sync-now')), findsNothing);
      expect(find.byKey(const ValueKey('lan-tv-manual-sync')), findsNothing);

      final list = find.byType(ListView).first;
      await tester.drag(list, const Offset(0, -1200));
      await tester.pumpAndSettle();
      await tester.ensureVisible(manualSync);
      await tester.pumpAndSettle();
      final manualRect = tester.getRect(manualSync);
      expect(manualRect.top, greaterThanOrEqualTo(0));
      expect(manualRect.bottom, lessThanOrEqualTo(720 - 48));
      expect(manualSync.hitTestable(), findsOneWidget);

      // The immediate action can also be brought into view independently.
      await tester.ensureVisible(syncNow);
      await tester.pumpAndSettle();
      final syncRect = tester.getRect(syncNow);
      expect(syncRect.top, greaterThanOrEqualTo(0));
      expect(syncRect.bottom, lessThanOrEqualTo(720 - 48));
      expect(syncNow.hitTestable(), findsOneWidget);

      await tester.pumpWidget(const SizedBox.shrink());
      controller.dispose();
      store.dispose();
      await tester.pump();
    },
  );
}
