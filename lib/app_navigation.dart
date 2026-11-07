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

/// Handles TV remote Back at the Navigator boundary so dialogs and child
/// routes receive Back even when the home route's PopScope blocks route exit.
/// When no child route is on top, the home route keeps its own tab/exit policy.
class AppRouteBackHandler extends StatelessWidget {
  const AppRouteBackHandler({
    super.key,
    required this.television,
    required this.child,
  });

  final bool television;
  final Widget child;

  KeyEventResult _handleKey(BuildContext context, KeyEvent event) {
    if (!television ||
        event is! KeyDownEvent ||
        (event.logicalKey != LogicalKeyboardKey.escape &&
            event.logicalKey != LogicalKeyboardKey.goBack) ||
        !appNavigationObserver.hasChildRoute) {
      return KeyEventResult.ignored;
    }

    // This handler is installed above MaterialApp's Navigator, so its own
    // context is intentionally above the Navigator. Resolve from the focused
    // route widget instead (dialog, settings page, or player content).
    final focusedContext = FocusManager.instance.primaryFocus?.context;
    final navigator = focusedContext == null
        ? null
        : Navigator.maybeOf(focusedContext, rootNavigator: true);
    if (navigator == null) return KeyEventResult.ignored;
    unawaited(navigator.maybePop());
    return KeyEventResult.handled;
  }

  @override
  Widget build(BuildContext context) => Focus(
    canRequestFocus: false,
    skipTraversal: true,
    onKeyEvent: (node, event) => _handleKey(context, event),
    child: child,
  );
}
