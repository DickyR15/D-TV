import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/models.dart';

void main() {
  test('legacy drama with source ID has a stable poster identity', () {
    const drama = Drama(
      id: '',
      source: 'duoduo',
      sourceId: '137937',
      title: '撒旦：黑暗',
    );

    expect(drama.stableId, 'duoduo:137937');
  });

  test('Drama merge matches a legacy row to its canonical provider ID', () {
    const legacy = Drama(
      id: '',
      source: 'duoduo',
      sourceId: '137937',
      title: '撒旦：黑暗',
    );
    const canonical = Drama(
      id: 'duoduo:137937',
      source: 'duoduo',
      sourceId: '137937',
      title: '撒旦：黑暗',
      cover: 'https://img.example.test/poster.jpg',
    );

    final merged = legacy.merge(canonical);
    expect(merged.id, 'duoduo:137937');
    expect(merged.cover, 'https://img.example.test/poster.jpg');
    expect(merged.stableId, 'duoduo:137937');
  });

  test('Drama keeps provider poster badges through JSON and merge', () {
    final cached = Drama.fromJson({
      'id': 'heitao:123',
      'source': 'heitao',
      'sourceId': '123',
      'title': '測試短劇',
      'badge': '完整版',
    });
    const refreshed = Drama(
      id: 'heitao:123',
      source: 'heitao',
      sourceId: '123',
      title: '測試短劇',
    );

    expect(cached.badge, '完整版');
    expect(cached.toJson()['badge'], '完整版');
    expect(cached.merge(refreshed).badge, '完整版');
  });

  test('Drama preserves Douban ID for later poster recovery', () {
    final drama = Drama.fromJson({
      'id': 'duoduo:54635',
      'source': 'duoduo',
      'sourceId': '54635',
      'title': '玩物',
      'category': '電影',
      'doubanId': '20502443',
    });

    expect(drama.doubanId, '20502443');
    expect(drama.toJson()['doubanId'], '20502443');
  });

  test('Drama merge keeps cached Douban ID when detail omits it', () {
    const cached = Drama(
      id: 'duoduo:54635',
      source: 'duoduo',
      sourceId: '54635',
      title: '玩物',
      category: '電影',
      doubanId: '20502443',
    );
    const refreshed = Drama(
      id: 'duoduo:54635',
      source: 'duoduo',
      sourceId: '54635',
      title: '玩物',
    );

    expect(cached.merge(refreshed).doubanId, '20502443');
  });

  test('Drama merge prefers the refreshed Douban ID', () {
    const cached = Drama(
      id: 'duoduo:54635',
      source: 'duoduo',
      title: '玩物',
      doubanId: '20502443',
    );
    const refreshed = Drama(
      id: 'duoduo:54635',
      source: 'duoduo',
      title: '玩物',
      doubanId: '99999999',
    );

    expect(cached.merge(refreshed).doubanId, '99999999');
  });

  test('Drama removes obsolete generic Duoduo category from cached data', () {
    final old = Drama.fromJson({
      'id': 'duoduo:161235',
      'source': 'duoduo',
      'title': '空槍',
      'category': '影視',
    });
    expect(old.category, isEmpty);

    const cached = Drama(
      id: 'duoduo:161235',
      source: 'duoduo',
      title: '空槍',
      category: '影視',
    );
    const refreshed = Drama(
      id: 'duoduo:161235',
      source: 'duoduo',
      title: '空槍',
    );
    expect(cached.merge(refreshed).category, isEmpty);
  });

  test('zero playback counts from current and legacy caches are hidden', () {
    for (final value in ['0', '0.0', '0.00萬', '0次', '0次播放', ' 0 播放量 ']) {
      final drama = Drama.fromJson({
        'id': 'huangdou:zero',
        'source': 'huangdou',
        'title': '零播放量',
        'views': value,
      });
      expect(drama.displayViews, isEmpty, reason: 'value=$value');
    }

    final numericLegacyCache = Drama.fromJson({
      'id': 'huangdou:legacy-zero',
      'source': 'huangdou',
      'title': '舊快取零播放量',
      'views': 0,
    });
    expect(numericLegacyCache.displayViews, isEmpty);
    expect(Drama.fromJson(numericLegacyCache.toJson()).displayViews, isEmpty);
  });

  test('positive and non-numeric provider playback labels remain visible', () {
    for (final value in ['1', '15,200', '1.2萬', '少量播放']) {
      final drama = Drama(
        id: 'huangdou:positive',
        source: 'huangdou',
        title: '有播放量',
        views: value,
      );
      expect(drama.displayViews, value);
    }
  });

  test('a refreshed zero count hides the old cached display value', () {
    const cached = Drama(
      id: 'huangdou:refresh',
      source: 'huangdou',
      title: '播放量更新',
      views: '1.2萬',
    );
    const refreshed = Drama(
      id: 'huangdou:refresh',
      source: 'huangdou',
      title: '播放量更新',
      views: '0',
    );
    expect(cached.merge(refreshed).displayViews, isEmpty);
  });

}
