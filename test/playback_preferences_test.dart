import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/local_store.dart';
import 'package:hgtv/playback_preferences.dart';
import 'package:shared_preferences/shared_preferences.dart';

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

  
  test('next-episode preload defaults on mobile and off on TV', () {
    final defaults = PlaybackPreferences.fromJson(const <String, dynamic>{});
    expect(defaults.preloads(television: false), isTrue);
    expect(defaults.preloads(television: true), isFalse);
    expect(PlaybackPreferences.fromJson(const {'preload': true}).preloads(television: true), isTrue);
    expect(PlaybackPreferences.fromJson(const {'preload': false}).preloads(television: false), isFalse);
  });

  test('automatic picture in picture is disabled by default and persists', () {
    final defaults = PlaybackPreferences.fromJson(const <String, dynamic>{});
    expect(defaults.autoPictureInPicture, isFalse);
    final enabled = defaults.copyWith(autoPictureInPicture: true);
    expect(PlaybackPreferences.fromJson(enabled.toJson()).autoPictureInPicture, isTrue);
  });

  test('TV decoder defaults to conservative hardware mode', () {
    expect(
      const PlaybackPreferences().televisionDecoder,
      TelevisionDecoder.hardware,
    );
    expect(
      PlaybackPreferences.fromJson(const <String, dynamic>{}).televisionDecoder,
      TelevisionDecoder.hardware,
    );
  });

  test('legacy optimized TV decoder preset migrates once', () async {
    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await store.setPlaybackPreferences(
      const PlaybackPreferences(televisionDecoder: TelevisionDecoder.optimized),
    );
    expect(store.needsTvDecoderSafePresetMigration, isTrue);

    await store.migrateTvDecoderSafePreset();
    expect(store.playbackPreferences.televisionDecoder, TelevisionDecoder.hardware);
    expect(store.needsTvDecoderSafePresetMigration, isFalse);

    await store.setPlaybackPreferences(
      const PlaybackPreferences(televisionDecoder: TelevisionDecoder.optimized),
    );
    await store.migrateTvDecoderSafePreset();
    expect(store.playbackPreferences.televisionDecoder, TelevisionDecoder.optimized);
  });
}
