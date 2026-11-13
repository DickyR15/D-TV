import 'dart:async';
import 'dart:io';
import 'dart:math' as math;

import 'package:flutter/services.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';

/// Converts coded/display dimensions and rotation into the visible frame size.
Size? videoDisplaySizeForSource({
  required Size? codedSize,
  Size? displaySize,
  int rotation = 0,
}) {
  final base = displaySize != null &&
          displaySize.width > 0 &&
          displaySize.height > 0
      ? displaySize
      : codedSize;
  if (base == null ||
      !base.width.isFinite ||
      !base.height.isFinite ||
      base.width <= 0 ||
      base.height <= 0) {
    return null;
  }
  return rotation.abs() % 180 == 90
      ? Size(base.height, base.width)
      : base;
}

Size? videoDisplaySize(VideoParams parameters) =>
    videoDisplaySizeForSource(
      codedSize: Size(
        (parameters.w ?? 0).toDouble(),
        (parameters.h ?? 0).toDouble(),
      ),
      displaySize: (parameters.dw ?? 0) > 0 && (parameters.dh ?? 0) > 0
          ? Size(parameters.dw!.toDouble(), parameters.dh!.toDouble())
          : null,
      rotation: parameters.rotate ?? 0,
    );

/// Returns the visible short-edge resolution used by common 720P/1080P labels.
/// This keeps portrait video such as 720x1280 labelled 720P, not 1280P.
int? videoQualityShortEdge(Size? size) {
  if (size == null ||
      !size.width.isFinite ||
      !size.height.isFinite ||
      size.width <= 0 ||
      size.height <= 0) {
    return null;
  }
  final quality = size.shortestSide.round();
  return quality > 0 ? quality : null;
}

const maxVideoOutputDimension = 16384;

bool isValidVideoOutputSize(Size value) =>
    value.width.isFinite &&
    value.height.isFinite &&
    value.width >= 1 &&
    value.height >= 1 &&
    value.width <= maxVideoOutputDimension &&
    value.height <= maxVideoOutputDimension;

/// The independent desktop mini-player should scale the existing Flutter
/// texture instead of asking native MPV to reconfigure its output while the
/// user is interactively resizing the child window.
bool shouldAdjustWindowsVideoOutput({
  required bool isWindows,
  required bool desktopMiniPlayer,
}) => isWindows && !desktopMiniPlayer;

/// Calculates an aspect-preserving MPV output size for the visible viewport.
/// The result never exceeds source dimensions, avoiding wasteful upscaling.
Size? videoOutputSizeForSource(Size? source, Size viewport) {
  if (source == null ||
      !isValidVideoOutputSize(source) ||
      !isValidVideoOutputSize(viewport)) {
    return null;
  }
  final scale = math.min(
    1.0,
    math.min(viewport.width / source.width, viewport.height / source.height),
  );
  final result = Size(
    math.max(1, (source.width * scale).round()).toDouble(),
    math.max(1, (source.height * scale).round()).toDouble(),
  );
  return isValidVideoOutputSize(result) ? result : null;
}

class VideoOutputSizeAdapter {
  VideoOutputSizeAdapter(this.video, {required this.onFailure});

  static const _channel = MethodChannel('com.alexmercerind/media_kit_video');
  static const _windowsResizeDelay = Duration(milliseconds: 300);

  final VideoController video;
  final void Function() onFailure;
  Future<void>? _initialization;
  StreamSubscription<VideoParams>? _subscription;
  Timer? _windowsResizeTimer;
  Size? _target;
  Size? _lastWindowsSize;
  Future<void>? _windowsResizeFuture;
  bool _windowsResizeQueued = false;
  bool _closed = false;

  Size? get actualSize => video.rect.value?.size;

  Future<void> initialize() => _initialization ??= _initialize();

