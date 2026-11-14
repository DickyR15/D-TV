import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/player_interactions.dart';

void main() {
  group('player hold-to-seek policy', () {
    test('accelerates gradually while the screen remains pressed', () {
      expect(playerHoldSeekMultiplier(Duration.zero), 2);
      expect(playerHoldSeekMultiplier(const Duration(milliseconds: 1499)), 2);
      expect(playerHoldSeekMultiplier(const Duration(milliseconds: 1500)), 4);
      expect(playerHoldSeekMultiplier(const Duration(seconds: 3)), 8);
      expect(playerHoldSeekStep(Duration.zero), const Duration(milliseconds: 700));
      expect(
        playerHoldSeekStep(const Duration(milliseconds: 1500)),
        const Duration(milliseconds: 1400),
      );
      expect(
        playerHoldSeekStep(const Duration(seconds: 3)),
        const Duration(milliseconds: 2800),
      );
    });

    test('seeks in the requested direction and clamps at both ends', () {
      expect(
        playerHoldSeekTarget(
          current: const Duration(seconds: 10),
          duration: const Duration(seconds: 12),
          direction: 1,
          step: const Duration(seconds: 5),
        ),
        const Duration(seconds: 12),
      );
      expect(
        playerHoldSeekTarget(
          current: const Duration(seconds: 2),
          duration: const Duration(seconds: 12),
          direction: -1,
          step: const Duration(seconds: 5),
        ),
        Duration.zero,
      );
      expect(
        playerHoldSeekTarget(
          current: const Duration(seconds: 5),
          duration: const Duration(seconds: 12),
          direction: -1,
          step: const Duration(seconds: 2),
        ),
        const Duration(seconds: 3),
      );
    });
  });
}
