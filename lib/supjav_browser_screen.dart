import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_inappwebview/flutter_inappwebview.dart';
import 'package:path/path.dart' as path;
import 'package:path_provider/path_provider.dart';

const String supJavHomeUrl = 'https://supjav.com/zh/';

bool isSupJavHost(String host) {
  final normalized = host.toLowerCase().replaceFirst(RegExp(r'\.$'), '');
  return normalized == 'supjav.com' ||
      normalized.endsWith('.supjav.com') ||
      normalized == 'supremejav.com' ||
      normalized.endsWith('.supremejav.com');
}

/// Returns a user-facing message only for main-frame SupJav responses.
/// It deliberately does not expose or copy any challenge cookies or tokens.
String? supJavHttpStatusNotice({
  required Uri? url,
  required int? statusCode,
  required Map<String, String>? headers,
  required bool isMainFrame,
}) {
  if (!isMainFrame || url == null || !isSupJavHost(url.host)) return null;

  final isChallenge = headers?.entries.any(
        (entry) =>
            entry.key.toLowerCase() == 'cf-mitigated' &&
            entry.value.trim().toLowerCase() == 'challenge',
      ) ??
      false;
  if (isChallenge) {
    return 'SupJav 要求 Cloudflare 安全驗證。請在目前頁面依網站指示親自完成；D-TV 不會自動解題或轉送驗證資料。';
  }
  if (statusCode == 403) {
    return 'SupJav 拒絕目前頁面的連線（HTTP 403）。這可能是網站存取政策或安全規則，內建瀏覽器不保證能解除。';
  }
  if (statusCode != null && statusCode >= 400) {
    return 'SupJav 頁面回應 HTTP $statusCode。可以稍後重新載入，或依網站顯示的訊息處理。';
  }
  return null;
}

class SupJavBrowserScreen extends StatefulWidget {
  const SupJavBrowserScreen({super.key});

  @override
  State<SupJavBrowserScreen> createState() => _SupJavBrowserScreenState();
}

class _SupJavBrowserScreenState extends State<SupJavBrowserScreen> {
  static Future<WebViewEnvironment>? _windowsEnvironmentFuture;

  InAppWebViewController? _controller;
  WebViewEnvironment? _webViewEnvironment;
  bool _environmentReady = false;
  String? _environmentError;
  String _pageTitle = 'SupJav 站內瀏覽器';
  String _currentUrl = supJavHomeUrl;
  String? _statusMessage;
  double _progress = 0;
  bool _canGoBack = false;
  bool _canGoForward = false;

  @override
  void initState() {
    super.initState();
    _prepareWebViewEnvironment();
  }

  static Future<WebViewEnvironment> _createWindowsEnvironment() async {
    final runtimeVersion = await WebViewEnvironment.getAvailableVersion();
    if (runtimeVersion == null) {
      throw StateError(
        '找不到 Microsoft Edge WebView2 Runtime。請先安裝 WebView2 Runtime，'
        '再重新開啟 D-TV。',
      );
    }

    final supportDirectory = await getApplicationSupportDirectory();
    final userDataDirectory = Directory(
      path.join(supportDirectory.path, 'D-TV', 'WebView2', 'SupJav'),
    );
    await userDataDirectory.create(recursive: true);

    return WebViewEnvironment.create(
      settings: WebViewEnvironmentSettings(
        userDataFolder: userDataDirectory.path,
      ),
    );
  }

  static Future<WebViewEnvironment> _windowsEnvironment() {
    final existing = _windowsEnvironmentFuture;
    if (existing != null) return existing;

    final created = _createWindowsEnvironment();
    _windowsEnvironmentFuture = created;
    return created.catchError((Object error) {
      if (identical(_windowsEnvironmentFuture, created)) {
        _windowsEnvironmentFuture = null;
      }
      throw error;
    });
  }

  Future<void> _prepareWebViewEnvironment() async {
    if (Platform.isWindows) {
      try {
        _webViewEnvironment = await _windowsEnvironment();
      } catch (error) {
        if (!mounted) return;
        setState(() {
          _environmentError = error.toString();
          _environmentReady = false;
        });
        return;
      }
    }

    if (!mounted) return;
    setState(() {
      _environmentError = null;
      _environmentReady = true;
    });
  }

  Future<void> _syncNavigationState() async {
    final controller = _controller;
    if (controller == null) return;
    try {
      final canGoBack = await controller.canGoBack();
      final canGoForward = await controller.canGoForward();
      if (!mounted) return;
      setState(() {
        _canGoBack = canGoBack;
        _canGoForward = canGoForward;
      });
    } catch (_) {
      // Navigation controls are best-effort and must not break the page.
    }
  }

  Future<void> _goBack() async {
    final controller = _controller;
    if (controller == null || !_canGoBack) return;
    await controller.goBack();
  }

  Future<void> _goForward() async {
    final controller = _controller;
    if (controller == null || !_canGoForward) return;
    await controller.goForward();
  }

  Future<void> _reload() async {
    final controller = _controller;
    if (controller == null) return;
    setState(() {
      _statusMessage = null;
      _progress = 0;
    });
    await controller.reload();
  }

  void _recordHttpError(
    WebResourceRequest request,
    WebResourceResponse response,
  ) {
    final notice = supJavHttpStatusNotice(
      url: Uri.tryParse(request.url.toString()),
      statusCode: response.statusCode,
      headers: response.headers,
      isMainFrame: request.isForMainFrame == true,
    );
    if (!mounted || notice == null) return;
    setState(() => _statusMessage = notice);
  }

