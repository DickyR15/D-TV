import 'dart:ui';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/desktop_window_state.dart';

void main() {
  test('F11 is registered as the desktop full-screen shortcut', () {
    const key = SingleActivator(
      LogicalKeyboardKey.f11,
      includeRepeats: false,
    );
    expect(desktopRootShortcuts[key], isA<ToggleDesktopFullscreenIntent>());
  });

  test('desktop window geometry round-trips with maximize state', () {
    const original = DesktopWindowState(
      left: -1280,
      top: 48,
      width: 1720,
      height: 980,
      maximized: true,
    );

    final restored = DesktopWindowState.tryParse(original.encode());
    expect(restored, isNotNull);
    expect(restored!.left, original.left);
    expect(restored.top, original.top);
    expect(restored.width, original.width);
    expect(restored.height, original.height);
    expect(restored.maximized, isTrue);
    expect(restored.bounds, const Rect.fromLTWH(-1280, 48, 1720, 980));
  });

  test('invalid, corrupted and implausible desktop geometry is ignored', () {
    expect(DesktopWindowState.tryParse(null), isNull);
    expect(DesktopWindowState.tryParse('{broken'), isNull);
    expect(
      DesktopWindowState.tryParse(
        '{"left":0,"top":0,"width":400,"height":300,"maximized":false}',
      ),
      isNull,
    );
    expect(
      DesktopWindowState.tryParse(
        '{"left":999999,"top":0,"width":1280,"height":800,"maximized":false}',
      ),
      isNull,
    );
  });

  test('normal window bounds remain separate from maximized state', () {
    final normal = DesktopWindowState.fromRect(
      const Rect.fromLTWH(120, 80, 1440, 900),
    );
    expect(normal, isNotNull);
    final maximized = normal!.copyWith(maximized: true);
    expect(maximized.bounds, normal.bounds);
    expect(maximized.maximized, isTrue);
  });
}
