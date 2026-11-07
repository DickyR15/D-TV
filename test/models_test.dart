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
}
