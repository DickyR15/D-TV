import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/app_layout.dart';
import 'package:hgtv/playback_settings_screen.dart';
import 'package:hgtv/playback_preferences.dart';
import 'package:hgtv/lan_screen.dart';
import 'package:hgtv/widgets.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('landscape devices enable blur by default and preserve manual choice', (tester) async {
    SharedPreferences.setMockInitialValues({
      'playback': '{"speed":1.0,"portraitBlurBackground":false}',
    });
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    tester.view.physicalSize = const Size(1920, 1080);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      MaterialApp(
        home: AppLayout(
          television: true,
          formFactor: FormFactor.television,
          child: PlaybackSettingsScreen(
            store: store,
            portraitBlurSupported: true,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('playback-default-speed')), findsOneWidget);
    expect(find.byKey(const ValueKey('playback-aspect-ratio')), findsOneWidget);
    expect(find.byKey(const ValueKey('playback-default-quality')), findsOneWidget);
    expect(find.byKey(const ValueKey('playback-decoder')), findsOneWidget);
    expect(find.byKey(const ValueKey('playback-preopen-next-episode')), findsOneWidget);
    expect(
      tester
          .widget<SwitchListTile>(
            find.byKey(const ValueKey('playback-preopen-next-episode')),
          )
          .value,
      isTrue,
      reason: 'fresh TV installs should enable next-episode preloading',
    );
    expect(find.byKey(const ValueKey('playback-auto-advance')), findsOneWidget);
    final blurFinder =
        find.byKey(const ValueKey('playback-portrait-blur-background'));
    expect(blurFinder, findsOneWidget);
    expect(
      tester.widget<SwitchListTile>(blurFinder).value,
      isTrue,
      reason: 'landscape devices migrate to blur enabled by default',
    );
    expect(store.playbackPreferences.portraitBlurBackground, isTrue);

    await tester.tap(blurFinder);
    await tester.pumpAndSettle();
    expect(store.playbackPreferences.portraitBlurBackground, isFalse);

    await tester.tap(find.byKey(const ValueKey('playback-default-speed')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('1.25x').last);
    await tester.pumpAndSettle();

    expect(store.playbackPreferences.speed, 1.25);
    expect(
      PlaybackPreferences.fromJson(store.playbackPreferences.toJson()).speed,
      1.25,
    );
    expect(
      store.playbackPreferences.toJson()['portraitBlurBackground'],
      isFalse,
    );
  });

  testWidgets('portrait devices keep blur off by default until rotated landscape', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    tester.view.physicalSize = const Size(600, 1000);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      MaterialApp(
        home: AppLayout(
          television: false,
          formFactor: FormFactor.phone,
          child: PlaybackSettingsScreen(
            store: store,
            portraitBlurSupported: true,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    final blurFinder = find.byKey(
      const ValueKey('playback-portrait-blur-background'),
    );
    await tester.scrollUntilVisible(blurFinder, 160);
    await tester.pumpAndSettle();
    final blur = tester.widget<SwitchListTile>(blurFinder);
    expect(blur.value, isFalse);
    expect(store.needsPortraitBlurLandscapeDefaultMigration, isTrue);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('decoder setting is visible in phone, tablet and window layouts', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    for (final factor in [
      FormFactor.phone,
      FormFactor.pad,
      FormFactor.window,
    ]) {
      await tester.pumpWidget(
        MaterialApp(
          home: AppLayout(
            television: false,
            formFactor: factor,
            child: PlaybackSettingsScreen(
              store: store,
              portraitBlurSupported: true,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(
        find.byKey(const ValueKey('playback-decoder')),
        findsOneWidget,
        reason: 'decoder setting should appear in ${factor.name} layout',
      );
    }

    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('TV playback settings exposes the normal portrait blur toggle', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({
      'playback': '{"portraitBlurBackground": true}',
    });
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: AppLayout(
          television: true,
          formFactor: FormFactor.television,
          child: PlaybackSettingsScreen(
            store: store,
            portraitBlurSupported: true,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    final blurFinder = find.byKey(
      const ValueKey('playback-portrait-blur-background'),
    );
    await tester.scrollUntilVisible(blurFinder, 120);
    await tester.pumpAndSettle();
    final blur = tester.widget<SwitchListTile>(blurFinder);
    expect(blur.value, isTrue);
    expect(blur.onChanged, isNotNull);
    expect((blur.title as Text).data, '直式影片模糊背景');
    expect(find.textContaining('試驗'), findsNothing);
    expect(
      blur.subtitle,
      isA<Text>().having(
        (value) => value.data,
        'TV blur explanation',
        allOf(contains('橫式畫面播放直式影片'), contains('不另開解碼器')),
      ),
    );

    final networkFinder = find.byKey(
      const ValueKey('playback-tv-network-speed'),
    );
    await tester.scrollUntilVisible(networkFinder, 120);
    await tester.pumpAndSettle();
    final network = tester.widget<SwitchListTile>(networkFinder);
    final clockFinder = find.byKey(const ValueKey('playback-tv-clock'));
    await tester.scrollUntilVisible(clockFinder, 120);
    await tester.pumpAndSettle();
    final clock = tester.widget<SwitchListTile>(clockFinder);
    expect(network.value, isTrue);
    expect(clock.value, isTrue);
    await tester.ensureVisible(networkFinder);
    await tester.tap(networkFinder);
    await tester.pumpAndSettle();
    expect(store.playbackPreferences.showTelevisionNetworkSpeed, isFalse);
    expect(store.playbackPreferences.showTelevisionClock, isTrue);
    await tester.ensureVisible(clockFinder);
    await tester.tap(clockFinder);
    await tester.pumpAndSettle();
    expect(store.playbackPreferences.showTelevisionNetworkSpeed, isFalse);
    expect(store.playbackPreferences.showTelevisionClock, isFalse);
  });


  testWidgets('decoder setting uses auto wording and confirms when it applies', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: AppLayout(
          television: false,
          formFactor: FormFactor.window,
          child: PlaybackSettingsScreen(
            store: store,
            portraitBlurSupported: true,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.descendant(of: find.byKey(const ValueKey('playback-decoder')), matching: find.text('自動')), findsOneWidget);
    expect(find.text('預設'), findsNothing);
    expect(find.textContaining('無須重啟 D TV'), findsOneWidget);

    final decoder = find.byKey(const ValueKey('playback-decoder'));
    await tester.ensureVisible(decoder);
    await tester.tap(decoder);
    await tester.pumpAndSettle();

    final softwareOption = find.text('軟體解碼').last;
    expect(softwareOption, findsOneWidget);
    await tester.tap(softwareOption);
    await tester.pumpAndSettle();

    expect(
      store.playbackPreferences.televisionDecoder,
      TelevisionDecoder.software,
    );
    expect(
      find.text('解碼方式已儲存；重新進入播放頁面即可套用，無須重啟 D TV。'),
      findsOneWidget,
    );
    expect(find.byType(DtvBrandMark), findsOneWidget);
    expect(find.byKey(const ValueKey('dtv-brand-toast')), findsOneWidget);
  });

  testWidgets('compatible renderer prompt starts on Cancel; Select cannot restart accidentally', (tester) async {
    Future<bool?>? result;
    await tester.pumpWidget(
      AppLayout(
        television: true,
        formFactor: FormFactor.television,
        child: MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: TextButton(
                key: const ValueKey('open-compatible-rendering-dialog'),
                onPressed: () {
                  result = showDialog<bool>(
                    context: context,
                    builder: (_) => const CompatibleRenderingRestartDialog(
                      enabled: true,
                    ),
                  );
                },
                child: const Text('開啟'),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.byKey(const ValueKey('open-compatible-rendering-dialog')));
    await tester.pumpAndSettle();
    final cancel = tester.widget<TextButton>(
      find.byKey(const ValueKey('compatible-rendering-cancel')),
    );
    expect(cancel.focusNode!.hasPrimaryFocus, isTrue);
    await tester.sendKeyEvent(LogicalKeyboardKey.select);
    await tester.pumpAndSettle();
    expect(await result, isFalse);
  });

  testWidgets('compatible renderer prompt lets D-pad move from Cancel to Restart Now', (tester) async {
    Future<bool?>? result;
    await tester.pumpWidget(
      AppLayout(
        television: true,
        formFactor: FormFactor.television,
        child: MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: TextButton(
                key: const ValueKey('open-compatible-rendering-dialog'),
                onPressed: () {
                  result = showDialog<bool>(
                    context: context,
                    builder: (_) => const CompatibleRenderingRestartDialog(
                      enabled: true,
                    ),
                  );
                },
                child: const Text('開啟'),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.byKey(const ValueKey('open-compatible-rendering-dialog')));
    await tester.pumpAndSettle();
    final restart = tester.widget<FilledButton>(
      find.byKey(const ValueKey('compatible-rendering-restart')),
    );
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowRight);
    await tester.pump();
    expect(restart.focusNode!.hasPrimaryFocus, isTrue);
    await tester.sendKeyEvent(LogicalKeyboardKey.select);
    await tester.pumpAndSettle();
    expect(await result, isTrue);
  });

  testWidgets('device rename defaults to Cancel and D-pad reaches Save', (tester) async {
    String? result;
    await tester.pumpWidget(
      AppLayout(
        television: true,
        formFactor: FormFactor.television,
        child: MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: TextButton(
                key: const ValueKey('open-rename'),
                onPressed: () async {
                  result = await showDialog<String>(
                    context: context,
                    builder: (_) => const DeviceRenameDialog(
                      initialName: '客廳電視',
                    ),
                  );
                },
                child: const Text('開啟'),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.byKey(const ValueKey('open-rename')));
    await tester.pumpAndSettle();
    final cancel = tester.widget<TextButton>(
      find.byKey(const ValueKey('lan-rename-cancel')),
    );
    final save = tester.widget<FilledButton>(
      find.byKey(const ValueKey('lan-rename-save')),
    );
    expect(cancel.focusNode!.hasPrimaryFocus, isTrue);
    expect(
      tester.widget<TextField>(
        find.byKey(const ValueKey('lan-rename-name')),
      ).autofocus,
      isFalse,
    );
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowRight);
    await tester.pump();
    expect(save.focusNode!.hasPrimaryFocus, isTrue);
    await tester.sendKeyEvent(LogicalKeyboardKey.select);
    await tester.pumpAndSettle();
    expect(result, '客廳電視');
  });

  testWidgets('device rename Cancel is activated by remote Select', (tester) async {
    String? result = 'not-returned';
    await tester.pumpWidget(
      AppLayout(
        television: true,
        formFactor: FormFactor.television,
        child: MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: TextButton(
                key: const ValueKey('open-rename'),
                onPressed: () async {
                  result = await showDialog<String>(
                    context: context,
                    builder: (_) => const DeviceRenameDialog(
                      initialName: '客廳電視',
                    ),
                  );
                },
                child: const Text('開啟'),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.byKey(const ValueKey('open-rename')));
    await tester.pumpAndSettle();
    expect(
      tester.widget<TextButton>(
        find.byKey(const ValueKey('lan-rename-cancel')),
      ).focusNode!.hasPrimaryFocus,
      isTrue,
    );
    await tester.sendKeyEvent(LogicalKeyboardKey.select);
    await tester.pumpAndSettle();
    expect(result, isNull);
  });

  testWidgets('favorites sync Cancel and confirm actions are D-pad reachable', (tester) async {
    final cancelFocus = FocusNode(debugLabel: 'test-lan-sync-cancel');
    final submitFocus = FocusNode(debugLabel: 'test-lan-sync-submit');
    addTearDown(cancelFocus.dispose);
    addTearDown(submitFocus.dispose);
    var action = '';

    await tester.pumpWidget(
      AppLayout(
        television: true,
        formFactor: FormFactor.television,
        child: MaterialApp(
          home: Scaffold(
            body: Center(
              child: FocusTraversalGroup(
                policy: OrderedTraversalPolicy(),
                child: LanSyncActionBar(
                  cancelFocus: cancelFocus,
                  submitFocus: submitFocus,
                  onCancel: () => action = 'cancel',
                  onSubmit: () => action = 'confirm',
                  submitLabel: '開始同步',
                ),
              ),
            ),
          ),
        ),
      ),
    );
    cancelFocus.requestFocus();
    await tester.pump();
    expect(cancelFocus.hasPrimaryFocus, isTrue);
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowRight);
    await tester.pump();
    expect(submitFocus.hasPrimaryFocus, isTrue);
    await tester.sendKeyEvent(LogicalKeyboardKey.select);
    await tester.pump();
    expect(action, 'confirm');
    cancelFocus.requestFocus();
    await tester.pump();
    await tester.sendKeyEvent(LogicalKeyboardKey.select);
    await tester.pump();
    expect(action, 'cancel');
  });

}
