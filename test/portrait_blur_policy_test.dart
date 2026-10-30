import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/portrait_blur_policy.dart';

void main() {
  bool eligible({
    bool enabled = true,
    bool textureRendererAvailable = true,
    bool loading = false,
    bool hasError = false,
    double aspectRatio = 9 / 16,
  }) => shouldUsePortraitBlurBackground(
    enabled: enabled,
    textureRendererAvailable: textureRendererAvailable,
    loading: loading,
    hasError: hasError,
    aspectRatio: aspectRatio,
  );

  test('portrait blur requires the shared GPU texture renderer and a ready portrait video', () {
    expect(eligible(), isTrue);
    expect(eligible(enabled: false), isFalse);
    expect(eligible(textureRendererAvailable: false), isFalse);
    expect(eligible(loading: true), isFalse);
    expect(eligible(hasError: true), isFalse);
    expect(eligible(aspectRatio: 16 / 9), isFalse);
    expect(eligible(aspectRatio: 0.999), isTrue);
    expect(eligible(aspectRatio: 1.0), isFalse);
    expect(eligible(aspectRatio: 0), isFalse);
    expect(eligible(aspectRatio: double.nan), isFalse);
    expect(eligible(aspectRatio: double.infinity), isFalse);
  });

  test('MPV Embed is bypassed only when portrait blur requires a reusable texture', () {
    bool embed({
      bool android = true,
      bool television = true,
      bool configured = true,
      bool hardware = true,
      bool customPlayer = false,
      bool blur = false,
    }) => shouldUseMpvEmbedForPlayback(
      android: android,
      television: television,
      mpvEmbedConfigured: configured,
      hardwareDecoderSelected: hardware,
      customPlayer: customPlayer,
      portraitBlurEnabled: blur,
    );

    expect(embed(), isTrue);
    expect(embed(blur: true), isFalse);
    expect(embed(television: false), isFalse);
    expect(embed(hardware: false), isFalse);
    expect(embed(customPlayer: true), isFalse);
    expect(embed(android: false), isFalse);
  });
}
