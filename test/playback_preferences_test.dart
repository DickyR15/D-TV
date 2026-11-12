import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/playback_preferences.dart';

void main() {
  test('legacy danmaku preference is ignored and omitted from saved settings', () {
    final restored = PlaybackPreferences.fromJson(const {'danmaku': true});
    expect(restored.toJson().containsKey('danmaku'), isFalse);
  });

  test('next episode preload defaults on for phones and TVs', () {
    final defaults = PlaybackPreferences.fromJson(const <String, dynamic>{});
    expect(defaults.preload, isNull);
    expect(defaults.preloads(television: false), isTrue);
    expect(defaults.preloads(television: true), isTrue);

    // Explicit choices remain authoritative on every device.
    expect(
      PlaybackPreferences.fromJson(const {'preopenNextEpisode': false})
          .preloads(television: false),
      isFalse,
    );
    expect(
      PlaybackPreferences.fromJson(const {'preopenNextEpisode': false})
          .preloads(television: true),
      isFalse,
    );
    expect(
      PlaybackPreferences.fromJson(const {'preopenNextEpisode': true})
          .preloads(television: true),
      isTrue,
    );
  });

  test('legacy forced preload key is ignored and auto PiP preference persists', () {
    final migrated = PlaybackPreferences.fromJson(const {
      'preload': true,
      'autoPictureInPicture': true,
    });
    expect(migrated.preload, isNull);
    expect(migrated.autoPictureInPicture, isTrue);
    expect(migrated.toJson().containsKey('preload'), isFalse);
    expect(
      PlaybackPreferences.fromJson(migrated.toJson()).autoPictureInPicture,
      isTrue,
    );
  });
  test('portrait blur defaults off and persists through JSON and copyWith', () {
    final defaults = PlaybackPreferences.fromJson(const <String, dynamic>{});
    expect(defaults.portraitBlurBackground, isFalse);

    final enabled = defaults.copyWith(portraitBlurBackground: true);
    expect(enabled.portraitBlurBackground, isTrue);
    expect(
      PlaybackPreferences.fromJson(enabled.toJson()).portraitBlurBackground,
      isTrue,
    );

    final disabled = enabled.copyWith(portraitBlurBackground: false);
    expect(disabled.portraitBlurBackground, isFalse);
    expect(
      PlaybackPreferences.fromJson(disabled.toJson()).portraitBlurBackground,
      isFalse,
    );
  });
  test('TV decoder defaults to automatic and persists each supported choice', () {
    final defaults = PlaybackPreferences.fromJson(const <String, dynamic>{});
    expect(defaults.televisionDecoder, TelevisionDecoder.automatic);

    for (final decoder in [
      TelevisionDecoder.automatic,
      TelevisionDecoder.hardware,
      TelevisionDecoder.software,
    ]) {
      final restored = PlaybackPreferences.fromJson(
        PlaybackPreferences(televisionDecoder: decoder).toJson(),
      );
      expect(restored.televisionDecoder, decoder);
    }
  });

  test('legacy decoder preference is ignored and receives automatic default', () {
    final migrated = PlaybackPreferences.fromJson(const {
      'televisionDecoder': 'hardware',
    });
    expect(migrated.televisionDecoder, TelevisionDecoder.automatic);
    expect(
      PlaybackPreferences.fromJson(migrated.toJson()).televisionDecoder,
      TelevisionDecoder.automatic,
    );
  });

  test('decoder modes map to native hardware/software paths across platforms', () {
    String? hwdec(
      TelevisionDecoder decoder, {
      bool android = false,
      bool ios = false,
      bool macos = false,
      bool windows = false,
      bool mpvEmbed = false,
    }) => playbackHwdecFor(
      decoder: decoder,
      android: android,
      ios: ios,
      macos: macos,
      windows: windows,
      mpvEmbed: mpvEmbed,
    );

    expect(hwdec(TelevisionDecoder.automatic, android: true), 'auto-safe');
    expect(hwdec(TelevisionDecoder.automatic, android: true, mpvEmbed: true),
        'mediacodec');
    expect(hwdec(TelevisionDecoder.hardware, android: true), 'mediacodec-copy');
    expect(
      hwdec(TelevisionDecoder.hardware, android: true, mpvEmbed: true),
      'mediacodec',
    );
    expect(hwdec(TelevisionDecoder.software, android: true), 'no');
    expect(hwdec(TelevisionDecoder.automatic, windows: true), 'd3d11va-copy');
    expect(hwdec(TelevisionDecoder.software, windows: true), 'no');
    expect(hwdec(TelevisionDecoder.hardware, macos: true), 'videotoolbox-copy');
    expect(hwdec(TelevisionDecoder.automatic, macos: true), 'auto-safe');
    expect(hwdec(TelevisionDecoder.hardware, ios: true), 'videotoolbox-copy');
    expect(hwdec(TelevisionDecoder.automatic, ios: true), isNull);
  });


  test('Picture-in-Picture is available on supported Android phones but not TVs', () {
    expect(
      supportsPictureInPicture(android: true, systemSupported: true),
      isTrue,
    );
    expect(
      supportsPictureInPicture(
        android: true,
        systemSupported: true,
        television: true,
      ),
      isFalse,
    );
    expect(
      supportsPictureInPicture(android: false, systemSupported: true),
      isFalse,
    );
    expect(
      supportsPictureInPicture(android: true, systemSupported: false),
      isFalse,
    );
  });

  test('automatic PiP requires a supported Android device, playback and opt-in', () {
    expect(
      shouldAutoEnterPictureInPicture(
        android: true,
        systemSupported: true,
        playing: true,
        enabled: true,
      ),
      isTrue,
    );
    expect(
      shouldAutoEnterPictureInPicture(
        android: true,
        systemSupported: true,
        playing: true,
        enabled: true,
        television: true,
      ),
      isFalse,
    );
    for (final values in [
      (android: false, systemSupported: true, playing: true, enabled: true),
      (android: true, systemSupported: false, playing: true, enabled: true),
      (android: true, systemSupported: true, playing: false, enabled: true),
      (android: true, systemSupported: true, playing: true, enabled: false),
    ]) {
      expect(
        shouldAutoEnterPictureInPicture(
          android: values.android,
          systemSupported: values.systemSupported,
          playing: values.playing,
          enabled: values.enabled,
        ),
        isFalse,
      );
    }
  });
  test('playback aspect ratio settings persist and old settings default to automatic', () {
    final defaults = PlaybackPreferences.fromJson(const <String, dynamic>{});
    expect(defaults.aspectRatio, PlaybackAspectRatio.automatic);
    for (final mode in PlaybackAspectRatio.values) {
      final restored = PlaybackPreferences.fromJson(PlaybackPreferences(aspectRatio: mode).toJson());
      expect(restored.aspectRatio, mode);
    }
    expect(PlaybackPreferences.fromJson(const {'speed': 1.0}).aspectRatio, PlaybackAspectRatio.automatic);
  });

  test('fixed aspect ratios return the expected target ratio', () {
    expect(playbackAspectRatioValue(PlaybackAspectRatio.ratio4x3), closeTo(4 / 3, .0001));
    expect(playbackAspectRatioValue(PlaybackAspectRatio.ratio16x9), closeTo(16 / 9, .0001));
    expect(playbackAspectRatioValue(PlaybackAspectRatio.automatic), isNull);
  });
}
