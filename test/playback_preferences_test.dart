import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/playback_preferences.dart';

void main() {
  test('legacy portrait blur preference is ignored while feature is deferred', () {
    final legacy = PlaybackPreferences.fromJson(const {
      'speed': 1.25,
      'quality': 720,
      'danmaku': false,
      'portraitBlurBackground': true,
    });

    expect(legacy.speed, 1.25);
    expect(legacy.quality, 720);
    expect(legacy.danmaku, isFalse);
    expect(legacy.toJson().containsKey('portraitBlurBackground'), isFalse);
    expect(
      legacy.copyWith(speed: 1.5).toJson().containsKey('portraitBlurBackground'),
      isFalse,
    );
  });
}
