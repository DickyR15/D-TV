import 'dart:async';

import 'package:dart_cast/dart_cast.dart' as dc;
import 'package:flutter/foundation.dart';

import 'models.dart';

enum GoogleCastMediaKind { hls, mp4, mkv, mpegTs }

GoogleCastMediaKind googleCastMediaKindForUrl(
  String url, {
  String contentType = '',
}) {
  final path = (Uri.tryParse(url)?.path ?? url).toLowerCase();
  final type = contentType.toLowerCase();
  if (path.endsWith('.m3u8') ||
      type.contains('mpegurl') ||
      type.contains('m3u')) {
    return GoogleCastMediaKind.hls;
  }
  if (path.endsWith('.mkv') || type.contains('matroska')) {
    return GoogleCastMediaKind.mkv;
  }
  if (path.endsWith('.ts') || type.contains('mp2t')) {
    return GoogleCastMediaKind.mpegTs;
  }
  return GoogleCastMediaKind.mp4;
}

/// Chromecast V2 sender. Only Google Cast is registered here; AirPlay is
/// deliberately excluded because iOS projection was removed from D-TV.
class GoogleCastController {
  GoogleCastController._();

  static final GoogleCastController instance = GoogleCastController._();

  late final dc.CastService _service = dc.CastService(
    discoveryProviders: [dc.ChromecastDiscoveryProvider()],
    sessionFactory: (device) => dc.ChromecastSession(device: device),
  );

  dynamic _session;
  StreamSubscription<dynamic>? _positionSubscription;
  StreamSubscription<dynamic>? _durationSubscription;

  final ValueNotifier<Duration> position = ValueNotifier(Duration.zero);
  final ValueNotifier<Duration> duration = ValueNotifier(Duration.zero);
  final ValueNotifier<bool> isPlaying = ValueNotifier(false);

  bool get isConnected => _session != null;

  Future<List<Map<String, dynamic>>> discover() async {
    final devices = await _service
        .startDiscovery()
        .first
        .timeout(const Duration(seconds: 12));
    return devices
        .where((device) => device.protocol == dc.CastProtocol.chromecast)
        .map((device) {
          final payload = Map<String, dynamic>.from(device.toJson());
          payload['transport'] = 'google_cast';
          return payload;
        })
        .toList(growable: false);
  }

  Future<void> play({
    required Map<String, dynamic> deviceJson,
    required PlaybackPlan plan,
    required String title,
    int startMs = 0,
  }) async {
    if (plan.url.isEmpty) throw StateError('沒有可供 Google Cast 播放的網址');
    final uri = Uri.tryParse(plan.url);
    if (uri == null || (uri.scheme != 'http' && uri.scheme != 'https')) {
      throw StateError('Google Cast 需要可由電視存取的 HTTP／HTTPS 影片網址');
    }
    if (plan.local || plan.decryptionKey.isNotEmpty) {
      throw StateError('本機檔案或本機解密片源目前無法直接 Google Cast');
    }

    await _disconnectCurrent();
    final json = Map<String, dynamic>.from(deviceJson)
      ..remove('transport');
    final device = dc.CastDevice.fromJson(json);
    final session = await _service
        .connect(device)
        .timeout(const Duration(seconds: 12));
    _session = session;
    _positionSubscription = session.positionStream.listen(_updatePosition);
    _durationSubscription = session.durationStream.listen(_updateDuration);

    var contentType = '';
    for (final entry in plan.headers.entries) {
      if (entry.key.toLowerCase() == 'content-type') {
        contentType = entry.value;
        break;
      }
    }
    final mediaType = switch (googleCastMediaKindForUrl(
      plan.url,
      contentType: contentType,
    )) {
      GoogleCastMediaKind.hls => dc.CastMediaType.hls,
      GoogleCastMediaKind.mp4 => dc.CastMediaType.mp4,
      GoogleCastMediaKind.mkv => dc.CastMediaType.mkv,
      GoogleCastMediaKind.mpegTs => dc.CastMediaType.mpegTs,
    };

    try {
      await session.loadMedia(
        dc.CastMedia(
          url: plan.url,
          type: mediaType,
          httpHeaders: plan.headers,
          title: title,
          startPosition: Duration(
            milliseconds: startMs.clamp(0, 1 << 52).toInt(),
          ),
        ),
      );
      position.value = Duration(
        milliseconds: startMs.clamp(0, 1 << 52).toInt(),
      );
      isPlaying.value = true;
    } catch (_) {
      await _disconnectCurrent();
      rethrow;
    }
  }

  void _updatePosition(dynamic value) {
    if (value is Duration) {
      position.value = value.isNegative ? Duration.zero : value;
    } else if (value is num) {
      position.value = Duration(
        milliseconds: value.round().clamp(0, 1 << 52).toInt(),
      );
    }
  }

  void _updateDuration(dynamic value) {
    if (value is Duration) {
      duration.value = value.isNegative ? Duration.zero : value;
    } else if (value is num) {
      duration.value = Duration(
        milliseconds: value.round().clamp(0, 1 << 52).toInt(),
      );
    }
  }

  Future<void> pause() async {
    if (_session == null) return;
    await _session.pause();
    isPlaying.value = false;
  }

  Future<void> resume() async {
    if (_session == null) return;
    await _session.play();
    isPlaying.value = true;
  }

  Future<void> seek(Duration value) async {
    if (_session == null) return;
    final safe = value.isNegative ? Duration.zero : value;
    await _session.seek(safe);
    position.value = safe;
  }

  Future<void> stop() async {
    if (_session == null) return;
    try {
      await _session.stop();
    } finally {
      await _disconnectCurrent();
    }
  }

  Future<void> _disconnectCurrent() async {
    await _positionSubscription?.cancel();
    await _durationSubscription?.cancel();
    _positionSubscription = null;
    _durationSubscription = null;
    final current = _session;
    _session = null;
    if (current != null) {
      try {
        await current.disconnect();
      } catch (_) {
        // The receiver may already have closed the connection.
      }
    }
    position.value = Duration.zero;
    duration.value = Duration.zero;
    isPlaying.value = false;
  }
}
