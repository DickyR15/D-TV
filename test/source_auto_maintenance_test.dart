import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/source_auto_maintenance.dart';

void main() {
  test('source health is checked when absent or at least 24 hours old', () {
    final now = DateTime(2026, 10, 28, 12);
    expect(sourceHealthIsStale(null, now), isTrue);
    expect(
      sourceHealthIsStale(now.subtract(const Duration(hours: 23)), now),
      isFalse,
    );
    expect(
      sourceHealthIsStale(now.subtract(const Duration(hours: 24)), now),
      isTrue,
    );
    expect(
      sourceHealthIsStale(now.subtract(const Duration(days: 3)), now),
      isTrue,
    );
  });

  test('future health timestamps are stale to recover from clock changes', () {
    final now = DateTime(2026, 10, 28, 12);
    expect(
      sourceHealthIsStale(now.add(const Duration(minutes: 1)), now),
      isTrue,
    );
  });
}
