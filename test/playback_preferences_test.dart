import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/local_store.dart';
import 'package:hgtv/playback_preferences.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  test('default speed and quality persist per user', () async {
    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await store.setPlaybackPreferences(
      const PlaybackPreferences(speed: 1.25, quality: 1080),
    );
    final restored = LocalStore(store.preferences);
    addTearDown(restored.dispose);
    expect(restored.playbackPreferences.speed, 1.25);
    expect(restored.playbackPreferences.quality, 1080);
  });

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
