import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/app_layout.dart';
import 'package:hgtv/app_theme.dart';
import 'package:hgtv/remote_widgets.dart';

void main() {
  test('selected chips use a teal container distinct from the orange focus palette', () {
    for (final theme in [AppTheme.light, AppTheme.dark]) {
      expect(theme.chipTheme.selectedColor, theme.colorScheme.secondaryContainer);
      expect(theme.colorScheme.secondaryContainer, isNot(theme.colorScheme.primaryContainer));
    }
  });

  testWidgets('TV selected state stays teal while focus uses a neutral fill and orange outline', (tester) async {
    const selectedKey = ValueKey('selected-option');
    const focusKey = ValueKey('focus-only-option');
    final focusNode = FocusNode(debugLabel: 'focus-only-option');
    addTearDown(focusNode.dispose);

    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.dark,
        home: Scaffold(
          body: AppLayout(
            television: true,
            child: Center(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  RemoteTarget(
                    key: selectedKey,
                    selected: true,
                    onPressed: () {},
                    child: const Text('已選取'),
                  ),
                  RemoteTarget(
                    key: focusKey,
                    focusNode: focusNode,
                    onPressed: () {},
                    child: const Text('只有焦點'),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
    await tester.pump();
    focusNode.requestFocus();
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 150));

    final colors = AppTheme.dark.colorScheme;
    final selected = tester.widget<AnimatedContainer>(
      find.descendant(of: find.byKey(selectedKey), matching: find.byType(AnimatedContainer)),
    );
    final focused = tester.widget<AnimatedContainer>(
      find.descendant(of: find.byKey(focusKey), matching: find.byType(AnimatedContainer)),
    );
    final selectedDecoration = selected.decoration! as BoxDecoration;
    final focusedDecoration = focused.decoration! as BoxDecoration;

    expect(selectedDecoration.color, colors.secondaryContainer);
    expect(focusedDecoration.color, colors.surfaceContainerHighest);
    expect(focusedDecoration.border!.top.color, colors.primary);
    expect(selectedDecoration.color, isNot(focusedDecoration.color));
  });
}
