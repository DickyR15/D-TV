import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/catalog_sort.dart';
import 'package:hgtv/catalog_sort_sheet.dart';
import 'package:hgtv/models.dart';

void main() {
  test('uses Taiwan Traditional Chinese labels for catalog sorting', () {
    expect(CatalogSort.source.label, '預設排序');
    expect(CatalogSort.name.label, '片名');
    expect(CatalogSort.season.label, '季數');
    expect(CatalogSort.date.label, '年份（由新到舊）');
    expect(CatalogSort.heat.label, '熱門程度');
    expect(CatalogSort.views.label, '播放次數');
  });

  group('catalog year sorting', () {
    test('sorts newest release year first across mixed sources', () {
      final rows = [
        const Drama(
          id: 'duoduo:older',
          source: 'duoduo',
          title: '舊片',
          onlineDate: '2022-11-09',
        ),
        const Drama(
          id: 'source-b:newest',
          source: 'source-b',
          title: '最新片',
          onlineDate: '2026年',
        ),
        const Drama(
          id: 'source-c:year-only',
          source: 'source-c',
          title: '年度資料',
          onlineDate: '2025',
        ),
        const Drama(
          id: 'source-d:dated',
          source: 'source-d',
          title: '日期資料',
          onlineDate: '2024-12-31',
        ),
        const Drama(
          id: 'source-e:unknown',
          source: 'source-e',
          title: '沒有年份',
          onlineDate: '待定',
        ),
      ];

      final sorted = sortCatalog(
        rows,
        const CatalogView(sort: CatalogSort.date),
      );

      expect(
        sorted.map((drama) => drama.id),
        [
          'source-b:newest',
          'source-c:year-only',
          'source-d:dated',
          'duoduo:older',
          'source-e:unknown',
        ],
      );
    });

    test('recognizes year-only and full-date source formats', () {
      expect(catalogYear('2026'), 2026);
      expect(catalogYear('2025年'), 2025);
      expect(catalogYear('2024-08-19'), 2024);
      expect(catalogYear('2023/01/05'), 2023);
      expect(catalogYear('待定'), isNull);
      expect(catalogYear('1899-12-31'), isNull);
    });

    test('preserves stable source order when year and date are both missing', () {
      final rows = [
        const Drama(id: 'a', source: 'one', title: 'A'),
        const Drama(id: 'b', source: 'two', title: 'B'),
      ];

      final sorted = sortCatalog(
        rows,
        const CatalogView(sort: CatalogSort.date),
      );

      expect(sorted.map((drama) => drama.id), ['a', 'b']);
    });
  });

  group('provider-aware sort and filters', () {
    test('Hanxiaoquan without metadata uses source order and no release filter', () {
      const rows = [
        Drama(id: 'hanxiaoquan:1', source: 'hanxiaoquan', title: '韓劇甲'),
        Drama(id: 'hanxiaoquan:2', source: 'hanxiaoquan', title: '韓劇乙'),
      ];
      final options = availableCatalogSorts(rows);
      expect(options, {CatalogSort.source, CatalogSort.name});
      expect(hasCatalogReleaseStatus(rows), isFalse);

      final effective = effectiveCatalogView(
        const CatalogView(sort: CatalogSort.date, release: 'finished'),
        rows,
      );
      expect(effective.sort, CatalogSort.source);
      expect(effective.release, isEmpty);
      expect(
        sortCatalog(rows, effective).map((drama) => drama.id),
        ['hanxiaoquan:1', 'hanxiaoquan:2'],
      );
    });

    test('Duoduo only offers year and heat sorts when those fields exist', () {
      const rows = [
        Drama(
          id: 'duoduo:1',
          source: 'duoduo',
          title: '電影甲',
          onlineDate: '2025',
          heat: '1.2萬',
        ),
        Drama(
          id: 'duoduo:2',
          source: 'duoduo',
          title: '電影乙',
          onlineDate: '2024',
          heat: '8000',
        ),
      ];
      final options = availableCatalogSorts(rows);
      expect(options, containsAll([
        CatalogSort.source,
        CatalogSort.name,
        CatalogSort.date,
        CatalogSort.heat,
      ]));
      expect(options, isNot(contains(CatalogSort.views)));
      expect(options, isNot(contains(CatalogSort.season)));
      expect(hasCatalogReleaseStatus(rows), isFalse);

      final effective = effectiveCatalogView(
        const CatalogView(sort: CatalogSort.date, release: 'finished'),
        rows,
      );
      expect(effective.sort, CatalogSort.date);
      expect(effective.release, isEmpty);
    });

    test('Red Fruit retains season, date, heat, views and episode state filters', () {
      const rows = [
        Drama(
          id: 'hongguo:1',
          source: 'hongguo',
          title: '第3季都市短劇',
          onlineDate: '2026-05-01',
          heat: '2.1萬',
          views: '100萬',
          releaseStatus: 'ongoing',
        ),
      ];
      final options = availableCatalogSorts(rows);
      expect(options, containsAll(CatalogSort.values));
      expect(hasCatalogReleaseStatus(rows), isTrue);
      final effective = effectiveCatalogView(
        const CatalogView(sort: CatalogSort.season, release: 'ongoing'),
        rows,
      );
      expect(effective.sort, CatalogSort.season);
      expect(effective.release, 'ongoing');
    });
  });


  testWidgets('sort and filter confirmation button uses Taiwan wording', (
    tester,
  ) async {
    CatalogView? result;
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => Scaffold(
            body: TextButton(
              onPressed: () async {
                result = await chooseCatalogView(context, const CatalogView());
              },
              child: const Text('開啟排序與篩選'),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('開啟排序與篩選'));
    await tester.pumpAndSettle();

    expect(find.text('套用'), findsOneWidget);
    expect(find.text('應用'), findsNothing);

    await tester.tap(find.text('套用'));
    await tester.pumpAndSettle();
    expect(result, isA<CatalogView>());
  });

}
