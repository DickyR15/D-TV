import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/lan_controller.dart';

void main() {
  group('lanPublicErrorMessage', () {
    test('keeps actionable Traditional Chinese state errors', () {
      expect(
        lanPublicErrorMessage(StateError('請先開啟「區域網路自動同步」')),
        '請先開啟「區域網路自動同步」',
      );
    });

    test('hides raw internal state and exception strings', () {
      final stateError = lanPublicErrorMessage(StateError('No element'));
      final exception = lanPublicErrorMessage(Exception('Bad state: no element'));
      expect(stateError, contains('裝置互聯暫時無法使用'));
      expect(exception, contains('裝置互聯暫時無法使用'));
      expect(stateError, isNot(contains('Bad state')));
      expect(exception, isNot(contains('Bad state')));
      expect(exception, isNot(contains('Exception')));
    });

    test('maps timeouts to an actionable Traditional Chinese message', () {
      expect(
        lanPublicErrorMessage(TimeoutException('timeout')),
        '連線逾時，請確認兩臺裝置位於同一個網路後重試。',
      );
    });
  });
}
