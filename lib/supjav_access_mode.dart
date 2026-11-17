/// Session-level routing policy for SupJav after the user chooses the
/// embedded browser fallback. It never reads or transfers browser cookies.
class SupJavAccessMode {
  SupJavAccessMode._();

  static bool _webViewOnly = false;

  static bool get isWebViewOnly => _webViewOnly;

  static void enterWebViewOnly() {
    _webViewOnly = true;
  }

  /// Available for controlled tests and a future explicit retry action.
  static void useNativeMode() {
    _webViewOnly = false;
  }

  static bool shouldBlockNativeRequest({
    required String action,
    required String? source,
  }) {
    if (!_webViewOnly || source != 'supjav') return false;
    return const {
      'catalog',
      'cached',
      'cachedPreview',
      'categories',
      'sourceJob',
      'cover',
      'prepareCover',
      'detail',
      'seasons',
      'metadata',
      'resolve',
      'preload',
      'prepareHandoff',
      'enqueueDownloads',
      'localPlayback',
    }.contains(action);
  }
}
