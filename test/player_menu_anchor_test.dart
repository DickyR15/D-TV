import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/player_menu.dart';

void main() {
  test('quick menu is centered above the selected playback control', () {
    const anchor = Rect.fromLTWH(600, 900, 48, 48);
    const viewport = Size(1920, 1080);
    const menu = Size(154, 344);

    final rect = playerMenuPopoverRect(
      anchor: anchor,
      viewport: viewport,
      menuSize: menu,
    );

    expect(rect.center.dx, anchor.center.dx);
    expect(rect.bottom, lessThan(anchor.top));
    expect(rect.width, menu.width);
    expect(rect.height, menu.height);
  });

  test('quick menu stays inside the screen near the left edge', () {
    const anchor = Rect.fromLTWH(2, 900, 48, 48);
    const viewport = Size(800, 600);

    final rect = playerMenuPopoverRect(
      anchor: anchor,
      viewport: viewport,
      menuSize: const Size(154, 280),
    );

    expect(rect.left, 8);
    expect(rect.top, greaterThanOrEqualTo(8));
    expect(rect.right, lessThanOrEqualTo(viewport.width - 8));
    expect(rect.bottom, lessThanOrEqualTo(viewport.height - 8));
  });

  test('quick menu flips below the anchor when there is no room above', () {
    const anchor = Rect.fromLTWH(300, 10, 48, 48);
    const viewport = Size(900, 700);

    final rect = playerMenuPopoverRect(
      anchor: anchor,
      viewport: viewport,
      menuSize: const Size(154, 260),
    );

    expect(rect.top, greaterThan(anchor.bottom));
    expect(rect.bottom, lessThanOrEqualTo(viewport.height - 8));
  });
}
