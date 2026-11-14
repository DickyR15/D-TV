import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/core_bridge.dart';

void main() {
  test('native series lookup supports Hongguo and Duoduo only', () {
    expect(supportsNativeSeriesSeasons('hongguo'), isTrue);
    expect(supportsNativeSeriesSeasons('duoduo'), isTrue);
    expect(supportsNativeSeriesSeasons('heitao'), isFalse);
    expect(supportsNativeSeriesSeasons('huangdou'), isFalse);
    expect(supportsNativeSeriesSeasons(''), isFalse);
  });
}
