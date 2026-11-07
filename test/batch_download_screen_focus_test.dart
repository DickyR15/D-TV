import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:hgtv/app_layout.dart';
import 'package:hgtv/batch_download_screen.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('TV batch download opens with the quality selector focused', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    await tester.pumpWidget(
      AppLayout(
        television: true,
        formFactor: FormFactor.television,
        child: MaterialApp(
          home: BatchDownloadScreen(
            repository: FixtureRepository(),
            store: store,
            dramas: const [],
          ),
        ),
      ),
    );
    await tester.pump();

    final quality = tester.widget<DropdownButtonFormField<int>>(
      find.byKey(const ValueKey('batch-download-quality')),
    );
    expect(quality.autofocus, isTrue);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump();
  });

  testWidgets('non-TV batch download keeps the native touch dropdown mode', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    await tester.pumpWidget(
      AppLayout(
        television: false,
        formFactor: FormFactor.phone,
        child: MaterialApp(
          home: BatchDownloadScreen(
            repository: FixtureRepository(),
            store: store,
            dramas: const [],
          ),
        ),
      ),
    );
    await tester.pump();

    final quality = tester.widget<DropdownButtonFormField<int>>(
      find.byKey(const ValueKey('batch-download-quality')),
    );
    expect(quality.autofocus, isFalse);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump();
  });
}