  InAppWebViewSettings get _webViewSettings => InAppWebViewSettings(
    javaScriptEnabled: true,
    domStorageEnabled: true,
    databaseEnabled: true,
    thirdPartyCookiesEnabled: true,
    cacheEnabled: true,
    mediaPlaybackRequiresUserGesture: true,
    allowsInlineMediaPlayback: true,
    iframeAllowFullscreen: true,
    supportZoom: true,
    useWideViewPort: true,
    loadWithOverviewMode: true,
    useShouldOverrideUrlLoading: true,
    supportMultipleWindows: false,
    javaScriptCanOpenWindowsAutomatically: false,
    safeBrowsingEnabled: true,
    isInspectable: false,
  );

  Widget _environmentFailure() => Center(
    child: ConstrainedBox(
      constraints: const BoxConstraints(maxWidth: 560),
      child: Padding(
        padding: const EdgeInsets.all(28),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.web_asset_off_rounded, size: 52),
            const SizedBox(height: 16),
            Text(
              '無法啟動 SupJav 站內瀏覽器',
              style: Theme.of(context).textTheme.titleLarge,
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 12),
            Text(
              _environmentError ?? '瀏覽器元件尚未就緒。',
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 20),
            FilledButton.icon(
              autofocus: true,
              onPressed: _prepareWebViewEnvironment,
              icon: const Icon(Icons.refresh_rounded),
              label: const Text('重新檢查'),
            ),
          ],
        ),
      ),
    ),
  );

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(_pageTitle, maxLines: 1, overflow: TextOverflow.ellipsis),
          Text(
            _currentUrl,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: Theme.of(context).textTheme.labelSmall,
          ),
        ],
      ),
      actions: [
        IconButton(
          tooltip: '上一頁',
          autofocus: true,
          onPressed: _canGoBack ? _goBack : null,
          icon: const Icon(Icons.arrow_back_rounded),
        ),
        IconButton(
          tooltip: '下一頁',
          onPressed: _canGoForward ? _goForward : null,
          icon: const Icon(Icons.arrow_forward_rounded),
        ),
        IconButton(
          tooltip: '重新載入',
          onPressed: _controller == null ? null : _reload,
          icon: const Icon(Icons.refresh_rounded),
        ),
      ],
    ),
    body: SafeArea(
      child: Column(
        children: [
          Material(
            color: Theme.of(context).colorScheme.surfaceContainer,
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 9),
              child: Row(
                children: [
                  Icon(
                    Icons.verified_user_outlined,
                    size: 18,
                    color: Theme.of(context).colorScheme.primary,
                  ),
                  const SizedBox(width: 9),
                  const Expanded(
                    child: Text(
                      '請在此頁面親自完成網站驗證。瀏覽器工作階段會保留在 D-TV，但不會轉交給原生片源請求。',
                      style: TextStyle(fontSize: 12),
                    ),
                  ),
                ],
              ),
            ),
          ),
          if (_statusMessage != null)
            MaterialBanner(
              leading: const Icon(Icons.info_outline_rounded),
              content: Text(_statusMessage!),
              actions: [
                TextButton(
                  onPressed: () => setState(() => _statusMessage = null),
                  child: const Text('知道了'),
                ),
              ],
            ),
          if (_progress > 0 && _progress < 1)
            LinearProgressIndicator(value: _progress, minHeight: 2),
          Expanded(
            child: !_environmentReady
                ? (_environmentError == null
                      ? const Center(child: CircularProgressIndicator())
                      : _environmentFailure())
                : InAppWebView(
                    key: const ValueKey('supjav-in-app-webview'),
                    webViewEnvironment: _webViewEnvironment,
                    initialUrlRequest: URLRequest(
                      url: WebUri(supJavHomeUrl),
                    ),
                    initialSettings: _webViewSettings,
                    onWebViewCreated: (controller) {
                      _controller = controller;
                      setState(() {});
                      _syncNavigationState();
                    },
                    onLoadStart: (controller, url) {
                      if (!mounted) return;
                      setState(() {
                        _progress = 0.02;
                        _statusMessage = null;
                        if (url != null) _currentUrl = url.toString();
                      });
                    },
                    onLoadStop: (controller, url) {
                      if (!mounted) return;
                      setState(() {
                        _progress = 1;
                        if (url != null) _currentUrl = url.toString();
                      });
                      _syncNavigationState();
                    },
                    onTitleChanged: (controller, title) {
                      if (!mounted || title == null || title.trim().isEmpty) {
                        return;
                      }
                      setState(() => _pageTitle = title.trim());
                    },
                    onUpdateVisitedHistory: (controller, url, isReload) {
                      if (!mounted || url == null) return;
                      setState(() => _currentUrl = url.toString());
                      _syncNavigationState();
                    },
                    onProgressChanged: (controller, progress) {
                      if (!mounted) return;
                      setState(() => _progress = progress / 100);
                    },
                    onReceivedHttpError: (controller, request, response) {
                      _recordHttpError(request, response);
                    },
                    onReceivedError: (controller, request, error) {
                      if (request.isForMainFrame != true || !mounted) return;
                      final url = Uri.tryParse(request.url.toString());
                      if (url == null || !isSupJavHost(url.host)) return;
                      setState(() {
                        _statusMessage =
                            'SupJav 網頁載入失敗：${error.description}。可以嘗試重新載入。';
                      });
                    },
                    shouldOverrideUrlLoading: (controller, action) async {
                      final target = action.request.url;
                      if (target == null) {
                        return NavigationActionPolicy.CANCEL;
                      }
                      final scheme = target.scheme.toLowerCase();
                      return scheme == 'http' || scheme == 'https'
                          ? NavigationActionPolicy.ALLOW
                          : NavigationActionPolicy.CANCEL;
                    },
                  ),
          ),
        ],
      ),
    ),
  );
}
