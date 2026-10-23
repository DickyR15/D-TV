import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:flutter/material.dart';
import 'package:qr_flutter/qr_flutter.dart';

/// Starts an app-lifetime, token-protected phone search page on the local network.
class PhoneSearchServer {
  PhoneSearchServer._(this._server, this.address, this.token) {
    _subscription = _server.listen((request) {
      unawaited(_handle(request));
    });
  }

  final HttpServer _server;
  final String address;
  final String token;
  final StreamController<String> _queries = StreamController<String>.broadcast();
  late final StreamSubscription<HttpRequest> _subscription;
  bool _closed = false;

  int get port => _server.port;
  Uri get url => Uri(
    scheme: 'http',
    host: address,
    port: port,
    path: '/search',
    queryParameters: {'token': token},
  );

  Stream<String> get queries => _queries.stream;

  static Future<PhoneSearchServer> start() async {
    final address = await _findLocalAddress();
    if (address == null) {
      throw const SocketException(
        '找不到可供手機連線的區域網路 IP，請確認電視已連上網路。',
      );
    }
    final server = await HttpServer.bind(
      InternetAddress.anyIPv4,
      0,
      shared: true,
    );
    final random = Random.secure();
    final token = List<int>.generate(20, (_) => random.nextInt(256))
        .map((value) => value.toRadixString(16).padLeft(2, '0'))
        .join();
    return PhoneSearchServer._(server, address, token);
  }

  static Future<String?> _findLocalAddress() async {
    final interfaces = await NetworkInterface.list(
      includeLoopback: false,
      type: InternetAddressType.IPv4,
    );
    final candidates = <String>[];
    for (final interface in interfaces) {
      for (final address in interface.addresses) {
        final value = address.address;
        if (address.isLoopback || value.startsWith('169.254.')) continue;
        candidates.add(value);
      }
    }
    if (candidates.isEmpty) return null;

    bool isPrivate(String value) {
      final parts = value.split('.').map(int.tryParse).toList();
      if (parts.length != 4 || parts.any((part) => part == null)) return false;
      final a = parts[0]!;
      final b = parts[1]!;
      return a == 10 ||
          (a == 192 && b == 168) ||
          (a == 172 && b >= 16 && b <= 31);
    }

    candidates.sort((a, b) {
      final ap = isPrivate(a);
      final bp = isPrivate(b);
      if (ap == bp) {
        final a192 = a.startsWith('192.168.');
        final b192 = b.startsWith('192.168.');
        if (a192 != b192) return a192 ? -1 : 1;
        return 0;
      }
      return ap ? -1 : 1;
    });
    return candidates.first;
  }

  Future<void> _handle(HttpRequest request) async {
    try {
      if (request.uri.queryParameters['token'] != token) {
        await _respond(
          request,
          HttpStatus.forbidden,
          _page('連線已失效', '請回到電視重新開啟手機掃描 QR Code。'),
        );
        return;
      }

      if (request.method == 'GET' && request.uri.path == '/search') {
        await _respond(
          request,
          HttpStatus.ok,
          _searchForm
              .replaceAll('TOKEN_PLACEHOLDER', token)
              .replaceAll('STATUS_MESSAGE', ''),
        );
        return;
      }

      if (request.method == 'POST' && request.uri.path == '/submit') {
        final body = await utf8.decoder.bind(request).join();
        final form = Uri.splitQueryString(body);
        final query = (form['q'] ?? '').trim();
        if (query.isEmpty || query.length > 100) {
          await _respond(
            request,
            HttpStatus.badRequest,
            _page('請輸入劇名', '劇名不可空白，請返回上一頁重新輸入。'),
          );
          return;
        }

        if (!_closed) _queries.add(query);
        await _respond(
          request,
          HttpStatus.ok,
          _searchForm
              .replaceAll('TOKEN_PLACEHOLDER', token)
              .replaceAll(
                'STATUS_MESSAGE',
                '<p class="notice">已送出「${_escape(query)}」。可繼續輸入其他短劇，不必重新掃碼。</p>',
              ),
        );
        return;
      }

      await _respond(
        request,
        HttpStatus.notFound,
        _page('找不到頁面', '請使用 D TV 電視畫面上的 QR Code。'),
      );
    } catch (_) {
      try {
        request.response.statusCode = HttpStatus.internalServerError;
        await request.response.close();
      } catch (_) {}
    }
  }

  Future<void> _respond(HttpRequest request, int status, String html) async {
    request.response.statusCode = status;
    request.response.headers.contentType = ContentType.html;
    request.response.headers.set(HttpHeaders.cacheControlHeader, 'no-store');
    request.response.headers.set('X-Content-Type-Options', 'nosniff');
    request.response.write(html);
    await request.response.close();
  }

  String _escape(String value) => value
      .replaceAll('&', '&amp;')
      .replaceAll('<', '&lt;')
      .replaceAll('>', '&gt;')
      .replaceAll('"', '&quot;')
      .replaceAll("'", '&#39;');

  String _page(String title, String message) => '''
<!doctype html><html lang="zh-Hant"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>$title · D TV</title>
<style>
body{font-family:system-ui,-apple-system,"Noto Sans TC",sans-serif;background:#111216;color:#f7f7fa;margin:0;min-height:100vh;display:grid;place-items:center}
main{width:min(88vw,480px);background:#202127;border-radius:22px;padding:28px;box-sizing:border-box}
h1{font-size:25px;margin:0 0 16px;color:#ff8b77}p{font-size:17px;line-height:1.7}
</style></head><body><main><h1>$title</h1><p>$message</p></main></body></html>
''';

