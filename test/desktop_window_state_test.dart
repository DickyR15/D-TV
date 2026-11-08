import 'dart:ui';

import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';
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

  test('desktop window geometry round-trips as normal bounds', () {
    const original = DesktopWindowState(
      left: -1280,
      top: 48,
      width: 1720,
      height: 980,
    );

    final restored = DesktopWindowState.tryParse(original.encode());
    expect(restored, isNotNull);
    expect(restored!.left, original.left);
    expect(restored.top, original.top);
    expect(restored.width, original.width);
    expect(restored.height, original.height);
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

  test('legacy maximized flag is ignored and omitted in subsequent writes', () {
    final restored = DesktopWindowState.tryParse(
      '{"left":120,"top":80,"width":1440,"height":900,"maximized":true}',
    );
    expect(restored, isNotNull);
    expect(restored!.bounds, const Rect.fromLTWH(120, 80, 1440, 900));
    expect(restored.encode(), isNot(contains('maximized')));
  });

  test('saved geometry is discarded when no connected display overlaps it', () {
    const bounds = Rect.fromLTWH(-1800, 80, 1280, 800);
    expect(
      desktopWindowOverlapsDisplay(
        bounds,
        [const Rect.fromLTWH(0, 0, 1920, 1080)],
      ),
      isFalse,
    );
    expect(
      desktopWindowOverlapsDisplay(bounds, [
        const Rect.fromLTWH(0, 0, 1920, 1080),
        const Rect.fromLTWH(-1920, 0, 1920, 1080),
      ]),
      isTrue,
    );
    expect(
      desktopWindowOverlapsDisplay(
        const Rect.fromLTWH(0, 0, 1200, 800),
        [const Rect.fromLTWH(0, 0, 1920, 1080)],
      ),
      isTrue,
    );
  });
}
