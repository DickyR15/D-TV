import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/models.dart';

void main() {
  test('legacy Red Fruit ranking cache does not display an unverified episode count', () {
    final cached = Drama(
      id: 'hongguo:123',
      source: 'hongguo',
      title: '舊快取榜單資料',
      episodes: 3,
    );
    expect(cached.canShowEpisodeCount, isFalse);
  });

  test('verified detail episode count survives ranking refresh and restart serialization', () {
    final detail = Drama(
      id: 'hongguo:123',
      source: 'hongguo',
      title: '紅果劇集',
      episodes: 88,
      episodesVerified: true,
    );
    final refreshedRanking = Drama(
      id: 'hongguo:123',
      source: 'hongguo',
      title: '紅果劇集',
    );
    final merged = detail.merge(refreshedRanking);
    final restored = Drama.fromJson(merged.toJson());
    expect(restored.episodes, 88);
    expect(restored.episodesVerified, isTrue);
    expect(restored.canShowEpisodeCount, isTrue);
  });

  test('other providers keep displaying an available count', () {
    final drama = Drama(
      id: 'huangdou:123',
      source: 'huangdou',
      title: '其他來源劇集',
      episodes: 12,
    );
    expect(drama.canShowEpisodeCount, isTrue);
  });
}
