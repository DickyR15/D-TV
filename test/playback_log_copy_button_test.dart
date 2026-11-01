import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/diary_service.dart';

void main() {
  testWidgets('playback log dialog has one copy action', (tester) async {
    DiaryService.nativeLogPath = null;
    DiaryService.clear();

    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => Scaffold(
            body: TextButton(
              onPressed: () => DiaryService.showDiaryDialog(context),
              child: const Text('開啟紀錄'),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('開啟紀錄'));
    await tester.pumpAndSettle();

    expect(find.text('一鍵複製'), findsOneWidget);
    expect(find.byTooltip('複製全部紀錄'), findsNothing);
    expect(find.byIcon(Icons.copy_rounded), findsOneWidget);

    await tester.tap(find.text('關閉'));
    await tester.pumpAndSettle();
    await tester.pumpWidget(const SizedBox.shrink());
  });
}
