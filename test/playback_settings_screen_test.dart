import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/playback_settings_screen.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('portrait blur background switch auto-saves and defaults off', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    await tester.pumpWidget(
      MaterialApp(home: PlaybackSettingsScreen(store: store)),
    );
    await tester.pumpAndSettle();

    final setting = find.byKey(
      const ValueKey('playback-portrait-blur-background'),
    );
    expect(setting, findsOneWidget);
    expect(tester.widget<SwitchListTile>(setting).value, isFalse);
    expect(
      find.text('直式短劇左右以目前影片影格放大、模糊後填滿，不裁切中央影片；預設關閉。使用原生 MPV 影格擷取，不另開播放器，會定時更新背景；可能增加少量 GPU／CPU 負載。'),
      findsOneWidget,
    );

    await tester.tap(setting);
    await tester.pumpAndSettle();
    expect(store.playbackPreferences.portraitBlurBackground, isTrue);

    await tester.pumpWidget(const SizedBox.shrink());
  });
}
