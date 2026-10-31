bool shouldUseMpvEmbedForPlayback({
  required bool android,
  required bool television,
  required bool mpvEmbedConfigured,
  required bool softwareDecoderSelected,
  required bool customPlayer,
}) =>
    android &&
    television &&
    mpvEmbedConfigured &&
    !softwareDecoderSelected &&
    !customPlayer;

bool shouldUsePortraitBlurBackground({
  required bool enabled,
  required bool textureRendererAvailable,
  required bool loading,
  required bool hasError,
  required double aspectRatio,
}) =>
    enabled &&
    textureRendererAvailable &&
    !loading &&
    !hasError &&
    aspectRatio.isFinite &&
    aspectRatio > 0 &&
    aspectRatio < 1;
