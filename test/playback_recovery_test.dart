import 'package:hgtv/models.dart';
import 'package:hgtv/playback_recovery.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  PlaybackPlan route(int index, {int count = 3}) => PlaybackPlan(
    url: 'https://media.test/$index.mp4',
    session: 'session-$index',
    routeIndex: index,
    routeCount: count,
  );

  test('tries routes once, then recreates the player instead of wrapping to route 1', () {
    final recovery = PlaybackRecovery();
    for (var i = 0; i < 9; i++) {
      expect(
        recovery.next(route(i, count: 10)),
        PlaybackRecoveryAction.alternative,
      );
    }
    expect(recovery.next(route(9, count: 10)), PlaybackRecoveryAction.recreatePlayer);

    // The replacement screen is only allowed one full route pass.
    final replacement = PlaybackRecovery();
    for (var i = 0; i < 9; i++) {
      expect(
        replacement.next(route(i, count: 10)),
        PlaybackRecoveryAction.alternative,
      );
    }
    expect(
      replacement.next(route(9, count: 10), allowPlayerRecreate: false),
      PlaybackRecoveryAction.stop,
    );
  });

  test('one-route sources recreate the player once without endless retrying', () {
    final recovery = PlaybackRecovery();
    expect(
      recovery.next(route(0, count: 1)),
      PlaybackRecoveryAction.recreatePlayer,
    );
    expect(
      PlaybackRecovery().next(
        route(0, count: 1),
        allowPlayerRecreate: false,
      ),
      PlaybackRecoveryAction.stop,
    );
  });

  test('large route lists remain bounded per player', () {
    final recovery = PlaybackRecovery();
    for (var i = 0; i < PlaybackRecovery.maxAlternativeAttempts; i++) {
      expect(
        recovery.next(route(i, count: 100)),
        PlaybackRecoveryAction.alternative,
      );
    }
    expect(
      recovery.next(route(12, count: 100)),
      PlaybackRecoveryAction.recreatePlayer,
    );
    expect(
      recovery.next(
        route(12, count: 100),
        allowPlayerRecreate: false,
      ),
      PlaybackRecoveryAction.stop,
    );
  });

  test(
    'a stalled foreground stream times out but paused and background playback do not',
    () {
      final health = PlaybackHealth();
      final start = DateTime(2026, 9, 18);
      bool stalled(
        int seconds, {
        bool intendedToPlay = true,
        bool foreground = true,
        int position = 5,
      }) => health.stalled(
        position: Duration(seconds: position),
        intendedToPlay: intendedToPlay,
        foreground: foreground,
        now: start.add(Duration(seconds: seconds)),
      );
      expect(stalled(0), isFalse);
      expect(stalled(11), isFalse);
      expect(stalled(12), isTrue);
      expect(stalled(13, position: 6), isFalse);
      expect(stalled(40, position: 6), isFalse);
      expect(stalled(60, intendedToPlay: false, position: 6), isFalse);
      expect(stalled(80, intendedToPlay: false, position: 6), isFalse);
      expect(stalled(100, foreground: false, position: 6), isFalse);
      expect(stalled(101, position: 6), isFalse);
      health.reset();
      expect(stalled(200, position: 6), isFalse);
    },
  );
}
