import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:hgtv/drama_actions.dart';
import 'package:hgtv/models.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('poster actions can open the work details page', (tester) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    var openedDetails = false;
    const drama = FixtureRepository.free;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (context) => TextButton(
              key: const ValueKey('open-poster-actions'),
              onPressed: () => showDramaActions(
                context,
                drama: drama,
                store: store,
                onDetails: () => openedDetails = true,
              ),
              child: const Text('更多操作'),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.byKey(const ValueKey('open-poster-actions')));
    await tester.pumpAndSettle();
    expect(find.byKey(const ValueKey('drama-action-details')), findsOneWidget);

    await tester.tap(find.byKey(const ValueKey('drama-action-details')));
    await tester.pumpAndSettle();
    expect(openedDetails, isTrue);
  });
}
