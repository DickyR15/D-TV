import 'dart:async';
import 'dart:math' as math;
import 'dart:typed_data';

import 'package:audioplayers/audioplayers.dart';
import 'package:flutter/material.dart';

/// D TV's one-time cold-start brand reveal. The real app is built underneath
/// this overlay, so data preparation continues while the 2.85-second identity
/// animation and its original sound signature play.
class DtvLaunchAnimation extends StatefulWidget {
  const DtvLaunchAnimation({super.key, this.onCompleted});

  final VoidCallback? onCompleted;

  @override
  State<DtvLaunchAnimation> createState() => _DtvLaunchAnimationState();
}

class _DtvLaunchAnimationState extends State<DtvLaunchAnimation>
    with TickerProviderStateMixin, WidgetsBindingObserver {
  static const _soundDuration = 2.85;
  late final AudioPlayer _audioPlayer;
  late final AnimationController _intro = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 2420),
  );
  late final AnimationController _fade = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 430),
  );
  late final Listenable _animations = Listenable.merge([_intro, _fade]);

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _audioPlayer = AudioPlayer();
    _intro.addStatusListener((status) {
      if (status == AnimationStatus.completed && mounted) {
        unawaited(_fade.forward());
      }
    });
    _fade.addStatusListener((status) {
      if (status == AnimationStatus.completed && mounted) {
        widget.onCompleted?.call();
      }
    });
    // Audio is started only when this cold-start overlay is first inserted.
    // Resuming the app never recreates or replays the brand signature.
    unawaited(_playBrandSound());
    unawaited(_intro.forward());
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state != AppLifecycleState.resumed) {
      // Do not let the startup sting continue in the background. It is not
      // replayed on resume because the animation state remains mounted.
      unawaited(_stopBrandSound());
    }
  }

  Future<void> _playBrandSound() async {
    try {
      await _audioPlayer.setVolume(0.86);
      await _audioPlayer.play(
        BytesSource(_buildBrandWave(), mimeType: 'audio/wav'),
      );
    } catch (_) {
      // The visual reveal must remain reliable even when audio is unavailable
      // or the device has no audio output.
    }
  }

  Future<void> _stopBrandSound() async {
    try {
      await _audioPlayer.stop();
    } catch (_) {
      // Audio teardown is best-effort and must not affect app lifecycle.
    }
  }

  /// Generates the approved three-note cinematic signature as a small PCM WAV
  /// in memory, avoiding an extra asset download or filesystem dependency.
  Uint8List _buildBrandWave() {
    const sampleRate = 24000;
    final sampleCount = (sampleRate * _soundDuration).round();
    final dataBytes = sampleCount * 2;
    final bytes = Uint8List(44 + dataBytes);
    final header = ByteData.sublistView(bytes);
    header.setUint32(0, 0x46464952, Endian.little); // RIFF
    header.setUint32(4, 36 + dataBytes, Endian.little);
    header.setUint32(8, 0x45564157, Endian.little); // WAVE
    header.setUint32(12, 0x20746d66, Endian.little); // fmt
    header.setUint32(16, 16, Endian.little);
    header.setUint16(20, 1, Endian.little); // PCM
    header.setUint16(22, 1, Endian.little); // mono
    header.setUint32(24, sampleRate, Endian.little);
    header.setUint32(28, sampleRate * 2, Endian.little);
    header.setUint16(32, 2, Endian.little);
    header.setUint16(34, 16, Endian.little);
    header.setUint32(36, 0x61746164, Endian.little); // data
    header.setUint32(40, dataBytes, Endian.little);

    double bell(double time, double start, double frequency, double amplitude) {
      final x = time - start;
      if (x < 0 || x > 1.55) return 0;
      final attack = math.min(1.0, x / 0.012);
      final envelope = attack * math.exp(-4.25 * x);
      final fundamental = math.sin(2 * math.pi * frequency * x);
      final second = 0.28 * math.sin(2 * math.pi * frequency * 2 * x + 0.12);
      final third = 0.105 * math.sin(2 * math.pi * frequency * 3.01 * x + 0.24);
      final fourth = 0.035 * math.sin(2 * math.pi * frequency * 4.18 * x + 0.38);
      return amplitude * envelope * (fundamental + second + third + fourth);
    }

    double pluck(double time, double start, double frequency, double amplitude) {
      final x = time - start;
      if (x < 0 || x > 0.62) return 0;
      final envelope = (1 - math.exp(-x / 0.004)) * math.exp(-5.2 * x);
      return amplitude *
          envelope *
          (math.sin(2 * math.pi * frequency * x) +
              0.20 * math.sin(2 * math.pi * frequency * 2 * x + 0.2) +
              0.07 * math.sin(2 * math.pi * frequency * 3 * x + 0.4));
    }

    for (var index = 0; index < sampleCount; index++) {
      final time = index / sampleRate;
      var value = 0.0;

      // Fine rising light/sweep texture.
      const sweepStart = 0.12;
      const sweepLength = 1.36;
      if (time >= sweepStart && time <= sweepStart + sweepLength) {
        final x = time - sweepStart;
        final u = x / sweepLength;
        final envelope = math.pow(math.sin(math.pi * u), 1.7).toDouble();
        final phase = 2 * math.pi * (250 * x + 0.5 * 390 / sweepLength * x * x);
        value += 0.075 * envelope * math.sin(phase);
        value += 0.018 * envelope * math.sin(phase * 2.03 + 0.3);
      }

      // Original D–A–D motif: three clear bell/pluck notes.
      value += bell(time, 0.42, 587.33, 0.30);
      value += bell(time, 0.82, 880.00, 0.26);
      value += bell(time, 1.28, 1174.66, 0.46);
      value += pluck(time, 0.438, 587.33, 0.055);
      value += pluck(time, 0.838, 880.00, 0.055);
      value += pluck(time, 1.298, 1174.66, 0.075);

      // Warm D-major harmonic bed under the logo reveal.
      if (time >= 1.24) {
        final x = time - 1.24;
        const padLength = 1.55;
        final attack = math.min(1.0, x / 0.34);
        final release = math.max(0.0, math.min(1.0, (padLength - x) / 0.45));
        final envelope = math.pow(attack * release, 1.15).toDouble();
        value += envelope *
            (0.027 * math.sin(2 * math.pi * 146.83 * x) +
                0.020 * math.sin(2 * math.pi * 220.00 * x + 0.08) +
                0.020 * math.sin(2 * math.pi * 293.66 * x + 0.15) +
                0.015 * math.sin(2 * math.pi * 369.99 * x + 0.12) +
                0.012 * math.sin(2 * math.pi * 440.00 * x - 0.12));
      }

      // Short, controlled logo-arrival accent. It adds body without making
      // the whole sound effect depend on excessive sub-bass.
      final impactTime = time - 1.43;
      if (impactTime >= 0 && impactTime <= 0.72) {
        final envelope = (1 - math.exp(-impactTime / 0.009)) *
            math.exp(-7.2 * impactTime);
        value += envelope *
            (0.105 * math.sin(2 * math.pi * 146.83 * impactTime) +
                0.045 * math.sin(2 * math.pi * 293.66 * impactTime + 0.2));
      }

      value += bell(time, 1.62, 1760, 0.035);
      value += bell(time, 1.78, 2349, 0.020);

      final fadeIn = math.min(1.0, time / 0.035);
      final fadeOut = math.max(0.0, math.min(1.0, (_soundDuration - time) / 0.42));
      value *= fadeIn * fadeOut;
      // Gentle soft clipping keeps the transient controlled.
      value = (value * 1.2).clamp(-1.0, 1.0).toDouble();
      final sample = (value * 30000).round().clamp(-32768, 32767).toInt();
      header.setInt16(44 + index * 2, sample, Endian.little);
    }
    return bytes;
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _intro.dispose();
    _fade.dispose();
    unawaited(_disposeAudioPlayer());
    super.dispose();
  }

  Future<void> _disposeAudioPlayer() async {
    try {
      await _audioPlayer.dispose();
    } catch (_) {
      // Best-effort cleanup.
    }
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: _animations,
    builder: (context, _) {
      final progress = _intro.value;
      final reveal = Curves.easeOutCubic
          .transform((progress / 0.58).clamp(0.0, 1.0).toDouble());
      final wordOpacity = Curves.easeOut
          .transform(((progress - 0.24) / 0.36).clamp(0.0, 1.0).toDouble());
      final taglineOpacity = Curves.easeOut
          .transform(((progress - 0.54) / 0.28).clamp(0.0, 1.0).toDouble());
      final sweepProgress = ((progress - 0.05) / 0.78).clamp(0.0, 1.0).toDouble();

      return Opacity(
        opacity: 1 - _fade.value,
        child: ColoredBox(
          color: const Color(0xFF030303),
          child: LayoutBuilder(
            builder: (context, constraints) => Stack(
              fit: StackFit.expand,
              children: [
                DecoratedBox(
                  decoration: BoxDecoration(
                    gradient: RadialGradient(
                      center: const Alignment(0, -0.02),
                      radius: 0.78,
                      colors: [
                        const Color(0xFF5D0613).withValues(alpha: 0.20 * reveal),
                        const Color(0xFF170307).withValues(alpha: 0.07 * reveal),
                        const Color(0xFF030303),
                      ],
                      stops: const [0, 0.56, 1],
                    ),
                  ),
                ),
                Positioned.fill(
                  child: CustomPaint(
                    painter: _DtvRedLightSweepPainter(progress: sweepProgress),
                  ),
                ),
                Center(
                  child: SizedBox(
                    width: math.min(constraints.maxWidth * 0.82, 500),
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Transform.scale(
                          scale: 0.78 + 0.22 * reveal,
                          child: SizedBox(
                            width: 148,
                            height: 148,
                            child: CustomPaint(
                              painter: _DtvMetalMarkPainter(progress: reveal),
                            ),
                          ),
                        ),
                        const SizedBox(height: 4),
                        Opacity(
                          opacity: wordOpacity,
                          child: Transform.translate(
                            offset: Offset(0, 10 * (1 - wordOpacity)),
                            child: const Text(
                              'D TV',
                              textAlign: TextAlign.center,
                              style: TextStyle(
                                color: Color(0xFFF3F4F6),
                                fontSize: 39,
                                height: 1,
                                fontWeight: FontWeight.w800,
                                letterSpacing: 7,
                                shadows: [
                                  Shadow(color: Color(0xAAE30A2D), blurRadius: 22),
                                ],
                              ),
                            ),
                          ),
                        ),
                        const SizedBox(height: 16),
                        Opacity(
                          opacity: taglineOpacity,
                          child: const Text(
                            '短劇・漫劇・AI 劇  |  精彩隨選即播',
                            textAlign: TextAlign.center,
                            style: TextStyle(
                              color: Color(0xFFE0B6BB),
                              fontSize: 13,
                              fontWeight: FontWeight.w600,
                              letterSpacing: 1.1,
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      );
    },
  );
}

class _DtvRedLightSweepPainter extends CustomPainter {
  const _DtvRedLightSweepPainter({required this.progress});

  final double progress;

  @override
  void paint(Canvas canvas, Size size) {
    if (progress <= 0 || size.isEmpty) return;
    final beamWidth = math.max(240.0, size.width * 0.28);
    final head = -beamWidth + (size.width + beamWidth * 2) * progress;
    final centerY = size.height * 0.49;
    final beam = Rect.fromLTWH(head - beamWidth, centerY - 10, beamWidth + 22, 20);
    final halo = beam.inflate(20);
    canvas.drawRect(
      halo,
      Paint()
        ..color = const Color(0x44FF092F)
        ..maskFilter = const MaskFilter.blur(BlurStyle.normal, 18),
    );
    canvas.drawRect(
      beam,
      Paint()
        ..shader = const LinearGradient(
          colors: [
            Color(0x00A50018),
            Color(0x66D00024),
            Color(0xFFFF173B),
            Color(0xFFFFDDE2),
            Color(0xFFFF173B),
            Color(0x44A50018),
          ],
          stops: [0, 0.48, 0.78, 0.86, 0.92, 1],
        ).createShader(beam),
    );
    final core = Rect.fromLTWH(head - 5, centerY - 30, 8, 60);
    canvas.drawRect(
      core,
      Paint()
        ..color = const Color(0xBFFFF5F6)
        ..maskFilter = const MaskFilter.blur(BlurStyle.normal, 7),
    );
  }

  @override
  bool shouldRepaint(_DtvRedLightSweepPainter oldDelegate) =>
      oldDelegate.progress != progress;
}

class _DtvMetalMarkPainter extends CustomPainter {
  const _DtvMetalMarkPainter({required this.progress});

  final double progress;

  @override
  void paint(Canvas canvas, Size size) {
    final side = math.min(size.width, size.height);
    final dx = (size.width - side) / 2;
    final dy = (size.height - side) / 2;
    canvas.save();
    canvas.translate(dx, dy);
    canvas.scale(side / 192);

    final bounds = const Rect.fromLTWH(0, 0, 192, 192);
    final outer = Path()
      ..moveTo(34, 21)
      ..lineTo(96, 21)
      ..cubicTo(137, 21, 158, 49, 158, 96)
      ..cubicTo(158, 143, 137, 171, 96, 171)
      ..lineTo(34, 171)
      ..close();

    canvas.drawPath(
      outer,
      Paint()
        ..color = const Color(0xFFFF173B).withValues(alpha: 0.22 + 0.16 * progress)
        ..maskFilter = MaskFilter.blur(BlurStyle.normal, 5 + 5 * progress),
    );
    canvas.drawPath(
      outer,
      Paint()
        ..shader = const LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [
            Color(0xFFFFFFFF),
            Color(0xFFDDE1E6),
            Color(0xFF7F8790),
            Color(0xFFE7EBF0),
          ],
          stops: [0, 0.28, 0.65, 1],
        ).createShader(bounds),
    );

    final highlight = Path()
      ..moveTo(37, 21)
      ..lineTo(93, 21)
      ..cubicTo(108, 21, 120, 25, 130, 34)
      ..lineTo(111, 47)
      ..cubicTo(101, 40, 91, 38, 77, 38)
      ..lineTo(37, 38)
      ..close();
    canvas.drawPath(highlight, Paint()..color = Colors.white.withValues(alpha: 0.9));

    final shade = Path()
      ..moveTo(35, 147)
      ..lineTo(65, 135)
      ..lineTo(96, 135)
      ..cubicTo(119, 135, 134, 124, 143, 106)
      ..lineTo(152, 132)
      ..cubicTo(141, 159, 122, 173, 96, 173)
      ..lineTo(35, 173)
      ..close();
    canvas.drawPath(shade, Paint()..color = const Color(0xFF777F89));

    final inner = Path()
      ..moveTo(65, 51)
      ..lineTo(96, 51)
      ..cubicTo(121, 51, 136, 68, 136, 96)
      ..cubicTo(136, 124, 121, 141, 96, 141)
      ..lineTo(65, 141)
      ..close();
    canvas.drawPath(inner, Paint()..color = const Color(0xFF08090D));

    final play = Path()
      ..moveTo(77, 70)
      ..lineTo(77, 122)
      ..lineTo(119, 96)
      ..close();
    canvas.drawPath(
      play,
      Paint()
        ..color = const Color(0xFFFF183D)
        ..maskFilter = MaskFilter.blur(BlurStyle.normal, 3 + 3 * progress),
    );
    canvas.drawPath(
      play,
      Paint()
        ..shader = const LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [Color(0xFFFFCCD3), Color(0xFFFF233F), Color(0xFFC40024)],
        ).createShader(bounds),
    );

    canvas.restore();
  }

  @override
  bool shouldRepaint(_DtvMetalMarkPainter oldDelegate) =>
      oldDelegate.progress != progress;
}
