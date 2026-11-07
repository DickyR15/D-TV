import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

enum AppNavigationEvent { push, pop, replace, remove }

class AppNavigationChange {
  const AppNavigationChange(this.event);
  final AppNavigationEvent event;
}

class AppNavigationObserver extends NavigatorObserver {
  final ValueNotifier<AppNavigationChange?> changes =
      ValueNotifier<AppNavigationChange?>(null);
  final List<Route<dynamic>> _routeStack = <Route<dynamic>>[];
  NavigatorState? _trackedNavigator;

  bool get hasChildRoute => _routeStack.length > 1;

  void _publish(AppNavigationEvent event) {
    changes.value = AppNavigationChange(event);
  }

  @override
  void didPush(Route<dynamic> route, Route<dynamic>? previousRoute) {
    super.didPush(route, previousRoute);
    // A shared observer may be attached to a new Navigator in tests or after
    // an app-root rebuild. Never carry routes from the previous Navigator.
    if (!identical(_trackedNavigator, navigator)) {
      _routeStack.clear();
      _trackedNavigator = navigator;
    }
    _routeStack.remove(route);
    _routeStack.add(route);
    _publish(AppNavigationEvent.push);
  }

  @override
  void didPop(Route<dynamic> route, Route<dynamic>? previousRoute) {
    super.didPop(route, previousRoute);
    _routeStack.remove(route);
    _publish(AppNavigationEvent.pop);
  }

  @override
  void didReplace({Route<dynamic>? newRoute, Route<dynamic>? oldRoute}) {
    super.didReplace(newRoute: newRoute, oldRoute: oldRoute);
    if (oldRoute != null) {
      final index = _routeStack.indexOf(oldRoute);
      if (index >= 0) {
        if (newRoute != null) {
          _routeStack[index] = newRoute;
        } else {
          _routeStack.removeAt(index);
        }
      } else if (newRoute != null) {
        _routeStack.add(newRoute);
      }
    } else if (newRoute != null) {
      _routeStack.add(newRoute);
    }
    _publish(AppNavigationEvent.replace);
  }

  @override
  void didRemove(Route<dynamic> route, Route<dynamic>? previousRoute) {
    super.didRemove(route, previousRoute);
    _routeStack.remove(route);
    _publish(AppNavigationEvent.remove);
  }
}

final appNavigationObserver = AppNavigationObserver();

class AppRouteBackIntent extends Intent {
  const AppRouteBackIntent();
}

class _AppRouteBackAction extends Action<AppRouteBackIntent> {
  _AppRouteBackAction({
    required this.enabled,
    required this.onInvoke,
  });

  final bool Function() enabled;
  final VoidCallback onInvoke;

  @override
  bool isEnabled(AppRouteBackIntent intent) => enabled();

  @override
  Object? invoke(AppRouteBackIntent intent) {
    onInvoke();
    return null;
  }
}

/// Handles TV remote Back above MaterialApp's Navigator. Shortcuts/Actions
/// are shared by the Navigator overlay, so dialogs and child pages receive
/// Back even when the home route's PopScope blocks route exit. When no child
/// route is on top, the shortcut stays disabled and HomeScreen keeps its own
/// tab/exit policy.
class AppRouteBackHandler extends StatelessWidget {
  const AppRouteBackHandler({
    super.key,
    required this.television,
    required this.child,
  });

  final bool television;
  final Widget child;

  void _popTopRoute() {
    // This handler is above Navigator, so resolve the active Navigator through
    // the currently focused widget inside its top route or dialog.
    final focusedContext = FocusManager.instance.primaryFocus?.context;
    final navigator = focusedContext == null
        ? null
        : Navigator.maybeOf(focusedContext, rootNavigator: true);
    if (navigator != null) unawaited(navigator.maybePop());
  }

  @override
  Widget build(BuildContext context) => Actions(
    actions: {
      AppRouteBackIntent: _AppRouteBackAction(
        enabled: () => television && appNavigationObserver.hasChildRoute,
        onInvoke: _popTopRoute,
      ),
    },
    child: Shortcuts(
      shortcuts: const <ShortcutActivator, Intent>{
        SingleActivator(LogicalKeyboardKey.escape): AppRouteBackIntent(),
        SingleActivator(LogicalKeyboardKey.goBack): AppRouteBackIntent(),
      },
      child: child,
    ),
  );
}
