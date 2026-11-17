import 'package:flutter_test/flutter_test.dart';
import 'package:hgtv/supjav_access_mode.dart';

void main() {
  tearDown(SupJavAccessMode.useNativeMode);

  test('WebView-only mode blocks SupJav native data and playback requests', () {
    SupJavAccessMode.enterWebViewOnly();

    for (final action in [
      'catalog',
      'cached',
      'cachedPreview',
      'categories',
      'cover',
      'prepareCover',
      'detail',
      'seasons',
      'metadata',
      'resolve',
      'preload',
      'prepareHandoff',
      'localPlayback',
      'sourceJob',
    ]) {
      expect(
        SupJavAccessMode.shouldBlockNativeRequest(
          action: action,
          source: 'supjav',
        ),
        isTrue,
        reason: action,
      );
    }
  });

  test('WebView-only mode never blocks another source', () {
    SupJavAccessMode.enterWebViewOnly();
    for (final source in ['missav', 'hongguo', 'duoduo']) {
      expect(
        SupJavAccessMode.shouldBlockNativeRequest(
          action: 'catalog',
          source: source,
        ),
        isFalse,
      );
    }
  });

  test('native SupJav routing remains available before fallback is chosen', () {
    expect(
      SupJavAccessMode.shouldBlockNativeRequest(
        action: 'catalog',
        source: 'supjav',
      ),
      isFalse,
    );
  });
}
