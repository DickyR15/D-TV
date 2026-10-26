import 'models.dart';

class _RankingSearchEntry {
  const _RankingSearchEntry(this.drama, this.checkedAt);
  final Drama drama;
  final DateTime checkedAt;
}

/// In-memory search index for the four Red Fruit hot-ranking boards.
class RankingSearchCache {
  static const _lifetime = Duration(minutes: 20);
  static const _maximumEntries = 600;
  static final Map<String, _RankingSearchEntry> _entries = {};

  static void remember(Iterable<Drama> dramas) {
    final now = DateTime.now();
    _prune(now);
    for (final drama in dramas) {
      if (drama.id.isEmpty || drama.source != 'hongguo') continue;
      final previous = _entries.remove(drama.id);
      _entries[drama.id] = _RankingSearchEntry(
        previous == null ? drama : previous.drama.merge(drama),
        now,
      );
    }
    while (_entries.length > _maximumEntries) {
      _entries.remove(_entries.keys.first);
    }
  }

  static List<Drama> matching(
    String query, {
    required Set<String> sources,
  }) {
    final needle = _searchText(query);
    if (needle.isEmpty || !sources.contains('hongguo')) return const [];
    _prune(DateTime.now());
    final results = [
      for (final entry in _entries.values)
        if (sources.contains(entry.drama.source) &&
            _searchText(entry.drama.title).contains(needle))
          entry.drama,
    ];
    results.sort((left, right) {
      final leftRank = _matchRank(left.title, needle);
      final rightRank = _matchRank(right.title, needle);
      if (leftRank != rightRank) return leftRank.compareTo(rightRank);
      return left.title.compareTo(right.title);
    });
    return results;
  }

  static void _prune(DateTime now) {
    _entries.removeWhere(
      (_, entry) =>
          now.difference(entry.checkedAt) >= _lifetime ||
          now.isBefore(entry.checkedAt),
    );
  }

  static String _searchText(String value) =>
      value.toLowerCase().replaceAll(RegExp(r'\s+'), '');

  static int _matchRank(String title, String query) {
    final value = _searchText(title);
    if (value == query) return 0;
    if (value.startsWith(query)) return 1;
    return 2;
  }

  /// Clears process-local entries for deterministic unit tests.
  static void clearForTesting() => _entries.clear();
}
