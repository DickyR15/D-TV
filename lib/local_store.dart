import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:crypto/crypto.dart' as crypto;
import 'package:path/path.dart' as path;
import 'package:path_provider/path_provider.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'local_profiles.dart';
import 'local_files.dart';
import 'local_snapshot.dart';
import 'models.dart';
import 'playback_preferences.dart';
import 'download_preferences.dart';
import 'catalog_sort.dart';
import 'follow_state.dart';
import 'hongguo_series.dart';
import 'lan_sync_models.dart';

part 'local_store_sync.dart';

Object? _canonicalBackupValue(Object? value) {
  if (value is Map) {
    final keys = value.keys.map((key) => key.toString()).toList()..sort();
    return <String, Object?>{
      for (final key in keys) key: _canonicalBackupValue(value[key]),
    };
  }
  if (value is List) {
    return value.map(_canonicalBackupValue).toList();
  }
  return value;
}

String _canonicalBackupJson(Object? value) =>
    jsonEncode(_canonicalBackupValue(value));

class LocalStore extends ChangeNotifier {
  /// [pinHasher] 可注入，便於測試替換掉預設的 isolate 雜湊實現。
  LocalStore(
    this.preferences, {
    Future<String> Function(String, String)? pinHasher,
    bool assumeAuthenticated = false,
  }) : _pinHasher = pinHasher ?? hashProfilePin,
       _assumeAuthenticated = assumeAuthenticated,
       _fileStore = null {
    _initialize();
  }

  LocalStore._fileBacked(
    this.preferences,
    this._pinHasher,
    this._assumeAuthenticated,
    this._fileStore,
    LocalSnapshot snapshot,
  ) : _snapshot = snapshot {
    _initialize(snapshot: snapshot);
  }

  static Future<LocalStore> open(
    SharedPreferences preferences, {
    Directory? storageDirectory,
    Future<String> Function(String, String)? pinHasher,
    bool assumeAuthenticated = false,
  }) async {
    final directory = storageDirectory ??
        Directory(
          path.join(
            (await getApplicationSupportDirectory()).path,
            'D TV',
            'store',
          ),
        );
    final files = LocalFiles(directory);
    try {
      final snapshot = await LocalSnapshot.open(preferences, files);
      // Move the two auxiliary per-user caches from their legacy preference
      // keys into the same per-user file store before deleting those keys.
      final ids = <String>{'default'};
      final rawProfiles = snapshot.getString('profiles');
      if (rawProfiles != null) {
        final rows = jsonDecode(rawProfiles);
        if (rows is List) {
          for (final row in rows) {
            if (row is Map && row['id'] is String) {
              ids.add(row['id'] as String);
            }
          }
        }
      }
      final migrated = snapshot.values;
      var auxiliaryChanged = false;
      for (final id in ids) {
        final seriesPreference = preferences.getString('seriesCandidates.$id');
        final seriesKey = id == 'default'
            ? 'seriesCandidates'
            : 'profile.$id.seriesCandidates';
        if (seriesPreference != null && !migrated.containsKey(seriesKey)) {
          migrated[seriesKey] = seriesPreference;
          auxiliaryChanged = true;
        }
        final checksPreference = preferences.getString('followChecks.$id');
        final checksKey = id == 'default'
            ? 'followChecks'
            : 'profile.$id.followChecks';
        if (checksPreference != null && !migrated.containsKey(checksKey)) {
          migrated[checksKey] = checksPreference;
          auxiliaryChanged = true;
        }
      }
      try {
        if (auxiliaryChanged) await snapshot.commit(migrated);
        for (final key in preferences.getKeys().toList()) {
          if (key.startsWith('seriesCandidates.') ||
              key.startsWith('followChecks.')) {
            await preferences.remove(key);
          }
        }
      } catch (_) {
        // Keep the legacy keys if their copy could not be written. The
        // file-backed getters below use them as a read-only fallback.
      }
      return LocalStore._fileBacked(
        preferences,
        pinHasher ?? hashProfilePin,
        assumeAuthenticated,
        files,
        snapshot,
      );
    } catch (_) {
      // A failed first-time migration leaves the old SharedPreferences
      // snapshot untouched, so keep the app usable from that source.
      if (!await files.exists()) {
        return LocalStore(
          preferences,
          pinHasher: pinHasher,
          assumeAuthenticated: assumeAuthenticated,
        );
      }
      // If the users index itself is damaged and no backup is readable, keep
      // access locked. A validated settings backup can atomically rebuild it.
      final store = LocalStore._fileBacked(
        preferences,
        pinHasher ?? hashProfilePin,
        assumeAuthenticated,
        files,
        LocalSnapshot.emptyFileBacked(preferences, files),
      );
      store._block('本機使用者索引損毀，原始檔案已保留；請從備份恢復使用者與設定。');
      return store;
    }
  }

  final SharedPreferences preferences;
  final Future<String> Function(String pin, String salt) _pinHasher;
  final LocalFiles? _fileStore;
  // Only a player window created by an already-unlocked app session may use
  // this. Profile and per-source permissions are still loaded and enforced.
  final bool _assumeAuthenticated;
  LocalSnapshot? _snapshot;
  LanDocument? _lanDocumentCache;
  int _lanRevision = 0;
  int _lanUrgentRevision = 0;
  int get lanRevision => _lanRevision;
  int get lanUrgentRevision => _lanUrgentRevision;
  final Map<String, WatchEntry> _history = {};
  final Map<String, Drama> _favorites = {};
  final Map<String, FollowState> _followStates = {};
  final Map<String, Drama> _seriesCandidates = {};
  Future<void> _writes = Future<void>.value();
  List<LocalProfile> _profiles = [];
  String _current = 'default';
  bool _locked = false;
  bool _disposed = false;
  String? _configurationError;
  int _epoch = 0;
  int _failures = 0;
  Map<String, bool> _sourceSwitches = {};
  bool _advancedSourcesUnlocked = false;
  int _sourceAccessEpoch = 0;
  DateTime _retryAfter = DateTime(2000);

  void _initialize({LocalSnapshot? snapshot}) {
    try {
      _snapshot = snapshot ?? LocalSnapshot(preferences);
      final raw = _snapshot!.getString('profiles');
      if (raw == null &&
          (_snapshot!.getString('activeProfile') != null ||
              _snapshot!.values.keys.any(
                (key) => key.startsWith('profile.'),
              ))) {
        throw const FormatException('已有使用者配置缺失');
      }
      _profiles = raw == null
          ? [const LocalProfile(id: 'default', name: '管理員', admin: true)]
          : _readProfiles(jsonDecode(raw));
      _current = _snapshot!.getString('activeProfile') ?? 'default';
      if (!_profiles.any((profile) => profile.id == _current)) {
        _current = _profiles.firstWhere((profile) => profile.admin).id;
      }
      _configurationError = null;
      _locked = !_assumeAuthenticated && forceLogin && profile.protected;
      _loadSourceSwitches();
      _loadLibrary();
      _loadSeriesCandidates();
      _queue(() => _moveSeriesCandidates().catchError((Object _) {}));
    } catch (_) {
      _block('本機使用者設定損毀，已鎖定存取。原始紀錄已保留，請重新讀取或從備份還原。');
    }
  }

