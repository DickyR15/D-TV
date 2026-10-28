import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/playback_preferences.dart';
import 'package:hgtv/portrait_video_background.dart';

void main() {
  test('portrait blur background defaults off for old settings', () {
    final old = PlaybackPreferences.fromJson(const {
      'speed': 1.0,
      'quality': 0,
      'danmaku': false,
    });
    expect(old.portraitBlurBackground, isFalse);
  });

  test('portrait blur background round-trips and survives other updates', () {
    const enabled = PlaybackPreferences(portraitBlurBackground: true);
    final restored = PlaybackPreferences.fromJson(enabled.toJson());
    expect(restored.portraitBlurBackground, isTrue);
    expect(restored.copyWith(speed: 1.25).portraitBlurBackground, isTrue);
    expect(
      restored.copyWith(portraitBlurBackground: false).portraitBlurBackground,
      isFalse,
    );
  });

  group('portrait video background aspect ratio', () {
    test('only enables for a valid portrait frame', () {
      expect(shouldUsePortraitVideoBackground(9 / 16), isTrue);
      expect(shouldUsePortraitVideoBackground(3 / 4), isTrue);
      expect(shouldUsePortraitVideoBackground(1.0), isFalse);
      expect(shouldUsePortraitVideoBackground(16 / 9), isFalse);
      expect(shouldUsePortraitVideoBackground(0), isFalse);
      expect(shouldUsePortraitVideoBackground(double.nan), isFalse);
      expect(shouldUsePortraitVideoBackground(double.infinity), isFalse);
    });
  });

}
