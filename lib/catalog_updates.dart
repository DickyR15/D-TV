import 'package:flutter/foundation.dart';

import 'models.dart';

class CatalogUpdates extends ChangeNotifier {
  final _items = <String, Drama>{};
  final _coverRetries = <String, int>{};
  Drama? latest;

  Drama current(Drama drama) {
    final key = drama.stableId;
    return _items[key] == null ? drama : drama.merge(_items[key]!);
  }

  int coverRevision(String id) => _coverRetries[id] ?? 0;

  void publish(Drama drama, {bool retryCover = false}) {
    final key = drama.stableId;
    final previous = _items[key];
    latest = previous?.merge(drama) ?? drama;
    _items[key] = latest!;
    if (retryCover) _coverRetries[key] = coverRevision(key) + 1;
    notifyListeners();
  }
}
