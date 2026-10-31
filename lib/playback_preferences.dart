import 'video_enhancement_preferences.dart';

const playbackSpeeds = [.5, .75, 1.0, 1.25, 1.5, 2.0, 3.0];

enum TelevisionDecoder { automatic, optimized, hardware, software }

/// Maps the shared decoder preference to the native MPV decoder backend.
/// This follows the upstream default / hardware-compatible / software-compatible
/// behavior while preserving D-TV's Android MPV Embed surface path.
String? playbackHwdecFor({
  required TelevisionDecoder decoder,
  required bool android,
  required bool ios,
  required bool macos,
  required bool windows,
  bool mpvEmbed = false,
}) {
  if (decoder == TelevisionDecoder.software) return 'no';

  // Android TV's validated default path is MPV Embed + MediaCodec. Automatic
  // means hardware-first there; don't silently fall back to the standard GPU
  // VO path, which previously caused video to lag behind audio on TV boxes.
  if (android && mpvEmbed) return 'mediacodec';

  if (decoder == TelevisionDecoder.hardware) {
    if (android) return 'mediacodec-copy';
    if (ios || macos) return 'videotoolbox-copy';
  }

  if (windows) return 'd3d11va-copy';
  if (android || macos) return 'auto-safe';
  return null;
}

class PlaybackPreferences {
  const PlaybackPreferences({
    this.speed = 1,
    this.quality = 0,
    this.autoAdvance = true,
    this.danmaku = false,
    this.preload,
    this.enhancement = const VideoEnhancementPreferences(),
    this.portraitBlurBackground = false,
    this.televisionDecoder = TelevisionDecoder.automatic,
    this.autoPictureInPicture = false,
  });

  final double speed;
  final int quality;
  final bool autoAdvance;
  final bool danmaku;
  final bool? preload;
  final VideoEnhancementPreferences enhancement;
  final bool portraitBlurBackground;
  final TelevisionDecoder televisionDecoder;
  final bool autoPictureInPicture;

  // Fresh installs enable next-episode preloading on phones and TVs.
  // A saved explicit choice (including false) always takes precedence.
  bool preloads({required bool television}) => preload ?? true;

  PlaybackPreferences copyWith({
    double? speed,
    int? quality,
    bool? autoAdvance,
    bool? danmaku,
    bool? preload,
    VideoEnhancementPreferences? enhancement,
    bool? portraitBlurBackground,
    TelevisionDecoder? televisionDecoder,
    bool? autoPictureInPicture,
  }) => PlaybackPreferences(
    speed: speed ?? this.speed,
    quality: quality ?? this.quality,
    autoAdvance: autoAdvance ?? this.autoAdvance,
    danmaku: danmaku ?? this.danmaku,
    preload: preload ?? this.preload,
    enhancement: enhancement ?? this.enhancement,
    portraitBlurBackground: portraitBlurBackground ?? this.portraitBlurBackground,
    televisionDecoder: televisionDecoder ?? this.televisionDecoder,
    autoPictureInPicture: autoPictureInPicture ?? this.autoPictureInPicture,
  );

  Map<String, dynamic> toJson() => {
    'speed': speed,
    'quality': quality,
    'autoAdvance': autoAdvance,
    'danmaku': danmaku,
    if (preload != null) 'preopenNextEpisode': preload,
    'enhancement': enhancement.toJson(),
    'portraitBlurBackground': portraitBlurBackground,
    'televisionDecoderMode': televisionDecoder.name,
    'autoPictureInPicture': autoPictureInPicture,
  };

  factory PlaybackPreferences.fromJson(Map<String, dynamic> value) {
    final speed = (value['speed'] as num? ?? 1).toDouble();
    final quality = value['quality'] as int? ?? 0;
    final autoAdvance = value['autoAdvance'] as bool? ?? true;
    final danmaku = value['danmaku'] as bool? ?? false;
    // The old 'preload' value was forced on by 1.0.0 playback code.
    // Read only the new key so older installations receive device-aware defaults.
    final preload = value['preopenNextEpisode'] as bool?;
    final savedDecoder =
        TelevisionDecoder.values.asNameMap()[value['televisionDecoderMode']];
    final decoder = savedDecoder == TelevisionDecoder.optimized
        ? TelevisionDecoder.hardware
        : savedDecoder ?? TelevisionDecoder.automatic;
    if (!playbackSpeeds.contains(speed) || quality < 0 || quality > 4320) {
      throw const FormatException('播放偏好無效');
    }
    return PlaybackPreferences(
      speed: speed,
      quality: quality,
      autoAdvance: autoAdvance,
      danmaku: danmaku,
      preload: preload,
      enhancement: VideoEnhancementPreferences.fromJson(value['enhancement']),
      portraitBlurBackground: value['portraitBlurBackground'] as bool? ?? false,
      televisionDecoder: decoder,
      autoPictureInPicture: value['autoPictureInPicture'] as bool? ?? false,
    );
  }
}
