bool shouldUsePortraitBlurBackground({
  required bool enabled,
  required bool mpvBackendAvailable,
  required bool loading,
  required bool hasError,
  required double aspectRatio,
}) =>
    enabled &&
    mpvBackendAvailable &&
    !loading &&
    !hasError &&
    aspectRatio.isFinite &&
    aspectRatio > 0 &&
    aspectRatio < 1;
