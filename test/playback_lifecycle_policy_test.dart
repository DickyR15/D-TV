import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/playback_lifecycle_policy.dart';

void main() {
  test('inactive remains visible for transient system UI interruptions', () {
    expect(playbackLifecycleVisible(AppLifecycleState.resumed), isTrue);
    expect(playbackLifecycleVisible(AppLifecycleState.inactive), isTrue);
    expect(playbackLifecycleShouldPause(AppLifecycleState.inactive), isFalse);
  });

  test('actual background and detached states pause playback', () {
    for (final state in [
      AppLifecycleState.paused,
      AppLifecycleState.hidden,
      AppLifecycleState.detached,
    ]) {
      expect(playbackLifecycleVisible(state), isFalse);
      expect(playbackLifecycleShouldPause(state), isTrue);
    }
  });
}
