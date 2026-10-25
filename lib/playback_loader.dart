import 'dart:async';

import 'core_bridge.dart';
import 'models.dart';

class PlaybackLoader {
  PlaybackLoader(
    this.repository, {
    this.resolveTimeout = const Duration(seconds: 20),
  });

  final AppRepository repository;

  /// Bounds both cancellation of the previous resolver and resolution of the
  /// next plan, so reload cannot leave the UI loading forever.
  final Duration resolveTimeout;
  int _generation = 0;
  bool _closed = false;
  bool _hasIssuedRequest = false;

  Future<PlaybackPlan?> load(
    Drama drama,
    Episode episode, {
    int quality = 0,
    bool localOnly = false,
    bool online = false,
  }) => _load(() async {
    if (online) {
      return repository.resolveOnline(drama, episode, quality: quality);
    }
    if (localOnly) {
      final plan = await repository.localPlayback(drama, episode);
      if (plan == null || !plan.local) {
        throw AppFailure('本地影片不可用，請重新下載或選擇線上播放。', code: 'local_media');
      }
      return plan;
    }
    return repository.resolve(drama, episode, quality: quality);
  });

  Future<PlaybackPlan?> fallback(PlaybackPlan current) =>
      _load(() => repository.fallback(current));

  Future<PlaybackPlan?> use(PlaybackPlan prepared) =>
      _load(() async => prepared);

  Future<PlaybackPlan?> _load(Future<PlaybackPlan> Function() resolve) async {
    if (_closed) {
      return null;
    }
    final generation = ++_generation;
    final cancelPrevious = _hasIssuedRequest;
    _hasIssuedRequest = true;
    var timedOut = false;
    var planHandled = false;

    // Include cancellation in the timeout window. A stalled native cancel
    // must not block all later reloads before the new resolver even starts.
    final pending = Future<PlaybackPlan?>(() async {
      // A brand-new player has no earlier resolver to cancel. Skip this
      // native round trip on cold start; later requests still cancel stale work.
      if (cancelPrevious) await repository.cancelPlayback();
      if (_closed || generation != _generation) {
        return null;
      }
      return resolve();
    }());

    // Future.timeout does not cancel the original Future. If a timed-out or
    // superseded resolver later creates a native stream session, release it.
    pending.then<void>(
      (plan) {
        if (plan != null &&
            (timedOut || _closed || generation != _generation) &&
            !planHandled) {
          planHandled = true;
          unawaited(repository.release(plan.session).catchError((Object _) {}));
        }
      },
      onError: (Object _, StackTrace __) {},
    );

    try {
      final plan = await pending.timeout(
        resolveTimeout,
        onTimeout: () {
          timedOut = true;
          if (!_closed && generation == _generation) {
            // Invalidate the hung continuation before requesting cancellation;
            // if cancellation eventually returns, it must not start a stale resolve.
            _generation++;
            unawaited(repository.cancelPlayback().catchError((Object _) {}));
          }
          throw TimeoutException(
            '取得播放網址或取消上一個請求超過 '
            '${resolveTimeout.inSeconds} 秒，已停止本次載入。',
            resolveTimeout,
          );
        },
      );
      if (plan == null) {
        return null;
      }
      if (_closed || generation != _generation) {
        if (!planHandled) {
          planHandled = true;
          await repository.release(plan.session);
        }
        return null;
      }
      planHandled = true;
      return plan;
    } catch (_) {
      if (timedOut) {
        rethrow;
      }
      if (_closed || generation != _generation) {
        return null;
      }
      rethrow;
    }
  }

  Future<void> close() async {
    _closed = true;
    _generation++;
    await repository.cancelPlayback();
  }
}
