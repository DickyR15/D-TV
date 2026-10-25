import 'dart:async';
import 'dart:math';

import 'core_bridge.dart';
import 'models.dart';
import 'ui_localization.dart';

String categoryName(String name) {
  final compact = name.trim().replaceAll(RegExp(r'\s+'), '');
  return switch (compact) {
    'AI成人短劇' || 'AI成人短剧' || 'AI短劇' || 'AI短剧' => 'AI 短劇',
    'AI成人漫劇' || 'AI成人漫剧' || 'AI漫劇' || 'AI漫剧' => 'AI 漫劇',
    'AI換臉' || 'AI换脸' => 'AI 換臉',
    'AI魔改' => 'AI 魔改',
    'AI劇' || 'AI剧' => 'AI 劇',
    '' => '未分類',
    _ => uiTraditional(name.trim()),
  };
}

class _CatalogChoice {
  _CatalogChoice(this.category);
  final CatalogCategory category;
  final requests = <String, String>{};
}

class _CatalogEntry {
  List<Drama> items = [];
  int page = 0;
  int nextPage = 1;
  bool hasMore = true;
  bool fresh = false;
  DateTime? checkedAt;
}

class _CatalogSession {
  final entries = <String, _CatalogEntry>{};
  int generation = 0;
}

class CatalogBrowser {
  static final Expando<CatalogBrowser> _sharedByRepository =
      Expando<CatalogBrowser>('D-TV catalog session');

  factory CatalogBrowser.shared(AppRepository repository) =>
      _sharedByRepository[repository] ??= CatalogBrowser(repository);

  CatalogBrowser(this.repository);
  final AppRepository repository;
  final _menus = <String, List<CatalogCategory>>{};
  final _library = <String, Map<String, Drama>>{};
  final _sessions = <String, _CatalogSession>{};
  final _cachedReads = <String, Future<CatalogPage>>{};
  // Category chips are rebuilt whenever the home screen rebuilds. Cache both
  // the merged choices and each source's distinct local category names so we
  // never rescan every cached drama just because focus or tabs changed.
  final _choicesCache = <String, List<_CatalogChoice>>{};
  final _localCategoryNames = <String, Set<String>>{};
  int _generation = 0;
  Future<void> _catalogCancellationQueue = Future<void>.value();

  /// Serialize native cancellation calls so an older cancellation can never
  /// race with (and accidentally cancel) a newer catalog request.
  Future<void> _cancelCatalogSerially() {
    final next = _catalogCancellationQueue.then<void>(
      (_) => repository.cancelCatalog(),
      onError: (Object _) => repository.cancelCatalog(),
    );
    _catalogCancellationQueue = next.catchError((Object _) {});
    return next;
  }

  Future<CatalogPage> cached(String source, {String category = ''}) {
    final key = '$source|$category';
    return _cachedReads[key] ??= repository
        .cached(source, category: category)
        .whenComplete(() {
          _cachedReads.remove(key);
        });
  }

  Future<void> cancel() {
    _generation++;
    for (final session in _sessions.values) {
      session.generation++;
    }
    return _cancelCatalogSerially();
  }

  void _invalidateChoices() => _choicesCache.clear();

  void _refreshLocalCategoryNames(String source) {
    final library = _library[source];
    _localCategoryNames[source] = {
      if (library != null)
        for (final drama in library.values) categoryName(drama.category),
    };
  }

  void _remember(String source, Iterable<Drama> items) {
    final library = _library.putIfAbsent(source, () => {});
    final knownNames = _localCategoryNames.putIfAbsent(
      source,
      () => {for (final drama in library.values) categoryName(drama.category)},
    );
    var choicesChanged = false;
    for (final drama in items) {
      if (drama.source != source) continue;
      final previous = library[drama.id];
      final merged = previous?.merge(drama) ?? drama;
      final mergedName = categoryName(merged.category);
      if (previous == null) {
        if (knownNames.add(mergedName)) choicesChanged = true;
      } else if (categoryName(previous.category) != mergedName) {
        choicesChanged = true;
      }
      library[drama.id] = merged;
    }
    if (choicesChanged) {
      _invalidateChoices();
      _refreshLocalCategoryNames(source);
    }
  }

