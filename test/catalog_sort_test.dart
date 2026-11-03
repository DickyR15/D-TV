import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/catalog_sort.dart';
import 'package:hgtv/models.dart';

void main() {
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
}
