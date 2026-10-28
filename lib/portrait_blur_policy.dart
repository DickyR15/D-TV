bool shouldUsePortraitBlurBackground({
  required bool enabled,
  required bool mpvEmbedBackend,
  required bool loading,
  required bool hasError,
  required double aspectRatio,
}) =>
    enabled &&
    mpvEmbedBackend &&
    !loading &&
    !hasError &&
    aspectRatio.isFinite &&
    aspectRatio > 0 &&
    aspectRatio < 1;
