import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/television_playback_info.dart';

void main() {

  test('TV clock uses a local 24-hour HH:mm display', () {
    expect(
      televisionClockLabel(DateTime(2026, 11, 13, 9, 5)),
      '09:05',
    );
    expect(
      televisionClockLabel(DateTime(2026, 11, 13, 23, 59)),
      '23:59',
    );
  });
}
