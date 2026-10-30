import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/portrait_blur_policy.dart';

void main() {
  bool eligible({
    bool enabled = true,
    bool mpvBackendAvailable = true,
    bool loading = false,
    bool hasError = false,
    double aspectRatio = 9 / 16,
  }) => shouldUsePortraitBlurBackground(
    enabled: enabled,
    mpvBackendAvailable: mpvBackendAvailable,
    loading: loading,
    hasError: hasError,
    aspectRatio: aspectRatio,
  );

  test('portrait background requires enabled MPV and a ready portrait video', () {
    expect(eligible(), isTrue);
    expect(eligible(enabled: false), isFalse);
    expect(eligible(mpvBackendAvailable: false), isFalse);
    expect(eligible(loading: true), isFalse);
    expect(eligible(hasError: true), isFalse);
    expect(eligible(aspectRatio: 16 / 9), isFalse);
    expect(eligible(aspectRatio: 0.999), isTrue);
    expect(eligible(aspectRatio: 1.0), isFalse);
    expect(eligible(aspectRatio: 0), isFalse);
    expect(eligible(aspectRatio: double.nan), isFalse);
    expect(eligible(aspectRatio: double.infinity), isFalse);
  });
}
