import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/remote_widgets.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets(
    'TV remote long press waits for key-up and never also activates poster tap',
    (tester) async {
      final focusNode = FocusNode(debugLabel: 'poster-focus');
      addTearDown(focusNode.dispose);
      var tapCount = 0;
      var longPressCount = 0;

      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Center(
              child: RemoteTarget(
                focusNode: focusNode,
                autofocus: true,
                onPressed: () => tapCount++,
                onLongPress: () => longPressCount++,
                child: const Text('測試海報'),
              ),
            ),
          ),
        ),
      );
      await tester.pump();
      expect(focusNode.hasFocus, isTrue);

      await tester.sendKeyDownEvent(LogicalKeyboardKey.select);
      await tester.pump(const Duration(milliseconds: 400));
      await tester.sendKeyRepeatEvent(LogicalKeyboardKey.select);
      await tester.pump(const Duration(milliseconds: 400));

      // Repeated key events must not leak to ActivateIntent, and the action
      // dialog must not take focus while the physical key remains held.
      expect(tapCount, 0);
      expect(longPressCount, 0);

      await tester.sendKeyRepeatEvent(LogicalKeyboardKey.select);
      await tester.pump();
      expect(tapCount, 0);
      expect(longPressCount, 0);

      await tester.sendKeyUpEvent(LogicalKeyboardKey.select);
      await tester.pump();
      expect(tapCount, 0);
      expect(longPressCount, 1);

      // A later short confirmation remains a normal single activation.
      await tester.sendKeyDownEvent(LogicalKeyboardKey.select);
      await tester.pump(const Duration(milliseconds: 150));
      await tester.sendKeyUpEvent(LogicalKeyboardKey.select);
      await tester.pump();
      expect(tapCount, 1);
      expect(longPressCount, 1);
    },
  );
}
