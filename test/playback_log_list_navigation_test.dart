import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/diary_service.dart';

void main() {
  tearDown(() => DiaryService.clear());

  testWidgets('clear button clears visible logs and native diagnostics', (
    tester,
  ) async {
    DiaryService.clear();
    DiaryService.add('old playback entry');
    var nativeCleared = false;

    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => Scaffold(
            body: Center(
              child: ElevatedButton(
                onPressed: () => DiaryService.showDiaryDialog(
                  context,
                  clearNativeLogs: () async {
                    nativeCleared = true;
                  },
                ),
                child: const Text('開啟紀錄'),
              ),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('開啟紀錄'));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('清空紀錄'));
    await tester.pumpAndSettle();

    expect(nativeCleared, isTrue);
    expect(DiaryService.entries, isEmpty);
  });

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

    expect(
      FocusManager.instance.primaryFocus?.debugLabel,
      'playback-log-close',
    );

    final listFinder = find.byKey(const ValueKey('playback-log-list'));
    expect(listFinder, findsOneWidget);
    final scrollableFinder = find.descendant(
      of: listFinder,
      matching: find.byType(Scrollable),
    );
    final scrollable = tester.state<ScrollableState>(scrollableFinder.first);
    expect(scrollable.position.pixels, 0);

    // The dialog now deliberately starts with focus on Close. Move focus
    // into the log list before asserting that D-pad scrolling changes its
    // scroll position.
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowUp);
    await tester.pumpAndSettle();
    expect(
      FocusManager.instance.primaryFocus?.debugLabel,
      'playback-debug-log-list',
    );

    await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
    await tester.pump();

    expect(scrollable.position.pixels, greaterThan(0));

    await tester.sendKeyEvent(LogicalKeyboardKey.arrowUp);
    await tester.pump();

    expect(scrollable.position.pixels, 0);
  });
}
