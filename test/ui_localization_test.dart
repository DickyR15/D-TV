import 'package:flutter_test/flutter_test.dart';

import '../lib/ui_localization.dart';
import '../lib/catalog_sort.dart';
import '../lib/models.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUpAll(() async {
    await initializeUiLocalization();
  });

  test('defaults catalog sorting to online date', () {
    expect(const CatalogView().sort, CatalogSort.date);
    expect(CatalogView.fromJson(<String, dynamic>{}).sort, CatalogSort.date);
  });

  test('converts drama categories to Taiwan Traditional Chinese', () {
    expect(
      uiTraditional('新剧、真人剧、漫剧、AI剧、剧情、反转、古装、总裁、恋爱、悬疑'),
      '新劇、真人劇、漫劇、AI 劇、劇情、反轉、古裝、總裁、戀愛、懸疑',
    );
  });

  test('converts common Mainland wording to Taiwan usage', () {
    expect(
      uiTraditional('软件、用户、鼠标、网络、视频、设置、加载更多、播放历史'),
      '軟體、使用者、滑鼠、網路、影片、設定、載入更多、播放歷史',
    );
  });

  test('Traditional input can find a Simplified source title', () {
    const drama = Drama(
      id: 'test-drama',
      source: 'hongguo',
      title: '新剧里的霸总',
      description: '一场相遇改写命运',
      category: '真人剧',
    );

    expect(matchesDramaQuery(drama, '新劇'), isTrue);
    expect(matchesDramaQuery(drama, '霸總'), isTrue);
    expect(matchesDramaQuery(drama, '改寫命運'), isTrue);
  });

  test('sorts Simplified source season titles in natural order', () {
    const rows = [
      Drama(id: 'season-10', source: 'hongguo', title: '第10季'),
      Drama(id: 'season-2', source: 'hongguo', title: '第2季'),
    ];

    final sorted = sortCatalog(
      rows,
      const CatalogView(sort: CatalogSort.season),
    );

    expect(sorted.map((drama) => drama.id), ['season-2', 'season-10']);
  });

  test('sorts heat metrics even when the source uses Simplified units', () {
    const rows = [
      Drama(id: 'heat-2', source: 'hongguo', title: '短劇甲', heat: '2万'),
      Drama(id: 'heat-12', source: 'hongguo', title: '短劇乙', heat: '12万'),
    ];

    final sorted = sortCatalog(
      rows,
      const CatalogView(sort: CatalogSort.heat),
    );

    expect(sorted.map((drama) => drama.id), ['heat-12', 'heat-2']);
  });

  test('uses Taiwan wording throughout source management terminology', () {
    expect(
      uiTraditional('番剧、动漫、缓存、文件夹、文件、密码、登录、账号、信息、连接、安装包'),
      '動畫連載、動畫、快取、資料夾、檔案、密碼、登入、帳號、資訊、連線、安裝檔',
    );
    expect(
      uiTraditional('番劇、動漫、緩存、文件夾、登錄、賬號、連接、安裝包'),
      '動畫連載、動畫、快取、資料夾、登入、帳號、連線、安裝檔',
    );
  });

  test('source names and descriptions use Taiwan Traditional Chinese', () {
    expect(
      SourceSite.allValues.map((source) => source.name).toList(),
      ['紅果', '韓小圈', '鬼片', '青空', '黃豆', '劇果', '野果', '帝果', '黃果影片', '黃果 AI', '黃果舊版'],
    );
    expect(SourceSite.sorani.description, '動畫連載 · 劇場版動畫 · 特攝');
    expect(SourceSite.guipian.description, '恐怖片 · 電視劇 · 動畫');
    expect(SourceSite.hanxiaoquan.description, '韓劇 · 韓國電影 · 綜藝節目 · 動畫');
  });

  test('uses Taiwan wording for QR code terminology', () {
    expect(uiTraditional('二維碼、掃碼、手機'), 'QR Code、掃描、手機');
  });

  test('keeps source identifiers and Latin labels untouched', () {
    expect(uiTraditional('hongguo-hot / D TV / AI'), 'hongguo-hot / D TV / AI');
  });
}
