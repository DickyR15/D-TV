import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/gestures.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';
import 'package:media_kit/media_kit.dart';

import 'app_layout.dart';
import 'player_gesture_policy.dart';
import 'widgets.dart';

const double _playerHoldBoostSpeed = 2.0;

const Duration _playerHoldSeekInterval = Duration(milliseconds: 350);

int playerHoldSeekMultiplier(Duration elapsed) {
  if (elapsed >= const Duration(seconds: 3)) return 8;
  if (elapsed >= const Duration(milliseconds: 1500)) return 4;
  return 2;
}

Duration playerHoldSeekStep(Duration elapsed) => Duration(
  milliseconds: _playerHoldSeekInterval.inMilliseconds *
      playerHoldSeekMultiplier(elapsed),
);

Duration playerHoldSeekTarget({
  required Duration current,
  required Duration duration,
  required int direction,
  required Duration step,
}) {
  if (duration <= Duration.zero) return Duration.zero;
  final sign = direction < 0 ? -1 : 1;
  final milliseconds = (current.inMilliseconds + sign * step.inMilliseconds)
      .clamp(0, duration.inMilliseconds)
      .toInt();
  return Duration(milliseconds: milliseconds);
}

enum SwipeAction { none, brightness, volume, episode, zoom }

class GestureHudState {
  const GestureHudState({this.type = SwipeAction.none, this.value = 0.0});
  final SwipeAction type;
  final double value; // 0.0 ~ 1.0
}

class PlayerInteractions extends ChangeNotifier {
  PlayerInteractions({
    required this.player,
    required this.available,
    required this.baseSpeed,
    required this.onTogglePlayback,
    required this.onFullscreen,
    required this.onEpisode,
    this.onSeek,
  }) {
    _playing = player.stream.playing.listen((playing) {
      if (!playing) cancel();
    });
    AppDevice.getBrightness()
        .then((val) {
          if (!_disposed) {
            _brightness = val;
            notifyListeners();
          }
        })
        .catchError((_) {});
  }

  final Player player;
  final bool Function() available;
  final double Function() baseSpeed;
  final VoidCallback onTogglePlayback;
  final VoidCallback onFullscreen;
  final String Function(int direction) onEpisode;
  final Future<void> Function(Duration)? onSeek;
  late final StreamSubscription<bool> _playing;
  Timer? _holdTimer;
  Timer? _seekHoldTimer;
  DateTime? _seekHoldStarted;
  Duration _seekHoldTarget = Duration.zero;
  Duration? _seekHoldPendingTarget;
  int _seekHoldDirection = 0;
  bool _seekHoldRunning = false;
  Future<void> _seekHoldWork = Future<void>.value();
  Timer? _hintTimer;
  Future<void> _rates = Future<void>.value();
  final Set<int> _pointers = {};
  final Map<int, Offset> _pointerPositions = {};
  int? _pointer;
  Offset? _origin;
  Offset? _lastPosition;
  bool _pinching = false;
  double _pinchStartDistance = 0.0;
  double _pinchStartScale = 1.0;
  double _zoomScale = 1.0;
  Duration _started = Duration.zero;
  double _swipeThreshold = 70;
  bool _swipeEnabled = false;
  bool _moved = false;
  bool _held = false;
  bool _boosting = false;
  bool _keyboardHold = false;
  bool _cancelUntilRelease = false;
  bool _disposed = false;
  double _unmutedVolume = 100;
  String _feedback = '';
  DateTime _ignoreTapUntil = DateTime(2000);
  SwipeAction _swipeAction = SwipeAction.none;
  GestureHudState _hudState = const GestureHudState();
  double _brightness = 0.5;
  double _initialBrightness = 0.5;
  double _initialVolume = 100.0;
  double _viewHeight = 0.0;
  bool _brightnessReady = false, _brightnessLoading = false;
  bool _volumeReady = false;
  Timer? _hudTimer;

  GestureHudState get hudState => _hudState;
  double get brightness => _brightness;
  double get zoomScale => _zoomScale;
  bool get isBrightnessActive => _hudState.type == SwipeAction.brightness;

  void setBrightnessDirect(double value) {
    if (_disposed) return;
    _brightness = value.clamp(0.01, 1.0);
    AppDevice.setBrightness(_brightness);
    _showHud(SwipeAction.brightness, _brightness);
  }

  void dismissBrightnessHud() {
    _scheduleDismissHud();
  }

  String get feedback => _feedback;
  bool get boosting => _boosting;
  bool get suppressTap => DateTime.now().isBefore(_ignoreTapUntil);
  Future<void> get pendingRates => _rates;
  Future<void> get pendingSeeks => _seekHoldWork;

