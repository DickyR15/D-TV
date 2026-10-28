import 'dart:async';
import 'dart:io';
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:media_kit/media_kit.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

import 'video_enhancement_mpv.dart';

/// Captures MPV's current video frame for a decorative, blurred side fill.
///
/// This widget deliberately does not create a second VideoController. If the
/// active MPV output cannot capture a valid frame, it leaves the normal video
/// surface untouched and stops retrying after three consecutive failures.
class PortraitBlurBackground extends StatefulWidget {
  const PortraitBlurBackground({
    super.key,
    required this.player,
    required this.active,
    required this.onDiagnostic,
    required this.child,
  });

  final Player player;
  final bool active;
  final ValueChanged<String> onDiagnostic;
  final Widget child;

  @override
  State<PortraitBlurBackground> createState() => _PortraitBlurBackgroundState();
}

class _PortraitBlurBackgroundState extends State<PortraitBlurBackground> {
  static const _captureInterval = Duration(milliseconds: 1800);
  static const _fileWaitInterval = Duration(milliseconds: 50);
  static const _fileWaitAttempts = 10;

  VideoEnhancementMpv? _mpv;
  Directory? _directory;
  Timer? _timer;
  Uint8List? _frame;
  int _sequence = 0;
  int _consecutiveFailures = 0;
  bool _starting = false;
  bool _capturing = false;
  bool _disabledForSession = false;
  bool _reportedFailure = false;
  bool _timingsRegistered = false;
  int _successfulCaptures = 0;
  Duration _maxBuildDuration = Duration.zero;
  Duration _maxRasterDuration = Duration.zero;

  @override
  void initState() {
    super.initState();
    if (widget.active) {
      _registerTimings(true);
      unawaited(_startCapture());
    }
  }

