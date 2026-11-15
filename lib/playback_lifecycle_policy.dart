import 'package:flutter/widgets.dart';

bool playbackLifecycleVisible(AppLifecycleState state) =>
    state == AppLifecycleState.resumed ||
    state == AppLifecycleState.inactive;

bool playbackLifecycleShouldPause(AppLifecycleState state) =>
    state == AppLifecycleState.paused ||
    state == AppLifecycleState.hidden ||
    state == AppLifecycleState.detached;

/// Recreate the Android video output only after PiP has ended and the Activity
/// has fully returned to the foreground. This avoids detaching a Surface
/// while Android is still animating the PiP window transition.
bool playbackShouldRefreshAndroidPipSurface({
  required bool android,
  required bool pendingRestore,
  required bool activityResumed,
  required bool pictureInPictureActive,
  required bool pictureInPictureRequested,
}) =>
    android &&
    pendingRestore &&
    activityResumed &&
    !pictureInPictureActive &&
    !pictureInPictureRequested;

/// Android 12+ can enter PiP automatically while the Activity is moving to
/// the background. That transition may deliver AppLifecycleState.paused before
/// onPictureInPictureModeChanged(true), so preserve playback when the same
/// conditions that enabled native auto-enter are still true.
bool playbackLifecycleShouldRetainAndroidPictureInPicture({
  required bool android,
  required bool systemSupported,
  required bool playing,
  required bool enabled,
  bool television = false,
}) =>
    android && systemSupported && playing && enabled && !television;

/// Back must bypass disabled playback controls while a stream is loading or
/// an error panel is shown, so a failed recovery can never trap the viewer.
bool playbackBackShouldExitImmediately({
  required bool loading,
  required bool hasError,
}) => loading || hasError;

/// The independent desktop mini-player is already a compact window and must
/// not inherit the main player's automatic fullscreen startup behavior.
bool playbackWindowStartsFullscreen({required bool desktopMiniPlayer}) =>
    !desktopMiniPlayer;
