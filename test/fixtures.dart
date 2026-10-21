import 'package:duanju_app/core_bridge.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:duanju_app/local_profiles.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/models.dart';

/// 測試用 pin 雜湊：與 [hashProfilePin] 同格式（64 位十六進位制），但不用 isolate。
/// flutter test 的 fake async 環境下 [Isolate.run] 永不完成，會讓用例掛到超時。
Future<String> testPinHasher(String pin, String salt) async {
  var hash = 0;
  for (final unit in '$salt|$pin'.codeUnits) {
    hash = (hash * 31 + unit) & 0xffffffff;
  }
  return List.filled(8, hash.toRadixString(16).padLeft(8, '0')).join();
}

/// 構造注入測試雜湊的 [LocalStore]。
LocalStore testStore(SharedPreferences preferences) =>
    LocalStore(preferences, pinHasher: testPinHasher);

class FixtureRepository extends AppRepository {
  int detailCalls = 0;
  bool fail = false;
  final requests = <String>[];
  final pages = <int>[];
  final forced = <bool>[];
  final cachedPages = <String, CatalogPage>{};
  static const free = Drama(
    id: 'hongguo:100',
    source: 'hongguo',
    title: '測試短劇',
    episodes: 2,
    category: '合成資料',
  );
  static const vip = Drama(
    id: 'huangdou:200',
    source: 'huangdou',
    title: '會員測試劇',
    episodes: 4,
    vip: true,
  );
  @override
  Future<void> initialize() async {}
  @override
  Future<CatalogPage> cached(String source, {String category = ''}) async =>
      cachedPages[source] ?? CatalogPage([]);
  @override
  Future<String> cover(Drama drama, {bool force = false}) async =>
      throw AppFailure('合成劇集沒有遠端海報');
  @override
  Future<CatalogPage> catalog(
    String source, {
    int page = 1,
    String query = '',
    String category = '',
    bool force = false,
  }) async {
    requests.add(source);
    pages.add(page);
    forced.add(force);
    if (fail) {
      throw AppFailure('合成網路錯誤');
    }
    return CatalogPage(
      [
        free,
        vip,
        Drama(
          id: '$source:auto-$page',
          source: source,
          title: '第$page頁短劇',
          episodes: 1,
          category: '合成資料',
        ),
      ],
      page: page,
      hasMore: page < 5,
    );
  }

  @override
  Future<DramaDetail> detail(Drama drama) async {
    detailCalls++;
    return DramaDetail(drama, [
      Episode({'id': '1', 'title': '第1集', 'currentEpisode': 1}, 1),
      Episode({'id': '2', 'title': '第2集', 'currentEpisode': 2}, 2),
    ]);
  }

  @override
  Future<PlaybackPlan> resolve(
    Drama drama,
    Episode episode, {
    int quality = 0,
  }) async => const PlaybackPlan(url: 'https://example.test/synthetic.mp4');
  @override
  Future<PlaybackPlan> fallback(PlaybackPlan current) async =>
      throw AppFailure('沒有備用線路');
  @override
  Future<void> cancelPlayback() async {}
  @override
  Future<void> release(String session) async {}
}
