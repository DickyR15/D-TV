import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';

enum AppNavigationEvent { push, pop, replace, remove }

class AppNavigationChange {
  const AppNavigationChange(this.event);
  final AppNavigationEvent event;
}

/// Observes all routes on the app Navigator, including dialogs and nested
/// settings screens. The home screen uses pop notifications to swallow the
/// trailing Back event after a dialog or page closes.
class AppNavigationObserver extends NavigatorObserver {
  final ValueNotifier<AppNavigationChange?> changes =
      ValueNotifier<AppNavigationChange?>(null);

  void _publish(AppNavigationEvent event) {
    changes.value = AppNavigationChange(event);
  }

  @override
  void didPush(Route<dynamic> route, Route<dynamic>? previousRoute) {
    super.didPush(route, previousRoute);
    _publish(AppNavigationEvent.push);
  }

  @override
  void didPop(Route<dynamic> route, Route<dynamic>? previousRoute) {
    super.didPop(route, previousRoute);
    _publish(AppNavigationEvent.pop);
  }

  @override
  void didReplace({Route<dynamic>? newRoute, Route<dynamic>? oldRoute}) {
    super.didReplace(newRoute: newRoute, oldRoute: oldRoute);
    _publish(AppNavigationEvent.replace);
  }

  @override
  void didRemove(Route<dynamic> route, Route<dynamic>? previousRoute) {
    super.didRemove(route, previousRoute);
    _publish(AppNavigationEvent.remove);
  }
}

final appNavigationObserver = AppNavigationObserver();
