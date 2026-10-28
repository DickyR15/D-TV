/// Returns true only for a valid portrait video aspect ratio.
///
/// Near-square clips are excluded so the side-fill effect remains focused on
/// genuinely vertical short-form videos.
bool shouldUsePortraitVideoBackground(double aspectRatio) =>
    aspectRatio.isFinite && aspectRatio > 0 && aspectRatio < 0.95;
