import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/widgets.dart';

void main() {
  testWidgets('D-TV branded toast keeps its message on one line', (
    tester,
  ) async {
    await tester.view.setPhysicalSize(const Size(800, 600));
    await tester.view.setDevicePixelRatio(1);
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (context) => TextButton(
              onPressed: () => showDtvToast(
                context,
                '解碼方式已儲存；重新進入播放頁面即可套用，無須重啟 D TV。',
                duration: const Duration(seconds: 10),
              ),
              child: const Text('顯示提示'),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('顯示提示'));
    await tester.pump();

    final toast = tester.widget<SnackBar>(
      find.byKey(const ValueKey<String>('dtv-brand-toast')),
    );
    expect(toast.behavior, SnackBarBehavior.floating);
    expect(toast.content, isA<Row>());

    final row = toast.content as Row;
    expect(row.children.first, isA<DtvBrandMark>());
    final flexible = row.children.whereType<Flexible>().single;
    final message = flexible.child as Text;
    expect(message.maxLines, 1);
    expect(message.softWrap, isFalse);
    expect(message.overflow, TextOverflow.ellipsis);
  });
}