  void updateDrama(Drama drama) {
    updateDramas([drama]);
  }

  void updateDramas(Iterable<Drama> dramas) {
    final updates = {for (final drama in dramas) drama.id: drama};
    final categoryChangedSources = <String>{};
    for (final drama in updates.values) {
      final library = _library[drama.source];
      final previous = library?[drama.id];
      if (library == null || previous == null) continue;
      final merged = previous.merge(drama);
      if (categoryName(previous.category) != categoryName(merged.category)) {
        categoryChangedSources.add(drama.source);
      }
      library[drama.id] = merged;
    }
    if (categoryChangedSources.isNotEmpty) {
      _invalidateChoices();
      for (final source in categoryChangedSources) {
        _refreshLocalCategoryNames(source);
      }
    }
    for (final session in _sessions.values) {
      for (final entry in session.entries.values) {
        entry.items = [
          for (final item in entry.items)
            updates[item.id] == null ? item : item.merge(updates[item.id]!),
        ];
      }
    }
  }

  Future<void> _each<T>(List<T> entries, Future<void> Function(T) visit) async {
    var index = 0;
    Future<void> worker() async {
      while (index < entries.length) {
        final entry = entries[index++];
        await visit(entry);
      }
    }

    await Future.wait(List.generate(min(2, entries.length), (_) => worker()));
  }

  Future<String?> loadCategories(
    SourceGroup group, {
    bool force = false,
    bool cacheOnly = false,
  }) async {
    final failures = <String>[];
    final loadedMenus = <String, List<CatalogCategory>>{};
    await _each(group.sources, (source) async {
      // Read cached dramas and fetch the server category menu concurrently.
      // Waiting for a large cached catalog must not delay the category chips.
      final cachedRead = cached(source.id).then<void>((page) {
        _remember(source.id, page.items);
      }).catchError((Object _) {});

      if (cacheOnly) {
        await cachedRead;
        return;
      }

      final categoriesRead = repository.categories(
        source.id,
        force: force,
      ).then<void>((categories) {
        loadedMenus[source.id] = categories;
      }).catchError((Object _) {
        failures.add(source.groupName);
      });
      await Future.wait<void>([cachedRead, categoriesRead]);
    });
    // Publish category menus only after every source has finished. Otherwise,
    // an unrelated catalog rebuild can expose a temporarily partial menu and
    // a selected category may query only some sources, intermittently yielding
    // an empty poster grid.
    if (loadedMenus.isNotEmpty) {
      _menus.addAll(loadedMenus);
      _invalidateChoices();
    }
    return failures.isEmpty ? null : '部分分類暫未載入，點選重試';
  }

  List<_CatalogChoice> _choices(SourceGroup group) {
    final cacheKey =
        '${group.id}|${group.sources.map((source) => source.id).join(',')}';
    final cached = _choicesCache[cacheKey];
    if (cached != null) return cached;

    final choices = <String, _CatalogChoice>{};
    for (final source in group.sources) {
      for (final category in _menus[source.id] ?? const <CatalogCategory>[]) {
        if (category.id.isEmpty) continue;
        final name = categoryName(category.name);
        final choice = choices.putIfAbsent(
          name,
          () => _CatalogChoice(CatalogCategory('category:$name', name)),
        );
        choice.requests[source.id] = category.id;
      }
    }
    final names = {
      for (final source in group.sources)
        ...?_localCategoryNames[source.id],
    }.toList()..sort();
    for (final name in names) {
      choices.putIfAbsent(
        name,
        () => _CatalogChoice(CatalogCategory('local:$name', name, local: true)),
      );
    }
    final result = choices.values.toList();
    _choicesCache[cacheKey] = result;
    return result;
  }

