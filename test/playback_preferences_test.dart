import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/playback_preferences.dart';

void main() {
  test('next episode preload defaults on for phones and TVs', () {
    final defaults = PlaybackPreferences.fromJson(const <String, dynamic>{});
    expect(defaults.preload, isNull);
    expect(defaults.preloads(television: false), isTrue);
    expect(defaults.preloads(television: true), isTrue);

    // A user's explicit choice remains authoritative on every device.
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

}
