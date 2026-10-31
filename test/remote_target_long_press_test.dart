import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/remote_widgets.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets(
    'TV remote long press opens immediately and suppresses repeats and key-up',
    (tester) async {
      final posterFocus = FocusNode(debugLabel: 'poster-focus');
      final menuFocus = FocusNode(debugLabel: 'menu-focus');
      addTearDown(posterFocus.dispose);
      addTearDown(menuFocus.dispose);
      var tapCount = 0;
      var longPressCount = 0;

      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Center(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  RemoteTarget(
                    focusNode: posterFocus,
                    autofocus: true,
                    onPressed: () => tapCount++,
                    onLongPress: () {
                      longPressCount++;
                      menuFocus.requestFocus();
                    },
                    child: const Text('測試海報'),
                  ),
                  Focus(
                    focusNode: menuFocus,
                    onKeyEvent: (_, event) =>
                        RemoteActivationKeyGuard.handle(event)
                            ? KeyEventResult.handled
                            : KeyEventResult.ignored,
                    child: const Text('選單焦點'),
                  ),
                ],
              ),
            ),
          ),
        ),
      );
      await tester.pump();
      expect(posterFocus.hasFocus, isTrue);

      await tester.sendKeyDownEvent(LogicalKeyboardKey.select);
      await tester.pump(const Duration(milliseconds: 650));

      // The menu callback fires while the physical confirmation key is held.
      expect(longPressCount, 1);
      expect(menuFocus.hasFocus, isTrue);
      expect(tapCount, 0);
      expect(RemoteActivationKeyGuard.isBlocking, isTrue);

      // Repeats and key-up from the same hold must not activate the menu or poster.
      await tester.sendKeyRepeatEvent(LogicalKeyboardKey.select);
      await tester.pump();
      await tester.sendKeyUpEvent(LogicalKeyboardKey.select);
      await tester.pump();
      expect(tapCount, 0);
      expect(longPressCount, 1);
      expect(RemoteActivationKeyGuard.isBlocking, isFalse);

      // A new short confirmation still plays the poster exactly once.
      menuFocus.unfocus();
      posterFocus.requestFocus();
      await tester.pump();
      await tester.sendKeyDownEvent(LogicalKeyboardKey.select);
      await tester.pump(const Duration(milliseconds: 150));
      await tester.sendKeyUpEvent(LogicalKeyboardKey.select);
      await tester.pump();
      expect(tapCount, 1);
      expect(longPressCount, 1);
    },
  );
}
