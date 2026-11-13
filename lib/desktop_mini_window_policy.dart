import 'dart:ui' show Size;

/// Keeps desktop mini-player windows within a usable media aspect ratio.
double clampDesktopMiniAspectRatio(double aspectRatio) {
  if (!aspectRatio.isFinite || aspectRatio <= 0) return 16 / 9;
  return aspectRatio.clamp(1 / 2.39, 2.39).toDouble();
}

/// Desktop mini-player is intentionally limited to macOS. Windows keeps its
/// normal resizable main window instead of shrinking it into the screen corner.
bool supportsDesktopMiniPlayer({
  required bool isWindows,
  required bool isMacOS,
  required bool secondaryPlayer,
}) =>
    !secondaryPlayer && isMacOS && !isWindows;

/// Returns the mini-window size using its longest edge as [extent].
Size desktopMiniWindowSize(double aspectRatio, double extent) {
  final ratio = clampDesktopMiniAspectRatio(aspectRatio);
  final safeExtent = extent.isFinite && extent > 0 ? extent : 480.0;
  return ratio >= 1
      ? Size(safeExtent, safeExtent / ratio)
      : Size(safeExtent * ratio, safeExtent);
}
