part of 'local_store.dart';

extension LocalStoreSync on LocalStore {
  Map<String, dynamic> _backupFollowSync(String profile) {
    final raw = _string(_key('lanRecords', profile));
    if (raw == null) return {};
    final document = LanDocument.fromJson(jsonDecode(raw));
    return {
      'followSync': document.records.values
          .map((record) => record.toJson())
          .toList(),
    };
  }

  Map<String, LanRecord>? _readBackupFollowSync(Map library) {
    final raw = library['followSync'];
    if (raw == null) return null;
    if (raw is! List || raw.length > LanDocument.limit) {
      throw const FormatException('備份同步記錄數量無效');
    }
    final records = <String, LanRecord>{};
    for (final row in raw) {
      final record = LanRecord.fromJson(row);
      if (records.containsKey(record.id)) {
        throw const FormatException('備份同步記錄重複');
      }
      records[record.id] = record;
    }
    String favoriteKey(Drama drama) =>
        drama.source == 'hongguo' && drama.seriesGroupId.isNotEmpty
        ? 'series:' + drama.seriesGroupId
        : 'id:' + drama.id;
    final favorites = (library['favorites'] as List)
        .map((row) => Drama.fromJson(Map<String, dynamic>.from(row as Map)))
        .map(favoriteKey)
        .toSet();
    final followed = records.values
        .where((record) => record.followed)
        .map((record) => favoriteKey(record.drama))
        .toSet();
    if (favorites.length != followed.length ||
        !followed.containsAll(favorites)) {
      throw const FormatException('備份同步記錄與收藏列表不一致');
    }
    return records;
  }

  Map<String, Drama> _lanSeriesFavoriteRows(
    Map<String, Drama> favorites,
    Map<String, WatchEntry> history,
  ) {
    final rows = Map<String, Drama>.from(favorites);
    final groups = favorites.values
        .where(
          (drama) =>
              drama.source == 'hongguo' && drama.seriesGroupId.isNotEmpty,
        )
        .map((drama) => drama.seriesGroupId)
        .toSet();
    for (final groupId in groups) {
      for (final drama in _seriesCandidates.values) {
        if (drama.source == 'hongguo' &&
            drama.seriesGroupId == groupId) {
          rows.putIfAbsent(drama.id, () => drama);
        }
      }
      for (final entry in history.entries) {
        final drama = entry.value.drama;
        if (drama.source == 'hongguo' &&
            drama.seriesGroupId == groupId) {
          rows.putIfAbsent(drama.id, () => drama);
        }
      }
    }
    return rows;
  }

  Map<String, FollowState> _lanSeriesFollowStates(
    Map<String, Drama> favorites,
    Map<String, FollowState> states,
    Map<String, WatchEntry> history,
  ) {
    final result = Map<String, FollowState>.from(states);
    final rows = _lanSeriesFavoriteRows(favorites, history);
    final groups = favorites.values
        .where(
          (drama) =>
              drama.source == 'hongguo' && drama.seriesGroupId.isNotEmpty,
        )
        .map((drama) => drama.seriesGroupId)
        .toSet();
    for (final groupId in groups) {
      final canonicalId = 'hongguo:' + groupId;
      final anchor = favorites[canonicalId] ??
          favorites.values.firstWhere(
            (drama) => drama.seriesGroupId == groupId,
          );
      final shared = states[canonicalId] ??
          states[anchor.id] ??
          FollowState.initial(anchor, history[anchor.id]);
      for (final drama in rows.values) {
        if (drama.seriesGroupId != groupId) continue;
        var own = states[drama.id] ??
            FollowState.initial(drama, history[drama.id]);
        final watched = history[drama.id];
        if (watched != null) own = own.afterPlayback(watched);
        result[drama.id] = FollowState(
          status: shared.status,
          manuallyWatched: shared.manuallyWatched,
          knownEpisodes: own.knownEpisodes,
          readEpisodes: own.readEpisodes,
          seriesSeasons: shared.seriesSeasons,
        );
      }
    }
    return result;
  }

