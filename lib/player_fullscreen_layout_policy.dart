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

/// Preserve the complete picture on iOS landscape streams so subtitles near
/// the bottom edge are not clipped by BoxFit.cover. Portrait fullscreen keeps
/// its immersive crop; all other platforms retain their existing contain fit.
BoxFit playerVideoFit({
  required bool ios,
  required bool fullscreen,
  required double aspectRatio,
}) {
  if (ios && fullscreen && aspectRatio > 0) {
    return aspectRatio >= 1 ? BoxFit.contain : BoxFit.cover;
  }
  return BoxFit.contain;
}
