import 'package:hgtv/back_safe_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets(
    'remote Back closes a nested player dialog without popping its route',
    (tester) async {
      var dismissed = false;
      await tester.pumpWidget(
        MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: TextButton(
                key: const ValueKey('player-route'),
                onPressed: () => showDialog<void>(
                  context: context,
                  builder: (_) => BackSafeDialog(
                    onDismissed: () => dismissed = true,
                    child: const AlertDialog(
                      key: ValueKey('player-nested-dialog'),
                      title: Text('選集'),
                    ),
                  ),
                ),
                child: const Text('播放器'),
              ),
            ),
          ),
        ),
      );

      await tester.tap(find.byKey(const ValueKey('player-route')));
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('player-nested-dialog')), findsOneWidget);

      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pumpAndSettle();

      expect(find.byKey(const ValueKey('player-nested-dialog')), findsNothing);
      expect(find.byKey(const ValueKey('player-route')), findsOneWidget);
      expect(dismissed, isTrue);
    },
  );
}
