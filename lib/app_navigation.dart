import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

enum AppNavigationEvent { push, pop, replace, remove }

class AppNavigationChange {
  const AppNavigationChange(this.event, {this.topRoute = true});
  final AppNavigationEvent event;

  /// Whether the changed route was the topmost route when Navigator reported it.
  /// A menu can finish popping after its selection callback has already pushed
  /// Settings; that late pop must not suppress the next intentional Back.
  final bool topRoute;
}

class AppNavigationObserver extends NavigatorObserver {
  final ValueNotifier<AppNavigationChange?> changes =
      ValueNotifier<AppNavigationChange?>(null);
  final List<Route<dynamic>> _routeStack = <Route<dynamic>>[];
  NavigatorState? _trackedNavigator;

  bool get hasChildRoute => _routeStack.length > 1;

  void _publish(AppNavigationEvent event, {bool topRoute = true}) {
    changes.value = AppNavigationChange(event, topRoute: topRoute);
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
    final wasTopRoute =
        _routeStack.isNotEmpty && identical(_routeStack.last, route);
    _routeStack.remove(route);
    _publish(AppNavigationEvent.pop, topRoute: wasTopRoute);
  }

  @override
  void didReplace({Route<dynamic>? newRoute, Route<dynamic>? oldRoute}) {
    super.didReplace(newRoute: newRoute, oldRoute: oldRoute);
    var wasTopRoute = true;
    if (oldRoute != null) {
      final index = _routeStack.indexOf(oldRoute);
      wasTopRoute = index >= 0 && index == _routeStack.length - 1;
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
    _publish(AppNavigationEvent.replace, topRoute: wasTopRoute);
  }

  @override
  void didRemove(Route<dynamic> route, Route<dynamic>? previousRoute) {
    super.didRemove(route, previousRoute);
    final wasTopRoute =
        _routeStack.isNotEmpty && identical(_routeStack.last, route);
    _routeStack.remove(route);
    _publish(AppNavigationEvent.remove, topRoute: wasTopRoute);
  }
}

final appNavigationObserver = AppNavigationObserver();

/// Gives a modal settings chooser its own TV remote Back target. The dialog
/// route owns this Focus, so Escape/GoBack dismisses that chooser rather than
/// bubbling to the home screen's navigation policy.
class DialogRouteBackHandler extends StatelessWidget {
  const DialogRouteBackHandler({
    super.key,
    required this.onBack,
    required this.child,
  });

  final VoidCallback onBack;
  final Widget child;

  @override
  Widget build(BuildContext context) => Focus(
    canRequestFocus: false,
    skipTraversal: true,
    onKeyEvent: (node, event) {
      if (event is KeyDownEvent &&
          (event.logicalKey == LogicalKeyboardKey.escape ||
              event.logicalKey == LogicalKeyboardKey.goBack)) {
        onBack();
        return KeyEventResult.handled;
      }
      return KeyEventResult.ignored;
    },
    child: FocusTraversalGroup(child: child),
  );
}
