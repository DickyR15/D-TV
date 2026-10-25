import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

/// Handles TV remote Back/Escape on a dialog route and reports its dismissal
/// so the underlying player can ignore a duplicated key event.
class BackSafeDialog extends StatelessWidget {
  const BackSafeDialog({
    super.key,
    required this.child,
    this.onDismissed,
  });

  final Widget child;
  final VoidCallback? onDismissed;

  @override
  Widget build(BuildContext context) {
    void dismiss() {
      Navigator.of(context).maybePop();
    }

    return PopScope<Object?>(
      canPop: true,
      onPopInvokedWithResult: (didPop, result) {
        if (didPop && result == null) onDismissed?.call();
      },
      child: CallbackShortcuts(
        bindings: {
          const SingleActivator(LogicalKeyboardKey.goBack): dismiss,
          const SingleActivator(LogicalKeyboardKey.escape): dismiss,
        },
        child: Focus(autofocus: true, child: child),
      ),
    );
  }
}
