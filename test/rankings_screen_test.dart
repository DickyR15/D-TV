import 'dart:io';
import 'dart:convert';

import 'package:hgtv/app_layout.dart';
import 'package:hgtv/models.dart';
import 'package:hgtv/ranking_models.dart';
import 'package:hgtv/rankings_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

class _RankingRepository extends FixtureRepository {
  @override
  Future<String> cover(Drama drama, {bool force = false}) async {
    final file = File('${Directory.systemTemp.path}/d-tv-test-cover.png');
    if (!await file.exists()) {
      await file.writeAsBytes(
        base64Decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/G7sAAAAASUVORK5CYII='),
      );
    }
    return file.path;
  }
  int rankingCalls = 0;
  bool timeoutFirstRanking = false;

  @override
  Future<List<RankingBoard>> rankingBoards() async => const [
    RankingBoard(id: 'hongguo-hot', source: 'hongguo', name: '熱播'),
  ];

  @override
  Future<RankingPage> rankings(
    String board, {
    int page = 1,
    bool force = false,
  }) async {
    rankingCalls++;
    if (timeoutFirstRanking && rankingCalls == 1) {
      throw StateError('context deadline exceeded');
    }
    return RankingPage(
    page: page,
    hasMore: page < 2,
    items: [
      for (var i = 1; i <= 10; i++)
        RankingItem(
          (page - 1) * 10 + i,
          Drama(
            id: 'hongguo:rank-${(page - 1) * 10 + i}',
            source: 'hongguo',
            title: '合成榜單${(page - 1) * 10 + i}',
            episodes: 1,
          ),
        ),
    ],
    );
  }
}

void main() {
  testWidgets('automatically retries an initial ranking timeout', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await tester.binding.setSurfaceSize(const Size(960, 540));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    final repository = _RankingRepository()..timeoutFirstRanking = true;
    await tester.pumpWidget(
      MaterialApp(
        home: RankingsScreen(
          repository: repository,
          store: store,
          initialGroup: 'hongguo',
        ),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 900));
    await tester.pumpAndSettle();

    expect(repository.rankingCalls, greaterThanOrEqualTo(2));
    expect(find.text('合成榜單1'), findsOneWidget);
    expect(find.text('榜單請求逾時，請重試或稍後再試。'), findsNothing);
  });

  testWidgets('television remote moves focus through ranking list', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await tester.binding.setSurfaceSize(const Size(960, 540));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    FocusManager.instance.highlightStrategy =
        FocusHighlightStrategy.alwaysTraditional;
    addTearDown(
      () => FocusManager.instance.highlightStrategy =
          FocusHighlightStrategy.automatic,
    );
    final repository = _RankingRepository();
    await tester.pumpWidget(
      MaterialApp(
        builder: (context, child) => AppLayout(
          television: true,
          child: Theme(
            data: televisionTheme(Theme.of(context)),
            child: FocusTraversalGroup(child: child!),
          ),
        ),
        home: RankingsScreen(
          repository: repository,
          store: store,
          initialGroup: 'hongguo',
        ),
      ),
    );
    await tester.pumpAndSettle();

    String? focusedRank() {
      String? rank;
      FocusManager.instance.primaryFocus?.context?.visitAncestorElements((
        element,
      ) {
        final key = element.widget.key;
        if (key is ValueKey<String> && key.value.startsWith('rank-')) {
          rank = key.value.split('-')[1];
          return false;
        }
        return true;
      });
      return rank;
    }

    Future<void> press(LogicalKeyboardKey key) async {
      await tester.sendKeyEvent(key);
      await tester.pumpAndSettle();
    }

    // Locate the initial ranking focus dynamically so this test remains
    // resilient when Flutter's first-focus policy changes.
    for (var attempt = 0; attempt < 12 && focusedRank() == null; attempt++) {
      await press(LogicalKeyboardKey.arrowDown);
    }
    final initial = int.tryParse(focusedRank() ?? '');
    expect(initial, isNotNull);
    var previous = initial!;
    final visited = <int>[previous];
    for (var step = 0; step < 12; step++) {
      await press(LogicalKeyboardKey.arrowDown);
      final current = int.tryParse(focusedRank() ?? '');
      // D-pad traversal may legitimately leave the ranking list for the next
      // control. Stop there; while focus is on a ranking card, verify that it
      // is visible and that movement reaches multiple cards.
      if (current == null || current < 1 || current > 20) break;
      if (current != previous) {
        final rect = tester.getRect(
          find.byKey(ValueKey('rank-$current-hongguo:rank-$current')),
        );
        expect(rect.top, greaterThanOrEqualTo(0));
        expect(rect.bottom, lessThanOrEqualTo(540));
        visited.add(current);
      }
      previous = current;
    }
    expect(visited.toSet().length, greaterThanOrEqualTo(4));

    // Home navigation recreates the embedded screen for each board tab. A
    // recently viewed board should be restored from memory without another
    // ranking request or a blank loading screen.
    final callsBeforeReentry = repository.rankingCalls;
    await tester.pumpWidget(
      MaterialApp(
        builder: (context, child) => AppLayout(
          television: true,
          child: Theme(
            data: televisionTheme(Theme.of(context)),
            child: FocusTraversalGroup(child: child!),
          ),
        ),
        home: RankingsScreen(
          key: const ValueKey('reentered-ranking-screen'),
          repository: repository,
          store: store,
          initialGroup: 'hongguo',
          initialBoardId: 'hongguo-hot',
        ),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 250));
    expect(repository.rankingCalls, callsBeforeReentry);
    expect(find.text('合成榜單1'), findsOneWidget);
  });
}
