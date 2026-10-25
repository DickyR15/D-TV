import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/widgets.dart';

void main() {
  test(
    'poster scheduler prioritizes the newest queued viewport requests',
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

        expect(started, [1, 2, 3, 4, 5, 6, 9, 8, 7]);

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
