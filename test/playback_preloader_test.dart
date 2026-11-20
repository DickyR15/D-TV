import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/diary_service.dart';
import 'package:hgtv/models.dart';
import 'package:hgtv/playback_preloader.dart';

import 'fixtures.dart';

class FailingPreloadRepository extends FixtureRepository {
  int preloadCalls = 0;

  @override
  Future<PlaybackPlan?> preload(
    Drama drama,
    Episode episode, {
    int quality = 0,
    bool online = false,
  }) async {
    preloadCalls++;
    throw TimeoutException('next episode resolver timed out');
  }
}

class DetailPreloadRepository extends FixtureRepository {
  DetailPreloadRepository(this.plan);

  final PlaybackPlan plan;
  final released = <String>[];
  int preloadCalls = 0;
  int cancelPreloadCalls = 0;
  String lastDramaId = '';
  int lastEpisodeNumber = 0;
  int lastQuality = 0;

  @override
  Future<PlaybackPlan?> preload(
    Drama drama,
    Episode episode, {
    int quality = 0,
    bool online = false,
  }) async {
    preloadCalls++;
    lastDramaId = drama.id;
    lastEpisodeNumber = episode.number;
    lastQuality = quality;
    return plan;
  }

  @override
  Future<void> cancelPreload() async {
    cancelPreloadCalls++;
  }

  @override
  Future<void> release(String session) async {
    released.add(session);
  }

}

void main() {
  Episode episode(int number) => Episode({
    'id': 'episode-$number',
    'currentEpisode': number,
  }, number);

  testWidgets('detail preload prepares and transfers only the selected episode', (
    tester,
  ) async {
    DiaryService.clear();
    final repository = DetailPreloadRepository(
      const PlaybackPlan(
        url: 'https://example.test/prepared.m3u8',
        session: 'detail-prepared',
      ),
    );
    final preloader = PlaybackPreloader(repository);
    addTearDown(preloader.dispose);

    preloader.prepare(
      FixtureRepository.free,
      episode(2),
      quality: 1080,
    );

    // The player may request the prepared session before the scheduled
    // preload has started. That early miss must not cancel the pending work.
    expect(
      preloader.take(FixtureRepository.free, episode(2), quality: 1080),
      isNull,
    );
    expect(DiaryService.fullText, contains('reason=still_loading'));

    await tester.pump(const Duration(milliseconds: 600));
    await tester.pump();

    expect(repository.preloadCalls, 1);
    expect(repository.lastDramaId, FixtureRepository.free.id);
    expect(repository.lastEpisodeNumber, 2);
    expect(repository.lastQuality, 1080);

    final transferred = preloader.take(
      FixtureRepository.free,
      episode(2),
      quality: 1080,
    );
    expect(transferred?.session, 'detail-prepared');
    expect(preloader.lastTakeResolveMs, greaterThanOrEqualTo(0));
    expect(repository.released, isEmpty);
    expect(DiaryService.fullText, contains('[NextEpisodePreload] scheduled'));
    expect(DiaryService.fullText, contains('[NextEpisodePreload] start'));
    expect(DiaryService.fullText, contains('[NextEpisodePreload] complete'));
    expect(DiaryService.fullText, contains('[NextEpisodePreload] take hit=true'));
    expect(DiaryService.fullText, contains('preloadResolveMs='));
  });

  testWidgets('a different episode never receives the prepared session', (
    tester,
  ) async {
    final repository = DetailPreloadRepository(
      const PlaybackPlan(
        url: 'https://example.test/prepared.m3u8',
        session: 'wrong-episode',
      ),
    );
    final preloader = PlaybackPreloader(repository);
    addTearDown(preloader.dispose);

    preloader.prepare(FixtureRepository.free, episode(1));
    await tester.pump(const Duration(milliseconds: 600));
    await tester.pump();

    expect(
      preloader.take(FixtureRepository.free, episode(2)),
      isNull,
    );
    expect(repository.released, ['wrong-episode']);
  });

  testWidgets('an expired detail plan is released instead of transferred', (
    tester,
  ) async {
    var now = DateTime(2026, 11, 13, 12);
    final repository = DetailPreloadRepository(
      const PlaybackPlan(
        url: 'https://example.test/prepared.m3u8',
        session: 'expired-detail-plan',
      ),
    );
    final preloader = PlaybackPreloader(repository, now: () => now);
    addTearDown(preloader.dispose);

    preloader.prepare(FixtureRepository.free, episode(1));
    await tester.pump(const Duration(milliseconds: 600));
    await tester.pump();

    now = now.add(const Duration(minutes: 3));
    expect(preloader.take(FixtureRepository.free, episode(1)), isNull);
    // A second query for the same episode should not become an identity
    // mismatch just because the previous session expired.
    expect(preloader.take(FixtureRepository.free, episode(1)), isNull);
    expect(DiaryService.fullText, contains('reason=expired'));
    expect(DiaryService.fullText, contains('reason=not_ready'));
    expect(DiaryService.fullText, isNot(contains('reason=identity_mismatch')));
    expect(repository.released, ['expired-detail-plan']);
  });

  testWidgets('failed next-episode preload records elapsed time and error type', (
    tester,
  ) async {
    DiaryService.clear();
    final repository = FailingPreloadRepository();
    final preloader = PlaybackPreloader(repository);
    addTearDown(preloader.dispose);

    preloader.prepare(FixtureRepository.free, episode(2));
    await tester.pump(const Duration(milliseconds: 600));
    await tester.pump();

    expect(repository.preloadCalls, 1);
    expect(preloader.error, isNotEmpty);
    expect(DiaryService.fullText, contains('[NextEpisodePreload] start'));
    expect(DiaryService.fullText, contains('[NextEpisodePreload] failed'));
    expect(DiaryService.fullText, contains('errorType=TimeoutException'));
  });

}