  static List<LocalProfile> _readProfiles(dynamic records) {
    final profiles = (records as List)
        .map(
          (row) => LocalProfile.fromJson(Map<String, dynamic>.from(row as Map)),
        )
        .toList();
    if (profiles.isEmpty ||
        profiles.length > 20 ||
        profiles.map((profile) => profile.id).toSet().length !=
            profiles.length ||
        profiles.where((profile) => profile.admin).length != 1 ||
        (profiles.length > 1 &&
            !profiles.firstWhere((profile) => profile.admin).protected)) {
      throw const FormatException('使用者配置無效');
    }
    return profiles;
  }

  void _block(String message) {
    _sourceAccessEpoch++;
    _advancedSourcesUnlocked = false;
    _configurationError = message;
    _locked = true;
    _profiles = [
      const LocalProfile(id: 'default', name: '設定待還原', download: false),
    ];
    _current = 'default';
    _history.clear();
    _favorites.clear();
    _followStates.clear();
    _seriesCandidates.clear();
    _epoch++;
  }

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  String? _string(String key) => _snapshot?.getString(key);
  bool? _bool(String key) => _snapshot?.getBool(key);
  String _key(String key, [String? id]) =>
      (id ?? _current) == 'default' ? key : 'profile.${id ?? _current}.$key';
  double get playerVolume {
    final value = preferences.getDouble('playerVolume') ?? 100.0;
    if (!value.isFinite || value <= 0) return 100.0;
    return value.clamp(1.0, 100.0).toDouble();
  }

  Future<void> setPlayerVolume(double value) {
    if (!value.isFinite || value <= 0) return Future.value();
    return _queue(() async {
      await preferences.setDouble(
        'playerVolume',
        value.clamp(1.0, 100.0).toDouble(),
      );
    });
  }

  String? get configurationError => _configurationError;
  List<LocalProfile> get profiles => List.unmodifiable(_profiles);
  LocalProfile get profile =>
      _profiles.firstWhere((profile) => profile.id == _current);
  bool get administratorPasswordConfigured =>
      _profiles.any((candidate) => candidate.admin && candidate.protected);
  bool get advancedSourcesUnlocked => _advancedSourcesUnlocked;
  bool get locked => _locked || _configurationError != null;
  int get profileEpoch => _epoch;
  bool get forceLogin {
    if (_configurationError != null) return true;
    final admin = _profiles.firstWhere((profile) => profile.admin);
    return _bool('forceLogin') ?? admin.protected;
  }

  bool get canDownload => !locked && (profile.admin || profile.download);
  bool allowsSource(String source) =>
      !locked &&
      SourceSite.isAvailable(source) &&
      profile.allows(source) &&
      sourceEnabled(source);
  List<SourceSite> get sources =>
      SourceSite.values.where((site) => allowsSource(site.id)).toList();
  List<SourceSite> get permittedSources => [
    for (final site in SourceSite.values)
      if (!locked && profile.allows(site.id)) site,
  ];

  bool sourceEnabled(String source) =>
      _sourceSwitches[source] ?? !SourceSite.isRestricted(source);

  /// Validates the existing administrator password for advanced source settings.
  Future<void> unlockAdvancedSources(String pin) {
    final requestEpoch = _sourceAccessEpoch;
    final profileEpoch = _epoch;
    return _queue(() async {
      _requireAdmin();
      final admin = _profiles.firstWhere((candidate) => candidate.admin);
      if (!admin.protected) throw StateError('請先為管理員設定密碼');
      await _checkPin(admin, pin);
      if (requestEpoch != _sourceAccessEpoch ||
          profileEpoch != _epoch ||
          locked ||
          !profile.admin) {
        throw StateError('管理員身分已變更，請重新驗證');
      }
      _advancedSourcesUnlocked = true;
      _notify();
    });
  }

  /// Revokes advanced source access when the management screen is closed.
  void lockAdvancedSources() {
    _sourceAccessEpoch++;
    if (!_advancedSourcesUnlocked) return;
    _advancedSourcesUnlocked = false;
    _notify();
  }

  void _loadSourceSwitches() {
    try {
      final raw = _string('sourceSwitches');
      final stored = raw == null
          ? <String, bool>{}
          : Map<String, bool>.from(jsonDecode(raw) as Map);
      final migrated = <String, bool>{
        for (final entry in stored.entries)
          if (SourceSite.isKnown(entry.key)) entry.key: entry.value,
      };
      final oldAggregateEnabled = stored['kanmovie'];
      if (oldAggregateEnabled != null) {
        migrated.putIfAbsent('duoduo', () => oldAggregateEnabled);
      }
      // The retired Guipian switch is intentionally discarded.
      _sourceSwitches = migrated;
    } catch (_) {
      _sourceSwitches = {};
    }
  }

  Future<void> setSourceEnabled(String source, bool enabled) {
    if (!SourceSite.isAvailable(source)) {
      return Future.error(StateError('片源設定無效'));
    }
    final epoch = _epoch;
    final sourceAccessEpoch = _sourceAccessEpoch;
    return _queue(() async {
      _requireAdmin();
      if (epoch != _epoch) throw StateError('目前使用者已變更，請重試');
      if (SourceSite.isRestricted(source) &&
          (!_advancedSourcesUnlocked || sourceAccessEpoch != _sourceAccessEpoch)) {
        throw StateError('請先輸入管理員密碼解鎖進階片源');
      }
      final switches = {..._sourceSwitches, source: enabled};
      await _commit({'sourceSwitches': jsonEncode(switches)});
      _sourceSwitches = switches;
      _notify();
    });
  }

  void _loadLibrary() {
    _lanDocumentCache = null;
    _history.clear();
    _favorites.clear();
    _followStates.clear();
    if (_configurationError != null) return;
    for (final row in readJsonList(_string(_key('history')))) {
      try {
        final entry = WatchEntry.fromJson(row);
        if (SourceSite.isKnown(entry.drama.source)) {
          _history[entry.drama.id] = entry;
        }
      } catch (_) {}
    }
    for (final row in readJsonList(_string(_key('favorites')))) {
      try {
        final drama = Drama.fromJson(row);
        if (SourceSite.isKnown(drama.source)) {
          _favorites[drama.id] = drama;
        }
      } catch (_) {}
    }
    Map<String, dynamic> states = {};
    try {
      states = Map<String, dynamic>.from(
        jsonDecode(_string(_key('followStates')) ?? '{}') as Map,
      );
    } catch (_) {}
    for (final drama in _favorites.values) {
      try {
        _followStates[drama.id] = FollowState.fromJson(
          Map<String, dynamic>.from(states[drama.id] as Map),
        );
      } catch (_) {
        _followStates[drama.id] = FollowState.initial(
          drama,
          _history[drama.id],
        );
      }
    }
  }

  String _seriesKey([String? id]) => 'seriesCandidates.${id ?? _current}';
  String _followChecksKey([String? id]) => 'followChecks.${id ?? _current}';

  String? _seriesCandidatesRaw([String? id]) {
    if (_fileStore != null) {
      return _string(_key('seriesCandidates', id)) ??
          preferences.getString(_seriesKey(id));
    }
    return preferences.getString(_seriesKey(id)) ??
        _string(_key('seriesCandidates', id));
  }

