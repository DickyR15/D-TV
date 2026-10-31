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

/// Portrait short dramas fill the tall iPhone display in fullscreen.
/// Other layouts preserve the whole frame instead of cropping it.
BoxFit playerVideoFit({
  required bool ios,
  required bool fullscreen,
  required double aspectRatio,
}) {
  if (ios && fullscreen && aspectRatio > 0 && aspectRatio < 1) {
    return BoxFit.cover;
  }
  return BoxFit.contain;
}
