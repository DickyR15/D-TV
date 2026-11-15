enum PlayerSwipeZone { brightness, episode, volume, none }

PlayerSwipeZone playerSwipeZoneForPosition({
  required double x,
  required double width,
}) {
  if (!x.isFinite || !width.isFinite || width <= 0 || x < 0 || x > width) {
    return PlayerSwipeZone.none;
  }
  // Match the requested 10% / 80% / 10% layout:
  // left edge = brightness, centre = previous/next episode, right edge = volume.
  if (x < width * 0.10) return PlayerSwipeZone.brightness;
  if (x < width * 0.90) return PlayerSwipeZone.episode;
  return PlayerSwipeZone.volume;
}

bool supportsPlayerTouchGestures({
  required bool android,
  required bool ios,
  required bool television,
}) => (android || ios) && !television;

double playerVerticalSwipeValue({
  required double initialValue,
  required double originY,
  required double currentY,
  required double height,
  required double maximumValue,
}) {
  if (!initialValue.isFinite ||
      !height.isFinite ||
      height <= 0 ||
      !maximumValue.isFinite ||
      maximumValue <= 0) {
    final safeMaximum = maximumValue.isFinite && maximumValue > 0
        ? maximumValue
        : 1.0;
    return (initialValue.isFinite ? initialValue : 0.0)
        .clamp(0.0, safeMaximum)
        .toDouble();
  }
  final delta = (originY - currentY) / (height * 0.6) * maximumValue;
  return (initialValue + delta).clamp(0.0, maximumValue).toDouble();
}

double playerPinchZoomScale({
  required double initialScale,
  required double initialDistance,
  required double currentDistance,
  double minimumScale = 1.0,
  double maximumScale = 3.0,
}) {
  final lowerBound = minimumScale.isFinite ? minimumScale : 1.0;
  final upperBound = maximumScale.isFinite && maximumScale >= lowerBound
      ? maximumScale
      : lowerBound;
  final safeScale = initialScale.isFinite ? initialScale : lowerBound;
  if (!initialDistance.isFinite ||
      initialDistance <= 0 ||
      !currentDistance.isFinite) {
    return safeScale.clamp(lowerBound, upperBound).toDouble();
  }
  return (safeScale * currentDistance / initialDistance)
      .clamp(lowerBound, upperBound)
      .toDouble();
}
