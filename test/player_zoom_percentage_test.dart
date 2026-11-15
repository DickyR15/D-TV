import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/player_interactions.dart';

void main() {
  test('pinch zoom HUD reports the zoom percentage, not a multiplier', () {
    expect(playerZoomPercentage(1.0), 100);
    expect(playerZoomPercentage(1.25), 125);
    expect(playerZoomPercentage(1.5), 150);
    expect(playerZoomPercentage(2.0), 200);
    expect(playerZoomPercentage(3.0), 300);
  });

  test('pinch zoom HUD clamps values to the actual zoom range', () {
    expect(playerZoomPercentage(0.5), 100);
    expect(playerZoomPercentage(9.0), 300);
  });
}
