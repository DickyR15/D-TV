import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/diary_service.dart';

void main() {
  tearDown(() => DiaryService.clear());

  testWidgets('playback debug log list scrolls with remote up/down keys',
      (tester) async {
    DiaryService.clear();
    for (var i = 0; i < 80; i++) {
      DiaryService.add('playback log row $i');
    }

    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => Scaffold(
            body: Center(
              child: ElevatedButton(
                onPressed: () => DiaryService.showDiaryDialog(context),
                child: const Text('開啟紀錄'),
              ),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('開啟紀錄'));
    await tester.pumpAndSettle();

    final listFinder = find.byKey(const ValueKey('playback-log-list'));
    expect(listFinder, findsOneWidget);
    final scrollableFinder = find.descendant(
      of: listFinder,
      matching: find.byType(Scrollable),
    );
    final scrollable = tester.state<ScrollableState>(scrollableFinder.first);
    expect(scrollable.position.pixels, 0);

    await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
    await tester.pump();

    expect(scrollable.position.pixels, greaterThan(0));

    await tester.sendKeyEvent(LogicalKeyboardKey.arrowUp);
    await tester.pump();

    expect(scrollable.position.pixels, 0);
  });
}
