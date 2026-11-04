import 'dart:convert';
import 'dart:ui' show Rect;

import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

/// Native window geometry is device-specific and is intentionally stored
/// outside portable settings backups.
class DesktopWindowState {
  const DesktopWindowState({
    required this.left,
    required this.top,
    required this.width,
    required this.height,
    this.maximized = false,
  });

  static const preferencesKey = 'dtv.desktopWindowState.v1';

  final double left;
  final double top;
  final double width;
  final double height;
  final bool maximized;

  Rect get bounds => Rect.fromLTWH(left, top, width, height);

  bool get isValid =>
      left.isFinite &&
      top.isFinite &&
      width.isFinite &&
      height.isFinite &&
      left.abs() <= 32768 &&
      top.abs() <= 32768 &&
      width >= 900 &&
      height >= 560 &&
      width <= 16384 &&
      height <= 16384;

  static DesktopWindowState? fromRect(
    Rect bounds, {
    bool maximized = false,
  }) {
    final state = DesktopWindowState(
      left: bounds.left,
      top: bounds.top,
      width: bounds.width,
      height: bounds.height,
      maximized: maximized,
    );
    return state.isValid ? state : null;
  }

  static DesktopWindowState? tryParse(String? encoded) {
    if (encoded == null || encoded.isEmpty) return null;
    try {
      final decoded = jsonDecode(encoded);
      if (decoded is! Map) return null;
      final state = DesktopWindowState(
        left: (decoded['left'] as num).toDouble(),
        top: (decoded['top'] as num).toDouble(),
        width: (decoded['width'] as num).toDouble(),
        height: (decoded['height'] as num).toDouble(),
        maximized: decoded['maximized'] == true,
      );
      return state.isValid ? state : null;
    } catch (_) {
      return null;
    }
  }

  DesktopWindowState copyWith({bool? maximized}) => DesktopWindowState(
    left: left,
    top: top,
    width: width,
    height: height,
    maximized: maximized ?? this.maximized,
  );

  String encode() => jsonEncode({
    'left': left,
    'top': top,
    'width': width,
    'height': height,
    'maximized': maximized,
  });
}

class ToggleDesktopFullscreenIntent extends Intent {
  const ToggleDesktopFullscreenIntent();
}

const desktopRootShortcuts = <ShortcutActivator, Intent>{
  SingleActivator(
    LogicalKeyboardKey.f11,
    includeRepeats: false,
  ): ToggleDesktopFullscreenIntent(),
};
