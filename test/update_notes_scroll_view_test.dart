import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/update_notes_scroll_view.dart';

void main() {
  testWidgets('keeps the complete release notes in a bounded scroll viewport', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(800, 600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final notes = List.generate(
      45,
      (index) => '更新項目 ${index + 1}：完整更新說明內容，必須可以捲動讀取。',
    ).join('\n');

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Center(
            child: UpdateNotesScrollView(
              text: notes,
              maxHeight: 220,
              autofocus: true,
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text(notes), findsOneWidget);
    final scrollable = tester.state<ScrollableState>(
      find.byKey(const ValueKey('update-release-notes-scroll')),
    );
    expect(scrollable.position.maxScrollExtent, greaterThan(0));
    expect(scrollable.position.pixels, 0);

    await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
    await tester.pumpAndSettle();
    expect(scrollable.position.pixels, greaterThan(0));

    await tester.sendKeyEvent(LogicalKeyboardKey.end);
    await tester.pumpAndSettle();
    expect(
      scrollable.position.pixels,
      closeTo(scrollable.position.maxScrollExtent, 1),
    );
  });

  testWidgets('short release notes do not create unnecessary scrolling', (
    tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: Center(
            child: UpdateNotesScrollView(
              text: '目前已是最新版本。',
              maxHeight: 220,
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    final scrollable = tester.state<ScrollableState>(
      find.byKey(const ValueKey('update-release-notes-scroll')),
    );
    expect(scrollable.position.maxScrollExtent, 0);
    expect(find.text('目前已是最新版本。'), findsOneWidget);
  });
}