  Map<String, FollowCheck> get followChecks {
    try {
      final raw = _fileStore != null
          ? _string(_key('followChecks')) ??
              preferences.getString(_followChecksKey())
          : preferences.getString(_followChecksKey());
      final data = jsonDecode(raw ?? '{}') as Map;
      return {
        for (final entry in data.entries)
          entry.key as String: FollowCheck.fromJson(entry.value),
      };
    } catch (_) {
      return {};
    }
  }

  Future<void> saveFollowChecks(
    Map<String, FollowCheck> checks, {
    required int epoch,
  }) => _queue(() async {
    if (locked || epoch != _epoch) return;
    final value = jsonEncode({
      for (final entry in checks.entries)
        if (_favorites.containsKey(entry.key))
          entry.key: entry.value.toJson(),
    });
    if (_fileStore != null) {
      await _commit({_key('followChecks'): value}, trackSync: false);
    } else {
      await preferences.setString(_followChecksKey(), value);
    }
  });

  void _loadSeriesCandidates() {
    _seriesCandidates.clear();
    if (_configurationError != null) return;
    for (final row in readJsonList(_seriesCandidatesRaw())) {
      try {
        final drama = Drama.fromJson(row);
        if (drama.source == SourceSite.hongguo.id) {
          _seriesCandidates[drama.id] = drama;
        }
      } catch (_) {}
    }
  }

  Future<void> _moveSeriesCandidates() async {
    if (_configurationError != null || _fileStore != null) return;
    final legacy = {
      for (final profile in _profiles)
        profile.id: ?_string(_key('seriesCandidates', profile.id)),
    };
    if (legacy.isEmpty) return;
    for (final entry in legacy.entries) {
      if (preferences.getString(_seriesKey(entry.key)) == null) {
        await preferences.setString(_seriesKey(entry.key), entry.value);
      }
    }
    await _commit(
      {},
      remove: legacy.keys.map((id) => _key('seriesCandidates', id)),
      trackSync: false,
    );
  }

  List<WatchEntry> get history =>
      _history.values
          .where((entry) => allowsSource(entry.drama.source))
          .toList()
        ..sort((a, b) => b.updatedAt.compareTo(a.updatedAt));
  List<Drama> get favorites => _favorites.values
      .where((drama) => allowsSource(drama.source))
      .toList()
      .reversed
      .toList();
  WatchEntry? watched(String id) {
    WatchEntry? entry = _history[id];
    if (entry == null && isFavorite(id)) {
      try {
        entry = lanDocument.records[id]?.watch;
      } catch (_) {}
    }
    return entry != null && allowsSource(entry.drama.source) ? entry : null;
  }

  bool isFavorite(String id) =>
      _favorites[id] != null && allowsSource(_favorites[id]!.source);
  FollowState? following(String id) {
    if (!isFavorite(id)) return null;
    final state = _followStates[id];
    final watched = _history[id];
    // Playback updates the watch-history file only. Derive the display status
    // from that record instead of rewriting a separate follow-state file.
    return state != null && watched != null
        ? state.afterPlayback(watched)
        : state;
  }
  List<Drama> seriesDramasFor(Drama anchor) {
    if (!allowsSource(SourceSite.hongguo.id) ||
        anchor.source != SourceSite.hongguo.id) {
      return const [];
    }
    final items = <String, Drama>{};
    for (final drama in _seriesCandidates.values) {
      if (allowsSource(drama.source)) items[drama.id] = drama;
    }
    for (final drama in _favorites.values) {
      if (allowsSource(drama.source)) items[drama.id] = drama;
    }
    for (final entry in _history.values) {
      if (allowsSource(entry.drama.source)) items[entry.drama.id] = entry.drama;
    }
    final notices = <SeriesSeasonNotice>[
      ...?_followStates[anchor.id]?.seriesSeasons.values,
      for (final state in _followStates.values)
        if (state.seriesSeasons.containsKey(anchor.id))
          state.seriesSeasons[anchor.id]!,
    ];
    for (final notice in notices) {
      items.putIfAbsent(
        notice.id,
        () => Drama(
          id: notice.id,
          source: SourceSite.hongguo.id,
          sourceId: notice.id.substring(SourceSite.hongguo.id.length + 1),
          title: notice.title,
        ),
      );
    }
    final current = items.remove(anchor.id) ?? anchor;
    final rest = items.values.toList()
      ..sort((a, b) => naturalTitleCompare(a.title, b.title));
    return [current, ...rest];
  }

  bool get hideVip =>
      _configurationError == null ? _bool(_key('hideVip')) ?? true : true;
  String get displayMode {
    final value = _configurationError == null ? _string('displayMode') : null;
    return {
      'auto', 'television', 'standard', 'phone', 'pad', 'window',
    }.contains(value) ? value! : 'auto';
  }

  String get ignoredUpdateVersion {
    if (_configurationError != null) return '';
    final value = _string('ignoredUpdateVersion') ?? '';
    return value.length <= 80 ? value : '';
  }

  bool get checkForUpdatesOnStartup {
    if (_configurationError != null) return true;
    return _bool('checkForUpdatesOnStartup') ?? true;
  }

  String get themeMode {
    final value = _configurationError == null ? _string('themeMode') : null;
    return {'light', 'dark', 'system'}.contains(value) ? value! : 'system';
  }

  String get themeAccent {
    final value = _configurationError == null ? _string('themeAccent') : null;
    // Migrate the previous selectable "brand" palette to system color without
    // invalidating older backups or persisted preferences.
    if (value == 'brand') return 'system';
    return const {
      'system', 'warmOrange', 'roseRed', 'amber', 'teal', 'ocean', 'indigo',
    }.contains(value)
        ? value!
        : 'system';
  }

  String get source {
    if (locked) return '';
    final value = _string(_key('source')) ?? '';
    return sources.any((site) => site.id == value)
        ? value
        : sources.firstOrNull?.id ?? '';
  }

  DownloadPreferences get downloadPreferences {
    if (locked) return const DownloadPreferences();
    try {
      return DownloadPreferences.fromJson(
        jsonDecode(_string(_key('downloadPreferences')) ?? '{}')
            as Map<String, dynamic>,
      );
    } catch (_) {
      return const DownloadPreferences();
    }
  }

  PlaybackPreferences get playbackPreferences {
    if (locked) return const PlaybackPreferences();
    try {
      return PlaybackPreferences.fromJson(
        jsonDecode(_string(_key('playback')) ?? '{}') as Map<String, dynamic>,
      );
    } catch (_) {
      return const PlaybackPreferences();
    }
  }

  /// One-time migration: old app versions defaulted to showing danmaku.
  /// Preserve the user's future manual choice after this migration is recorded.
  bool get needsDanmakuDefaultOffMigration =>
      !locked && _bool(_key('danmakuDefaultOffV1')) != true;

  Future<void> migrateDanmakuDefaultOff() {
    final epoch = _epoch;
    return _queue(() async {
      if (locked ||
          epoch != _epoch ||
          _bool(_key('danmakuDefaultOffV1')) == true) {
        return;
      }
      final migrated = playbackPreferences.copyWith(danmaku: false);
      await _commit({
        _key('playback'): jsonEncode(migrated.toJson()),
        _key('danmakuDefaultOffV1'): true,
      });
      _notify();
    });
  }

  /// One-time Android TV decoder migration. Old versions defaulted to an
  /// aggressive MPV preset; preserve explicit choices except that legacy
  /// optimized defaults are moved to the conservative hardware preset.
  bool get needsTvDecoderSafePresetMigration =>
      !locked && _bool(_key('tvDecoderSafePresetV1')) != true;