  void hint(String message, {bool persistent = false}) {
    if (_disposed) return;
    _hintTimer?.cancel();
    if (_feedback != message) {
      _feedback = message;
      notifyListeners();
    }
    if (!persistent && message.isNotEmpty) {
      _hintTimer = Timer(const Duration(milliseconds: 1200), () {
        hint(_boosting ? '${_playerHoldBoostSpeed.toStringAsFixed(0)} 倍速播放中 · 鬆開恢復' : '', persistent: true);
      });
    }
  }

  Future<void> applySpeed() => _setRate(baseSpeed());

  Future<void> _setRate(double value) {
    _rates = _rates
        .catchError((Object _) {})
        .then((_) => player.setRate(value));
    unawaited(
      _rates.catchError((Object _) {
        hint('倍速調整失敗，請重試');
      }),
    );
    return _rates;
  }

  void _beginHold({bool keyboard = false}) {
    if (!available() || _holdTimer != null || _boosting) return;
    _keyboardHold = keyboard;
    _holdTimer = Timer(const Duration(milliseconds: 350), () {
      _holdTimer = null;
      if (_disposed ||
          !available() ||
          !player.state.playing ||
          player.state.completed) {
        return;
      }
      _boosting = true;
      _held = true;
      unawaited(_setRate(_playerHoldBoostSpeed));
      hint('${_playerHoldBoostSpeed.toStringAsFixed(0)} 倍速播放中 · 鬆開恢復', persistent: true);
    });
  }

  void _endHold({bool tap = false, bool silent = false}) {
    final wasKeyboard = _keyboardHold;
    final boosted = _boosting;
    _holdTimer?.cancel();
    _holdTimer = null;
    _keyboardHold = false;
    _boosting = false;
    if (boosted) {
      unawaited(_setRate(baseSpeed()));
      if (!silent) hint('恢復 ${baseSpeed()} 倍速');
    } else if (tap && wasKeyboard) {
      seek(5);
    }
  }

  void beginSeekHold(int direction) {
    final duration = player.state.duration;
    if (_disposed || !available() || direction == 0 || duration <= Duration.zero) {
      return;
    }
    endSeekHold(silent: true, discardPending: true);
    _endHold(silent: true);
    _seekHoldDirection = direction < 0 ? -1 : 1;
    _seekHoldStarted = DateTime.now();
    _seekHoldTarget = player.state.position;
    _held = true;
    _ignoreTapUntil = DateTime.now().add(const Duration(milliseconds: 600));
    _seekHoldTimer = Timer.periodic(_playerHoldSeekInterval, (_) {
      _tickSeekHold();
    });
    final label = _seekHoldDirection > 0 ? '快進' : '快退';
    hint(label + ' 2× · 鬆開停止', persistent: true);
  }

  void endSeekHold({bool silent = false, bool discardPending = false}) {
    if (_seekHoldDirection == 0 && _seekHoldTimer == null) return;
    final direction = _seekHoldDirection;
    _seekHoldTimer?.cancel();
    _seekHoldTimer = null;
    _seekHoldDirection = 0;
    _seekHoldStarted = null;
    if (discardPending) _seekHoldPendingTarget = null;
    if (!silent && direction != 0) {
      hint(direction > 0 ? '已停止快進' : '已停止快退');
    }
  }

  void _tickSeekHold() {
    final started = _seekHoldStarted;
    final direction = _seekHoldDirection;
    final duration = player.state.duration;
    if (_disposed || direction == 0 || started == null || !available() ||
        duration <= Duration.zero) {
      endSeekHold(silent: true, discardPending: true);
      return;
    }
    final elapsed = DateTime.now().difference(started);
    final multiplier = playerHoldSeekMultiplier(elapsed);
    final target = playerHoldSeekTarget(
      current: _seekHoldTarget,
      duration: duration,
      direction: direction,
      step: playerHoldSeekStep(elapsed),
    );
    if (target == _seekHoldTarget) {
      endSeekHold();
      return;
    }
    _seekHoldTarget = target;
    _seekHoldPendingTarget = target;
    _scheduleSeekHold();
    final label = direction > 0 ? '快進' : '快退';
    hint(
      label + ' ' + multiplier.toString() + '× · ' +
          formatPosition(target.inMilliseconds / 1000) + ' · 鬆開停止',
      persistent: true,
    );
  }

