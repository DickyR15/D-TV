import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/widgets.dart';

void main() {
  test('poster scheduler only bypasses background work during playback', () async {
    final started = <int>[];
    final gates = <int, Completer<void>>{
      for (var id = 1; id <= 10; id++) id: Completer<void>(),
    };
    final jobs = <int, Future<void>>{};
    var paused = false;

    void enqueue(int id, {bool priority = false}) {
      CoverRequestScheduler.add(() {
        jobs[id] = CoverRequestScheduler.run(() async {
          started.add(id);
          await gates[id]!.future;
          return '$id';
        });
      }, priority: priority);
    }

    try {
      for (var id = 1; id <= 6; id++) {
        enqueue(id);
      }
      expect(started, [1, 2, 3, 4, 5, 6]);

      CoverRequestScheduler.pauseBackground();
      paused = true;
      enqueue(7);
      enqueue(8, priority: true);
      enqueue(9);
      enqueue(10, priority: true);

      for (var id = 1; id <= 6; id++) {
        gates[id]!.complete();
      }
      await Future<void>.delayed(Duration.zero);
      await Future<void>.delayed(Duration.zero);
      expect(started, [1, 2, 3, 4, 5, 6, 8, 10]);

      gates[8]!.complete();
      gates[10]!.complete();
      await Future<void>.delayed(Duration.zero);
      await Future<void>.delayed(Duration.zero);
      expect(started, [1, 2, 3, 4, 5, 6, 8, 10]);

      CoverRequestScheduler.resumeBackground();
      paused = false;
      expect(started, [1, 2, 3, 4, 5, 6, 8, 10, 7, 9]);
      gates[7]!.complete();
      gates[9]!.complete();
      await Future.wait(jobs.values);
    } finally {
      if (paused) CoverRequestScheduler.resumeBackground();
      for (final gate in gates.values) {
        if (!gate.isCompleted) gate.complete();
      }
      await Future.wait(jobs.values);
    }
  });

  test(
    'poster scheduler completes older visible requests before newer backlog',
    () async {
      final started = <int>[];
      final gates = <int, Completer<void>>{
        for (var id = 1; id <= 9; id++) id: Completer<void>(),
      };
      final jobs = <int, Future<void>>{};

      void enqueue(int id) {
        CoverRequestScheduler.add(() {
          jobs[id] = CoverRequestScheduler.run(() async {
            started.add(id);
            await gates[id]!.future;
            return '$id';
          });
        });
      }

      try {
        // Fill the six active slots, then build up a short backlog.
        for (var id = 1; id <= 6; id++) {
          enqueue(id);
        }
        expect(started, [1, 2, 3, 4, 5, 6]);

        for (var id = 7; id <= 9; id++) {
          enqueue(id);
        }
        expect(started, [1, 2, 3, 4, 5, 6]);

        for (var id = 1; id <= 6; id++) {
          gates[id]!.complete();
        }
        await Future<void>.delayed(Duration.zero);
        await Future<void>.delayed(Duration.zero);

        expect(started, [1, 2, 3, 4, 5, 6, 7, 8, 9]);

        for (var id = 7; id <= 9; id++) {
          gates[id]!.complete();
        }
        await Future.wait(jobs.values);
      } finally {
        for (final gate in gates.values) {
          if (!gate.isCompleted) gate.complete();
        }
        await Future.wait(jobs.values);
      }
    },
  );
}
