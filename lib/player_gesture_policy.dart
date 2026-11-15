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
  if (!height.isFinite || height <= 0 || !maximumValue.isFinite || maximumValue <= 0) {
    return initialValue.clamp(0.0, maximumValue > 0 ? maximumValue : 1.0).toDouble();
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
  if (!initialDistance.isFinite ||
      initialDistance <= 0 ||
      !currentDistance.isFinite ||
      !minimumScale.isFinite ||
      !maximumScale.isFinite ||
      maximumScale < minimumScale) {
    return initialScale.clamp(minimumScale, maximumScale).toDouble();
  }
  return (initialScale * currentDistance / initialDistance)
      .clamp(minimumScale, maximumScale)
      .toDouble();
}
