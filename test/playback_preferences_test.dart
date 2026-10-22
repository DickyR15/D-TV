import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/playback_preferences.dart';

void main() {
  test('danmaku is disabled in new playback preferences', () {
    expect(const PlaybackPreferences().danmaku, isFalse);
    expect(
      PlaybackPreferences.fromJson(const <String, dynamic>{}).danmaku,
      isFalse,
    );
  });

  test('explicit danmaku choice is preserved', () {
    expect(
      PlaybackPreferences.fromJson(const <String, dynamic>{'danmaku': true})
          .danmaku,
      isTrue,
    );
    expect(
      PlaybackPreferences.fromJson(const <String, dynamic>{'danmaku': false})
          .danmaku,
      isFalse,
    );
  });
}
