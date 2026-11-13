import 'package:flutter/widgets.dart';

bool playbackLifecycleVisible(AppLifecycleState state) =>
    state == AppLifecycleState.resumed ||
    state == AppLifecycleState.inactive;

bool playbackLifecycleShouldPause(AppLifecycleState state) =>
    state == AppLifecycleState.paused ||
    state == AppLifecycleState.hidden ||
    state == AppLifecycleState.detached;

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
