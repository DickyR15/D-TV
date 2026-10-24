import 'package:hgtv/app_navigation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('reports route pop so TV Back handler can swallow trailing events', (
    tester,
  ) async {
    final observer = AppNavigationObserver();
    await tester.pumpWidget(
      MaterialApp(
        navigatorObservers: [observer],
        home: Builder(
          builder: (context) => Scaffold(
            body: Center(
              child: TextButton(
                key: const ValueKey('open'),
                onPressed: () {
                  Navigator.of(context).push<void>(
                    MaterialPageRoute<void>(
                      builder: (context) => Scaffold(
                        body: Center(
                          child: TextButton(
                            key: const ValueKey('close'),
                            onPressed: () => Navigator.of(context).pop(),
                            child: const Text('Close'),
                          ),
                        ),
                      ),
                    ),
                  );
                },
                child: const Text('Open'),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const ValueKey('open')));
    await tester.pumpAndSettle();
    expect(observer.changes.value?.event, AppNavigationEvent.push);

    await tester.tap(find.byKey(const ValueKey('close')));
    await tester.pumpAndSettle();
    expect(observer.changes.value?.event, AppNavigationEvent.pop);
  });
}