  LanDocument get lanDocument {
    if (locked) throw StateError('請先解鎖目前使用者');
    final cached = _lanDocumentCache;
    if (cached != null) return cached.copy();
    final raw = _string(_key('lanRecords'));
    final document = raw == null
        ? LanDocument.empty()
        : LanDocument.fromJson(jsonDecode(raw));
    final favoriteRows = _lanSeriesFavoriteRows(_favorites, _history);
    final stateRows = _lanSeriesFollowStates(
      _favorites,
      _followStates,
      _history,
    );
    if (raw == null) {
      document.reconcile(
        previous: const {},
        favorites: favoriteRows,
        states: stateRows,
        history: _history,
        oldHistory: const {},
      );
    } else {
      final oldHistory = {
        for (final record in document.records.values)
          if (record.watch case final watch?) record.id: watch,
      };
      document.reconcile(
        previous: favoriteRows,
        favorites: favoriteRows,
        states: stateRows,
        history: _history,
        oldHistory: oldHistory,
      );
    }
    _lanDocumentCache = document;
    return document.copy();
  }

  Map<String, dynamic> get lanSettings {
    if (locked) return {};
    return lanMap(jsonDecode(_string(_key('lanSettings')) ?? '{}'));
  }

  Future<void> saveLanSettings(Map<String, dynamic> value) =>
      _setting(_key('lanSettings'), jsonEncode(value));

  Future<void> ensureLanRecords() {
    final epoch = _epoch;
    return _queue(() async {
      if (locked || epoch != _epoch) throw StateError('目前使用者已變更');
      final document = lanDocument;
      await _commit({
        _key('lanRecords'): jsonEncode(document.toJson()),
      }, trackSync: false);
      _lanDocumentCache = document;
    });
  }

  void _trackLanChanges(
    Map<String, Object> values, {
    required Set<String> keys,
    Set<String> clearProgress = const {},
  }) {
    if (locked ||
        !keys.any(
          (key) =>
              key == _key('favorites') ||
              key == _key('history') ||
              key == _key('followStates'),
        )) {
      return;
    }
    final document = lanDocument;
    final favorites = <String, Drama>{};
    final history = <String, WatchEntry>{};
    final rawStates = lanMap(
      jsonDecode(values[_key('followStates')] as String? ?? '{}'),
    );
    final states = <String, FollowState>{
      for (final entry in rawStates.entries)
        entry.key: FollowState.fromJson(lanMap(entry.value)),
    };
    for (final row in readJsonList(values[_key('favorites')] as String?)) {
      final drama = Drama.fromJson(row);
      favorites[drama.id] = drama;
    }
    for (final row in readJsonList(values[_key('history')] as String?)) {
      final watch = WatchEntry.fromJson(row);
      history[watch.drama.id] = watch;
    }
    final previousFavorites = _lanSeriesFavoriteRows(_favorites, _history);
    final nextFavorites = _lanSeriesFavoriteRows(favorites, history);
    final nextStates = _lanSeriesFollowStates(favorites, states, history);
    document.reconcile(
      previous: previousFavorites,
      favorites: nextFavorites,
      states: nextStates,
      history: history,
      oldHistory: _history,
      clearProgress: clearProgress,
    );
    values[_key('lanRecords')] = jsonEncode(document.toJson());
  }

  Map<String, dynamic>? lanReceipt(String operation) {
    final rows = readJsonList(_string(_key('lanReceipts')));
    return rows.where((row) => row['operation'] == operation).firstOrNull;
  }

