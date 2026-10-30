import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/app_layout.dart';
import 'package:hgtv/lan_controller.dart';
import 'package:hgtv/lan_screen.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('TV keeps immediate and manual sync actions visible', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    final repository = FixtureRepository();
    final controller = LanController(repository, store, kind: 'tv');
    addTearDown(controller.dispose);
    addTearDown(store.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: AppLayout(
          television: true,
          formFactor: FormFactor.television,
          child: LanSyncScreen(controller: controller),
        ),
      ),
    );
    await tester.pump();

    expect(find.byKey(const ValueKey('lan-tv-sync-now')), findsOneWidget);
    expect(find.byKey(const ValueKey('lan-tv-manual-sync')), findsOneWidget);
    expect(find.text('立即同步'), findsOneWidget);
    expect(find.text('手動同步'), findsOneWidget);

    // The mobile/scroll-only copies are omitted on TV; the pinned actions
    // cannot be pushed below the visible viewport by the status cards.
    expect(find.byKey(const ValueKey('lan-sync-now')), findsNothing);
    expect(find.byKey(const ValueKey('lan-manual-sync')), findsNothing);
  });
}
