import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/app_layout.dart';
import 'package:hgtv/home_screen.dart';
import 'package:hgtv/local_store.dart';
import 'package:hgtv/rankings_screen.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('AppDevice.resolveTelevisionMode', () {
    test('auto mode treats an Android emulator as a TV test environment', () {
      expect(
        AppDevice.resolveTelevisionMode(
          displayMode: 'auto',
          television: false,
          emulator: true,
        ),
        isTrue,
      );
    });

    test('auto mode keeps a physical phone in standard mode', () {
      expect(
        AppDevice.resolveTelevisionMode(
          displayMode: 'auto',
          television: false,
          emulator: false,
        ),
        isFalse,
      );
    });

    test('explicit standard mode overrides emulator detection', () {
      expect(
        AppDevice.resolveTelevisionMode(
          displayMode: 'standard',
          television: false,
          emulator: true,
        ),
        isFalse,
      );
    });

    test('explicit television mode remains available on a physical device', () {
      expect(
        AppDevice.resolveTelevisionMode(
          displayMode: 'television',
          television: false,
          emulator: false,
        ),
        isTrue,
      );
    });
  });

  testWidgets('emulator auto mode starts HomeScreen on Redfruit Hot with focus', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    final repository = FixtureRepository();
    final television = AppDevice.resolveTelevisionMode(
      displayMode: 'auto',
      television: false,
      emulator: true,
    );

    await tester.pumpWidget(
      AppLayout(
        television: television,
        child: MaterialApp(
          home: HomeScreen(repository: repository, store: store),
        ),
      ),
    );
    // HomeScreen has ongoing activity; only pump the initial focus/layout frames.
    await tester.pump(const Duration(milliseconds: 350));

    expect(find.byType(RankingsScreen), findsOneWidget);
    expect(
      FocusManager.instance.primaryFocus?.debugLabel,
      'D TV navigation 4',
    );

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump();
    store.dispose();
  });
}