  Future<void> migrateTvDecoderSafePreset() {
    final epoch = _epoch;
    return _queue(() async {
      if (locked ||
          epoch != _epoch ||
          _bool(_key('tvDecoderSafePresetV1')) == true) {
        return;
      }
      final current = playbackPreferences;
      final migrated =
          current.televisionDecoder == TelevisionDecoder.optimized
          ? current.copyWith(televisionDecoder: TelevisionDecoder.hardware)
          : current;
      await _commit({
        _key('playback'): jsonEncode(migrated.toJson()),
        _key('tvDecoderSafePresetV1'): true,
      });
      _notify();
    });
  }

  /// One-time default migration for Windows and landscape Android MPV devices.
  /// Earlier versions serialized the default-off value whenever another
  /// playback preference was saved, so a missing-key check alone is not enough.
  bool get needsPortraitBlurLandscapeDefaultMigration =>
      !locked && _bool(_key('portraitBlurLandscapeDefaultV1')) != true;

  Future<void> migratePortraitBlurLandscapeDefault() {
    final epoch = _epoch;
    return _queue(() async {
      if (locked ||
          epoch != _epoch ||
          _bool(_key('portraitBlurLandscapeDefaultV1')) == true) {
        return;
      }
      final migrated = playbackPreferences.copyWith(
        portraitBlurBackground: true,
      );
      await _commit({
        _key('playback'): jsonEncode(migrated.toJson()),
        _key('portraitBlurLandscapeDefaultV1'): true,
      });
      _notify();
    });
  }

  CatalogView get catalogView {
    if (locked) return const CatalogView();
    try {
      return CatalogView.fromJson(
        Map<String, dynamic>.from(
          jsonDecode(_string(_key('catalogView')) ?? '{}') as Map,
        ),
      );
    } catch (_) {
      return const CatalogView();
    }
  }

  List<String> get recentSearches {
    if (locked) return const [];
    try {
      return (jsonDecode(_string(_key('recentSearches')) ?? '[]') as List)
          .whereType<String>()
          .take(20)
          .toList();
    } catch (_) {
      return const [];
    }
  }

  Future<void> setCatalogView(CatalogView value) =>
      _setting(_key('catalogView'), jsonEncode(value.toJson()));

  Future<void> setCatalogSource(String source, {required bool allSources}) {
    final epoch = _epoch;
    return _queue(() async {
      if (!allowsSource(source) || epoch != _epoch) {
        throw StateError('目前使用者沒有此片源的使用權限');
      }
      await _commit({
        _key('source'): source,
        _key('catalogView'): jsonEncode(
          catalogView.copyWith(allSources: allSources).toJson(),
        ),
      });
      _notify();
    });
  }

  Future<void> rememberSearch(String text) {
    final query = text.trim();
    if (query.isEmpty || query.runes.length > 80) return Future.value();
    final epoch = _epoch;
    return _queue(() async {
      if (locked || epoch != _epoch) return;
      final entries = [
        query,
        ...recentSearches.where(
          (entry) => normalizedSearchText(entry) != normalizedSearchText(query),
        ),
      ].take(20).toList();
      await _commit({_key('recentSearches'): jsonEncode(entries)});
      _notify();
    });
  }

  Future<void> clearRecentSearches() => _setting(_key('recentSearches'), '[]');

  Future<void> _commit(
    Map<String, Object> changes, {
    Iterable<String> remove = const [],
    bool replace = false,
    bool trackSync = true,
    bool syncUrgent = true,
    Set<String> clearSyncProgress = const {},
  }) async {
    final snapshot = _snapshot;
    if (snapshot == null) throw StateError('請先還原本機設定');
    final values = replace ? <String, Object>{} : snapshot.values;
    for (final key in remove) {
      values.remove(key);
    }
    values.addAll(changes);
    if (trackSync && !replace) {
      _trackLanChanges(
        values,
        keys: {...changes.keys, ...remove},
        clearProgress: clearSyncProgress,
      );
    }
    values.putIfAbsent(
      'profiles',
      () => jsonEncode(_profiles.map((profile) => profile.toJson()).toList()),
    );
    values.putIfAbsent('activeProfile', () => _current);
    try {
      await snapshot.commit(values, replace: replace);
      if (trackSync &&
          !replace &&
          {...changes.keys, ...remove}.any(
            (key) =>
                key == _key('favorites') ||
                key == _key('history') ||
                key == _key('followStates'),
          )) {
        _lanRevision++;
        if (syncUrgent) _lanUrgentRevision++;
      }
    } on SnapshotRecoveryRequired catch (error) {
      _block(error.toString());
      _notify();
      rethrow;
    }
  }

  Future<void> _setting(String key, Object value, {bool admin = false}) {
    final epoch = _epoch;
    return _queue(() async {
      if (admin) _requireAdmin();
      if (locked || epoch != _epoch) throw StateError('目前使用者已變更，請重試');
      await _commit({key: value});
      _notify();
    });
  }

  Future<void> setForceLogin(bool value) =>
      _setting('forceLogin', value, admin: true);
  Future<void> setHideVip(bool value) => _setting(_key('hideVip'), value);
  Future<void> setSource(String value) {
    if (!allowsSource(value)) return Future.error(StateError('目前使用者沒有此片源的使用權限'));
    return _setting(_key('source'), value);
  }

  Future<void> setDisplayMode(String value) =>
      {
        'auto', 'television', 'standard', 'phone', 'pad', 'window',
      }.contains(value)
      ? _setting('displayMode', value)
      : Future.value();
  Future<void> setThemeMode(String value) =>
      {'light', 'dark', 'system'}.contains(value)
      ? _setting('themeMode', value)
      : Future.value();

  Future<void> ignoreUpdateVersion(String version) {
    final normalized = version.trim();
    if (normalized.isEmpty || normalized.length > 80) {
      return Future.error(const FormatException('忽略更新版本格式無效'));
    }
    return _setting('ignoredUpdateVersion', normalized);
  }

  Future<void> clearIgnoredUpdateVersion() =>
      _setting('ignoredUpdateVersion', '');

  Future<void> setCheckForUpdatesOnStartup(bool value) =>
      _setting('checkForUpdatesOnStartup', value);

  Future<void> setThemeAccent(String value) =>
      const {
        'system', 'warmOrange', 'roseRed', 'amber', 'teal', 'ocean', 'indigo',
      }.contains(value)
      ? _setting('themeAccent', value)
      : Future.value();
  Future<void> setPlaybackPreferences(PlaybackPreferences value) {
    PlaybackPreferences.fromJson(value.toJson());
    return _setting(_key('playback'), jsonEncode(value.toJson()));
  }

  Future<void> setDownloadPreferences(DownloadPreferences value) {
    DownloadPreferences.fromJson(value.toJson());
    return _setting(_key('downloadPreferences'), jsonEncode(value.toJson()));
  }

