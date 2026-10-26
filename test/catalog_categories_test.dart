import 'dart:async';
import 'package:hgtv/catalog_browser.dart';
import 'package:hgtv/core_bridge.dart';
import 'package:hgtv/models.dart';
import 'package:hgtv/ranking_search_cache.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fixtures.dart';

class CategoryRepository extends FixtureRepository {
  final categoryRequests = <String>[];
  int cachedReads = 0;
  int cachedPreviewReads = 0;
  int categoryLoads = 0;
  Completer<void>? cachedWaitStarted;
  Completer<void>? releaseCached;
  bool failLegacy = false;
  bool paginate = false;

  @override
  Future<CatalogPage?> cachedPreview(
    String source, {
    String category = '',
  }) async {
    cachedPreviewReads++;
    if (category.isNotEmpty) return null;
    final page = cachedPages[source];
    if (page == null || page.items.isEmpty) return null;
    return CatalogPage(
      page.items.take(30).toList(),
      hasMore: false,
      page: page.page,
      warning: page.warning,
      fresh: page.fresh,
    );
  }

  @override
  Future<CatalogPage> cached(String source, {String category = ''}) async {
    cachedReads++;
    final started = cachedWaitStarted;
    final release = releaseCached;
    if (started != null && release != null) {
      if (!started.isCompleted) started.complete();
      await release.future;
    }
    return category.isEmpty
        ? cachedPages[source] ?? CatalogPage([])
        : CatalogPage([]);
  }

  @override
  Future<List<CatalogCategory>> categories(
    String source, {
    bool force = false,
  }) async {
    categoryLoads++;
    return switch (source) {
    'hongguo' => const [
      CatalogCategory.all,
      CatalogCategory('short_play', '真人劇'),
      CatalogCategory('comic_series', '漫劇'),
      CatalogCategory('ai_series', 'AI 劇'),
    ],
    'huangguo-video' => const [CatalogCategory.all, CatalogCategory('2', '短片')],
    'huangguoai' => const [
      CatalogCategory.all,
      CatalogCategory('ai-duanju', 'AI 短劇'),
      CatalogCategory('ai-manju', 'AI 漫劇'),
    ],
    'cloudfront' => const [
      CatalogCategory.all,
      CatalogCategory('old-short', 'AI成人短劇'),
    ],
    _ => const [CatalogCategory.all],
    };
  }

  @override
  Future<CatalogPage> catalog(
    String source, {
    int page = 1,
    String query = '',
    String category = '',
    bool force = false,
  }) async {
    categoryRequests.add('$source|$category|$page');
    if (source == 'cloudfront' && failLegacy) throw AppFailure('合成入口失敗');
    return CatalogPage(
      [
        Drama(
          id: '$source:$category:$page',
          source: source,
          title: '$source · ${category.isEmpty ? '全部' : category}',
          category: source == 'hongguo' ? '異能' : '',
        ),
      ],
      page: page,
      hasMore: paginate && source == 'huangguo-video' && page == 1,
    );
  }
}

