import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:hgtv/app_layout.dart';
import 'package:hgtv/core_bridge.dart';
import 'package:hgtv/detail_screen.dart';
import 'package:hgtv/models.dart';

import 'fixtures.dart';

class _SeriesFixtureRepository extends FixtureRepository {
  _SeriesFixtureRepository(this.seasonRows);

  final List<DramaSeason> seasonRows;
  String lastDetailId = '';

  @override
  Future<DramaDetail> detail(Drama drama) async {
    detailCalls++;
    lastDetailId = drama.id;
    // Deliberately return no episodes: the season selector must still work.
    return DramaDetail(drama, const []);
  }

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
      expect(
        tester.widget<ChoiceChip>(upcomingChipFinder).onSelected,
        isNotNull,
      );

      await tester.ensureVisible(upcomingChipFinder);
      await tester.tap(upcomingChipFinder);
      await tester.pumpAndSettle();

      expect(repository.lastDetailId, upcomingSeason.id);
      expect(find.text('測試系列第二季'), findsOneWidget);
      expect(tester.takeException(), isNull);
    },
  );

  test('native season lookup is currently limited to Redfruit and Duoduo', () {
    expect(supportsNativeSeriesSeasons('hongguo'), isTrue);
    expect(supportsNativeSeriesSeasons('duoduo'), isTrue);
    for (final source in SourceSite.values) {
      if (source.id == 'hongguo' || source.id == 'duoduo') continue;
      expect(
        supportsNativeSeriesSeasons(source.id),
        isFalse,
        reason: '${source.name} does not yet have a native season endpoint',
      );
    }
  });
}
