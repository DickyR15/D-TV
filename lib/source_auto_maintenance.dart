import 'dart:async';

import 'core_bridge.dart';
import 'diary_service.dart';
import 'local_store.dart';

const sourceHealthCheckInterval = Duration(hours: 24);

bool sourceHealthIsStale(DateTime? checkedAt, DateTime now) {
  if (checkedAt == null) return true;
  final age = now.difference(checkedAt);
  return age.isNegative || age >= sourceHealthCheckInterval;
}

abstract final class SourceAutoMaintenance {
  static bool _running = false;
  static int _activePlayers = 0;
  static AppRepository? _lastRepository;
  static LocalStore? _lastStore;
  static Timer? _afterPlaybackTimer;
  static final Map<String, DateTime> _lastAttempt = {};

  static void playbackStarted() {
    _activePlayers++;
    _afterPlaybackTimer?.cancel();
    _afterPlaybackTimer = null;
  }

  static void playbackStopped() {
    if (_activePlayers > 0) _activePlayers--;
    if (_activePlayers > 0) return;
    final repository = _lastRepository;
    final store = _lastStore;
    if (repository == null || store == null) return;
    _afterPlaybackTimer?.cancel();
    _afterPlaybackTimer = Timer(const Duration(seconds: 15), () {
      _afterPlaybackTimer = null;
      unawaited(checkSelectedSourceIfStale(repository, store));
    });
  }

  static void shutdown() {
    _afterPlaybackTimer?.cancel();
    _afterPlaybackTimer = null;
    _lastRepository = null;
    _lastStore = null;
    _activePlayers = 0;
  }

  /// Checks only the currently selected source, at most once per 24 hours.
  /// The native "check" operation refreshes the first catalog page and probes
  /// one free playback sample; it deliberately does not crawl the whole archive.
  static Future<void> checkSelectedSourceIfStale(
    AppRepository repository,
    LocalStore store,
  ) async {
    _lastRepository = repository;
    _lastStore = store;
    if (_running ||
        _activePlayers > 0 ||
        store.locked ||
        !repository.supportsSourceManagement) {
      return;
    }
    final source = store.source;
    if (source.isEmpty || !store.allowsSource(source)) return;

    _running = true;
    try {
      final status = await repository.sourceStatus(source);
      if (store.locked ||
          !store.allowsSource(source) ||
          status.running ||
          status.retrySeconds > 0) {
        return;
      }
      final now = DateTime.now();
      if (!sourceHealthIsStale(status.health?.checkedAt, now)) return;
      final previous = _lastAttempt[source];
      if (previous != null &&
          now.difference(previous) < const Duration(minutes: 30)) {
        return;
      }
      _lastAttempt[source] = now;
      await repository.startAutomaticSourceCheck(source);
      DiaryService.add(
        '[SourceAuto] started daily connection/playback check for $source',
      );
    } catch (error) {
      DiaryService.add(
        '[SourceAuto] could not start health check (${error.runtimeType})',
      );
    } finally {
      _running = false;
    }
  }
}
