/// Returns true only after the player has started video output in the
/// foreground and is not waiting for data. Audio-only or metadata-only states
/// must not dismiss the startup overlay.
bool playbackStartupVideoIsVisible({
  required bool playing,
  required bool buffering,
  required bool playIntent,
  required bool foreground,
  required num? width,
  required num? height,
}) {
  return playing &&
      !buffering &&
      playIntent &&
      foreground &&
      (width ?? 0) > 0 &&
      (height ?? 0) > 0;
}
