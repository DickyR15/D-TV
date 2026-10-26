import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/app_layout.dart';
import 'package:hgtv/local_store.dart';
import 'package:hgtv/sources_screen.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

class _SourceManagementRepository extends FixtureRepository {
  @override
  bool get supportsSourceManagement => true;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('source management focuses the first visible Update button', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    final repository = _SourceManagementRepository();

    await tester.pumpWidget(
      AppLayout(
        television: true,
        child: MaterialApp(
          home: SourcesScreen(repository: repository, store: store),
        ),
      ),
    );
    await tester.pumpAndSettle();

    final updateButton = find.byKey(const ValueKey('update-hongguo'));
    expect(updateButton, findsOneWidget);
    final updateNode = tester.widget<FilledButton>(updateButton).focusNode!;
    expect(updateNode.hasPrimaryFocus, isTrue);
    expect(FocusManager.instance.primaryFocus, same(updateNode));

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump();
    store.dispose();
  });
}
