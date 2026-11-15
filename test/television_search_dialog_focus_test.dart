import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:hgtv/remote_widgets.dart';

void main() {
  testWidgets(
    'TV recent searches can be selected with the D-pad',
    (tester) async {
      await tester.binding.setSurfaceSize(const Size(960, 540));
      addTearDown(() => tester.binding.setSurfaceSize(null));
      String? submitted;

      await tester.pumpWidget(
        MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: Center(
                child: FilledButton(
                  onPressed: () async {
                    submitted = await showDialog<String>(
                      context: context,
                      builder: (_) => const TelevisionSearchDialog(
                        initialValue: '',
                        title: '搜尋短劇',
                        recentSearches: ['最近搜尋劇名'],
                      ),
                    );
                  },
                  child: const Text('開啟搜尋'),
                ),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('開啟搜尋'));
      await tester.pumpAndSettle();

      await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
      await tester.pumpAndSettle();
      final recentFinder = find.byKey(
        const ValueKey('recent-search-最近搜尋劇名'),
      );
      expect(recentFinder, findsOneWidget);
      expect(
        tester.widget<RemoteButton>(recentFinder).focusNode!.hasFocus,
        isTrue,
      );

      await tester.sendKeyEvent(LogicalKeyboardKey.enter);
      await tester.pumpAndSettle();
      expect(submitted, '最近搜尋劇名');
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets(
    'TV recent searches expose a D-pad clear-history action',
    (tester) async {
      await tester.binding.setSurfaceSize(const Size(960, 540));
      addTearDown(() => tester.binding.setSurfaceSize(null));
      var cleared = false;

      await tester.pumpWidget(
        MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: Center(
                child: FilledButton(
                  onPressed: () {
                    showDialog<void>(
                      context: context,
                      builder: (_) => TelevisionSearchDialog(
                        initialValue: '',
                        title: '搜尋短劇',
                        recentSearches: const ['最近搜尋劇名'],
                        onClearRecentSearches: () => cleared = true,
                      ),
                    );
                  },
                  child: const Text('開啟搜尋'),
                ),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('開啟搜尋'));
      await tester.pumpAndSettle();

      await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
      await tester.pumpAndSettle();
      final recentFinder = find.byKey(
        const ValueKey('recent-search-最近搜尋劇名'),
      );
      expect(
        tester.widget<RemoteButton>(recentFinder).focusNode!.hasFocus,
        isTrue,
      );

      await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
      await tester.pumpAndSettle();
      final clearFinder = find.byKey(
        const ValueKey('tv-drama-search-clear-recent'),
      );
      expect(clearFinder, findsOneWidget);
      expect(
        tester.widget<RemoteButton>(clearFinder).focusNode!.hasFocus,
        isTrue,
      );

      await tester.sendKeyEvent(LogicalKeyboardKey.enter);
      await tester.pumpAndSettle();
      expect(cleared, isTrue);
      expect(recentFinder, findsNothing);
      expect(clearFinder, findsNothing);
      expect(tester.takeException(), isNull);
    },
  );
}
