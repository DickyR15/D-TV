import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:nsd/nsd.dart' as nsd;
import 'package:hgtv/google_cast.dart';

void main() {
  test('Google Cast identifies HLS streams', () {
    expect(
      googleCastMediaKindForUrl('https://media.example.test/episode.m3u8'),
      GoogleCastMediaKind.hls,
    );
    expect(
      googleCastMediaKindForUrl(
        'https://media.example.test/manifest',
        contentType: 'application/vnd.apple.mpegurl',
      ),
      GoogleCastMediaKind.hls,
    );
  });

  test('Google Cast identifies common direct video formats', () {
    expect(
      googleCastMediaKindForUrl('https://media.example.test/movie.mp4'),
      GoogleCastMediaKind.mp4,
    );
    expect(
      googleCastMediaKindForUrl('https://media.example.test/movie.mkv'),
      GoogleCastMediaKind.mkv,
    );
    expect(
      googleCastMediaKindForUrl('https://media.example.test/segment.ts'),
      GoogleCastMediaKind.mpegTs,
    );
  });

  test('unknown extension defaults to MP4 only when no content type is known', () {
    expect(
      googleCastMediaKindForUrl('https://media.example.test/play'),
      GoogleCastMediaKind.mp4,
    );
  });
  test('Windows DNS-SD Chromecast service is converted to Cast payload', () async {
    final payload = await googleCastPayloadFromNsdService(
      nsd.Service(
        name: 'onn-4k-pro',
        type: '_googlecast._tcp',
        host: 'onn-4k-pro.local',
        port: 8009,
        addresses: [InternetAddress('192.168.1.88')],
        txt: {
          'id': Uint8List.fromList(utf8.encode('cast-device-id')),
          'fn': Uint8List.fromList(utf8.encode('onn. 4K Pro')),
          'md': Uint8List.fromList(utf8.encode('Google TV')),
        },
      ),
    );

    expect(payload, isNotNull);
    expect(payload!['id'], 'cast-device-id');
    expect(payload['name'], 'onn. 4K Pro');
    expect(payload['protocol'], 'chromecast');
    expect(payload['address'], '192.168.1.88');
    expect(payload['port'], 8009);
    expect(payload['transport'], 'google_cast');
    expect(
      (payload['metadata'] as Map<String, String>)['md'],
      'Google TV',
    );
  });

  test('Windows DNS-SD ignores services without a resolved IPv4 address', () async {
    final payload = await googleCastPayloadFromNsdService(
      const nsd.Service(
        name: 'unresolved-receiver',
        type: '_googlecast._tcp',
        host: 'unresolved-receiver.local',
        port: 8009,
      ),
    );

    expect(payload, isNull);
  });

  test('Windows DNS-SD falls back to resolved address when TXT id is absent', () async {
    final payload = await googleCastPayloadFromNsdService(
      nsd.Service(
        name: 'Chromecast',
        type: '_googlecast._tcp',
        port: 8009,
        addresses: [InternetAddress('192.168.1.90')],
      ),
    );

    expect(payload?['id'], '192.168.1.90:8009');
    expect(payload?['name'], 'Chromecast');
  });

}
