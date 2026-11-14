import 'package:flutter/painting.dart';

import 'video_enhancement_preferences.dart';

const playbackSpeeds = [.5, .75, 1.0, 1.1, 1.25, 1.5, 2.0, 3.0];

enum TelevisionDecoder { automatic, optimized, hardware, software }

enum PlaybackAspectRatio { automatic, stretch, crop, fitWidth, fitHeight, original, ratio4x3, ratio16x9 }

String playbackAspectRatioLabel(PlaybackAspectRatio value) => switch (value) {
  PlaybackAspectRatio.automatic => '自動',
  PlaybackAspectRatio.stretch => '拉伸',
  PlaybackAspectRatio.crop => '裁切',
  PlaybackAspectRatio.fitWidth => '等寬',
  PlaybackAspectRatio.fitHeight => '等高',
  PlaybackAspectRatio.original => '原始',
  PlaybackAspectRatio.ratio4x3 => '4:3',
  PlaybackAspectRatio.ratio16x9 => '16:9',
};

BoxFit playbackAspectRatioFit(PlaybackAspectRatio value) => switch (value) {
  PlaybackAspectRatio.automatic || PlaybackAspectRatio.ratio4x3 || PlaybackAspectRatio.ratio16x9 => BoxFit.contain,
  PlaybackAspectRatio.stretch => BoxFit.fill,
  PlaybackAspectRatio.crop => BoxFit.cover,
  PlaybackAspectRatio.fitWidth => BoxFit.fitWidth,
  PlaybackAspectRatio.fitHeight => BoxFit.fitHeight,
  PlaybackAspectRatio.original => BoxFit.none,
};

double? playbackAspectRatioValue(
  PlaybackAspectRatio value, {
  double sourceAspectRatio = 16 / 9,
}) => switch (value) {
  PlaybackAspectRatio.ratio4x3 => sourceAspectRatio < 1 ? 3 / 4 : 4 / 3,
  PlaybackAspectRatio.ratio16x9 => sourceAspectRatio < 1 ? 9 / 16 : 16 / 9,
  _ => null,
};

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

/// Whether the current Android device exposes the system Picture-in-Picture API.
bool supportsPictureInPicture({
  required bool android,
  bool ios = false,
  required bool systemSupported,
  bool television = false,
}) => (android || ios) && !television && systemSupported;

/// PiP auto-entry is only enabled while a supported Android player is running.
bool shouldAutoEnterPictureInPicture({
  required bool android,
  required bool systemSupported,
  required bool playing,
  required bool enabled,
  bool television = false,
}) =>
    supportsPictureInPicture(
      android: android,
      systemSupported: systemSupported,
      television: television,
    ) &&
    playing &&
    enabled;

class PlaybackPreferences {
  const PlaybackPreferences({
    this.speed = 1,
    this.quality = 0,
    this.autoAdvance = true,
    this.preload,
    this.enhancement = const VideoEnhancementPreferences(),
    this.portraitBlurBackground = false,
    this.televisionDecoder = TelevisionDecoder.automatic,
    this.autoPictureInPicture = false,
    this.aspectRatio = PlaybackAspectRatio.automatic,
    this.desktopAlwaysOnTop = false,
    this.showTelevisionNetworkSpeed = true,
    this.showTelevisionClock = true,
  });

  final double speed;
  final int quality;
  final bool autoAdvance;
  final bool? preload;
  final VideoEnhancementPreferences enhancement;
  final bool portraitBlurBackground;
  final TelevisionDecoder televisionDecoder;
  final bool autoPictureInPicture;
  final PlaybackAspectRatio aspectRatio;
  final bool desktopAlwaysOnTop;
  final bool showTelevisionNetworkSpeed;
  final bool showTelevisionClock;

  // Fresh installs enable next-episode preloading on phones and TVs.
  // A saved explicit choice (including false) always takes precedence.
  bool preloads({required bool television}) => preload ?? true;

  PlaybackPreferences copyWith({
    double? speed,
    int? quality,
    bool? autoAdvance,
    bool? preload,
    VideoEnhancementPreferences? enhancement,
    bool? portraitBlurBackground,
    TelevisionDecoder? televisionDecoder,
    bool? autoPictureInPicture,
    PlaybackAspectRatio? aspectRatio,
    bool? desktopAlwaysOnTop,
    bool? showTelevisionNetworkSpeed,
    bool? showTelevisionClock,
  }) => PlaybackPreferences(
    speed: speed ?? this.speed,
    quality: quality ?? this.quality,
    autoAdvance: autoAdvance ?? this.autoAdvance,
    preload: preload ?? this.preload,
    enhancement: enhancement ?? this.enhancement,
    portraitBlurBackground: portraitBlurBackground ?? this.portraitBlurBackground,
    televisionDecoder: televisionDecoder ?? this.televisionDecoder,
    autoPictureInPicture: autoPictureInPicture ?? this.autoPictureInPicture,
    aspectRatio: aspectRatio ?? this.aspectRatio,
    desktopAlwaysOnTop: desktopAlwaysOnTop ?? this.desktopAlwaysOnTop,
    showTelevisionNetworkSpeed:
        showTelevisionNetworkSpeed ?? this.showTelevisionNetworkSpeed,
    showTelevisionClock: showTelevisionClock ?? this.showTelevisionClock,
  );

  Map<String, dynamic> toJson() => {
    'speed': speed,
    'quality': quality,
    'autoAdvance': autoAdvance,
    if (preload != null) 'preopenNextEpisode': preload,
    'enhancement': enhancement.toJson(),
    'portraitBlurBackground': portraitBlurBackground,
    'televisionDecoderMode': televisionDecoder.name,
    'autoPictureInPicture': autoPictureInPicture,
    'aspectRatioMode': aspectRatio.name,
    'desktopAlwaysOnTop': desktopAlwaysOnTop,
    'showTelevisionNetworkSpeed': showTelevisionNetworkSpeed,
    'showTelevisionClock': showTelevisionClock,
  };

  factory PlaybackPreferences.fromJson(Map<String, dynamic> value) {
    final speed = (value['speed'] as num? ?? 1).toDouble();
    final quality = value['quality'] as int? ?? 0;
    final autoAdvance = value['autoAdvance'] as bool? ?? true;
    // The old 'preload' value was forced on by 1.0.0 playback code.
    // Read only the new key so older installations receive device-aware defaults.
    final preload = value['preopenNextEpisode'] as bool?;
    final savedDecoder =
        TelevisionDecoder.values.asNameMap()[value['televisionDecoderMode']];
    final savedAspectRatio = PlaybackAspectRatio.values.asNameMap()[value['aspectRatioMode']];
    final aspectRatio = savedAspectRatio ?? PlaybackAspectRatio.automatic;
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
      preload: preload,
      enhancement: VideoEnhancementPreferences.fromJson(value['enhancement']),
      portraitBlurBackground: value['portraitBlurBackground'] as bool? ?? false,
      televisionDecoder: decoder,
      autoPictureInPicture: value['autoPictureInPicture'] as bool? ?? false,
      aspectRatio: aspectRatio,
      desktopAlwaysOnTop: value['desktopAlwaysOnTop'] as bool? ?? false,
      showTelevisionNetworkSpeed:
          value['showTelevisionNetworkSpeed'] as bool? ?? true,
      showTelevisionClock: value['showTelevisionClock'] as bool? ?? true,
    );
  }
}
