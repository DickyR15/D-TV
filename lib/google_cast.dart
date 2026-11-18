import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:dart_cast/dart_cast.dart' as dc;
import 'package:flutter/foundation.dart';
import 'package:nsd/nsd.dart' as nsd;

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

/// Converts a resolved Google Cast endpoint into the payload consumed by
/// dart_cast. Invalid, loopback, multicast and non-IPv4 endpoints are rejected.
Map<String, dynamic>? googleCastPayloadFromAddress({
  required InternetAddress address,
  required int port,
  String? advertisedName,
  String? modelName,
  String? serviceName,
  String? advertisedId,
  Map<String, String> metadata = const {},
}) {
  if (port <= 0 ||
      port > 65535 ||
      address.type != InternetAddressType.IPv4 ||
      address.isLoopback ||
      address.isMulticast) {
    return null;
  }

  final name = advertisedName?.trim().isNotEmpty == true
      ? advertisedName!.trim()
      : modelName?.trim().isNotEmpty == true
      ? modelName!.trim()
      : serviceName?.trim().isNotEmpty == true
      ? serviceName!.trim()
      : address.address;
  final id = advertisedId?.trim().isNotEmpty == true
      ? advertisedId!.trim()
      : '${address.address}:$port';
  final device = dc.CastDevice(
    id: id,
    name: name,
    protocol: dc.CastProtocol.chromecast,
    address: address,
    port: port,
    metadata: metadata,
  );
  return <String, dynamic>{
    ...device.toJson(),
    'transport': 'google_cast',
  };
}

