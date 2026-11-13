import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/television_playback_info.dart';

void main() {
  test('network speed is shown only for a confirmed network input', () {
    expect(
      televisionNetworkSpeedLabel('1048576', networkInput: true),
      '1.0 MB/s',
    );
    expect(
      televisionNetworkSpeedLabel('1536', networkInput: true),
      '1.5 KB/s',
    );
    expect(
      televisionNetworkSpeedLabel('512', networkInput: true),
      '512 B/s',
    );
    expect(
      televisionNetworkSpeedLabel('0', networkInput: true),
      '0 B/s',
    );
    expect(
      televisionNetworkSpeedLabel('1048576', networkInput: false),
      isNull,
    );
  });

  test('missing or invalid MPV speed values do not fabricate a reading', () {
    for (final value in ['', 'n/a', 'unknown', '-1', 'NaN', 'Infinity']) {
      expect(
        televisionNetworkSpeedLabel(value, networkInput: true),
        isNull,
        reason: value,
      );
    }
  });

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