  Future<void> toggleFavorite(Drama drama) {
    final epoch = _epoch;
    return _queue(() async {
      if (!allowsSource(drama.source) || epoch != _epoch) return;
      final entries = Map.of(_favorites);
      final states = Map.of(_followStates);
      if (entries.containsKey(drama.id)) {
        entries.remove(drama.id);
        states.remove(drama.id);
      } else {
        if (entries.length >= 20000) throw StateError('收藏數量已達上限，請先整理收藏');
        entries[drama.id] = drama;
        states[drama.id] = FollowState.initial(drama, _history[drama.id]);
      }
      await _commit({
        _key('favorites'): jsonEncode(
          entries.values.map((entry) => entry.toJson()).toList(),
        ),
        _key('followStates'): _encodeFollowStates(states),
      });
      _loadLibrary();
      _notify();
    });
  }

  String _encodeFollowStates(Map<String, FollowState> states) => jsonEncode({
    for (final entry in states.entries) entry.key: entry.value.toJson(),
  });

  Future<void> setFollowStatus(Drama drama, FollowStatus status) {
    final epoch = _epoch;
    return _queue(() async {
      if (!allowsSource(drama.source) || epoch != _epoch) return;
      if (!_favorites.containsKey(drama.id) && _favorites.length >= 20000) {
        throw StateError('收藏數量已達上限，請先整理收藏');
      }
      final current = _favorites[drama.id]?.merge(drama) ?? drama;
      final entries = Map.of(_favorites)..[drama.id] = current;
      final state =
          (_followStates[drama.id] ??
                  FollowState.initial(current, _history[drama.id]))
              .observe(current)
              .withStatus(status);
      final states = Map.of(_followStates)..[drama.id] = state;
      await _commit({
        _key('favorites'): jsonEncode(
          entries.values.map((entry) => entry.toJson()).toList(),
        ),
        _key('followStates'): _encodeFollowStates(states),
      });
      _loadLibrary();
      _notify();
    });
  }

  Future<void> markUpdatesRead(String id) {
    final epoch = _epoch;
    return _queue(() async {
      if (!isFavorite(id) || epoch != _epoch) return;
      final states = Map.of(_followStates)
        ..[id] = _followStates[id]!.markRead();
      await _commit({_key('followStates'): _encodeFollowStates(states)});
      _loadLibrary();
      _notify();
    });
  }

  Future<void> markSeriesSeasonRead(String id, String seasonId) {
    final epoch = _epoch;
    return _queue(() async {
      if (!isFavorite(id) || epoch != _epoch) return;
      final current = _followStates[id]!;
      final updated = current.markSeriesSeasonRead(seasonId);
      if (identical(current, updated)) return;
      final states = Map.of(_followStates)..[id] = updated;
      await _commit({_key('followStates'): _encodeFollowStates(states)});
      _loadLibrary();
      _notify();
    });
  }

  Future<void> saveWatch(WatchEntry entry) {
    final epoch = _epoch;
    return _queue(() async {
      if (!allowsSource(entry.drama.source) || epoch != _epoch) return;
      final previous = _history[entry.drama.id]?.drama;
      final current = previous == null
          ? entry
          : WatchEntry(
              drama: previous.merge(entry.drama),
              episode: entry.episode,
              position: entry.position,
              duration: entry.duration,
              updatedAt: entry.updatedAt,
            );
      final entries = Map.of(_history)..[entry.drama.id] = current;
      final sorted = entries.values.toList()
        ..sort((a, b) => b.updatedAt.compareTo(a.updatedAt));
      await _commit({
        _key('history'): jsonEncode(
          sorted.take(300).map((entry) => entry.toJson()).toList(),
        ),
      }, syncUrgent: false);
      _loadLibrary();
      _notify();
    });
  }

  WatchEntry? mediaWatched(String id) {
    if (!canDownload) return null;
    try {
      final data = jsonDecode(_string(_key('mediaHistory')) ?? '{}') as Map;
      final direct = data[id] == null
          ? null
          : WatchEntry.fromJson(Map<String, dynamic>.from(data[id] as Map));
      if (direct != null && allowsSource(direct.drama.source)) return direct;
      // Older aggregate-source keys are migrated by Drama.fromJson. Search
      // legacy entries once by their migrated ID so resume state is retained.
      for (final value in data.values) {
        final candidate = WatchEntry.fromJson(
          Map<String, dynamic>.from(value as Map),
        );
        if (candidate.drama.id == id && allowsSource(candidate.drama.source)) {
          return candidate;
        }
      }
      return null;
    } catch (_) {
      return null;
    }
  }

  Future<void> saveMediaWatch(String id, WatchEntry entry) {
    final epoch = _epoch;
    return _queue(() async {
      if (!canDownload ||
          !allowsSource(entry.drama.source) ||
          epoch != _epoch) {
        return;
      }
      final data = jsonDecode(_string(_key('mediaHistory')) ?? '{}') as Map;
      data.remove(id);
      data[id] = entry.toJson();
      while (data.length > 300) {
        data.remove(data.keys.first);
      }
      await _commit({_key('mediaHistory'): jsonEncode(data)});
    });
  }

  Future<void> clearHistory() {
    final epoch = _epoch;
    return _queue(() async {
      if (locked || epoch != _epoch) return;
      await _commit(
        {},
        remove: [_key('history')],
        clearSyncProgress: {..._history.keys, ...lanDocument.records.keys},
      );
      _loadLibrary();
      _notify();
    });
  }

  Future<void> clearFavorites() {
    final epoch = _epoch;
    return _queue(() async {
      if (locked || epoch != _epoch) return;
      await _commit(
        {
          _key('favorites'): '[]',
          _key('followStates'): '{}',
        },
        clearSyncProgress: {
          ..._favorites.keys,
          ..._followStates.keys,
          ...lanDocument.records.keys,
        },
      );
      _loadLibrary();
      _notify();
    });
  }

  Future<void> removeHistory(String id) {
    final epoch = _epoch;
    return _queue(() async {
      if (watched(id) == null || epoch != _epoch) return;
      final entries = Map.of(_history)..remove(id);
      await _commit(
        {
          _key('history'): jsonEncode(
            entries.values.map((entry) => entry.toJson()).toList(),
          ),
        },
        clearSyncProgress: {id},
      );
      _loadLibrary();
      _notify();
    });
  }

  Future<void> refreshDrama(Drama drama) =>
      _refreshDramas([drama], cacheSeriesCandidates: false);

  Future<void> refreshDramas(Iterable<Drama> dramas) =>
      _refreshDramas(dramas, cacheSeriesCandidates: true);

