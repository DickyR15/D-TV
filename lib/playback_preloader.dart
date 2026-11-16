import 'dart:async';

import 'package:flutter/foundation.dart';

import 'core_bridge.dart';
import 'diary_service.dart';
import 'models.dart';

class PlaybackPreloader extends ChangeNotifier {
  PlaybackPreloader(this.repository, {DateTime Function()? now})
    : _now = now ?? DateTime.now;

  final AppRepository repository;
  final DateTime Function() _now;
  PlaybackPlan? _ready;
  String? _identity;
  Timer? _delay;
  DateTime _expires = DateTime(2000), _retryAt = DateTime(2000);
  int _readyResolveMs = 0;
  int _lastTakeResolveMs = 0;
  int get lastTakeResolveMs => _lastTakeResolveMs;
  int _generation = 0;
  bool _closed = false, loading = false;
  String error = '';
  void _release(PlaybackPlan plan) =>
      unawaited(repository.release(plan.session).catchError((Object _) {}));

  String get status {
    if (_ready != null && _now().isBefore(_expires)) {
      final bytes = _ready!.prefetchedBytes;
      return _ready!.local
          ? '下一集已下載'
          : bytes > 0
          ? '下一集已準備 · 預取 ${(bytes / 1024).ceil()} KB'
          : '下一集播放網址已準備';
    }
    if (loading || _delay != null) return '正在準備下一集';
    return error.isNotEmpty ? '下一集預載入暫不可用，切換集數時會重新取得' : '播放後段會提前準備下一集';
  }

  String _key(Drama drama, Episode episode, int quality, bool online) =>
      '${drama.id}\u0000${episode.number}\u0000$quality\u0000$online';

  void prepare(
    Drama drama,
    Episode episode, {
    int quality = 0,
    bool online = false,
  }) {
    if (_closed) return;
    final identity = _key(drama, episode, quality, online);
    if (_identity != identity) {
      clear();
      _identity = identity;
    }
    if (_ready != null && !_now().isBefore(_expires)) {
      final expired = _ready!;
      _ready = null;
      _release(expired);
    }
    if (_ready != null ||
        loading ||
        _delay != null ||
        _now().isBefore(_retryAt)) {
      return;
    }
    final ticket = _generation;
    DiaryService.add(
      '[NextEpisodePreload] scheduled source=${drama.source} dramaId=${drama.id} episode=${episode.number} quality=$quality online=$online delayMs=600',
    );
    _delay = Timer(const Duration(milliseconds: 600), () async {
      _delay = null;
      loading = true;
      notifyListeners();
      final preloadClock = Stopwatch()..start();
      DiaryService.add(
        '[NextEpisodePreload] start source=${drama.source} dramaId=${drama.id} episode=${episode.number} quality=$quality online=$online',
      );
      try {
        final plan = await repository.preload(
          drama,
          episode,
          quality: quality,
          online: online,
        );
        if (_closed || ticket != _generation) {
          DiaryService.add(
            '[NextEpisodePreload] cancelled source=${drama.source} dramaId=${drama.id} episode=${episode.number} elapsedMs=${preloadClock.elapsedMilliseconds}',
          );
          if (plan != null) await repository.release(plan.session);
          return;
        }
        if (plan == null || plan.url.isEmpty) throw AppFailure('未取得預載入結果');
        var expires = _now().add(const Duration(minutes: 2));
        if (plan.expiresAt > 0) {
          final authorizationExpiry = DateTime.fromMillisecondsSinceEpoch(
            plan.expiresAt,
          ).subtract(const Duration(seconds: 5));
          if (authorizationExpiry.isBefore(expires)) {
            expires = authorizationExpiry;
          }
        }
        if (!expires.isAfter(_now())) {
          _release(plan);
          throw AppFailure('下一集播放憑證即將過期');
        }
        _ready = plan;
        _readyResolveMs = preloadClock.elapsedMilliseconds;
        _expires = expires;
        error = '';
        DiaryService.add(
          '[NextEpisodePreload] complete source=${drama.source} dramaId=${drama.id} episode=${episode.number} elapsedMs=$_readyResolveMs bytes=${plan.prefetchedBytes} local=${plan.local} expiresInMs=${expires.difference(_now()).inMilliseconds}',
        );
      } catch (failure) {
        if (_closed || ticket != _generation) return;
        error = failure.toString();
        _retryAt = _now().add(const Duration(seconds: 30));
        DiaryService.add(
          '[NextEpisodePreload] failed source=${drama.source} dramaId=${drama.id} episode=${episode.number} elapsedMs=${preloadClock.elapsedMilliseconds} errorType=${failure.runtimeType} retryAfterSeconds=30',
        );
      } finally {
        if (!_closed && ticket == _generation) {
          loading = false;
          notifyListeners();
        }
      }
    });
    notifyListeners();
  }

  PlaybackPlan? take(
    Drama drama,
    Episode episode, {
    int quality = 0,
    bool online = false,
  }) {
    final identityMatches =
        _identity == _key(drama, episode, quality, online);
    final notExpired = _now().isBefore(_expires);
    final result = identityMatches && notExpired ? _ready : null;
    final reason = result != null
        ? 'ready'
        : !identityMatches
        ? 'identity_mismatch'
        : !notExpired
        ? 'expired'
        : loading || _delay != null
        ? 'still_loading'
        : error.isNotEmpty
        ? 'preload_failed'
        : 'not_ready';
    final resolvedMs = result == null ? 0 : _readyResolveMs;
    _lastTakeResolveMs = resolvedMs;
    DiaryService.add(
      '[NextEpisodePreload] take hit=${result != null} source=${drama.source} dramaId=${drama.id} episode=${episode.number} quality=$quality reason=$reason preloadResolveMs=$resolvedMs',
    );
    if (result != null) _ready = null;
    clear();
    return result;
  }

  void pause() {
    final changed = loading || _delay != null;
    _generation++;
    _delay?.cancel();
    _delay = null;
    if (loading) {
      unawaited(repository.cancelPreload().catchError((Object _) {}));
    }
    loading = false;
    if (changed && !_closed) notifyListeners();
  }

  void clear() {
    pause();
    final old = _ready;
    _ready = null;
    _readyResolveMs = 0;
    _identity = null;
    _retryAt = DateTime(2000);
    error = '';
    if (old != null) _release(old);
    if (!_closed) notifyListeners();
  }

  @override
  void dispose() {
    _closed = true;
    clear();
    super.dispose();
  }
}
