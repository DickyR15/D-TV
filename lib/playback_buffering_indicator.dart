import 'dart:async';

import 'package:flutter/material.dart';
import 'package:media_kit/media_kit.dart';

import 'playback_controls_visibility.dart';

/// Displays a buffering indicator only when playback position has actually
/// stalled. Some MPV startup paths can leave buffering=true while frames and
/// timestamps are already advancing; that stale flag must not obscure video.
class PlaybackBufferingIndicator extends StatefulWidget {
  const PlaybackBufferingIndicator({
    super.key,
    required this.player,
    required this.enabled,
  });

  final Player player;
  final bool enabled;

  @override
  State<PlaybackBufferingIndicator> createState() =>
      _PlaybackBufferingIndicatorState();
}

class _PlaybackBufferingIndicatorState
    extends State<PlaybackBufferingIndicator> {
  final _policy = PlaybackBufferingIndicatorPolicy();
  final List<StreamSubscription<dynamic>> _subscriptions = [];
  Timer? _visibilityTimer;
  bool _visible = false;

  @override
  void initState() {
    super.initState();
    _attach();
  }

  void _attach() {
    _subscriptions
      ..add(widget.player.stream.position.listen((_) => _refresh()))
      ..add(widget.player.stream.buffering.listen((_) => _refresh()))
      ..add(widget.player.stream.playing.listen((_) => _refresh()));
    _refresh();
  }

  void _refresh() {
    if (!mounted) return;
    final state = widget.player.state;
    final now = DateTime.now();
    _policy.update(
      buffering: widget.enabled && state.buffering,
      playing: state.playing,
      position: state.position,
      now: now,
    );
    final visible = widget.enabled && _policy.visibleAt(now);
    final remaining = widget.enabled ? _policy.remainingAt(now) : null;
    _visibilityTimer?.cancel();
    if (remaining != null && remaining > Duration.zero) {
      _visibilityTimer = Timer(remaining, _refresh);
    }
    if (_visible != visible) {
      setState(() => _visible = visible);
    }
  }

  @override
  void didUpdateWidget(covariant PlaybackBufferingIndicator oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.player != widget.player) {
      for (final subscription in _subscriptions) {
        unawaited(subscription.cancel());
      }
      _subscriptions.clear();
      _visibilityTimer?.cancel();
      _attach();
    } else if (oldWidget.enabled != widget.enabled) {
      _refresh();
    }
  }

  @override
  void dispose() {
    _visibilityTimer?.cancel();
    for (final subscription in _subscriptions) {
      unawaited(subscription.cancel());
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!_visible) return const SizedBox.shrink();
    return const IgnorePointer(
      child: Center(child: CircularProgressIndicator()),
    );
  }
}
