import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/main.dart';

void main() {
  testWidgets('startup reveal displays animated D TV wordmark and tagline', (
    tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          backgroundColor: Color(0xFF0B0D12),
          body: DtvLaunchAnimation(),
        ),
      ),
    );

    expect(find.text('D TV'), findsOneWidget);
    expect(find.text('短劇・漫劇・AI 劇  |  精彩隨選即播'), findsOneWidget);

    await tester.pump(const Duration(milliseconds: 900));
    expect(find.text('D TV'), findsOneWidget);
    await tester.pumpWidget(const SizedBox.shrink());
  });
}