  void _scheduleSeekHold() {
    if (_disposed || _seekHoldRunning || _seekHoldPendingTarget == null) return;
    _seekHoldRunning = true;
    _seekHoldWork = () async {
      while (!_disposed && _seekHoldPendingTarget != null) {
        final target = _seekHoldPendingTarget!;
        _seekHoldPendingTarget = null;
        try {
          await (onSeek ?? player.seek)(target);
        } catch (_) {
          _seekHoldTimer?.cancel();
          _seekHoldTimer = null;
          _seekHoldDirection = 0;
          _seekHoldStarted = null;
          _seekHoldPendingTarget = null;
          if (!_disposed) hint('快轉定位失敗，請放開後重試');
          break;
        }
      }
      _seekHoldRunning = false;
      if (!_disposed && _seekHoldPendingTarget != null) {
        _scheduleSeekHold();
      }
    }();
    unawaited(_seekHoldWork);
  }

  void cancel() {
    if (_disposed) return;
    if (_pointers.isNotEmpty) {
      _cancelUntilRelease = true;
      _ignoreTapUntil = DateTime.now().add(const Duration(milliseconds: 600));
    }
    _pinching = false;
    _pointerPositions.clear();
    _pointer = null;
    _origin = null;
    _lastPosition = null;
    _volumeReady = false;
    endSeekHold(silent: true, discardPending: true);
    _endHold(silent: true);
    hint('');
  }

  void pointerDown(
    PointerDownEvent event, {
    required bool swipeEnabled,
    double width = 0.0,
    required double height,
    double bottomEdgeExclusion = 0.0,
  }) {
    _pointers.add(event.pointer);
    _pointerPositions[event.pointer] = event.localPosition;

    if (_pointers.length == 2 &&
        !_cancelUntilRelease &&
        swipeEnabled &&
        available() &&
        event.kind == PointerDeviceKind.touch) {
      _beginPinch();
      return;
    }
    if (_pointers.length != 1 || _cancelUntilRelease) {
      cancel();
      return;
    }
    if (!available() || event.buttons != kPrimaryButton) return;
    _pointer = event.pointer;
    _origin = _lastPosition = event.localPosition;
    _started = event.timeStamp;
    // Keep the iOS Home-indicator / Android system-gesture edge clear.
    final swipeableHeight = math.max(0.0, height - bottomEdgeExclusion);
    _swipeEnabled = swipeEnabled &&
        event.kind == PointerDeviceKind.touch &&
        event.localPosition.dy < swipeableHeight;
    _swipeThreshold = math.max(30, math.min(80, height * .08));
    _viewHeight = height;
    _moved = _held = false;
    _brightnessReady = false;
    _brightnessLoading = false;
    _volumeReady = false;
    _swipeAction = SwipeAction.none;

    if (_swipeEnabled && width > 0) {
      // Match the upstream player: left half adjusts brightness, right half volume.
      _swipeAction = event.localPosition.dx < width / 2
          ? SwipeAction.brightness
          : SwipeAction.volume;
      if (_swipeAction == SwipeAction.volume) {
        _initialVolume = player.state.volume.clamp(0.0, 100.0);
        _volumeReady = true;
      }
    }
  }

  void pointerMove(PointerMoveEvent event) {
    _pointerPositions[event.pointer] = event.localPosition;
    if (_pinching) {
      _updatePinchZoom();
      return;
    }
    if (_cancelUntilRelease || _pointer != event.pointer || _origin == null) {
      return;
    }
    _lastPosition = event.localPosition;
    if (_seekHoldDirection != 0) return;
    final diff = event.localPosition - _origin!;
    if (diff.distance > 12) {
      _moved = true;
      _endHold();
    }
    if (!_swipeEnabled || !_moved || _viewHeight <= 0) return;

    final isVertical = diff.dy.abs() >= _swipeThreshold &&
        diff.dy.abs() > diff.dx.abs() * 1.5;
    if (_swipeAction == SwipeAction.volume) {
      if (_volumeReady && isVertical) _applyVolumeFromLatestPosition();
      return;
    }
    if (_swipeAction != SwipeAction.brightness) return;
    if (!_brightnessReady) {
      // Read the system brightness only after a deliberate vertical swipe.
      if (!isVertical || _brightnessLoading) return;
      _brightnessLoading = true;
      final pointer = event.pointer;
      unawaited(
        AppDevice.getBrightness()
            .then((value) {
              if (_pointer != pointer ||
                  !_moved ||
                  _swipeAction != SwipeAction.brightness) {
                return;
              }
              _initialBrightness = _brightness = value;
              _brightnessReady = true;
              _applyBrightnessFromLatestPosition();
            })
            .catchError((Object _) {})
            .whenComplete(() {
              if (_pointer == pointer) _brightnessLoading = false;
            }),
      );
      return;
    }
    _applyBrightnessFromLatestPosition();
  }

