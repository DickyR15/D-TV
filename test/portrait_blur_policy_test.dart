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

  test('Windows and Android MPV support portrait blur but Android ExoPlayer does not', () {
    expect(
      supportsPortraitBlurBackground(
        android: false,
        windows: true,
        androidMpvConfigured: false,
      ),
      isTrue,
    );
    expect(
      supportsPortraitBlurBackground(
        android: true,
        windows: false,
        androidMpvConfigured: true,
      ),
      isTrue,
    );
    expect(
      supportsPortraitBlurBackground(
        android: true,
        windows: false,
        androidMpvConfigured: false,
      ),
      isFalse,
    );
    expect(
      supportsPortraitBlurBackground(
        android: false,
        windows: false,
        androidMpvConfigured: true,
      ),
      isFalse,
    );
  });

  test('Android TV keeps MPV Embed while Flutter composites portrait blur', () {
    bool embed({
      bool android = true,
      bool television = true,
      bool configured = true,
      bool software = false,
      bool customPlayer = false,
    }) => shouldUseMpvEmbedForPlayback(
      android: android,
      television: television,
      mpvEmbedConfigured: configured,
      softwareDecoderSelected: software,
      customPlayer: customPlayer,
    );

    // Blur is a Flutter composition effect and must not select mediacodec-copy.
    expect(embed(), isTrue);
    expect(embed(television: false), isFalse);
    expect(embed(software: true), isFalse);
    expect(embed(customPlayer: true), isFalse);
    expect(embed(android: false), isFalse);
    expect(embed(configured: false), isFalse);
  });
}
