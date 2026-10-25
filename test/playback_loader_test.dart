import 'dart:async';

import 'package:hgtv/core_bridge.dart';
import 'package:hgtv/models.dart';
import 'package:hgtv/playback_loader.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fixtures.dart';

class DeferredRepository extends FixtureRepository {
  final pending = <int, Completer<PlaybackPlan>>{};
  final released = <String>[];
  Completer<PlaybackPlan>? alternate;
  int cancellations = 0;
  Completer<void>? cancellationGate;

  @override
  Future<PlaybackPlan> resolve(
    Drama drama,
    Episode episode, {
    int quality = 0,
  }) {
    final request = Completer<PlaybackPlan>();
    pending[episode.number] = request;
    return request.future;
  }

  @override
  Future<PlaybackPlan> fallback(PlaybackPlan current) {
    alternate = Completer<PlaybackPlan>();
    return alternate!.future;
  }

  @override
  Future<void> cancelPlayback() async {
    cancellations++;
    final gate = cancellationGate;
    if (gate != null) await gate.future;
  }

  @override
  Future<void> release(String session) async => released.add(session);
}

void main() {
  Episode episode(int number) =>
      Episode({'id': '$number', 'currentEpisode': number}, number);
  Future<void> tick() => Future<void>.delayed(Duration.zero);

  test('a later episode can start before an old resolver completes', () async {
    final repository = DeferredRepository();
    final loader = PlaybackLoader(repository);
    final old = loader.load(FixtureRepository.free, episode(1));
    await tick();
    final latest = loader.load(FixtureRepository.free, episode(2));
    await tick();
    repository.pending[2]!.complete(
      const PlaybackPlan(url: 'https://example.test/2.mp4', session: 'second'),
    );
    expect((await latest)?.session, 'second');
    repository.pending[1]!.complete(
      const PlaybackPlan(url: 'https://example.test/1.mp4', session: 'first'),
    );
    expect(await old, isNull);
    expect(repository.released, ['first']);
    expect(repository.cancellations, 1);
    await loader.close();
  });

  test('a stale failure does not replace the new playback result', () async {
    final repository = DeferredRepository();
    final loader = PlaybackLoader(repository);
    final old = loader.load(FixtureRepository.free, episode(1));
    await tick();
    final latest = loader.load(FixtureRepository.free, episode(2));
    await tick();
    repository.pending[1]!.completeError(AppFailure('old request failed'));
    expect(await old, isNull);
    repository.pending[2]!.completeError(AppFailure('current request failed'));
    await expectLater(latest, throwsA(isA<AppFailure>()));
    await loader.close();
  });

  test(
    'leaving playback releases a plan that arrives after disposal',
    () async {
      final repository = DeferredRepository();
      final loader = PlaybackLoader(repository);
      final result = loader.load(FixtureRepository.free, episode(1));
      await tick();
      await loader.close();
      repository.pending[1]!.complete(
        const PlaybackPlan(url: 'https://example.test/1.mp4', session: 'late'),
      );
      expect(await result, isNull);
      expect(repository.released, ['late']);
      expect(await loader.load(FixtureRepository.free, episode(2)), isNull);
      expect(repository.pending.length, 1);
    },
  );

  test('a stalled resolver times out and releases a late session', () async {
    final repository = DeferredRepository();
    final loader = PlaybackLoader(
      repository,
      resolveTimeout: const Duration(milliseconds: 10),
    );
    final pending = loader.load(FixtureRepository.free, episode(1));
    await tick();

    await expectLater(pending, throwsA(isA<TimeoutException>()));
    expect(repository.cancellations, 1);

    repository.pending[1]!.complete(
      const PlaybackPlan(url: 'https://example.test/late.mp4', session: 'late'),
    );
    await tick();
    await tick();
    expect(repository.released, ['late']);
    await loader.close();
  });

  test(
    'a stalled cancellation cannot leave reload spinning or start a stale resolve',
    () async {
      final repository = DeferredRepository();
      final loader = PlaybackLoader(
        repository,
        resolveTimeout: const Duration(milliseconds: 10),
      );
      final first = loader.load(FixtureRepository.free, episode(1));
      await tick();
      repository.pending[1]!.complete(
        const PlaybackPlan(url: 'https://example.test/1.mp4', session: 'first'),
      );
      expect((await first)?.session, 'first');

      repository.cancellationGate = Completer<void>();
      final reload = loader.load(FixtureRepository.free, episode(2));
      await expectLater(reload, throwsA(isA<TimeoutException>()));
      expect(repository.pending.keys, [1]);

      repository.cancellationGate!.complete();
      await tick();
      await tick();
      expect(repository.pending.keys, [1]);
      await loader.close();
    },
  );

  test(
    'changing episode cancels an in-flight fallback and releases its late session',
    () async {
      final repository = DeferredRepository();
      final loader = PlaybackLoader(repository);
      final pending = loader.fallback(
        const PlaybackPlan(
          url: 'https://example.test/first.mp4',
          session: 'first',
          routeCount: 2,
        ),
      );
      await tick();
      final current = loader.load(FixtureRepository.free, episode(2));
      await tick();
      repository.pending[2]!.complete(
        const PlaybackPlan(
          url: 'https://example.test/current.mp4',
          session: 'current',
        ),
      );
      expect((await current)?.session, 'current');
      repository.alternate!.complete(
        const PlaybackPlan(
          url: 'https://example.test/late.mp4',
          session: 'late',
        ),
      );
      expect(await pending, isNull);
      expect(repository.released, ['late']);
      await loader.close();
    },
  );
}