  void _applyBrightnessFromLatestPosition() {
    final origin = _origin, latest = _lastPosition;
    if (origin == null || latest == null || _viewHeight <= 0) return;
    _brightness = playerVerticalSwipeValue(
      initialValue: _initialBrightness,
      originY: origin.dy,
      currentY: latest.dy,
      height: _viewHeight,
      maximumValue: 1.0,
    ).clamp(0.01, 1.0);
    AppDevice.setBrightness(_brightness);
    _showHud(SwipeAction.brightness, _brightness);
  }

  void _applyVolumeFromLatestPosition() {
    final origin = _origin, latest = _lastPosition;
    if (origin == null || latest == null || _viewHeight <= 0) return;
    final volume = playerVerticalSwipeValue(
      initialValue: _initialVolume,
      originY: origin.dy,
      currentY: latest.dy,
      height: _viewHeight,
      maximumValue: 100.0,
    );
    if ((volume - player.state.volume).abs() < 0.5) return;
    unawaited(player.setVolume(volume));
    _showHud(SwipeAction.volume, volume / 100.0);
  }

  void _beginPinch() {
    final positions = _pointerPositions.values.toList(growable: false);
    if (positions.length < 2) return;
    final distance = (positions[0] - positions[1]).distance;
    if (!distance.isFinite || distance <= 0) return;

    _pinching = true;
    _pinchStartDistance = distance;
    _pinchStartScale = _zoomScale;
    _pointer = null;
    _origin = null;
    _lastPosition = null;
    _moved = true;
    _held = true;
    _brightnessReady = false;
    _brightnessLoading = false;
    _volumeReady = false;
    _swipeAction = SwipeAction.none;
    _holdTimer?.cancel();
    _holdTimer = null;
    _endHold(silent: true);
    endSeekHold(silent: true, discardPending: true);
    _ignoreTapUntil = DateTime.now().add(const Duration(milliseconds: 700));
  }

  void _updatePinchZoom() {
    if (!_pinching || _pointerPositions.length < 2) return;
    final positions = _pointerPositions.values.toList(growable: false);
    final distance = (positions[0] - positions[1]).distance;
    final next = playerPinchZoomScale(
      initialScale: _pinchStartScale,
      initialDistance: _pinchStartDistance,
      currentDistance: distance,
    );
    if ((next - _zoomScale).abs() < 0.005) return;
    _zoomScale = next;
    _showHud(SwipeAction.zoom, _zoomScale);
  }

  void resetZoom() {
    if (_disposed || (_zoomScale - 1.0).abs() < 0.005) return;
    _zoomScale = 1.0;
    _showHud(SwipeAction.zoom, _zoomScale);
    _scheduleDismissHud();
  }

  void pointerUp(PointerUpEvent event) {
    _pointers.remove(event.pointer);
    _pointerPositions.remove(event.pointer);

    if (_pinching) {
      _pinching = false;
      _cancelUntilRelease = _pointers.isNotEmpty;
      _ignoreTapUntil = DateTime.now().add(const Duration(milliseconds: 700));
      if (_pointers.isEmpty) _scheduleDismissHud();
      return;
    }
    if (_cancelUntilRelease) {
      _ignoreTapUntil = DateTime.now().add(const Duration(milliseconds: 600));
      if (_pointers.isEmpty) {
        _cancelUntilRelease = false;
        _pointerPositions.clear();
      }
      return;
    }
    if (_pointer != event.pointer || _origin == null) return;
    final delta = (_lastPosition ?? event.localPosition) - _origin!;
    final isVertical =
        delta.dy.abs() >= _swipeThreshold &&
        delta.dy.abs() > delta.dx.abs() * 1.5;

    if (_swipeAction == SwipeAction.episode &&
        _swipeEnabled &&
        !_held &&
        _moved &&
        isVertical &&
        event.timeStamp - _started < const Duration(milliseconds: 1500)) {
      if (available()) hint(onEpisode(delta.dy < 0 ? 1 : -1));
    } else if ((_swipeAction == SwipeAction.brightness && _brightnessReady) ||
        (_swipeAction == SwipeAction.volume && _volumeReady && isVertical)) {
      _scheduleDismissHud();
    }

    if (_moved || _held) {
      _ignoreTapUntil = DateTime.now().add(const Duration(milliseconds: 600));
    }
    _pointer = null;
    _origin = null;
    _brightnessReady = false;
    _brightnessLoading = false;
    _volumeReady = false;
    _endHold();
    endSeekHold();
  }

