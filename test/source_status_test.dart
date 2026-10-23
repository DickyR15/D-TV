import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/source_status.dart';

void main() {
  test('source health labels use Taiwan Traditional Chinese wording', () {
    expect(SourceHealth.fromJson({'state': 'ok'}).label, '連線檢測通過');
    expect(SourceHealth.fromJson({'state': 'failed'}).label, '檢測未通過');
  });
}
