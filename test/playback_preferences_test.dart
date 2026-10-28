import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/playback_preferences.dart';

void main() {
  test('portrait blur is off by default and persists through JSON and copyWith', () {
    final legacy = PlaybackPreferences.fromJson(const {
      'speed': 1.25,
      'quality': 720,
      'danmaku': false,
    });

    expect(legacy.speed, 1.25);
    expect(legacy.quality, 720);
    expect(legacy.danmaku, isFalse);
    expect(legacy.portraitBlurBackground, isFalse);

    final enabled = PlaybackPreferences.fromJson({
      ...legacy.toJson(),
      'portraitBlurBackground': true,
    });
    expect(enabled.portraitBlurBackground, isTrue);
    expect(enabled.copyWith(speed: 1.5).portraitBlurBackground, isTrue);
    expect(
      PlaybackPreferences.fromJson(enabled.toJson()).portraitBlurBackground,
      isTrue,
    );
  });
}
