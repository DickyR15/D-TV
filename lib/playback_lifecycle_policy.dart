import 'package:flutter/widgets.dart';

bool playbackLifecycleVisible(AppLifecycleState state) =>
    state == AppLifecycleState.resumed ||
    state == AppLifecycleState.inactive;

bool playbackLifecycleShouldPause(AppLifecycleState state) =>
    state == AppLifecycleState.paused ||
    state == AppLifecycleState.hidden ||
    state == AppLifecycleState.detached;