  Future<void> _refreshDramas(
    Iterable<Drama> dramas, {
    required bool cacheSeriesCandidates,
  }) {
    final epoch = _epoch;
    final updates = dramas.toList();
    return _queue(() async {
      if (locked || epoch != _epoch) return;
      final changes = <String, Object>{};
      final favorites = Map.of(_favorites);
      final history = Map.of(_history);
      final states = Map.of(_followStates);
      final seriesCandidates = Map.of(_seriesCandidates);
      for (final drama in updates) {
        if (!allowsSource(drama.source)) continue;
        if (cacheSeriesCandidates && drama.source == SourceSite.hongguo.id) {
          seriesCandidates[drama.id] = (seriesCandidates[drama.id] ?? drama)
              .merge(drama);
        }
        final favorite = favorites[drama.id];
        if (favorite != null) {
          favorites[drama.id] = favorite.merge(drama);
          states[drama.id] = states[drama.id]!.observe(favorites[drama.id]!);
        }
        final watched = history[drama.id];
        if (watched != null) {
          history[drama.id] = WatchEntry(
            drama: watched.drama.merge(drama),
            episode: watched.episode,
            position: watched.position,
            duration: watched.duration,
            updatedAt: watched.updatedAt,
          );
        }
      }
      final candidates = <String, Drama>{
        for (final entry in history.values) entry.drama.id: entry.drama,
        ...favorites,
        for (final drama in updates)
          if (allowsSource(drama.source)) drama.id: drama,
      };
      for (final state in states.values) {
        for (final notice in state.seriesSeasons.values) {
          candidates.putIfAbsent(
            notice.id,
            () => Drama(
              id: notice.id,
              source: SourceSite.hongguo.id,
              sourceId: notice.id.substring(SourceSite.hongguo.id.length + 1),
              title: notice.title,
            ),
          );
        }
      }
      final favoriteIds = favorites.keys.toSet();
      for (final favorite in favorites.values) {
        if (favorite.source != SourceSite.hongguo.id ||
            states[favorite.id] == null) {
          continue;
        }
        states[favorite.id] = observeHongguoSeriesSeasons(
          anchor: favorite,
          state: states[favorite.id]!,
          candidates: candidates.values,
          favoriteIds: favoriteIds,
        );
      }
      final favoriteJson = jsonEncode(
        favorites.values.map((entry) => entry.toJson()).toList(),
      );
      final historyJson = jsonEncode(
        history.values.map((entry) => entry.toJson()).toList(),
      );
      final statesJson = _encodeFollowStates(states);
      final cachedCandidates = cacheSeriesCandidates
          ? (seriesCandidates.values.toList()
                  ..sort((a, b) => a.id.compareTo(b.id)))
                .take(2000)
                .toList()
          : null;
      final seriesCandidateJson = cachedCandidates == null
          ? null
          : jsonEncode(
              cachedCandidates.map((entry) => entry.toJson()).toList(),
            );
      if (favoriteJson !=
          jsonEncode(
            _favorites.values.map((entry) => entry.toJson()).toList(),
          )) {
        changes[_key('favorites')] = favoriteJson;
      }
      if (historyJson !=
          jsonEncode(_history.values.map((entry) => entry.toJson()).toList())) {
        changes[_key('history')] = historyJson;
      }
      if (statesJson != _encodeFollowStates(_followStates)) {
        changes[_key('followStates')] = statesJson;
      }
      final currentSeriesCandidates = _seriesCandidatesRaw();
      final seriesChanged =
          seriesCandidateJson != null &&
          seriesCandidateJson != currentSeriesCandidates;
      if (seriesChanged) {
        if (_fileStore != null) {
          changes[_key('seriesCandidates')] = seriesCandidateJson;
        } else {
          await preferences.setString(_seriesKey(), seriesCandidateJson);
        }
        _seriesCandidates
          ..clear()
          ..addEntries(
            cachedCandidates!.map((drama) => MapEntry(drama.id, drama)),
          );
      }
      if (changes.isEmpty) {
        if (seriesChanged) _notify();
        return;
      }
      await _commit(changes);
      _loadLibrary();
      _notify();
    });
  }

  void _requireAdmin() {
    if (locked || !profile.admin) throw StateError('僅管理員可以修改此設定');
  }

  Future<void> _checkPin(LocalProfile target, String pin) async {
    if (!target.protected) return;
    if (DateTime.now().isBefore(_retryAfter)) {
      throw StateError('密碼輸入過於頻繁，請稍後再試');
    }
    final actual = await _pinHasher(pin, target.salt);
    final expected = target.pinHash;
    var difference = actual.length ^ expected.length;
    for (var index = 0; index < actual.length && index < expected.length; index++) {
      difference |= actual.codeUnitAt(index) ^ expected.codeUnitAt(index);
    }
    if (difference != 0) {
      _failures++;
      if (_failures >= 3) {
        _retryAfter = DateTime.now().add(
          Duration(seconds: (_failures * 2).clamp(0, 30)),
        );
      }
      throw StateError('密碼不正確');
    }
    _failures = 0;
  }

  Future<void> switchProfile(String id, {String pin = ''}) => _queue(() async {
    if (_configurationError != null) throw StateError('本機使用者設定損毀，請先還原');
    final target = _profiles.firstWhere((profile) => profile.id == id);
    await _checkPin(target, pin);
    await _commit({'activeProfile': id});
    _current = id;
    _locked = false;
    _sourceAccessEpoch++;
    _advancedSourcesUnlocked = false;
    _loadLibrary();
    _loadSeriesCandidates();
    _epoch++;
    _notify();
  });
  void lock() {
    final wasAdvancedUnlocked = _advancedSourcesUnlocked;
    _sourceAccessEpoch++;
    _advancedSourcesUnlocked = false;
    if (!profile.protected) {
      if (wasAdvancedUnlocked) _notify();
      return;
    }
    _locked = true;
    _epoch++;
    _notify();
  }

  Future<void> saveProfile({
    String? id,
    required String name,
    required List<String> sources,
    required bool download,
    String? pin,
    String? currentPin,
  }) => _queue(() async {
    _requireAdmin();
    final old = _profiles.where((profile) => profile.id == id).firstOrNull;
    final targetId = old?.id ?? randomProfileToken();
    final cleanName = name.trim();
    if (cleanName.isEmpty || cleanName.length > 40) {
      throw StateError('使用者名稱需要 1 至 40 個字元');
    }
    if (sources.any((source) => !SourceSite.isKnown(source))) {
      throw StateError('片源設定無效');
    }
    if (targetId != 'default' &&
        !_profiles.firstWhere((profile) => profile.admin).protected) {
      throw StateError('請先為管理員設定密碼，再建立或修改其他使用者');
    }
    if (old == null && _profiles.length >= 20) {
      throw StateError('最多支援 20 個本機使用者');
    }
    var salt = old?.salt ?? '', hash = old?.pinHash ?? '';
    if (pin != null) {
      if (targetId == 'default' && old?.protected == true) {
        await _checkPin(old!, currentPin ?? '');
      }
      if (pin.isEmpty) {
        if (targetId == 'default' && _profiles.length > 1) {
          throw StateError('存在其他使用者時不能取消管理員密碼');
        }
        salt = '';
        hash = '';
      } else {
        if (pin.length < 6 || pin.length > 128) {
          throw StateError('密碼需要 6 至 128 個字元');
        }
        salt = randomProfileToken();
        hash = await _pinHasher(pin, salt);
      }
    }
    _requireAdmin();
    final updated = LocalProfile(
      id: targetId,
      name: cleanName,
      admin: targetId == 'default',
      sources: sources.toSet().toList(),
      download: download,
      salt: salt,
      pinHash: hash,
    );
    final profiles =
        [
          for (final profile in _profiles)
            if (profile.id != targetId) profile,
          updated,
        ]..sort(
          (a, b) => a.admin
              ? -1
              : b.admin
              ? 1
              : a.name.compareTo(b.name),
        );
    await _commit({
      'profiles': jsonEncode(
        profiles.map((profile) => profile.toJson()).toList(),
      ),
    });
    _profiles = profiles;
    if (targetId == 'default' && pin != null) {
      _sourceAccessEpoch++;
      _advancedSourcesUnlocked = false;
    }
    _notify();
  });
  Future<void> deleteProfile(String id) => _queue(() async {
    _requireAdmin();
    if (id == 'default') throw StateError('不能刪除管理員');
    final profiles = _profiles.where((profile) => profile.id != id).toList();
    await _commit({
      'profiles': jsonEncode(
        profiles.map((profile) => profile.toJson()).toList(),
      ),
    }, remove: LocalSnapshot.libraryKeys.map((key) => _key(key, id)));
    await preferences.remove(_seriesKey(id));
    await preferences.remove(_followChecksKey(id));
    _profiles = profiles;
    _notify();
  });