/// Converts a Windows DNS-SD Google Cast service into a dart_cast payload.
///
/// Some receivers expose their address in the DNS-SD host record but the
/// NSD plugin does not populate [nsd.Service.addresses]. Resolve that host
/// before dropping the receiver; this is important for Google TV devices
/// whose service record is present but whose A record arrives separately.
Future<Map<String, dynamic>?> googleCastPayloadFromNsdService(
  nsd.Service service,
) async {
  var currentService = service;
  var port = currentService.port;
  InternetAddress? address;

  bool acceptable(InternetAddress candidate) =>
      candidate.type == InternetAddressType.IPv4 &&
      !candidate.isLoopback &&
      !candidate.isMulticast;

  for (final candidate
      in currentService.addresses ?? const <InternetAddress>[]) {
    if (acceptable(candidate)) {
      address = candidate;
      break;
    }
  }

  // Auto-resolve can still leave the port or address list incomplete on
  // Windows when the PTR/SRV record arrives before its A record. Ask the
  // platform resolver for a fresh DNS-SD result before rejecting the device.
  if (port == null || port <= 0 || port > 65535 || address == null) {
    try {
      currentService = await nsd
          .resolve(currentService)
          .timeout(const Duration(seconds: 3));
      port = currentService.port ?? port;
      for (final candidate
          in currentService.addresses ?? const <InternetAddress>[]) {
        if (acceptable(candidate)) {
          address = candidate;
          break;
        }
      }
    } catch (error) {
      debugPrint('[GoogleCast] DNS-SD resolve failed error=$error');
    }
  }

  if (port == null || port <= 0 || port > 65535) return null;

  final host = currentService.host ?? service.host;
  if (address == null && host != null) {
    var normalizedHost = host.trim();
    if (normalizedHost.endsWith('.')) {
      normalizedHost = normalizedHost.substring(0, normalizedHost.length - 1);
    }
    address = InternetAddress.tryParse(normalizedHost);
    if (address == null && normalizedHost.isNotEmpty) {
      try {
        final resolved = await InternetAddress.lookup(
          normalizedHost,
          type: InternetAddressType.IPv4,
        ).timeout(const Duration(seconds: 2));
        for (final candidate in resolved) {
          if (acceptable(candidate)) {
            address = candidate;
            break;
          }
        }
      } catch (error) {
        debugPrint(
          '[GoogleCast] host resolve failed host=$normalizedHost error=$error',
        );
      }
    }
  }
  if (address == null) return null;

  final metadata = <String, String>{};
  for (final entry in currentService.txt?.entries ?? const []) {
    final bytes = entry.value;
    if (bytes == null) continue;
    final value = utf8.decode(bytes, allowMalformed: true).trim();
    if (value.isNotEmpty) metadata[entry.key] = value;
  }

  return googleCastPayloadFromAddress(
    address: address,
    port: port,
    advertisedName: metadata['fn'],
    modelName: metadata['md'],
    serviceName: currentService.name,
    advertisedId: metadata['id'],
    metadata: metadata,
  );
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
    if (!Platform.isWindows) return _discoverWithDartCast();

    // Run the Windows DNS-SD API and the original mDNS provider together.
    // Different receivers / interfaces can be visible to only one discovery
    // path; merging prevents a partial native result from hiding those devices.
    final results = await Future.wait([
      _discoverWindowsDnsSd().catchError((Object error) {
        debugPrint('[GoogleCast] Windows DNS-SD discovery failed: $error');
        return <Map<String, dynamic>>[];
      }),
      _discoverWithDartCast().catchError((Object error) {
        debugPrint('[GoogleCast] dart_cast mDNS discovery failed: $error');
        return <Map<String, dynamic>>[];
      }),
    ]);

    final devices = <String, Map<String, dynamic>>{};
    for (final payload in results.expand((items) => items)) {
      final address = payload['address']?.toString() ?? '';
      final port = payload['port']?.toString() ?? '';
      final key = address.isNotEmpty && port.isNotEmpty
          ? '$address:$port'
          : payload['id']?.toString() ?? '';
      if (key.isNotEmpty) devices.putIfAbsent(key, () => payload);
    }
    final merged = devices.values.toList(growable: false)
      ..sort((a, b) => (a['name'] as String)
          .toLowerCase()
          .compareTo((b['name'] as String).toLowerCase()));
    return merged;
  }

  Future<List<Map<String, dynamic>>> _discoverWindowsDnsSd() async {
    nsd.Discovery? discovery;
    try {
      discovery = await nsd.startDiscovery(
        '_googlecast._tcp',
        autoResolve: true,
        ipLookupType: nsd.IpLookupType.v4,
      );
      // Windows NSD reports discoveries asynchronously. Keep the browse alive
      // for the same window as the existing Chromecast search before reading
      // the fully resolved host, port and TXT data.
      await Future<void>.delayed(const Duration(seconds: 10));
      final services = discovery.services.take(64).toList(growable: false);
      final resolved = await Future.wait(
        services.map(googleCastPayloadFromNsdService),
      );
      final devices = <String, Map<String, dynamic>>{};
      for (final payload in resolved) {
        if (payload == null) continue;
        final address = payload['address']?.toString() ?? '';
        final port = payload['port']?.toString() ?? '';
        final key = address.isNotEmpty && port.isNotEmpty
            ? '$address:$port'
            : payload['id']?.toString() ?? '';
        if (key.isNotEmpty) devices.putIfAbsent(key, () => payload);
      }
      final result = devices.values.toList(growable: false)
        ..sort((a, b) => (a['name'] as String)
            .toLowerCase()
            .compareTo((b['name'] as String).toLowerCase()));
      return result;
    } finally {
      if (discovery != null) {
        await nsd.stopDiscovery(discovery);
      }
    }
  }

  Future<List<Map<String, dynamic>>> _discoverWithDartCast() async {
    final discovered = <String, dc.CastDevice>{};
    final discovery = _service
        .startDiscovery(timeout: const Duration(seconds: 12))
        .timeout(
          const Duration(seconds: 14),
          onTimeout: (sink) => sink.close(),
        );
    await for (final devices in discovery) {
      // Merge each partial snapshot so a later network-interface update cannot
      // accidentally hide a receiver that was already discovered.
      for (final device in devices) {
        if (device.protocol != dc.CastProtocol.chromecast) continue;
        final key = '${device.address.address}:${device.port}';
        discovered.putIfAbsent(key, () => device);
      }
    }
    return discovered.values.map((device) {
      final payload = Map<String, dynamic>.from(device.toJson());
      payload['transport'] = 'google_cast';
      return payload;
    }).toList(growable: false);
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
), '');
    address = InternetAddress.tryParse(host);
    if (address == null && host.isNotEmpty) {
      try {
        final resolved = await InternetAddress.lookup(
          host,
          type: InternetAddressType.IPv4,
        ).timeout(const Duration(seconds: 2));
        for (final candidate in resolved) {
          if (candidate.type == InternetAddressType.IPv4 &&
              !candidate.isLoopback &&
              !candidate.isMulticast) {
            address = candidate;
            break;
          }
        }
      } catch (error) {
        debugPrint('[GoogleCast] host resolve failed host=$host error=$error');
      }
    }
  }
  if (address == null) return null;

  final metadata = <String, String>{};
  for (final entry in service.txt?.entries ?? const []) {
    final bytes = entry.value;
    if (bytes == null) continue;
    final value = utf8.decode(bytes, allowMalformed: true).trim();
    if (value.isNotEmpty) metadata[entry.key] = value;
  }

  return googleCastPayloadFromAddress(
    address: address,
    port: port,
    advertisedName: metadata['fn'],
    serviceName: service.name,
    advertisedId: metadata['id'],
    metadata: metadata,
  );
}