  List<CatalogCategory> categories(SourceGroup group) => [
    CatalogCategory.all,
    for (final choice in _choices(group)) choice.category,
  ];

  _CatalogChoice? _choice(SourceGroup group, String category) => _choices(
    group,
  ).where((choice) => choice.category.id == category).firstOrNull;

  Future<CatalogPage> load(
    SourceGroup group, {
    String category = '',
    String query = '',
    bool more = false,
    bool force = false,
    bool useCache = false,
    bool cacheOnly = false,
    void Function(CatalogPage)? onCached,
  }) async {
    if (cacheOnly && (more || force || query.trim().isNotEmpty)) {
      throw AppFailure('快取讀取不能同時請求搜尋或續頁');
    }
    final request = ++_generation;
    for (final session in _sessions.values) {
      session.generation++;
    }
    // Start cancelling obsolete network work immediately, but let disk cache
    // reads run in parallel so a previously seen catalog can paint the UI
    // before the native cancellation round-trip finishes.
    final cancellation = _cancelCatalogSerially();
    final choice = query.isEmpty ? _choice(group, category) : null;
    final requests = choice != null && !choice.category.local
        ? choice.requests
        : {for (final source in group.sources) source.id: ''};
    final key =
        '${group.sources.map((s) => s.id).join(',')}|'
        '${choice?.category.local == false ? category : ''}|$query';
    final session = _sessions.putIfAbsent(key, _CatalogSession.new);
    final generation = ++session.generation;
    final failures = <String, String>{};
    final sourceOrder = requests.keys.toList();
    for (final source in sourceOrder) {
      session.entries.putIfAbsent(source, _CatalogEntry.new);
    }

    CatalogPage snapshot() {
      final rows = sourceOrder
          .map((source) => session.entries[source]!.items)
          .toList();
      final items = <String, Drama>{};
      final count = rows.fold<int>(0, (count, row) => max(count, row.length));
      for (var index = 0; index < count; index++) {
        for (final row in rows) {
          if (index < row.length) items[row[index].id] = row[index];
        }
      }
      if (choice != null) {
        for (final source in group.sources) {
          for (final item in _library[source.id]?.values ?? const <Drama>[]) {
            if (choice.category.local ||
                categoryName(item.category) == choice.category.name) {
              items.putIfAbsent(item.id, () => item);
            }
          }
        }
      }
      return CatalogPage(
        items.values.toList(),
        hasMore: sourceOrder.any((source) => session.entries[source]!.hasMore),
        page: sourceOrder.fold<int>(
          1,
          (page, source) => max(page, session.entries[source]!.page),
        ),
        fresh: sourceOrder.every((source) => session.entries[source]!.fresh),
        warning: failures.values.toSet().join('；'),
      );
    }

    final canReuseFreshSession =
        useCache &&
        !cacheOnly &&
        !force &&
        !more &&
        query.isEmpty &&
        sourceOrder.isNotEmpty &&
        sourceOrder.every((source) {
          final entry = session.entries[source]!;
          final checkedAt = entry.checkedAt;
          if (!entry.fresh || checkedAt == null) return false;
          final age = DateTime.now().difference(checkedAt);
          return !age.isNegative && age < const Duration(minutes: 14);
        });
    if (canReuseFreshSession) {
      final memorySnapshot = snapshot();
      if (memorySnapshot.items.isNotEmpty) onCached?.call(memorySnapshot);
      // A fresh in-memory session requires no network refresh or cancellation.
      // Cancel any obsolete read in the background without delaying this result.
      unawaited(cancellation.catchError((Object _) {}));
      return memorySnapshot;
    }

    // A category switch should paint whatever is already known for that
    // exact category immediately, then refresh only if that snapshot is stale.
    // This avoids a second catalog round-trip before showing previously seen
    // posters when the selected category has already been visited.
    final hasUsableCategorySession = useCache &&
        !cacheOnly &&
        !force &&
        !more &&
        query.isEmpty &&
        sourceOrder.isNotEmpty &&
        sourceOrder.every((source) {
          final entry = session.entries[source]!;
          return entry.items.isNotEmpty;
        });
    if (hasUsableCategorySession) {
      final memorySnapshot = snapshot();
      if (memorySnapshot.items.isNotEmpty) onCached?.call(memorySnapshot);
      unawaited(cancellation.catchError((Object _) {}));
      final hasFreshEntries = sourceOrder.every((source) {
        final entry = session.entries[source]!;
        final checkedAt = entry.checkedAt;
        if (!entry.fresh || checkedAt == null) return false;
        final age = DateTime.now().difference(checkedAt);
        return !age.isNegative && age < const Duration(minutes: 14);
      });
      if (hasFreshEntries) return memorySnapshot;
    }

    if ((useCache || cacheOnly) && !force && !more && query.isEmpty) {
      await _each(sourceOrder, (source) async {
        try {
          final cached = await this.cached(source, category: requests[source]!);
          if (generation != session.generation || cached.items.isEmpty) return;
          final entry = session.entries[source]!;
          entry.items = cached.items;
          entry.page = cached.page;
          entry.nextPage = cached.page + 1;
          entry.hasMore = cached.hasMore || cached.warning.isNotEmpty;
          entry.fresh = cached.fresh && cached.warning.isEmpty;
          entry.checkedAt = DateTime.now();
          if (cached.warning.isNotEmpty) failures[source] = cached.warning;
          _remember(source, cached.items);
        } catch (error) {
          if (cacheOnly) failures[source] = error.toString();
        }
      });
      if (generation != session.generation) {
        unawaited(cancellation.catchError((Object _) {}));
        return snapshot();
      }
      final cached = snapshot();
      if (cached.items.isNotEmpty) onCached?.call(cached);
      if (cacheOnly) {
        unawaited(cancellation.catchError((Object _) {}));
        return cached;
      }
      if (cached.fresh && cached.items.isNotEmpty) {
        unawaited(cancellation.catchError((Object _) {}));
        return cached;
      }
    }

    await cancellation;
    if (request != _generation) throw AppFailure('已取消載入');

    await _each(sourceOrder, (source) async {
      if (generation != session.generation) return;
      final entry = session.entries[source]!;
      if (more && !entry.hasMore) return;
      if (useCache && !force && !more && entry.fresh) return;
      final page = more ? entry.nextPage : 1;
      try {
        final result = await repository.catalog(
          source,
          category: requests[source]!,
          query: query,
          page: page,
          force: force,
        );
        if (generation != session.generation) return;
        final items = <String, Drama>{
          if (more)
            for (final item in entry.items) item.id: item,
          for (final item in result.items) item.id: item,
        };
        entry.items = items.values.toList();
        entry.page = result.page;
        entry.nextPage = result.warning.isEmpty ? result.page + 1 : page;
        entry.hasMore =
            (query.isEmpty || SourceSite.byId(source).pagedSearch) &&
            (result.hasMore || result.warning.isNotEmpty);
        entry.fresh = result.fresh && result.warning.isEmpty;
        entry.checkedAt = DateTime.now();
        if (query.isEmpty) _remember(source, result.items);
        if (result.warning.isNotEmpty) {
          failures[source] = result.warning;
        } else {
          failures.remove(source);
        }
      } catch (error) {
        if (generation != session.generation) return;
        entry.nextPage = page;
        entry.hasMore = query.isEmpty || SourceSite.byId(source).pagedSearch;
        entry.fresh = false;
        failures[source] = error.toString();
      }
    });
    final result = snapshot();
    if (result.items.isEmpty && failures.isNotEmpty) {
      throw AppFailure(result.warning);
    }
    return result;
  }
}