  Future<String> exportBackup() async {
    await _writes.catchError((Object _) {});
    _requireAdmin();
    final payload = <String, dynamic>{

      'schema': 1,
      'app': 'dtv',
      'profiles': _profiles.map((profile) => profile.toJson()).toList(),
      'displayMode': displayMode,
      'themeMode': themeMode,
      'themeAccent': themeAccent,
      'forceLogin': forceLogin,
      'ignoredUpdateVersion': ignoredUpdateVersion,
      'checkForUpdatesOnStartup': checkForUpdatesOnStartup,
      'libraries': {
        for (final profile in _profiles)
          profile.id: {
            ..._backupFollowSync(profile.id),
            'history': readJsonList(_string(_key('history', profile.id))),
            'favorites': readJsonList(_string(_key('favorites', profile.id))),
            'followStates': jsonDecode(
              _string(_key('followStates', profile.id)) ?? '{}',
            ),
            'seriesCandidates': readJsonList(
              _seriesCandidatesRaw(profile.id),
            ),
            'followChecks': jsonDecode(
              (_fileStore != null
                      ? _string(_key('followChecks', profile.id))
                      : preferences.getString(_followChecksKey(profile.id))) ??
                  '{}',
            ),
            'mediaHistory': jsonDecode(
              _string(_key('mediaHistory', profile.id)) ?? '{}',
            ),
            'source': _string(_key('source', profile.id)) ?? '',
            'hideVip': _bool(_key('hideVip', profile.id)) ?? true,
            'playback': jsonDecode(
              _string(_key('playback', profile.id)) ?? '{}',
            ),
            'portraitBlurLandscapeDefaultV1':
                _bool(_key('portraitBlurLandscapeDefaultV1', profile.id)) ?? false,
            'downloadPreferences': jsonDecode(
              _string(_key('downloadPreferences', profile.id)) ?? '{}',
            ),
            'catalogView': jsonDecode(
              _string(_key('catalogView', profile.id)) ?? '{}',
            ),
            'recentSearches': jsonDecode(
              _string(_key('recentSearches', profile.id)) ?? '[]',
            ),
          },
      },
    };
    final digest = crypto.sha256
        .convert(utf8.encode(_canonicalBackupJson(payload)))
        .toString();
    final content = jsonEncode({
      ...payload,
      'integrity': {
        'algorithm': 'sha256',
        'digest': digest,
      },
    });
    if (utf8.encode(content).length > 8 * 1024 * 1024) {
      throw StateError('備份超過 8 MiB 儲存上限，請先整理記錄；尚未寫出備份檔案');
    }
    validateBackup(content);
    return content;
  }

  Map<String, dynamic> validateBackup(String content) {
    if (utf8.encode(content).length > 8 * 1024 * 1024) {
      throw const FormatException('備份檔案過大');
    }
    final data = jsonDecode(content) as Map<String, dynamic>;
    if (data['schema'] != 1 ||
        !{'dtv', 'zhenguojian', 'hongguojian'}.contains(data['app'])) {
      throw const FormatException('不支援的備份格式');
    }
    final integrity = data['integrity'];
    if (integrity != null) {
      if (integrity is! Map ||
          integrity['algorithm'] != 'sha256' ||
          integrity['digest'] is! String) {
        throw const FormatException('備份完整性資訊無效');
      }
      final expectedDigest = integrity['digest'] as String;
      final validDigest = expectedDigest.length == 64 &&
          expectedDigest.codeUnits.every(
            (unit) =>
                (unit >= 48 && unit <= 57) ||
                (unit >= 97 && unit <= 102),
          );
      if (!validDigest) {
        throw const FormatException('備份完整性資訊無效');
      }
      final payload = Map<String, dynamic>.from(data)..remove('integrity');
      final actualDigest = crypto.sha256
          .convert(utf8.encode(_canonicalBackupJson(payload)))
          .toString();
      if (actualDigest != expectedDigest) {
        throw const FormatException('備份完整性檢查失敗，檔案可能已遭修改或損毀');
      }
    }

    final profiles = _readProfiles(data['profiles']);
    if (data.containsKey('themeMode') &&
        !{'light', 'dark', 'system'}.contains(data['themeMode'])) {
      throw const FormatException('備份主題設定無效');
    }
    if (data.containsKey('themeAccent') &&
        !const {
          'system', 'brand', 'warmOrange', 'roseRed', 'amber', 'teal', 'ocean', 'indigo',
        }.contains(data['themeAccent'])) {
      throw const FormatException('備份主題配色無效');
    }
    if (data.containsKey('forceLogin') && data['forceLogin'] is! bool) {
      throw const FormatException('備份登入設定無效');
    }
    if (data.containsKey('ignoredUpdateVersion') &&
        (data['ignoredUpdateVersion'] is! String ||
            (data['ignoredUpdateVersion'] as String).length > 80)) {
      throw const FormatException('備份忽略更新版本無效');
    }
    if (data.containsKey('checkForUpdatesOnStartup') &&
        data['checkForUpdatesOnStartup'] is! bool) {
      throw const FormatException('備份啟動更新檢查設定無效');
    }
    final libraries = data['libraries'] as Map;
    for (final profile in profiles) {
      final library = libraries[profile.id] as Map;
      final history = library['history'] as List,
          favorites = library['favorites'] as List;
      final media = library['mediaHistory'] as Map? ?? {};
      if (media.length > 300 ||
          history.length > 300 ||
          favorites.length > 20000) {
        throw const FormatException('備份記錄過多');
      }
      for (final row in [...history, ...media.values]) {
        WatchEntry.fromJson(Map<String, dynamic>.from(row as Map));
      }
      for (final row in favorites) {
        Drama.fromJson(Map<String, dynamic>.from(row as Map));
      }
      final states = library['followStates'] as Map? ?? {};
      final seriesCandidates = library['seriesCandidates'] as List? ?? [];
      final favoriteIds = favorites.map((row) => (row as Map)['id']).toSet();
      _readBackupFollowSync(library);
      if (states.length > 20000 ||
          states.keys.any((id) => id is! String || !favoriteIds.contains(id))) {
        throw const FormatException('備份收藏狀態無效');
      }
      for (final row in states.values) {
        FollowState.fromJson(Map<String, dynamic>.from(row as Map));
      }
      if (seriesCandidates.length > 2000) {
        throw const FormatException('備份系列候選過多');
      }
      for (final row in seriesCandidates) {
        Drama.fromJson(Map<String, dynamic>.from(row as Map));
      }
      if (library['hideVip'] is! bool || library['source'] is! String) {
        throw const FormatException('備份設定無效');
      }
      PlaybackPreferences.fromJson(
        Map<String, dynamic>.from(library['playback'] as Map? ?? {}),
      );
      if (library.containsKey('portraitBlurLandscapeDefaultV1') &&
          library['portraitBlurLandscapeDefaultV1'] is! bool) {
        throw const FormatException('備份模糊背景預設設定無效');
      }
      DownloadPreferences.fromJson(
        Map<String, dynamic>.from(library['downloadPreferences'] as Map? ?? {}),
      );
      CatalogView.fromJson(
        Map<String, dynamic>.from(library['catalogView'] as Map? ?? {}),
      );
      final searches = library['recentSearches'] as List? ?? [];
      if (searches.length > 20 ||
          searches.any(
            (value) => value is! String || value.runes.length > 80,
          )) {
        throw const FormatException('備份搜尋記錄無效');
      }
    }
    return data;
  }

