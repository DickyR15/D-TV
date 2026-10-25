import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:hgtv/app_layout.dart';
import 'package:hgtv/app_navigation.dart';
import 'package:hgtv/home_screen.dart';
import 'package:hgtv/models.dart';
import 'package:hgtv/ranking_models.dart';
import 'package:hgtv/rankings_screen.dart';

import 'fixtures.dart';

class _SearchNavigationRepository extends FixtureRepository {
  final queries = <String>[];

  @override
  Future<CatalogPage> catalog(
    String source, {
    int page = 1,
    String query = '',
    String category = '',
    bool force = false,
  }) async {
    queries.add(query);
    if (query.isEmpty) {
      return super.catalog(
        source,
        page: page,
        query: query,
        category: category,
        force: force,
      );
    }
    return CatalogPage(
      const [
        Drama(
          id: 'hongguo:query-result',
          source: 'hongguo',
          title: '搜尋命中短劇',
          episodes: 1,
          category: '合成資料',
        ),
      ],
      page: page,
      hasMore: false,
    );
  }

  @override
  Future<List<RankingBoard>> rankingBoards() async => const [
    RankingBoard(id: 'hongguo-hot', source: 'hongguo', name: '熱播'),
  ];

  @override
  Future<RankingPage> rankings(
    String board, {
    int page = 1,
    bool force = false,
  }) async => RankingPage(
    page: page,
    hasMore: false,
    items: const [
      RankingItem(
        1,
        Drama(
          id: 'hongguo:kept-ranking',
          source: 'hongguo',
          title: '保留的榜單項目',
          episodes: 1,
        ),
      ),
    ],
  );

  @override
  Future<String> cover(Drama drama, {bool force = false}) async {
    final file = File(
      '${Directory.systemTemp.path}/d-tv-search-navigation-cover.png',
    );
    if (!await file.exists()) {
      await file.writeAsBytes(
        base64Decode(
          'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/G7sAAAAASUVORK5CYII=',
        ),
      );
    }
    return file.path;
  }
}

void main() {
  testWidgets(
    'TV search from a ranking shows results without switching to the home tab',
    (tester) async {
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

      final repository = _SearchNavigationRepository();
      await tester.pumpWidget(
        MaterialApp(
          navigatorObservers: [appNavigationObserver],
          builder: (context, child) => AppLayout(
            television: true,
            child: Theme(
              data: televisionTheme(Theme.of(context)),
              child: FocusTraversalGroup(child: child!),
            ),
          ),
          home: HomeScreen(repository: repository, store: store),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.text('紅果熱播榜').first);
      await tester.pumpAndSettle();
      expect(find.byType(RankingsScreen), findsOneWidget);
      expect(find.text('保留的榜單項目'), findsOneWidget);

      await tester.tap(find.byKey(const ValueKey('tv-search-trigger')));
      await tester.pumpAndSettle();
      await tester.enterText(
        find.byKey(const ValueKey('tv-drama-search-input')),
        '關鍵字',
      );
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('tv-drama-search-submit')));
      await tester.pumpAndSettle();

      expect(repository.queries, contains('關鍵字'));
      expect(find.text('搜尋結果：關鍵字'), findsOneWidget);
      expect(find.text('搜尋命中短劇'), findsOneWidget);
      expect(find.byType(RankingsScreen), findsNothing);

      await tester.tap(
        find.byKey(const ValueKey('search-results-return-to-ranking')),
      );
      await tester.pumpAndSettle();
      expect(find.byType(RankingsScreen), findsOneWidget);
      expect(find.text('保留的榜單項目'), findsOneWidget);
      expect(find.text('搜尋命中短劇'), findsNothing);
    },
  );
}
