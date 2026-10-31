import 'package:flutter/painting.dart';

/// Keep the video canvas edge-to-edge on iOS only while the player is
/// fullscreen. Playback controls continue to use MediaQuery safe insets.
bool playerTopSafeAreaEnabled({
  required bool ios,
  required bool television,
  required bool fullscreen,
  required bool mobile,
}) {
  if (ios && fullscreen) return false;
  return !television && (fullscreen || mobile);
}

/// iOS fullscreen playback fills the display for both portrait and landscape
/// streams. When the device and stream aspect ratios differ, a small crop is
/// intentional so that no black bars remain at the left and right edges.
BoxFit playerVideoFit({
  required bool ios,
  required bool fullscreen,
  required double aspectRatio,
}) {
  if (ios && fullscreen && aspectRatio > 0) {
    return BoxFit.cover;
  }
  return BoxFit.contain;
}