  Future<void> _installBackup(Map<String, dynamic> data) async {
    final profiles = _readProfiles(data['profiles']);
    final values = <String, Object>{
      'profiles': jsonEncode(
        profiles.map((profile) => profile.toJson()).toList(),
      ),
      'activeProfile': profiles.firstWhere((profile) => profile.admin).id,
      'displayMode':
          {
            'auto', 'television', 'standard', 'phone', 'pad', 'window',
          }.contains(data['displayMode'])
          ? data['displayMode'] as String
          : 'auto',
      'themeMode': data['themeMode'] as String? ?? themeMode,
      'themeAccent': data['themeAccent'] as String? ?? themeAccent,
      'forceLogin': data['forceLogin'] is bool
          ? data['forceLogin'] as bool
          : profiles.firstWhere((profile) => profile.admin).protected,
      'ignoredUpdateVersion': data['ignoredUpdateVersion'] as String? ?? '',
      'checkForUpdatesOnStartup':
          data['checkForUpdatesOnStartup'] as bool? ?? true,
    };
    final libraries = data['libraries'] as Map;
    for (final profile in profiles) {
      final library = libraries[profile.id] as Map;
      final syncRecords = _readBackupFollowSync(library);
      values.addAll({
        if (syncRecords != null)
          _key('lanRecords', profile.id): jsonEncode(
            LanDocument(replica: lanID(), records: syncRecords).toJson(),
          ),
        _key('history', profile.id): jsonEncode(library['history']),
        _key('favorites', profile.id): jsonEncode(library['favorites']),
        _key('followStates', profile.id): jsonEncode(
          library['followStates'] ?? {},
        ),
        _key('mediaHistory', profile.id): jsonEncode(
          library['mediaHistory'] ?? {},
        ),
        _key('seriesCandidates', profile.id): jsonEncode(
          library['seriesCandidates'] ?? [],
        ),
        _key('followChecks', profile.id): jsonEncode(
          library['followChecks'] ?? {},
        ),
        _key('source', profile.id): library['source'] as String,
        _key('hideVip', profile.id): library['hideVip'] as bool,
        _key('playback', profile.id): jsonEncode(library['playback'] ?? {}),
        if (library.containsKey('portraitBlurLandscapeDefaultV1'))
          _key('portraitBlurLandscapeDefaultV1', profile.id):
              library['portraitBlurLandscapeDefaultV1'] as bool,
        _key('downloadPreferences', profile.id): jsonEncode(
          library['downloadPreferences'] ?? {},
        ),
        _key('catalogView', profile.id): jsonEncode(
          library['catalogView'] ?? {},
        ),
        _key('recentSearches', profile.id): jsonEncode(
          library['recentSearches'] ?? [],
        ),
      });
    }
    await _commit(values, replace: true);
    for (final key in preferences.getKeys().toList()) {
      if (key.startsWith('seriesCandidates.') ||
          key.startsWith('followChecks.')) {
        await preferences.remove(key);
      }
    }
    if (_fileStore == null) {
      for (final profile in profiles) {
        await preferences.setString(
          _seriesKey(profile.id),
          jsonEncode((libraries[profile.id] as Map)['seriesCandidates'] ?? []),
        );
        await preferences.setString(
          _followChecksKey(profile.id),
          jsonEncode((libraries[profile.id] as Map)['followChecks'] ?? {}),
        );
      }
    }
    _profiles = profiles;
    _current = profiles.firstWhere((profile) => profile.admin).id;
    _configurationError = null;
    _locked = !_assumeAuthenticated && forceLogin && profile.protected;
    _loadSourceSwitches();
    _loadLibrary();
    _loadSeriesCandidates();
    _epoch++;
    _notify();
  }

  Future<void> importBackup(String content) => _queue(() async {
    _requireAdmin();
    await _installBackup(validateBackup(content));
  });

  Future<void> recoverBackup(String content, {required String pin}) =>
      _queue(() async {
        if (_configurationError == null) throw StateError('請從設定中恢復備份');
        final data = validateBackup(content);
        LocalProfile? originalAdmin;
        try {
          final activeProfiles = _string('profiles');
          final raw = preferences.getString(LocalSnapshot.storageKey);
          final legacyValues = raw == null
              ? null
              : (jsonDecode(raw) as Map)['values'] as Map;
          final records = jsonDecode(
            activeProfiles ??
                (legacyValues?['profiles'] as String?) ??
                preferences.getString('profiles') ??
                '[]',
          ) as List;
          for (final row in records) {
            if (row is Map && row['admin'] == true) {
              originalAdmin = LocalProfile.fromJson(
                Map<String, dynamic>.from(row),
              );
            }
          }
        } catch (_) {}
        final backupAdmin = _readProfiles(
          data['profiles'],
        ).firstWhere((profile) => profile.admin);
        final verifier = originalAdmin?.protected == true
            ? originalAdmin!
            : backupAdmin;
        if (!verifier.protected) throw StateError('無法驗證管理員身份，請使用包含管理員密碼的備份恢復');
        await _checkPin(verifier, pin);
        _snapshot ??= _fileStore == null
            ? LocalSnapshot.empty(preferences)
            : LocalSnapshot.emptyFileBacked(preferences, _fileStore!);
        await _installBackup(data);
      });

  Future<String> exportRecoveryData() async {
    if (_configurationError == null) throw StateError('請從設定中匯出備份');
    final files = _fileStore;
    return jsonEncode({
      'app': 'zhenguojian',
      'recovery': true,
      'values': _snapshot?.values ?? {
        for (final key in preferences.getKeys())
          if (LocalSnapshot.owns(key) || key == LocalSnapshot.storageKey)
            key: preferences.get(key)!,
      },
      if (files != null) 'files': await files.dump(),
    });
  }

  Future<void> reload() => _queue(() async {
    final previousProfiles = _configurationError == null
        ? _string('profiles')
        : null;
    final previousId = _current;
    final previousLocked = locked;
    await preferences.reload();
    if (_fileStore != null) {
      try {
        final snapshot = await LocalSnapshot.open(preferences, _fileStore!);
        _initialize(snapshot: snapshot);
      } catch (_) {
        _block('本機使用者索引損毀，原始檔案已保留；請從備份恢復使用者與設定。');
      }
    } else {
      _initialize();
    }
    if (_configurationError == null &&
        previousProfiles == _string('profiles') &&
        previousId == _current) {
      _locked = previousLocked;
    } else {
      _epoch++;
    }
    _notify();
  });

  Future<void> _queue(Future<void> Function() action) {
    _writes = _writes.catchError((Object _) {}).then((_) => action());
    return _writes;
  }

  @override
  void dispose() {
    _disposed = true;
    super.dispose();
  }
}