  @override
  void didUpdateWidget(covariant PortraitBlurBackground oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.player != widget.player || oldWidget.active != widget.active) {
      if (!widget.active || oldWidget.player != widget.player) {
        _registerTimings(false);
        _stopCapture(clearFrame: true);
      }
      if (widget.active) {
        _registerTimings(true);
        unawaited(_startCapture());
      }
    }
  }

  void _registerTimings(bool enabled) {
    if (enabled && !_timingsRegistered) {
      SchedulerBinding.instance.addTimingsCallback(_onFrameTimings);
      _timingsRegistered = true;
    } else if (!enabled && _timingsRegistered) {
      SchedulerBinding.instance.removeTimingsCallback(_onFrameTimings);
      _timingsRegistered = false;
    }
  }

  void _onFrameTimings(List<FrameTiming> timings) {
    if (!widget.active || _frame == null) return;
    for (final timing in timings) {
      if (timing.buildDuration > _maxBuildDuration) {
        _maxBuildDuration = timing.buildDuration;
      }
      if (timing.rasterDuration > _maxRasterDuration) {
        _maxRasterDuration = timing.rasterDuration;
      }
    }
  }

  Future<void> _startCapture() async {
    if (!mounted ||
        !widget.active ||
        _starting ||
        _timer != null ||
        _disabledForSession) {
      return;
    }
    _starting = true;
    VideoEnhancementMpv? candidate;
    try {
      final platform = widget.player.platform;
      if (platform is! NativePlayer) {
        throw StateError('目前播放器沒有可用的原生 MPV 介面');
      }
      candidate = VideoEnhancementMpv(platform);
      await candidate.initialize();
      final temporary = await getTemporaryDirectory();
      final directory = Directory(p.join(temporary.path, 'dtv-portrait-blur'));
      await directory.create(recursive: true);
      if (!mounted || !widget.active) {
        candidate.closed = true;
        return;
      }
      _mpv = candidate;
      _directory = directory;
      _timer = Timer.periodic(
        _captureInterval,
        (_) => unawaited(_capture()),
      );
      await _capture();
    } catch (error) {
      candidate?.closed = true;
      _recordFailure('初始化失敗：$error');
    } finally {
      _starting = false;
    }
  }

  void _stopCapture({required bool clearFrame}) {
    _timer?.cancel();
    _timer = null;
    _registerTimings(false);
    _mpv?.closed = true;
    _mpv = null;
    _consecutiveFailures = 0;
    _disabledForSession = false;
    _reportedFailure = false;
    if (clearFrame && _frame != null && mounted) {
      setState(() => _frame = null);
    } else if (clearFrame) {
      _frame = null;
    }
  }

  Future<void> _capture() async {
    final mpv = _mpv;
    final directory = _directory;
    if (!mounted ||
        !widget.active ||
        mpv == null ||
        directory == null ||
        _capturing ||
        _disabledForSession ||
        !widget.player.state.playing) {
      return;
    }
    _capturing = true;
    final captureWatch = Stopwatch()..start();
    final file = File(
      p.join(directory.path, 'frame_' + (++_sequence).toString() + '.jpg'),
    );
    try {
      mpv.command(['screenshot-to-file', file.path, 'video']);
      final bytes = await _readStableScreenshot(file);
      if (bytes == null) {
        throw StateError('MPV 截圖沒有產生有效 JPEG 檔案');
      }
      if (!await _isVisibleFrame(bytes)) {
        throw StateError('MPV 截圖影格為黑畫面或無法解碼');
      }
      if (!mounted || !widget.active) return;
      _consecutiveFailures = 0;
      _reportedFailure = false;
      _successfulCaptures++;
      setState(() => _frame = bytes);
      if (_successfulCaptures == 1 || _successfulCaptures % 10 == 0) {
        widget.onDiagnostic(
          '影格擷取成功 #$_successfulCaptures：JPEG ${bytes.length} bytes；擷取／解碼 ${captureWatch.elapsedMilliseconds} ms；Flutter build 最大 ${_maxBuildDuration.inMilliseconds} ms；raster 最大 ${_maxRasterDuration.inMilliseconds} ms',
        );
        _maxBuildDuration = Duration.zero;
        _maxRasterDuration = Duration.zero;
      }
    } catch (error) {
      _recordFailure('影格擷取失敗：$error');
    } finally {
      try {
        if (await file.exists()) await file.delete();
      } catch (_) {}
      _capturing = false;
    }
  }

  Future<Uint8List?> _readStableScreenshot(File file) async {
    int? previousLength;
    for (var attempt = 0; attempt < _fileWaitAttempts; attempt++) {
      if (await file.exists()) {
        final length = await file.length();
        if (length > 1024 && length == previousLength) {
          final bytes = await file.readAsBytes();
          if (bytes.length > 1024 &&
              bytes.length >= 2 &&
              bytes[0] == 0xFF &&
              bytes[1] == 0xD8) {
            return bytes;
          }
        }
        previousLength = length;
      }
      await Future<void>.delayed(_fileWaitInterval);
    }
    return null;
  }

  Future<bool> _isVisibleFrame(Uint8List bytes) async {
    ui.Codec? codec;
    ui.Image? image;
    try {
      codec = await ui.instantiateImageCodec(
        bytes,
        targetWidth: 32,
        targetHeight: 32,
      );
      final frame = await codec.getNextFrame();
      image = frame.image;
      final data = await image.toByteData(format: ui.ImageByteFormat.rawRgba);
      if (data == null || data.lengthInBytes < 32 * 32 * 4) return false;
      var maximumChannel = 0;
      for (var offset = 0; offset < data.lengthInBytes; offset += 4) {
        for (var channel = 0; channel < 3; channel++) {
          final value = data.getUint8(offset + channel);
          if (value > maximumChannel) maximumChannel = value;
          if (maximumChannel > 8) return true;
        }
      }
      return false;
    } catch (_) {
      return false;
    } finally {
      image?.dispose();
      codec?.dispose();
    }
  }

  void _recordFailure(String message) {
    _consecutiveFailures++;
    if (!_reportedFailure) {
      _reportedFailure = true;
      widget.onDiagnostic('$message；模糊背景暫不顯示，原畫播放保持不變');
    }
    if (_consecutiveFailures >= 3) {
      _disabledForSession = true;
      _timer?.cancel();
      _timer = null;
      _registerTimings(false);
      if (_frame != null && mounted) setState(() => _frame = null);
      widget.onDiagnostic('連續三次無法取得有效影格，已停用本次模糊背景擷取');
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    _registerTimings(false);
    _mpv?.closed = true;
    final directory = _directory;
    if (directory != null) {
      unawaited(directory.delete(recursive: true).then<void>((_) {}).catchError((_) {}));
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final frame = _frame;
    return Stack(
      fit: StackFit.expand,
      children: [
        if (widget.active && frame != null)
          Positioned.fill(
            child: ClipRect(
              child: ImageFiltered(
                imageFilter: ui.ImageFilter.blur(sigmaX: 24, sigmaY: 24),
                child: Transform.scale(
                  scale: 1.25,
                  child: Image.memory(
                    frame,
                    fit: BoxFit.cover,
                    gaplessPlayback: true,
                    filterQuality: FilterQuality.low,
                  ),
                ),
              ),
            ),
          ),
        if (widget.active && frame != null)
          Positioned.fill(
            child: ColoredBox(color: Colors.black.withValues(alpha: .32)),
          ),
        widget.child,
      ],
    );
  }
}