  Future<void> applyLanRecords({
    required String operation,
    required String base,
    required Set<String> sources,
    required Iterable<LanRecord> records,
    required int epoch,
    bool Function()? isCurrent,
  }) {
    final incoming = records.toList();
    return _queue(() async {
      if (locked ||
          epoch != _epoch ||
          isCurrent?.call() == false ||
          sources.isEmpty ||
          !sources.every(allowsSource)) {
        throw StateError('目前使用者或片源權限已變更，請重新連線');
      }
      if (lanReceipt(operation) != null) return;
      final document = lanDocument;
      if (document.hashFor(sources) != base) {
        throw StateError('記錄剛剛發生變化，請重新同步或預覽');
      }
      if (incoming.length > LanDocument.limit ||
          incoming.map((record) => record.id).toSet().length !=
              incoming.length) {
        throw const FormatException('同步記錄數量無效');
      }
      final favorites = Map.of(_favorites);
      final states = Map.of(_followStates);
      final history = Map.of(_history);
      for (final record in incoming) {
        if (!sources.contains(record.drama.source)) {
          throw StateError('同步包含本次範圍外的站源');
        }
        final checked = LanRecord.fromJson(record.toJson());
        document.records[checked.id] = checked;
        if (checked.followed) {
          favorites[checked.id] = checked.drama;
          states[checked.id] = checked.following.copyWith(
            seriesSeasons: states[checked.id]?.seriesSeasons ?? const {},
          );
          final watch = checked.watch;
          if (watch == null) {
            history.remove(checked.id);
          } else {
            history[checked.id] = watch;
          }
        } else {
          favorites.remove(checked.id);
          states.remove(checked.id);
        }
      }
      if (favorites.length > 20000 ||
          document.records.length > LanDocument.limit) {
        throw StateError('合併後的收藏記錄超過本機上限，原記錄已保留');
      }
      final sorted = history.values.toList()
        ..sort((a, b) => b.updatedAt.compareTo(a.updatedAt));
      final receipts = readJsonList(_string(_key('lanReceipts')))
        ..add({
          'operation': operation,
          'hash': document.hashFor(sources),
          'savedAt': DateTime.now().toUtc().toIso8601String(),
        });
      await _commit({
        _key('favorites'): jsonEncode(
          favorites.values.map((drama) => drama.toJson()).toList(),
        ),
        _key('followStates'): _encodeFollowStates(states),
        _key('history'): jsonEncode(
          sorted.take(300).map((entry) => entry.toJson()).toList(),
        ),
        _key('lanRecords'): jsonEncode(document.toJson()),
        _key('lanReceipts'): jsonEncode(
          receipts.reversed.take(64).toList().reversed.toList(),
        ),
      }, trackSync: false);
      _loadLibrary();
      _notify();
    });
  }

  Future<void> resolveLanConflict(
    String id,
    String field,
    LanValue choice,
    String base,
  ) {
    final epoch = _epoch;
    return _queue(() async {
      if (locked || epoch != _epoch) throw StateError('目前使用者已變更');
      final document = lanDocument;
      final record = document.records[id];
      final cell = record?.fields[field];
      if (record == null ||
          cell == null ||
          record.hash != base ||
          !allowsSource(record.drama.source)) {
        throw StateError('記錄已更新，請重新選擇');
      }
      if (!cell.values.any(
        (candidate) => lanJSON(candidate.toJson()) == lanJSON(choice.toJson()),
      )) {
        throw StateError('此衝突選項已失效');
      }
      final next = record.withField(field, document.edit(cell, choice.value));
      document.records[id] = next;
      final favorites = Map.of(_favorites);
      final states = Map.of(_followStates);
      final history = Map.of(_history);
      if (next.followed) {
        favorites[id] = next.drama;
        states[id] = next.following.copyWith(
          seriesSeasons: states[id]?.seriesSeasons ?? const {},
        );
        if (next.watch case final watch?) {
          history[id] = watch;
        } else {
          history.remove(id);
        }
      } else {
        favorites.remove(id);
        states.remove(id);
      }
      final sorted = history.values.toList()
        ..sort((a, b) => b.updatedAt.compareTo(a.updatedAt));
      await _commit({
        _key('favorites'): jsonEncode(
          favorites.values.map((drama) => drama.toJson()).toList(),
        ),
        _key('followStates'): _encodeFollowStates(states),
        _key('history'): jsonEncode(
          sorted.take(300).map((entry) => entry.toJson()).toList(),
        ),
        _key('lanRecords'): jsonEncode(document.toJson()),
      }, trackSync: false);
      _lanRevision++;
      _lanUrgentRevision++;
      _loadLibrary();
      _notify();
    });
  }
}