  static const String _searchForm = '''
<!doctype html><html lang="zh-Hant"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>D TV 手機搜尋</title>
<style>
:root{color-scheme:dark}*{box-sizing:border-box}
body{font-family:system-ui,-apple-system,"Noto Sans TC",sans-serif;background:#111216;color:#f7f7fa;margin:0;min-height:100vh;display:grid;place-items:center}
main{width:min(92vw,520px);background:#202127;border-radius:24px;padding:28px;box-shadow:0 12px 36px #0006}
.brand{color:#ff8b77;letter-spacing:2px;font-weight:800;font-size:16px}
h1{font-size:27px;margin:10px 0 8px}p{line-height:1.6;color:#c7c8d0}
input{width:100%;font:inherit;font-size:19px;padding:15px 14px;margin:16px 0 12px;border:1px solid #555764;border-radius:12px;background:#14151a;color:#fff;outline:none}
input:focus{border-color:#ff8b77;box-shadow:0 0 0 2px #ff8b7733}
button{width:100%;padding:15px;border:0;border-radius:12px;background:#ff8b77;color:#251715;font:inherit;font-size:18px;font-weight:800}
footer{margin-top:18px;font-size:13px;color:#a8a9b3}
.notice{padding:10px 12px;border-radius:10px;background:#24382f;color:#a8f0c6}
</style></head><body><main><div class="brand">D TV</div><h1>手機搜尋短劇</h1>
<p>輸入劇名並送出，電視會自動顯示搜尋結果。手機與電視需保持連線至同一個區域網路。</p>
STATUS_MESSAGE
<form method="post" action="/submit?token=TOKEN_PLACEHOLDER">
<label for="q">短劇名稱</label><input id="q" name="q" type="search" maxlength="100" placeholder="輸入想看的劇名" autocomplete="off" required autofocus>
<button type="submit">搜尋 D TV</button></form>
<footer>此頁面只會在目前的 D TV 搜尋工作階段使用。</footer></main></body></html>
''';

  Future<void> close() async {
    if (_closed) return;
    _closed = true;
    await _subscription.cancel();
    await _server.close(force: true);
    await _queries.close();
  }
}

Future<void> showPhoneSearchDialog(
  BuildContext context, {
  required PhoneSearchServer server,
}) async {
  if (!context.mounted) return;
  await showDialog<void>(
    context: context,
    barrierDismissible: true,
    builder: (_) => _PhoneSearchDialog(server: server),
  );
}

class _PhoneSearchDialog extends StatefulWidget {
  const _PhoneSearchDialog({required this.server});
  final PhoneSearchServer server;

  @override
  State<_PhoneSearchDialog> createState() => _PhoneSearchDialogState();
}

class _PhoneSearchDialogState extends State<_PhoneSearchDialog> {
  StreamSubscription<String>? _subscription;

  @override
  void initState() {
    super.initState();
    // Close only the QR overlay after the first submission so the TV can
    // display results. The app-owned server and phone page remain available.
    _subscription = widget.server.queries.listen((_) {
      if (mounted) Navigator.of(context).pop();
    });
  }

  @override
  void dispose() {
    unawaited(_subscription?.cancel());
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final size = MediaQuery.sizeOf(context);
    final url = widget.server.url.toString();
    return Dialog(
      insetPadding: const EdgeInsets.symmetric(horizontal: 24, vertical: 24),
      child: ConstrainedBox(
        constraints: BoxConstraints(maxWidth: 480, maxHeight: size.height * 0.82),
        child: SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(24, 22, 24, 18),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text(
                      '手機掃描 QR Code',
                      style: Theme.of(context).textTheme.titleLarge?.copyWith(
                        fontWeight: FontWeight.w800,
                      ),
                    ),
                  ),
                  const SizedBox(width: 12),
                  Text(
                    'D TV',
                    style: TextStyle(
                      color: colors.primary,
                      fontSize: 18,
                      fontWeight: FontWeight.w900,
                      letterSpacing: 0.6,
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 16),
              Container(
                padding: const EdgeInsets.all(8),
                decoration: BoxDecoration(
                  color: Colors.white,
                  borderRadius: BorderRadius.circular(14),
                ),
                child: QrImageView(
                  data: url,
                  version: QrVersions.auto,
                  size: 172,
                  backgroundColor: Colors.white,
                  semanticsLabel: '掃描以開啟 D TV 手機搜尋',
                ),
              ),
              const SizedBox(height: 14),
              Text(
                '手機只需掃描一次。第一次送出後，電視會自動關閉此視窗顯示結果；手機頁面可繼續搜尋其他劇，不必重新掃碼。',
                textAlign: TextAlign.center,
                style: Theme.of(context).textTheme.bodyMedium,
              ),
              const SizedBox(height: 10),
              Text(
                url,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                textAlign: TextAlign.center,
                style: TextStyle(color: colors.primary, fontSize: 12),
              ),
              const SizedBox(height: 8),
              Text(
                '手機與電視需連線至同一個 Wi-Fi／區域網路。',
                textAlign: TextAlign.center,
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                  color: colors.onSurfaceVariant,
                ),
              ),
              const SizedBox(height: 14),
              Align(
                alignment: Alignment.centerRight,
                child: TextButton(
                  autofocus: true,
                  onPressed: () => Navigator.of(context).pop(),
                  child: const Text('取消'),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
