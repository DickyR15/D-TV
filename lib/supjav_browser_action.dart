import 'package:flutter/material.dart';

import 'app_layout.dart';
import 'supjav_browser_screen.dart';
import 'supjav_access_mode.dart';

class SupJavBrowserAction extends StatelessWidget {
  const SupJavBrowserAction({
    super.key,
    this.browserBuilder,
    this.onModeChanged,
  });

  /// Test seam. Production always opens the embedded native browser.
  final WidgetBuilder? browserBuilder;
  final VoidCallback? onModeChanged;

  @override
  Widget build(BuildContext context) => Column(
    mainAxisSize: MainAxisSize.min,
    crossAxisAlignment: CrossAxisAlignment.stretch,
    children: [
      Text(
        '在 D-TV 內建瀏覽器開啟 SupJav。若網站要求安全驗證，請依頁面指示親自完成；瀏覽器工作階段不會自動解除 D-TV 原生片源連線的 403。',
        textAlign: TextAlign.center,
        style: Theme.of(context).textTheme.bodySmall,
      ),
      const SizedBox(height: 8),
      OutlinedButton.icon(
        key: const ValueKey('supjav-open-browser'),
        autofocus: AppLayout.isTelevision(context),
        onPressed: () {
          SupJavAccessMode.enterWebViewOnly();
          onModeChanged?.call();
          Navigator.of(context).push<void>(
            MaterialPageRoute<void>(
              builder: browserBuilder ?? (_) => const SupJavBrowserScreen(),
            ),
          );
        },
        icon: const Icon(Icons.open_in_browser_rounded),
        label: const Text('在 D-TV 內開啟 SupJav'),
      ),
    ],
  );
}