  Future<void> _initialize() async {
    await video.platform.future;
    if (_closed) return;
    if (Platform.isAndroid || Platform.isWindows) {
      _subscription = video.player.stream.videoParams.listen((_) {
        if (_closed || _target == null) return;
        if (Platform.isAndroid) {
          unawaited(
            _resizeAndroid().catchError((Object _) {
              if (!_closed) onFailure();
            }),
          );
        } else {
          _scheduleWindowsResize();
        }
      });
    }
  }

  Future<void> setTarget(Size? value) async {
    await initialize();
    if (_closed) return;
    // Layout transitions can briefly report unbounded or invalid constraints.
    // Ignore those intermediate frames instead of passing NaN/Infinity to
    // round() or into the native MPV video output API.
    if (value != null && !isValidVideoOutputSize(value)) return;
    final normalized = value == null
        ? null
        : Size(value.width.roundToDouble(), value.height.roundToDouble());
    if (normalized != null && !isValidVideoOutputSize(normalized)) return;
    final changed = _target != normalized;
    _target = normalized;
    if (Platform.isAndroid) {
      await _resizeAndroid();
    } else if (Platform.isWindows) {
      if (changed || normalized == null) _scheduleWindowsResize();
    } else if (changed || normalized == null) {
      await video.setSize(
        width: normalized?.width.toInt(),
        height: normalized?.height.toInt(),
      );
    }
  }

  void _scheduleWindowsResize() {
    if (_closed) return;
    _windowsResizeTimer?.cancel();
    _windowsResizeTimer = Timer(_windowsResizeDelay, () {
      _windowsResizeTimer = null;
      unawaited(
        _resizeWindows().catchError((Object _) {
          if (!_closed) onFailure();
        }),
      );
    });
  }

  Future<void> _resizeWindows() {
    if (_closed) return Future<void>.value();
    final running = _windowsResizeFuture;
    if (running != null) {
      // Coalesce overlapping WM_SIZE / Flutter layout notifications. The
      // active operation finishes first, then one pass applies the newest size.
      _windowsResizeQueued = true;
      return running;
    }
    late final Future<void> tracked;
    tracked = _applyWindowsResize().whenComplete(() {
      if (identical(_windowsResizeFuture, tracked)) {
        _windowsResizeFuture = null;
      }
      if (_windowsResizeQueued && !_closed) {
        _windowsResizeQueued = false;
        _scheduleWindowsResize();
      }
    });
    _windowsResizeFuture = tracked;
    return tracked;
  }

  Future<void> _applyWindowsResize() async {
    if (_closed) return;
    final target = _target;
    if (target == null) {
      if (_lastWindowsSize != null) {
        await video.setSize(width: null, height: null);
        if (_closed) return;
        _lastWindowsSize = null;
      }
      return;
    }
    final desired = videoOutputSizeForSource(
      videoDisplaySize(video.player.state.videoParams),
      target,
    );
    if (desired == null || desired == _lastWindowsSize) return;
    await video.setSize(
      width: desired.width.round(),
      height: desired.height.round(),
    );
    if (_closed) return;
    _lastWindowsSize = desired;
  }

  Future<void> _resizeAndroid() async {
    if (_closed) return;
    final source = videoDisplaySize(video.player.state.videoParams);
    if (source == null) return;
    final desired = _target ?? source;
    final width = desired.width.round();
    final height = desired.height.round();
    if (width < 1 || height < 1 || width > 16384 || height > 16384) return;
    if (video.rect.value?.size == desired) return;
    final handle = await video.player.handle;
    if (_closed) return;
    await _channel.invokeMethod<void>('VideoOutputManager.SetSurfaceSize', {
      'handle': handle.toString(),
      'width': width.toString(),
      'height': height.toString(),
    });
    if (_closed) return;
    video.rect.value = Rect.fromLTWH(0, 0, width.toDouble(), height.toDouble());
  }

  Future<void> close() async {
    _closed = true;
    _windowsResizeTimer?.cancel();
    _windowsResizeTimer = null;
    try {
      await _windowsResizeFuture;
    } catch (_) {
      // Closing the adapter must not propagate a late native resize failure.
    }
    await _initialization;
    await _subscription?.cancel();
  }
}
