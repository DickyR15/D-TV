import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/diary_service.dart';

void main() {
  testWidgets('playback log dialog exposes only one copy action', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (context) => ElevatedButton(
              onPressed: () => DiaryService.showDiaryDialog(context),
              child: const Text('Open logs'),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('Open logs'));
    await tester.pumpAndSettle();

    expect(find.text('一鍵複製'), findsOneWidget);
    expect(find.byTooltip('複製全部紀錄'), findsNothing);
    expect(find.byTooltip('清空紀錄'), findsOneWidget);

    await tester.tap(find.text('關閉'));
    await tester.pumpAndSettle();
  });
}
