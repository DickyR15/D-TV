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

/// Keep the landscape portrait-blur migration on desktop and phones,
/// but do not turn on the extra blur composition automatically on Android TV.
/// Users can still opt in from playback settings.
bool shouldMigratePortraitBlurLandscapeDefault({
  required bool windows,
  required bool android,
  required bool television,
  required bool landscape,
  required bool androidMpvConfigured,
}) =>
    landscape &&
    (windows || (android && !television && androidMpvConfigured));

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

/// Windows uses the regular MPV texture renderer; Android needs the MPV
/// backend because ExoPlayer does not expose the texture path used for blur.
bool supportsPortraitBlurBackground({
  required bool android,
  required bool windows,
  required bool androidMpvConfigured,
}) =>
    windows || (android && androidMpvConfigured);