  void pointerCancel(PointerCancelEvent event) {
    _pointers.remove(event.pointer);
    _pointerPositions.remove(event.pointer);
    cancel();
    _brightnessReady = false;
    _brightnessLoading = false;
    _volumeReady = false;
    _ignoreTapUntil = DateTime.now().add(const Duration(milliseconds: 700));
    if (_pointers.isEmpty) _cancelUntilRelease = false;
  }

  void _showHud(SwipeAction action, double value) {
    if (_disposed) return;
    _hudTimer?.cancel();
    _hudState = GestureHudState(type: action, value: value);
    notifyListeners();
  }

  void _scheduleDismissHud() {
    _hudTimer?.cancel();
    _hudTimer = Timer(const Duration(milliseconds: 1000), () {
      if (_disposed) return;
      _hudState = const GestureHudState();
      notifyListeners();
    });
  }

  void seek(int seconds) {
    if (!available() || player.state.duration <= Duration.zero) return;
    endSeekHold(silent: true, discardPending: true);
    _endHold();
    final target = (player.state.position.inMilliseconds + seconds * 1000)
        .clamp(0, player.state.duration.inMilliseconds);
    unawaited((onSeek ?? player.seek)(Duration(milliseconds: target)));
    hint('${seconds > 0 ? '快進至' : '後退至'} ${formatPosition(target / 1000)}');
  }

  void changeVolume(double delta) {
    if (!available()) return;
    final volume = (player.state.volume + delta).clamp(0.0, 100.0);
    unawaited(player.setVolume(volume));
    if (volume > 0) _unmutedVolume = volume;
    hint(volume == 0 ? '已靜音' : '音量 ${volume.round()}%');
  }

  void toggleMute() {
    if (!available()) return;
    final current = player.state.volume;
    if (current > 0) _unmutedVolume = current;
    final target = current > 0 ? 0.0 : _unmutedVolume;
    unawaited(player.setVolume(target));
    hint(target == 0 ? '已靜音' : '音量 ${target.round()}%');
  }

  KeyEventResult key(KeyEvent event) {
    final key = event.logicalKey;
    if (event is KeyUpEvent) {
      if (key == LogicalKeyboardKey.arrowRight && _keyboardHold) {
        _endHold(tap: available());
        return KeyEventResult.handled;
      }
      return KeyEventResult.ignored;
    }
    final hardware = HardwareKeyboard.instance;
    if (hardware.isAltPressed ||
        hardware.isMetaPressed ||
        hardware.isShiftPressed) {
      cancel();
      return KeyEventResult.ignored;
    }
    if (key == LogicalKeyboardKey.f11 || key == LogicalKeyboardKey.keyF) {
      if (event is KeyDownEvent) {
        cancel();
        onFullscreen();
      }
      return KeyEventResult.handled;
    }
    if (hardware.isControlPressed || !available()) {
      return KeyEventResult.ignored;
    }
    if (key != LogicalKeyboardKey.arrowRight) _endHold();
    if (key == LogicalKeyboardKey.arrowRight) {
      if (event is KeyDownEvent) _beginHold(keyboard: true);
    } else if (key == LogicalKeyboardKey.arrowLeft) {
      seek(-5);
    } else if (key == LogicalKeyboardKey.arrowUp) {
      changeVolume(5);
    } else if (key == LogicalKeyboardKey.arrowDown) {
      changeVolume(-5);
    } else if (key == LogicalKeyboardKey.space ||
        key == LogicalKeyboardKey.mediaPlayPause) {
      if (event is KeyDownEvent) onTogglePlayback();
    } else if (key == LogicalKeyboardKey.keyM) {
      if (event is KeyDownEvent) toggleMute();
    } else if (key == LogicalKeyboardKey.mediaTrackNext ||
        key == LogicalKeyboardKey.mediaTrackPrevious) {
      if (event is KeyDownEvent) {
        hint(onEpisode(key == LogicalKeyboardKey.mediaTrackNext ? 1 : -1));
      }
    } else {
      return KeyEventResult.ignored;
    }
    return KeyEventResult.handled;
  }

  @override
  void dispose() {
    _disposed = true;
    _holdTimer?.cancel();
    _seekHoldTimer?.cancel();
    _seekHoldDirection = 0;
    _seekHoldPendingTarget = null;
    _hintTimer?.cancel();
    _hudTimer?.cancel();
    AppDevice.resetBrightness(); // 離開播放器時自動恢復手機/平板系統預設亮度
    if (_boosting) unawaited(_setRate(baseSpeed()));
    _boosting = false;
    _playing.cancel();
    _pointers.clear();
    _pointerPositions.clear();
    _pinching = false;
    super.dispose();
  }
}
