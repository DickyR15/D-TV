import 'dart:async';
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
  bool useCachedTop100 = false;
  bool duplicateCrossPageIDs = false;
  int cachedTop100Calls = 0;
  final rankingCallsByPage = <int, int>{};
  bool timeoutFirstRanking = false;
  int rankingRevision = 0;
  Completer<void>? rankingPageGate;
  int activeGatedRankingCalls = 0;
  int maxConcurrentGatedRankingCalls = 0;

  int _rankingIDFor(int rank) =>
      duplicateCrossPageIDs && rank >= 21 && rank <= 35 ? rank - 20 : rank;

  static const hotBoards = [
    RankingBoard(id: 'hongguo-hot', source: 'hongguo', name: '熱播'),
    RankingBoard(id: 'hongguo-real', source: 'hongguo', name: '真人劇'),
    RankingBoard(id: 'hongguo-ai', source: 'hongguo', name: 'AI劇'),
    RankingBoard(id: 'hongguo-comic', source: 'hongguo', name: '漫劇'),
  ];

  final rankingCallsByBoardPage = <String, int>{};

  @override
  Future<List<RankingBoard>> rankingBoards() async => hotBoards;

  @override
  Future<RankingPage?> cachedRankingTop100(String board) async {
    cachedTop100Calls++;
    if (!useCachedTop100) return null;
    return RankingPage(
      page: 5,
      hasMore: false,
      updatedText: '本機快取',
      items: [
        for (var rank = 1; rank <= 100; rank++)
          RankingItem(
            rank,
            Drama(
              id: 'hongguo:cache-$board-$rank',
              source: 'hongguo',
              title: '快取$board-$rank',
              episodes: 12,
              episodesVerified: true,
            ),
          ),
      ],
    );
  }

  @override
  Future<RankingPage> rankings(
    String board, {
    int page = 1,
    bool force = false,
  }) async {
    rankingCalls++;
    rankingCallsByPage.update(page, (count) => count + 1, ifAbsent: () => 1);
    final boardPageKey = '$board:$page';
    rankingCallsByBoardPage.update(
      boardPageKey,
      (count) => count + 1,
      ifAbsent: () => 1,
    );
    if (timeoutFirstRanking && rankingCalls == 1) {
      throw StateError('context deadline exceeded');
    }
    final gate = rankingPageGate;
    if (gate != null && page > 1) {
      activeGatedRankingCalls++;
      if (activeGatedRankingCalls > maxConcurrentGatedRankingCalls) {
        maxConcurrentGatedRankingCalls = activeGatedRankingCalls;
      }
      try {
        await gate.future;
      } finally {
        activeGatedRankingCalls--;
      }
    }
    const pageSize = 20;
    return RankingPage(
      page: page,
      hasMore: page < 5,
      items: [
        for (var i = 1; i <= pageSize; i++)
          RankingItem(
            (page - 1) * pageSize + i,
            Drama(
              id: 'hongguo:rank-${_rankingIDFor((page - 1) * pageSize + i)}',
              source: 'hongguo',
              title:
                  '合成榜單${_rankingIDFor((page - 1) * pageSize + i)}'
                  '${rankingRevision == 0 ? '' : '-v$rankingRevision'}',
              episodes: 12,
              episodesVerified: true,
            ),
          ),
      ],
    );
  }
}

