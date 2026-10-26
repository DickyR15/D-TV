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

  test('refreshes a failed route list once before recreating the player', () {
    final recovery = PlaybackRecovery();
    for (var i = 0; i < 9; i++) {
      expect(
        recovery.next(route(i, count: 10)),
        PlaybackRecoveryAction.alternative,
      );
    }
    expect(recovery.next(route(9, count: 10)), PlaybackRecoveryAction.refresh);

    // The refreshed URL list gets its own bounded pass.
    for (var i = 0; i < 9; i++) {
      expect(
        recovery.next(route(i, count: 10)),
        PlaybackRecoveryAction.alternative,
      );
    }
    expect(
      recovery.next(route(9, count: 10)),
      PlaybackRecoveryAction.recreatePlayer,
    );

    // After one player replacement, a second refresh is allowed, but stop
    // remains the terminal state when a second route list is exhausted.
    final replacement = PlaybackRecovery();
    for (var i = 0; i < 9; i++) {
      expect(
        replacement.next(route(i, count: 10)),
        PlaybackRecoveryAction.alternative,
      );
    }
    expect(
      replacement.next(route(9, count: 10), allowPlayerRecreate: false),
      PlaybackRecoveryAction.refresh,
    );
    for (var i = 0; i < 9; i++) {
      expect(
        replacement.next(route(i, count: 10), allowPlayerRecreate: false),
        PlaybackRecoveryAction.alternative,
      );
    }
    expect(
      replacement.next(route(9, count: 10), allowPlayerRecreate: false),
      PlaybackRecoveryAction.stop,
    );
  });

  test('one-route sources force one fresh URL before terminal recovery', () {
    final recovery = PlaybackRecovery();
    expect(
      recovery.next(route(0, count: 1)),
      PlaybackRecoveryAction.refresh,
    );
    expect(
      recovery.next(route(0, count: 1)),
      PlaybackRecoveryAction.recreatePlayer,
    );

    final replacement = PlaybackRecovery();
    expect(
      replacement.next(route(0, count: 1), allowPlayerRecreate: false),
      PlaybackRecoveryAction.refresh,
    );
    expect(
      replacement.next(route(0, count: 1), allowPlayerRecreate: false),
      PlaybackRecoveryAction.stop,
    );
  });

  test('large route lists get at most twelve attempts per resolution', () {
    final recovery = PlaybackRecovery();
    for (var i = 0; i < PlaybackRecovery.maxAlternativeAttempts; i++) {
      expect(
        recovery.next(route(i, count: 100)),
        PlaybackRecoveryAction.alternative,
      );
    }
    expect(recovery.next(route(12, count: 100)), PlaybackRecoveryAction.refresh);
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

  test('recovery continues beyond 45 seconds but remains bounded at three minutes', () {
    final start = DateTime(2026, 10, 25, 12);
    var now = start;
    final recovery = PlaybackRecovery(now: () => now);
    final plan = route(0, count: 20);

    expect(recovery.next(plan), PlaybackRecoveryAction.alternative);
    now = start.add(const Duration(seconds: 30));
    expect(recovery.next(plan), PlaybackRecoveryAction.alternative);
    now = start.add(const Duration(seconds: 46));
    // The previous 45-second limit stopped before all candidate routes ran.
    expect(recovery.next(plan), PlaybackRecoveryAction.alternative);
    now = start.add(PlaybackRecovery.maxRecoveryDuration);
    expect(recovery.next(plan), PlaybackRecoveryAction.stop);

    recovery.reset();
    expect(recovery.next(plan), PlaybackRecoveryAction.alternative);
  });

  test(
    'a stalled foreground stream fails over after a bounded resume window',
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
      expect(stalled(31), isFalse);
      expect(stalled(32), isTrue);
      expect(stalled(33, position: 6), isFalse);
      expect(stalled(64, position: 6), isFalse);
      expect(stalled(65, position: 6), isTrue);
      expect(stalled(60, intendedToPlay: false, position: 6), isFalse);
      expect(stalled(80, intendedToPlay: false, position: 6), isFalse);
      expect(stalled(100, foreground: false, position: 6), isFalse);
      expect(stalled(101, position: 6), isFalse);
      health.reset();
      expect(stalled(200, position: 6), isFalse);
    },
  );

  test('startup gets 20 seconds and buffer or duration changes reset the watchdog', () {
    final health = PlaybackHealth();
    final start = DateTime(2026, 10, 24);

    bool stalled(
      int seconds, {
      int buffer = 0,
      int duration = 0,
      int position = 0,
    }) => health.stalled(
      position: Duration(seconds: position),
      duration: Duration(seconds: duration),
      buffer: Duration(seconds: buffer),
      intendedToPlay: true,
      foreground: true,
      now: start.add(Duration(seconds: seconds)),
    );

    expect(stalled(0), isFalse);
    expect(stalled(10), isFalse); // Initial MP4 response can be slow.
    expect(stalled(18, buffer: 1), isFalse); // Buffer activity is progress.
    expect(stalled(27, buffer: 2), isFalse); // New buffer activity resets time.
    expect(stalled(36, buffer: 2), isFalse);
    expect(stalled(46, buffer: 2), isFalse); // 19 seconds since last progress.
    expect(stalled(47, buffer: 2), isTrue); // 20 seconds since last progress.

    health.reset();
    expect(stalled(100, duration: 120), isFalse); // Duration discovery is progress.
    expect(stalled(131, duration: 120), isFalse);
    expect(stalled(132, duration: 120), isTrue); // Mid-playback timeout is 32s.
  });

  test('recovery budget resets only after 30 seconds of progressing playback', () {
    final tracker = PlaybackRecoveryStability();
    final start = DateTime(2026, 10, 25, 12);
    var becameStable = false;

    for (var second = 0; second <= 30; second++) {
      becameStable = tracker.observe(
        position: Duration(seconds: second + 1),
        playing: true,
        buffering: false,
        intendedToPlay: true,
        foreground: true,
        completed: false,
        now: start.add(Duration(seconds: second)),
      );
      if (second < 30) expect(becameStable, isFalse);
    }
    expect(becameStable, isTrue);
  });

  test('buffering, pause, and stalled position restart recovery stability window', () {
    final tracker = PlaybackRecoveryStability();
    final start = DateTime(2026, 10, 25, 13);

    bool observe(
      int second, {
      int position = 1,
      bool playing = true,
      bool buffering = false,
      bool intendedToPlay = true,
      bool foreground = true,
    }) => tracker.observe(
      position: Duration(seconds: position),
      playing: playing,
      buffering: buffering,
      intendedToPlay: intendedToPlay,
      foreground: foreground,
      completed: false,
      now: start.add(Duration(seconds: second)),
    );

    for (var second = 0; second <= 10; second++) {
      expect(observe(second, position: second + 1), isFalse);
    }
    expect(observe(11, position: 12, buffering: true), isFalse);
    for (var second = 12; second <= 41; second++) {
      expect(observe(second, position: second + 1), isFalse);
    }
    expect(observe(42, position: 43), isTrue);

    tracker.reset();
    expect(observe(50, position: 10), isFalse);
    expect(observe(51, position: 10, playing: false), isFalse);
    expect(observe(52, position: 10), isFalse);
    expect(observe(55, position: 10), isFalse);
    expect(observe(56, position: 11), isFalse);
    for (var second = 57; second <= 86; second++) {
      final stable = observe(second, position: second - 45);
      if (second < 86) expect(stable, isFalse);
      if (second == 86) expect(stable, isTrue);
    }
  });

}
