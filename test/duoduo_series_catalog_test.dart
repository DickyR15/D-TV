import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/duoduo_series_catalog.dart';
import 'package:hgtv/local_store.dart';
import 'package:hgtv/models.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('Duoduo catalogue series grouping', () {
    test('groups exact season titles and preserves season one artwork', () {
      final result = collapseDuoduoSeriesCatalogItems([
        const Drama(id: 'duoduo:102', source: 'duoduo', title: '星際旅人第二季', cover: 's2.jpg'),
        const Drama(id: 'duoduo:101', source: 'duoduo', title: '星際旅人第一季', cover: 's1.jpg'),
        const Drama(id: 'duoduo:103', source: 'duoduo', title: '星際旅人第三季', cover: 's3.jpg'),
        const Drama(id: 'duoduo:200', source: 'duoduo', title: '單季作品'),
      ]);
      expect(result, hasLength(2));
      expect(result.first.id, 'duoduo:101');
      expect(result.first.title, '星際旅人');
      expect(result.first.cover, 's1.jpg');
      expect(result.first.seriesGroupId, 'duoduo-series:星際旅人:季');
      expect(result.last.id, 'duoduo:200');
    });

    test('uses one unsuffixed exact title as season one when a sibling exists', () {
      final result = collapseDuoduoSeriesCatalogItems([
        const Drama(id: 'duoduo:301', source: 'duoduo', title: '無後綴劇名'),
        const Drama(id: 'duoduo:302', source: 'duoduo', title: '無後綴劇名第二季'),
      ]);
      expect(result, hasLength(1));
      expect(result.single.id, 'duoduo:301');
      expect(result.single.title, '無後綴劇名');
      expect(result.single.seriesSeasonNumber, 1);
    });

    test('supports English season labels and Chinese numbering', () {
      final english = collapseDuoduoSeriesCatalogItems([
        const Drama(id: 'duoduo:401', source: 'duoduo', title: 'Example Show S02'),
        const Drama(id: 'duoduo:402', source: 'duoduo', title: 'Example Show Season 1'),
      ]);
      expect(english, hasLength(1));
      expect(english.single.seriesSeasonNumber, 1);
      final chinese = collapseDuoduoSeriesCatalogItems([
        const Drama(id: 'duoduo:411', source: 'duoduo', title: '古裝傳奇第十二季'),
        const Drama(id: 'duoduo:412', source: 'duoduo', title: '古裝傳奇第十季'),
      ]);
      expect(chinese.single.seriesSeasonNumber, 10);
    });

    test('collapses duplicate season rows but keeps mixed units and similar titles separate', () {
      expect(collapseDuoduoSeriesCatalogItems([
        const Drama(id: 'duoduo:501', source: 'duoduo', title: '同名第一季'),
        const Drama(id: 'duoduo:502', source: 'duoduo', title: '同名第1季'),
        const Drama(id: 'duoduo:503', source: 'duoduo', title: '同名第二季'),
      ]), hasLength(1));
      expect(collapseDuoduoSeriesCatalogItems([
        const Drama(id: 'duoduo:511', source: 'duoduo', title: '混合系列第一季'),
        const Drama(id: 'duoduo:512', source: 'duoduo', title: '混合系列第二部'),
      ]), hasLength(2));
      expect(collapseDuoduoSeriesCatalogItems([
        const Drama(id: 'duoduo:521', source: 'duoduo', title: '系列名稱第一季'),
        const Drama(id: 'duoduo:522', source: 'duoduo', title: '系列名稱外傳第二季'),
      ]), hasLength(2));
    });
  });

  test('IMDb identity survives the model JSON roundtrip', () {
    const drama = Drama(
      id: 'duoduo:801',
      source: 'duoduo',
      title: 'Example Show 第一季',
      imdbId: 'tt12345678',
    );
    expect(Drama.fromJson(drama.toJson()).imdbId, 'tt12345678');
  });

  test('groups verified Chicago Med aliases and deduplicates repeated seasons', () {
    const seriesId = 'duoduo-series:alias:chicago-med:季';
    final result = collapseDuoduoSeriesCatalogItems([
      const Drama(id:'duoduo:912',source:'duoduo',title:'芝加哥醫院第12季',seriesGroupId:seriesId,seriesSeasonNumber:12,seriesSeasonUnit:'季',cover:'med12.jpg',episodes:21),
      const Drama(id:'duoduo:901',source:'duoduo',title:'芝加哥急救第一季',seriesGroupId:seriesId,seriesSeasonNumber:1,seriesSeasonUnit:'季',cover:'med1.jpg',episodes:18),
      const Drama(id:'duoduo:902',source:'duoduo',title:'芝加哥急救第二季',seriesGroupId:seriesId,seriesSeasonNumber:2,seriesSeasonUnit:'季',cover:'med2.jpg',episodes:23),
      const Drama(id:'duoduo:904',source:'duoduo',title:'芝加哥醫院第2季',seriesGroupId:seriesId,seriesSeasonNumber:2,seriesSeasonUnit:'季'),
      const Drama(id:'duoduo:990',source:'duoduo',title:'芝加哥烈焰第一季',cover:'fire1.jpg'),
    ]);
    expect(result,hasLength(2));
    expect(result.first.seriesGroupId,seriesId);
    expect(result.first.id,'duoduo:901');
    expect(result.first.title,'芝加哥急救');
    expect(result.last.id,'duoduo:990');
  });

  test('preserves official poster fallback candidates across season grouping and model roundtrips', () {
    final first = Drama.fromJson(const {
      'id': 'duoduo:701',
      'source': 'duoduo',
      'sourceId': '701',
      'title': '芝加哥烈焰第一季',
      'cover': 'https://img.example.test/chicago-primary.jpg',
      'coverCandidates': [
        'https://img.example.test/chicago-thumb.jpg',
        'https://img.example.test/chicago-detail.jpg',
      ],
    });
    expect(first.coverCandidates, [
      'https://img.example.test/chicago-thumb.jpg',
      'https://img.example.test/chicago-detail.jpg',
    ]);
    expect(first.toJson()['coverCandidates'], first.coverCandidates);
    final grouped = first.withSeriesIdentity(
      groupId: 'duoduo-series:芝加哥烈焰:季',
      seasonNumber: 1,
      seasonUnit: '季',
      displayTitle: '芝加哥烈焰',
    );
    expect(grouped.coverCandidates, first.coverCandidates);
    final fresh = Drama(
      id: first.id, source: first.source, sourceId: first.sourceId,
      title: first.title, cover: first.cover,
      coverCandidates: const ['https://img.example.test/chicago-large.jpg'],
    );
    final merged = first.merge(fresh);
    expect(merged.coverCandidates, [
      'https://img.example.test/chicago-large.jpg',
      'https://img.example.test/chicago-thumb.jpg',
      'https://img.example.test/chicago-detail.jpg',
    ]);
    expect(Drama.fromJson(merged.toJson()).coverCandidates, merged.coverCandidates);
  });

  test('Duoduo seasons use one saved favorite for the entire series', () async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await store.saveProfile(
      id: 'default', name: '管理員', sources: const [], download: true,
      pin: 'source-pass-123',
    );
    await store.unlockAdvancedSources('source-pass-123');
    await store.setSourceEnabled('duoduo', true);
    const first = Drama(
      id: 'duoduo:601', source: 'duoduo', title: '收藏系列',
      seriesGroupId: 'duoduo-series:收藏系列:季',
      seriesSeasonNumber: 1, seriesSeasonUnit: '季',
    );
    const second = Drama(
      id: 'duoduo:602', source: 'duoduo', title: '收藏系列第二季',
      seriesGroupId: 'duoduo-series:收藏系列:季',
      seriesSeasonNumber: 2, seriesSeasonUnit: '季',
    );
    await store.refreshDramas([first, second]);
    await store.toggleFavorite(first);
    expect(store.isFavorite(first.id), isTrue);
    expect(store.isFavorite(second.id), isTrue);
    expect(store.favorites, hasLength(1));
    await store.saveWatch(WatchEntry(
      drama: first, episode: 3, position: 42, duration: 120,
      updatedAt: DateTime(2026, 11, 14, 1),
    ));
    await store.saveWatch(WatchEntry(
      drama: second, episode: 8, position: 16, duration: 90,
      updatedAt: DateTime(2026, 11, 14, 2),
    ));
    expect(store.watched(first.id)?.episode, 3);
    expect(store.watched(second.id)?.episode, 8);
    expect(store.watched(first.id)?.position, 42);
    expect(store.watched(second.id)?.position, 16);
    await store.toggleFavorite(second);
    expect(store.favorites, isEmpty);
  });
}
