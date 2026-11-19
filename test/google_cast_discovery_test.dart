import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/google_cast.dart';

void main() {
  group('Google Cast discovery address mapping', () {
    test('merges receiver aliases by TXT id across network addresses', () {
      final merged = mergeGoogleCastPayloads([
        {
          'id': 'receiver-123',
          'name': 'onn 4K Pro',
          'address': '192.168.50.20',
          'port': 8009,
        },
        {
          'id': 'receiver-123',
          'name': 'onn 4K Pro',
          'address': '192.168.50.21',
          'port': 8009,
        },
        {
          'id': 'receiver-456',
          'name': 'Living Room',
          'address': '192.168.50.22',
          'port': 8009,
        },
      ]);
      expect(merged, hasLength(2));
      expect(merged.map((item) => item['id']), contains('receiver-123'));
    });

    test('merges the same receiver endpoint even when TXT IDs differ', () {
      final merged = mergeGoogleCastPayloads([
        {
          'id': 'receiver-first-interface',
          'name': 'onn 4K Pro',
          'address': '192.168.50.20',
          'port': 8009,
        },
        {
          'id': 'receiver-second-interface',
          'name': 'Chromecast-onn',
          'address': '192.168.50.20',
          'port': 8009,
        },
      ]);

      expect(merged, hasLength(1));
      expect(googleCastDiscoveryIdentities(merged.single), contains('id:receiver-first-interface'));
      expect(
        googleCastDiscoveryIdentities(merged.single),
        contains('endpoint:192.168.50.20:8009'),
      );
    });

    test('uses endpoint identity when TXT id is absent', () {
      expect(
        googleCastDiscoveryIdentity({
          'name': 'Receiver',
          'address': '192.168.50.10',
          'port': 8009,
        }),
        'endpoint:192.168.50.10:8009',
      );
    });

    test('creates a Google Cast device from a valid IPv4 endpoint', () {
      final payload = googleCastPayloadFromAddress(
        address: InternetAddress('192.168.50.190'),
        port: 8009,
        advertisedName: '  onn 4K Pro  ',
        advertisedId: 'onn-cast-device',
        metadata: const {'fn': 'onn 4K Pro', 'id': 'onn-cast-device'},
      );

      expect(payload, isNotNull);
      expect(payload!['transport'], 'google_cast');
      expect(payload['name'], 'onn 4K Pro');
      expect(payload['id'], 'onn-cast-device');
      expect(payload['port'], 8009);
    });

    test('falls back to service name and endpoint identity', () {
      final payload = googleCastPayloadFromAddress(
        address: InternetAddress('192.168.50.191'),
        port: 8009,
        serviceName: 'Living Room TV',
      );

      expect(payload, isNotNull);
      expect(payload!['name'], 'Living Room TV');
      expect(payload['id'], '192.168.50.191:8009');
    });

    test('uses receiver model metadata when friendly name is missing', () {
      final payload = googleCastPayloadFromAddress(
        address: InternetAddress('192.168.50.192'),
        port: 8009,
        modelName: 'onn 4K Pro',
        serviceName: 'Chromecast-7f31',
      );

      expect(payload, isNotNull);
      expect(payload!['name'], 'onn 4K Pro');
    });

    test('rejects loopback, multicast and IPv6 endpoints', () {
      expect(
        googleCastPayloadFromAddress(
          address: InternetAddress.loopbackIPv4,
          port: 8009,
        ),
        isNull,
      );
      expect(
        googleCastPayloadFromAddress(
          address: InternetAddress('224.0.0.251'),
          port: 8009,
        ),
        isNull,
      );
      expect(
        googleCastPayloadFromAddress(
          address: InternetAddress('fe80::1'),
          port: 8009,
        ),
        isNull,
      );
    });

    test('rejects invalid receiver ports', () {
      for (final port in [0, -1, 65536]) {
        expect(
          googleCastPayloadFromAddress(
            address: InternetAddress('192.168.50.190'),
            port: port,
          ),
          isNull,
        );
      }
    });
  });
}
