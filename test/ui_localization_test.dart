import 'package:flutter_test/flutter_test.dart';

import '../lib/ui_localization.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUpAll(() async {
    await initializeUiLocalization();
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

  test('keeps source identifiers and Latin labels untouched', () {
    expect(uiTraditional('hongguo-hot / HGTV / AI'), 'hongguo-hot / HGTV / AI');
  });
}
