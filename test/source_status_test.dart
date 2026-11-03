import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/source_status.dart';

void main() {
  test('source health labels use Taiwan Traditional Chinese wording', () {
    expect(SourceHealth.fromJson({'state': 'ok'}).label, '連線檢測通過');
    expect(SourceHealth.fromJson({'state': 'failed'}).label, '檢測未通過');
  });

  test('diagnostics hide advanced sources without an administrator password', () {
    final visible = sourceDiagnosticsSources(
      enabledSources: [
        SourceSite.hongguo,
        SourceSite.hanxiaoquan,
        SourceSite.duoduo,
        SourceSite.dsd,
      ],
      administratorPasswordConfigured: false,
    );
    expect(
      visible.map((source) => source.id),
      ['hongguo', 'hanxiaoquan', 'duoduo'],
    );
  });

  test('diagnostics include advanced sources only when password is configured', () {
    final visible = sourceDiagnosticsSources(
      enabledSources: [SourceSite.hongguo, SourceSite.dsd],
      administratorPasswordConfigured: true,
    );
    expect(visible.map((source) => source.id), ['hongguo', 'dsd']);
  });
}
