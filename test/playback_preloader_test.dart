import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/models.dart';
import 'package:hgtv/playback_preloader.dart';

import 'fixtures.dart';

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
    expect(repository.released, isEmpty);
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
    expect(repository.released, ['expired-detail-plan']);
  });
}