void main() {
  testWidgets(
    'preserves 100 ranked slots even when adjacent pages repeat drama IDs',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);
      await tester.binding.setSurfaceSize(const Size(960, 540));
      addTearDown(() => tester.binding.setSurfaceSize(null));

      final repository = _RankingRepository()..duplicateCrossPageIDs = true;
      await tester.pumpWidget(
        MaterialApp(
          home: AppLayout(
            television: true,
            child: RankingsScreen(
              repository: repository,
              store: store,
              initialGroup: 'hongguo',
              initialBoardId: 'hongguo-hot',
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.pump(const Duration(milliseconds: 100));
      await tester.pumpAndSettle();

      expect(
        find.textContaining('已載入 100 部短劇'),
        findsAtLeastNWidgets(1),
        reason: 'rank positions 1 through 100 should remain visible',
      );
      for (final board in _RankingRepository.hotBoards) {
        expect(
          repository.rankingCallsByBoardPage['${board.id}:5'],
          1,
          reason: '${board.id} should request the fifth page',
        );
      }
    },
  );

  testWidgets('restores all four cached top-100 boards without network calls', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await tester.binding.setSurfaceSize(const Size(960, 540));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    final repository = _RankingRepository()..useCachedTop100 = true;
    for (final board in _RankingRepository.hotBoards) {
      await tester.pumpWidget(
        MaterialApp(
          home: AppLayout(
            television: true,
            child: RankingsScreen(
              key: ValueKey('cached-board-${board.id}'),
              repository: repository,
              store: store,
              initialGroup: 'hongguo',
              initialBoardId: board.id,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(
        find.textContaining('已載入 100 部短劇'),
        findsOneWidget,
        reason: '${board.id} should paint all 100 cached items immediately',
      );
      expect(
        repository.rankingCalls,
        0,
        reason: 'a complete local cache must not wait for the network',
      );
    }
    expect(repository.cachedTop100Calls, greaterThanOrEqualTo(4));
  });

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

  testWidgets(
    'automatically refreshes all four hot boards and replaces stale page caches',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);
      await tester.binding.setSurfaceSize(const Size(960, 540));
      addTearDown(() => tester.binding.setSurfaceSize(null));

      final repository = _RankingRepository();
      await tester.pumpWidget(
        MaterialApp(
          home: RankingsScreen(
            repository: repository,
            store: store,
            initialGroup: 'hongguo',
            initialBoardId: 'hongguo-hot',
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.pump(const Duration(milliseconds: 50));
      await tester.pumpAndSettle();

      for (final board in _RankingRepository.hotBoards) {
        for (var page = 1; page <= 5; page++) {
          expect(repository.rankingCallsByBoardPage['${board.id}:$page'], 1);
        }
      }

      // Simulate the ranking source changing while the app stays open.
      repository.rankingRevision = 2;
      await tester.pump(const Duration(minutes: 15));
      await tester.pumpAndSettle();

      expect(find.text('合成榜單1-v2'), findsOneWidget);
      for (final board in _RankingRepository.hotBoards) {
        for (var page = 1; page <= 5; page++) {
          expect(
            repository.rankingCallsByBoardPage['${board.id}:$page'],
            2,
            reason: '${board.id} page $page should be refetched after refresh',
          );
        }
      }
    },
  );

  testWidgets(
    'preloads 100 items for all four Red Fruit hot-ranking boards',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);
      await tester.binding.setSurfaceSize(const Size(960, 540));
      addTearDown(() => tester.binding.setSurfaceSize(null));

      final repository = _RankingRepository();
      await tester.pumpWidget(
        MaterialApp(
          home: AppLayout(
            television: true,
            child: RankingsScreen(
              repository: repository,
              store: store,
              initialGroup: 'hongguo',
              initialBoardId: 'hongguo-hot',
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(
        find.text('共 12 集'),
        findsWidgets,
        reason: 'ranking posters should show verified Red Fruit episode totals',
      );

      // All four Red Fruit hot boards should be populated with 100 items,
      // rather than leaving the active board stuck at the first 20.
      await tester.pump(const Duration(milliseconds: 50));
      await tester.pumpAndSettle();
      for (final board in _RankingRepository.hotBoards) {
        for (var page = 1; page <= 5; page++) {
          expect(
            repository.rankingCallsByBoardPage['${board.id}:$page'],
            1,
            reason: '${board.id} page $page should be cached once',
          );
        }
      }
      // The fifth-page call is recorded before its Future completion is
      // delivered, so pump the final result through the widget tree as well.
      await tester.pump(const Duration(milliseconds: 100));
      await tester.pumpAndSettle();
      expect(
        find.textContaining('已載入 100 部短劇'),
        findsAtLeastNWidgets(1),
        reason: 'the visible hot board should publish all 100 preloaded items',
      );

      // Re-entering each board should immediately show its own 100-item cache.
      for (final board in _RankingRepository.hotBoards) {
        await tester.pumpWidget(
          MaterialApp(
            home: AppLayout(
              television: true,
              child: RankingsScreen(
                key: ValueKey('cached-${board.id}'),
                repository: repository,
                store: store,
                initialGroup: 'hongguo',
                initialBoardId: board.id,
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();
        expect(
          find.textContaining('已載入 100 部短劇'),
          findsAtLeastNWidgets(1),
          reason: '${board.id} should reopen with all 100 cached items',
        );
      }
    },
  );


  testWidgets(
    'preloads the four hot boards concurrently instead of serially',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);
      await tester.binding.setSurfaceSize(const Size(960, 540));
      addTearDown(() => tester.binding.setSurfaceSize(null));

      final repository = _RankingRepository();
      final gate = Completer<void>();
      repository.rankingPageGate = gate;
      addTearDown(() {
        if (!gate.isCompleted) gate.complete();
      });

      await tester.pumpWidget(
        MaterialApp(
          home: RankingsScreen(
            repository: repository,
            store: store,
            initialGroup: 'hongguo',
            initialBoardId: 'hongguo-hot',
          ),
        ),
      );

      for (var attempt = 0;
          attempt < 10 && repository.maxConcurrentGatedRankingCalls < 2;
          attempt++) {
        await tester.pump(const Duration(milliseconds: 50));
      }
      expect(
        repository.maxConcurrentGatedRankingCalls,
        greaterThanOrEqualTo(2),
        reason: 'independent hot boards should preload pages concurrently',
      );

      gate.complete();
      await tester.pumpAndSettle();
      for (final board in _RankingRepository.hotBoards) {
        for (var page = 1; page <= 5; page++) {
          expect(
            repository.rankingCallsByBoardPage['${board.id}:$page'],
            1,
            reason: '${board.id} page $page should be cached once',
          );
        }
      }
    },
  );

  testWidgets('source without its own ranking board shows an empty state', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    final repository = _RankingRepository();
    await tester.pumpWidget(
      MaterialApp(
        home: RankingsScreen(
          repository: repository,
          store: store,
          initialGroup: 'cloudfront',
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('此片源沒有獨立榜單'), findsOneWidget);
    expect(find.textContaining('目前片源沒有獨立熱門榜單'), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsNothing);
    expect(repository.rankingCalls, 0);
  });

}
