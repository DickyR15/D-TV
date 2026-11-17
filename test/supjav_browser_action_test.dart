import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/supjav_browser_action.dart';
import 'package:hgtv/supjav_browser_screen.dart';

void main() {
  group('SupJav HTTP challenge notices', () {
    test('detects a Cloudflare challenge from the documented response header', () {
      final notice = supJavHttpStatusNotice(
        url: Uri.parse('https://supjav.com/zh/'),
        statusCode: 403,
        headers: const {'cf-mitigated': 'challenge'},
        isMainFrame: true,
      );

      expect(notice, contains('Cloudflare 安全驗證'));
    });

    test('detects response headers without relying on header casing', () {
      final notice = supJavHttpStatusNotice(
        url: Uri.parse('https://www.supjav.com/zh/'),
        statusCode: 403,
        headers: const {'CF-Mitigated': ' Challenge '},
        isMainFrame: true,
      );

      expect(notice, contains('Cloudflare 安全驗證'));
    });

    test('does not label an ordinary 403 as a Cloudflare challenge', () {
      final notice = supJavHttpStatusNotice(
        url: Uri.parse('https://supjav.com/zh/'),
        statusCode: 403,
        headers: const {'content-type': 'text/html'},
        isMainFrame: true,
      );

      expect(notice, contains('HTTP 403'));
      expect(notice, isNot(contains('Cloudflare 安全驗證')));
    });

    test('ignores errors from subresources and unrelated hosts', () {
      expect(
        supJavHttpStatusNotice(
          url: Uri.parse('https://supjav.com/logo.png'),
          statusCode: 403,
          headers: const {'cf-mitigated': 'challenge'},
          isMainFrame: false,
        ),
        isNull,
      );
      expect(
        supJavHttpStatusNotice(
          url: Uri.parse('https://example.com/'),
          statusCode: 403,
          headers: const {'cf-mitigated': 'challenge'},
          isMainFrame: true,
        ),
        isNull,
      );
    });
  });

  testWidgets('opens the embedded browser route from the SupJav error action', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SupJavBrowserAction(
            browserBuilder: (_) => const Scaffold(
              body: Center(child: Text('SupJav 站內瀏覽器測試頁')),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.byKey(const ValueKey('supjav-open-browser')));
    await tester.pumpAndSettle();

    expect(find.text('SupJav 站內瀏覽器測試頁'), findsOneWidget);
  });
}