void main() {
  test(
    'cached preview paints posters before the full catalog cache finishes reading',
    () async {
      final repository = CategoryRepository();
      repository.cachedPages['hongguo'] = CatalogPage(
        [
          for (var i = 0; i < 45; i++)
            Drama(
              id: 'hongguo:preview-$i',
              source: 'hongguo',
              title: '快取劇 $i',
            ),
        ],
        page: 1,
        hasMore: true,
        fresh: true,
      );
      repository.cachedWaitStarted = Completer<void>();
      repository.releaseCached = Completer<void>();
      final browser = CatalogBrowser(repository);
      const group = SourceGroup('hongguo', '紅果', [SourceSite.hongguo]);
      final painted = Completer<CatalogPage>();
      final loading = browser.load(
        group,
        useCache: true,
        onCached: (page) {
          if (!painted.isCompleted) painted.complete(page);
        },
      );

      final firstPaint = await painted.future.timeout(
        const Duration(seconds: 1),
      );
      expect(firstPaint.items, hasLength(30));
      expect(repository.cachedPreviewReads, 1);
      expect(repository.cachedReads, 1);
      repository.releaseCached!.complete();
      final complete = await loading;
      expect(complete.items, hasLength(45));
    },
  );

  test(
    'category menu starts loading while cached catalog read is still pending',
    () async {
      final repository = CategoryRepository();
      repository.cachedWaitStarted = Completer<void>();
      repository.releaseCached = Completer<void>();
      final browser = CatalogBrowser(repository);
      const group = SourceGroup('hongguo', '紅果', [SourceSite.hongguo]);

      final loading = browser.loadCategories(group);
      await repository.cachedWaitStarted!.future.timeout(
        const Duration(seconds: 1),
      );
      expect(repository.categoryLoads, 1);
      // The category menu should finish while the full catalog cache is pending.
      await loading.timeout(const Duration(seconds: 1));
      repository.releaseCached!.complete();
    },
  );

  test(
    'recent fresh catalog session reuses memory instead of rereading the native cache',
    () async {
      final repository = CategoryRepository();
      repository.cachedPages['hongguo'] = CatalogPage([
        const Drama(
          id: 'hongguo:cached-fresh',
          source: 'hongguo',
          title: '快取熱門劇',
        ),
      ], fresh: true);
      final browser = CatalogBrowser.shared(repository);
      const group = SourceGroup('hongguo', '紅果', [SourceSite.hongguo]);

      final first = await browser.load(group, useCache: true);
      expect(first.items.map((item) => item.id), contains('hongguo:cached-fresh'));
      expect(repository.cachedReads, 1);

      final recreatedBrowser = CatalogBrowser.shared(repository);
      expect(identical(browser, recreatedBrowser), isTrue);
      final second = await recreatedBrowser.load(group, useCache: true);
      expect(second.items.map((item) => item.id), contains('hongguo:cached-fresh'));
      expect(repository.cachedReads, 1);
    },
  );


  test(
    'cache-only warmup seeds memory so opening catalog can paint immediately',
    () async {
      final repository = CategoryRepository();
      repository.cachedPages['hongguo'] = CatalogPage(const [
        Drama(
          id: 'hongguo:warmed',
          source: 'hongguo',
          title: '預熱快取劇',
        ),
      ], fresh: true);
      final browser = CatalogBrowser.shared(repository);
      const group = SourceGroup('hongguo', '紅果', [SourceSite.hongguo]);

      final warmed = await browser.load(
        group,
        useCache: true,
        cacheOnly: true,
      );
      expect(warmed.items.map((item) => item.id), contains('hongguo:warmed'));
      expect(repository.categoryRequests, isEmpty);
      expect(repository.cachedReads, 1);

      final visible = await browser.load(group, useCache: true);
      expect(visible.items.map((item) => item.id), contains('hongguo:warmed'));
      expect(repository.cachedReads, 1);
      expect(repository.categoryRequests, isEmpty);
    },
  );


  test(
    'remote content types never hide fine categories from cached pages',
    () async {
      final repository = CategoryRepository();
      repository.cachedPages['hongguo'] = CatalogPage(const [
        Drama(
          id: 'hongguo:old1',
          source: 'hongguo',
          title: '快取劇甲',
          category: '都市',
        ),
        Drama(
          id: 'hongguo:old2',
          source: 'hongguo',
          title: '快取劇乙',
          category: '成長',
        ),
      ], page: 7);
      final browser = CatalogBrowser(repository);
      const group = SourceGroup('hongguo', '紅果', [SourceSite.hongguo]);
      await browser.loadCategories(group);
      await browser.load(group, category: 'category:漫劇');
      expect(
        browser.categories(group).map((category) => category.name),
        containsAll(['真人劇', '漫劇', 'AI 劇', '都市', '成長', '異能']),
      );
      final local = await browser.load(group, category: 'local:都市');
      expect(local.items.map((drama) => drama.id), contains('hongguo:old1'));
    },
  );

  test(
    'empty cache snapshot does not block later local category warmup',
    () async {
      final repository = CategoryRepository();
      final browser = CatalogBrowser(repository);
      const group = SourceGroup('hongguo', '紅果', [SourceSite.hongguo]);

      await browser.loadCategories(group);
      // Allow the background empty cache request to complete before content appears.
      await Future<void>.delayed(Duration.zero);
      repository.cachedPages['hongguo'] = CatalogPage(const [
        Drama(
          id: 'hongguo:later-cache',
          source: 'hongguo',
          title: '後到的科幻短劇',
          category: '科幻',
        ),
      ]);

      await browser.load(group, useCache: true, cacheOnly: true);
      expect(
        browser.categories(group).map((category) => category.name),
        contains('科幻'),
      );
    },
  );

  test(
    'category choices reuse cached results and refresh when new local categories arrive',
    () async {
      final repository = CategoryRepository();
      repository.cachedPages['hongguo'] = CatalogPage(const [
        Drama(
          id: 'hongguo:cached-city',
          source: 'hongguo',
          title: '都市短劇',
          category: '都市',
        ),
      ]);
      final browser = CatalogBrowser(repository);
      const group = SourceGroup('hongguo', '紅果', [SourceSite.hongguo]);

      await browser.loadCategories(group);
      final first = browser.categories(group);
      final second = browser.categories(group);
      expect(identical(first[1], second[1]), isTrue);
      expect(
        first.map((category) => category.name),
        contains('都市'),
      );

      repository.cachedPages['hongguo'] = CatalogPage(const [
        Drama(
          id: 'hongguo:cached-city',
          source: 'hongguo',
          title: '都市短劇',
          category: '都市',
        ),
        Drama(
          id: 'hongguo:cached-scifi',
          source: 'hongguo',
          title: '科幻短劇',
          category: '科幻',
        ),
      ]);
      await browser.loadCategories(group, cacheOnly: true);

      expect(
        browser.categories(group).map((category) => category.name),
        contains('科幻'),
      );
    },
  );


  test(
    'revisited stale category paints remembered posters before refresh completes',
    () async {
      final repository = CategoryRepository();
      final browser = CatalogBrowser(repository);
      const group = SourceGroup('hongguo', '紅果', [SourceSite.hongguo]);

      await browser.load(group, useCache: true);
      final remembered = await browser.load(
        group,
        category: 'category:漫劇',
        useCache: true,
      );
      expect(remembered.items, isNotEmpty);
      final callsBeforeRevisit = repository.categoryRequests.length;
      final start = DateTime.now();
      final revisited = await browser.load(
        group,
        category: 'category:漫劇',
        useCache: true,
        onCached: (_) {},
      );
      expect(revisited.items, isNotEmpty);
      expect(repository.categoryRequests.length, greaterThanOrEqualTo(callsBeforeRevisit));
      expect(DateTime.now().difference(start), lessThan(const Duration(seconds: 1)));
    },
  );

  test(
    'group pagination retries failed member without skipping or reloading exhausted members',
    () async {
      final repository = CategoryRepository()
        ..failLegacy = true
        ..paginate = true;
      final browser = CatalogBrowser(repository);
      final group = SourceGroup.fromSources(
        SourceSite.knownValues,
      ).firstWhere((group) => group.id == 'huangguo');
      final first = await browser.load(group);
      expect(first.items.length, 2);
      expect(first.warning, '合成入口失敗');
      repository.failLegacy = false;
      final next = await browser.load(group, more: true);
      expect(
        repository.categoryRequests
            .where((request) => request == 'cloudfront||1')
            .length,
        2,
      );
      expect(repository.categoryRequests, contains('huangguo-video||2'));
      expect(repository.categoryRequests, isNot(contains('huangguoai||2')));
      expect(next.items.length, 4);
      expect(next.warning, isEmpty);
    },
  );

  test(
    'search merges matches from the loaded catalog and four-board ranking cache',
    () async {
      RankingSearchCache.clearForTesting();
      addTearDown(RankingSearchCache.clearForTesting);
      final repository = CategoryRepository();
      repository.cachedPages['hongguo'] = CatalogPage(
        const [
          Drama(
            id: 'hongguo:catalog-cache-match',
            source: 'hongguo',
            title: '分類快取命中測試劇',
            category: '真人劇',
          ),
        ],
        fresh: true,
      );
      const group = SourceGroup('hongguo', '紅果', [SourceSite.hongguo]);
      final browser = CatalogBrowser(repository);

      // Warm the browser's catalog cache, as happens on the home screen.
      await browser.load(group, useCache: true);
      RankingSearchCache.remember(const [
        Drama(
          id: 'hongguo:ranking-cache-match',
          source: 'hongguo',
          title: '真人榜快取命中測試劇',
          category: '真人劇',
        ),
      ]);

      final result = await browser.load(group, query: '快取命中測試劇');
      final ids = result.items.map((item) => item.id).toSet();
      expect(ids, contains('hongguo:catalog-cache-match'));
      expect(ids, contains('hongguo:ranking-cache-match'));
    },
  );

}
