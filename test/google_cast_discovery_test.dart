import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/google_cast.dart';

void main() {
  group('Google Cast discovery address mapping', () {
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
