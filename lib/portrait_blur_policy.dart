/// Select ExoPlayer only for Android TV when the native player has not
/// been replaced by a test/custom player. Phones and desktop keep their
/// existing playback engines.
bool shouldUseExoPlayerForAndroidPlayback({
  required bool android,
  required bool television,
  required bool exoConfigured,
  required bool customPlayer,
}) =>
    android && television && exoConfigured && !customPlayer;

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

/// Windows uses the regular MPV texture renderer; Android needs the MPV
/// backend because ExoPlayer does not expose the texture path used for blur.
bool supportsPortraitBlurBackground({
  required bool android,
  required bool windows,
  required bool androidMpvConfigured,
}) =>
    windows || (android && androidMpvConfigured);
