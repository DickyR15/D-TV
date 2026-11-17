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

    DiaryService.add('[Playback] synthetic test log');
    await tester.tap(find.text('Open logs'));
    await tester.pumpAndSettle();

    expect(find.text('一鍵複製'), findsOneWidget);
    expect(find.text('全部'), findsOneWidget);
    expect(find.text('播放'), findsOneWidget);
    expect(find.text('網路'), findsOneWidget);
    expect(find.text('錯誤'), findsOneWidget);
    expect(find.byTooltip('複製全部紀錄'), findsNothing);
    expect(find.byTooltip('清空紀錄'), findsOneWidget);
    await tester.tap(find.byTooltip('清空紀錄'));
    await tester.pumpAndSettle();
    expect(find.text('清空播放除錯紀錄？'), findsOneWidget);
    expect(
      find.text('只會清除診斷紀錄，不會刪除最近觀看、收藏、播放進度或其他本機資料。'),
      findsOneWidget,
    );
    await tester.tap(find.text('取消'));
    await tester.pumpAndSettle();
    expect(find.textContaining('[Playback] synthetic test log'), findsOneWidget);

    await tester.tap(find.text('關閉'));
    await tester.pumpAndSettle();
  });
}
