import 'dart:async';

import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import 'app_layout.dart';

typedef SupJavExternalLauncher = Future<bool> Function(Uri url);

class SupJavBrowserAction extends StatelessWidget {
  const SupJavBrowserAction({super.key, this.launcher});

  final SupJavExternalLauncher? launcher;

  static final Uri websiteUri = Uri.parse('https://supjav.com/zh/');

  static Future<bool> _launchExternal(Uri url) => launchUrl(
    url,
    mode: LaunchMode.externalApplication,
  );

  Future<void> _open(BuildContext context) async {
    var opened = false;
    try {
      opened = await (launcher ?? _launchExternal)(websiteUri);
    } catch (_) {
      opened = false;
    }
    if (!context.mounted || opened) return;
    ScaffoldMessenger.maybeOf(context)
      ?..hideCurrentSnackBar()
      ..showSnackBar(
        const SnackBar(
          content: Text('無法開啟 SupJav。請確認裝置已安裝可開啟網頁的瀏覽器。'),
        ),
      );
  }

  @override
  Widget build(BuildContext context) => Column(
    mainAxisSize: MainAxisSize.min,
    crossAxisAlignment: CrossAxisAlignment.stretch,
    children: [
      Text(
        '若 SupJav 要求安全驗證，請在外部瀏覽器由你本人完成。外部瀏覽器的驗證狀態不會同步到 D-TV；返回後重新整理仍可能無法解除 403。',
        textAlign: TextAlign.center,
        style: Theme.of(context).textTheme.bodySmall,
      ),
      const SizedBox(height: 8),
      OutlinedButton.icon(
        key: const ValueKey('supjav-open-browser'),
        autofocus: AppLayout.isTelevision(context),
        onPressed: () => unawaited(_open(context)),
        icon: const Icon(Icons.open_in_browser_rounded),
        label: const Text('開啟 SupJav 官方網站'),
      ),
    ],
  );
}
