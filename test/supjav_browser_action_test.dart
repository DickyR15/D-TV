import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/supjav_browser_action.dart';

void main() {
  testWidgets('opens the official SupJav page using the supplied launcher', (
    tester,
  ) async {
    Uri? requestedUrl;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SupJavBrowserAction(
            launcher: (url) async {
              requestedUrl = url;
              return true;
            },
          ),
        ),
      ),
    );

    await tester.tap(find.byKey(const ValueKey('supjav-open-browser')));
    await tester.pumpAndSettle();

    expect(requestedUrl, Uri.parse('https://supjav.com/zh/'));
  });

  testWidgets('explains that browser verification is not synced to D TV', (
    tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(body: SupJavBrowserAction()),
      ),
    );

    expect(find.textContaining('驗證狀態不會同步到 D-TV'), findsOneWidget);
    expect(find.text('開啟 SupJav 官方網站'), findsOneWidget);
  });
}
