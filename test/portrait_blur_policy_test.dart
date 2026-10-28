import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/portrait_blur_policy.dart';

void main() {
  bool eligible({
    bool enabled = true,
    bool mpvEmbedBackend = true,
    bool loading = false,
    bool hasError = false,
    double aspectRatio = 9 / 16,
  }) => shouldUsePortraitBlurBackground(
    enabled: enabled,
    mpvEmbedBackend: mpvEmbedBackend,
    loading: loading,
    hasError: hasError,
    aspectRatio: aspectRatio,
  );

  test('portrait background requires enabled MPV Embed and a ready portrait video', () {
    expect(eligible(), isTrue);
    expect(eligible(enabled: false), isFalse);
    expect(eligible(mpvEmbedBackend: false), isFalse);
    expect(eligible(loading: true), isFalse);
    expect(eligible(hasError: true), isFalse);
    expect(eligible(aspectRatio: 16 / 9), isFalse);
    expect(eligible(aspectRatio: 0), isFalse);
    expect(eligible(aspectRatio: double.nan), isFalse);
  });
}
