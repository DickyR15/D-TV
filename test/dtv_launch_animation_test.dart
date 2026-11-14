import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/dtv_launch_animation.dart';

void main() {
  testWidgets('D TV cold-start reveal completes and fades out', (tester) async {
    var completed = false;
    await tester.pumpWidget(
      MaterialApp(
        home: DtvLaunchAnimation(onCompleted: () => completed = true),
      ),
    );

    expect(find.text('D TV'), findsOneWidget);
    expect(find.text('短劇・漫劇・AI 劇  |  精彩隨選即播'), findsOneWidget);

    await tester.pump(const Duration(milliseconds: 2500));
    await tester.pump(const Duration(milliseconds: 500));

    expect(completed, isTrue);
  });
}
