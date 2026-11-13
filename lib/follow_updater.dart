import 'dart:async';
import 'dart:math';

import 'core_bridge.dart';
import 'follow_state.dart';
import 'local_store.dart';
import 'models.dart';

class FollowUpdater {
  FollowUpdater(
    this.repository,
    this.store, {
    this.startDelay = const Duration(seconds: 30),
    this.interval = const Duration(minutes: 30),
    this.recheck = const Duration(hours: 6),
    this.limit = 10,
    DateTime Function()? clock,
  }) : _epoch = store.profileEpoch,
       _clock = clock ?? DateTime.now;

  static const _maxBackoffShift = 3;

  final AppRepository repository;
  final LocalStore store;
  final Duration startDelay;
  final Duration interval;
  final Duration recheck;
  final int limit;
  final DateTime Function() _clock;
  final int _epoch;
  Timer? _timer;
  bool _running = false;
  bool _disposed = false;

  bool get _valid =>
      !_disposed && !store.locked && _epoch == store.profileEpoch;

  void start() {
    if (_timer != null || !repository.supportsSourceManagement || !_valid) {
      return;
    }
    _timer = Timer(startDelay, () {
      _timer = Timer.periodic(interval, (_) => _tick());
      _tick();
    });
  }

  void _tick() => unawaited(run().catchError((Object _) {}));

  List<Drama> _due(Map<String, FollowCheck> checks) {
    final now = _clock();
    final never = DateTime.fromMillisecondsSinceEpoch(0);
    final items = store.favorites.where((drama) {
      final state = store.following(drama.id);
      if (state == null ||
          state.manuallyWatched ||
          state.status == FollowStatus.planned ||
          drama.seriesGroupId.isEmpty &&
              const {'finished', 'completed'}.contains(drama.releaseStatus)) {
        return false;
      }
      final check = checks[drama.id];
      return check == null ||
          !now.isBefore(
            check.checkedAt.add(
              recheck * (1 << min(check.failures, _maxBackoffShift)),
            ),
          );
    }).toList();
    items.sort(
      (a, b) => (checks[a.id]?.checkedAt ?? never).compareTo(
        checks[b.id]?.checkedAt ?? never,
      ),
    );
    return items.take(limit).toList();
  }

  Future<void> run() async {
    if (_running || !_valid) return;
    _running = true;
    try {
      final checks = store.followChecks;
      final updates = <Drama>[];
      for (final drama in _due(checks)) {
        if (!_valid) return;
        try {
          final updated = await repository.followCheck(drama);
          updates.add(updated);
          if (updated.source == SourceSite.hongguo.id && _valid) {
            // Episode-count checks alone cannot discover a brand-new season.
            // Reconcile the official season list as part of the same follow pass.
            try {
              final seasons = await repository.seasons(updated);
              if (_valid && seasons.isNotEmpty) {
                await store.refreshDramas(
                  seasons.map((season) => season.drama),
                );
              }
            } catch (_) {
              // Keep the successful episode check; the season API can retry
              // during the next follow cycle without blocking other dramas.
            }
          }
          checks[drama.id] = FollowCheck(_clock());
        } catch (_) {
          checks[drama.id] = FollowCheck(
            _clock(),
            failures: (checks[drama.id]?.failures ?? 0) + 1,
          );
        }
      }
      if (!_valid) return;
      if (updates.isNotEmpty) await store.refreshDramas(updates);
      await store.saveFollowChecks(checks, epoch: _epoch);
    } finally {
      _running = false;
    }
  }

  void dispose() {
    _disposed = true;
    _timer?.cancel();
  }
}
