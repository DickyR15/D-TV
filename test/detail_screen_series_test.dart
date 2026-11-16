import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:hgtv/app_layout.dart';
import 'package:hgtv/core_bridge.dart';
import 'package:hgtv/detail_screen.dart';
import 'package:hgtv/models.dart';
import 'package:hgtv/remote_widgets.dart';

import 'fixtures.dart';

class _SeriesFixtureRepository extends FixtureRepository {
  _SeriesFixtureRepository(this.seasonRows, {this.episodes = const []});

  final List<DramaSeason> seasonRows;
  final List<Episode> episodes;
  String lastDetailId = '';

  @override
  Future<DramaDetail> detail(Drama drama) async {
    detailCalls++;
    lastDetailId = drama.id;
    return DramaDetail(drama, episodes);
  }

  @override
  Future<String> cover(Drama drama, {bool force = false}) async => '';

  @override
  Future<List<DramaSeason>> seasons(Drama drama) async => seasonRows;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets(
    'TV can select an upcoming Redfruit season when current season has no episodes',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);

      const currentSeason = Drama(
        id: 'hongguo:1001',
        source: 'hongguo',
        title: '測試系列第一季',
        seriesGroupId: '123456',
      );
      const upcomingSeason = Drama(
        id: 'hongguo:1002',
        source: 'hongguo',
        title: '測試系列第二季',
        seriesGroupId: '123456',
      );
      final repository = _SeriesFixtureRepository([
        const DramaSeason(
          drama: currentSeason,
          number: 1,
          label: '第1季',
        ),
        const DramaSeason(
          drama: upcomingSeason,
          number: 2,
          label: '第2季',
          subtitle: '即將上線',
          upcoming: true,
          playable: false,
        ),
      ]);

      await tester.pumpWidget(
        AppLayout(
          television: true,
          formFactor: FormFactor.television,
          child: MaterialApp(
            home: DetailScreen(
              drama: currentSeason,
              repository: repository,
              store: store,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('系列劇'), findsOneWidget);
      final upcomingChipFinder = find.byKey(
        const ValueKey('official-season-hongguo:1002'),
      );
      expect(upcomingChipFinder, findsOneWidget);
      final upcomingButton = tester.widget<RemoteButton>(
        upcomingChipFinder,
      );
      expect(upcomingButton.onPressed, isNotNull);
      final currentButton = tester.widget<RemoteButton>(
        find.byKey(const ValueKey('official-season-hongguo:1001')),
      );
      expect(currentButton.focusNode!.hasFocus, isTrue);

      await tester.sendKeyEvent(LogicalKeyboardKey.arrowRight);
      await tester.pumpAndSettle();
      expect(
        tester.widget<RemoteButton>(upcomingChipFinder).focusNode!.hasFocus,
        isTrue,
      );
      await tester.sendKeyEvent(LogicalKeyboardKey.enter);
      await tester.pumpAndSettle();

      expect(repository.lastDetailId, upcomingSeason.id);
      expect(
        find.widgetWithText(AppBar, '測試系列第二季'),
        findsOneWidget,
      );
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets('TV synopsis can be scrolled with the remote directional keys', (tester) async {
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    final longSynopsis = List.filled(100, '這是一段測試劇情簡介。').join();
    final drama = Drama(
      id: 'hongguo:synopsis-test',
      source: 'hongguo',
      title: '簡介捲動測試',
      description: longSynopsis,
      seriesGroupId: 'synopsis-series',
    );
    final repository = _SeriesFixtureRepository(const []);
    tester.view.physicalSize = const Size(1920, 1080);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(
      AppLayout(
        television: true,
        formFactor: FormFactor.television,
        child: MaterialApp(
          home: DetailScreen(
            drama: drama,
            repository: repository,
            store: store,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    final scrollFinder = find.byKey(const ValueKey('tv-detail-synopsis-scroll'));
    expect(scrollFinder, findsOneWidget);
    final scroll = tester.widget<SingleChildScrollView>(scrollFinder);
    expect(scroll.controller!.position.maxScrollExtent, greaterThan(0));
    final focus = tester.widget<Focus>(
      find.byKey(const ValueKey('tv-detail-synopsis-focus')),
    );
    focus.focusNode!.requestFocus();
    await tester.pump();
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
    await tester.pumpAndSettle();
    expect(scroll.controller!.offset, greaterThan(0));
    expect(tester.takeException(), isNull);
  });

  testWidgets(
    'TV detail收藏 can be focused and activated with the remote',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      final store = testStore(await SharedPreferences.getInstance());
      addTearDown(store.dispose);
      const drama = Drama(
        id: 'hongguo:favorite-remote-test',
        source: 'hongguo',
        title: '收藏遙控器測試',
        episodes: 1,
      );
      final repository = _SeriesFixtureRepository(
        const [],
        episodes: [
          Episode(
            {'id': '1', 'title': '第1集', 'currentEpisode': 1},
            1,
          ),
        ],
      );
      tester.view.physicalSize = const Size(1920, 1080);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);

      await tester.pumpWidget(
        AppLayout(
          television: true,
          formFactor: FormFactor.television,
          child: MaterialApp(
            home: DetailScreen(
              drama: drama,
              repository: repository,
              store: store,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      final playFinder = find.byKey(const ValueKey('start-play'));
      final favoriteFinder = find.byKey(const ValueKey('favorite-toggle'));
      expect(playFinder, findsOneWidget);
      expect(favoriteFinder, findsOneWidget);
      expect(
        tester.widget<FilledButton>(playFinder).focusNode!.hasPrimaryFocus,
        isTrue,
      );

      // The bottom play action provides a direct D-pad route to 收藏.
      await tester.sendKeyEvent(LogicalKeyboardKey.arrowLeft);
      await tester.pump();
      expect(
        tester.widget<FilledButton>(favoriteFinder).focusNode!.hasPrimaryFocus,
        isTrue,
      );

      await tester.sendKeyEvent(LogicalKeyboardKey.enter);
      await tester.pumpAndSettle();
      expect(store.following(drama.id), isNotNull);
      expect(find.text('取消收藏'), findsOneWidget);
      expect(tester.takeException(), isNull);
    },
  );

  test('native season lookup is currently limited to Redfruit and Duoduo', () {
    expect(supportsNativeSeriesSeasons('hongguo'), isTrue);
    expect(supportsNativeSeriesSeasons('duoduo'), isTrue);
    for (final source in SourceSite.knownValues) {
      if (source.id == 'hongguo' || source.id == 'duoduo') continue;
      expect(
        supportsNativeSeriesSeasons(source.id),
        isFalse,
        reason: '${source.name} does not yet have a native season endpoint',
      );
    }
  });
}
