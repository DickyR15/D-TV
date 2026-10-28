import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/playback_preferences.dart';

void main() {
  test('1.0.0 ignores the deferred legacy portrait blur preference', () {
    final preferences = PlaybackPreferences.fromJson(const {
      'speed': 1.25,
      'quality': 720,
      'danmaku': false,
      'portraitBlurBackground': true,
    });

    expect(preferences.speed, 1.25);
    expect(preferences.quality, 720);
    expect(preferences.danmaku, isFalse);
    expect(preferences.toJson().containsKey('portraitBlurBackground'), isFalse);
    expect(
      PlaybackPreferences.fromJson(preferences.toJson()).speed,
      1.25,
    );
  });
}
