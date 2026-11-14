import 'dart:convert';
import 'dart:io';

import 'package:shared_preferences/shared_preferences.dart';

import 'local_files.dart';

class LocalSnapshot {
  LocalSnapshot(this.preferences) : fileStore = null {
    reloadValues();
  }

  LocalSnapshot.empty(this.preferences) : fileStore = null;

  LocalSnapshot._fileBacked(
    this.preferences,
    this.fileStore,
    Map<String, Object> values,
  ) : _values = Map.of(values);

  static const storageKey = 'localSnapshot.v1';
  static const libraryKeys = {
    'history',
    'favorites',
    'followStates',
    'followChecks',
    'seriesCandidates',
    'mediaHistory',
    'source',
    'hideVip',
    'playback',
    'tvDecoderSafePresetV1',
    'portraitBlurLandscapeDefaultV1',
    'downloadPreferences',
    'catalogView',
    'recentSearches',
    'playerVolume',
    'danmakuDefaultOffV1',
    'lanRecords',
    'lanSettings',
    'lanReceipts',
  };
  static const globalKeys = {
    'profiles',
    'profileTombstones',
    'activeProfile',
    'displayMode',
    'themeMode',
    'themeAccent',
    'pureBlackTheme',
    'sourceSwitches',
    'advancedSourceOrder',
    'forceLogin',
    'ignoredUpdateVersion',
    'checkForUpdatesOnStartup',
  };
  static const _retiredKeys = {
    'sourceGateEnabled',
    'sourceGateOff',
    'sourceGateSalt',
    'sourceGateHash',
    'autoExport',
    'exportPosters',
  };

  static const _userRecordKeys = {
    'history',
    'favorites',
    'followStates',
    'followChecks',
    'seriesCandidates',
    'mediaHistory',
    'lanRecords',
    'lanSettings',
    'lanReceipts',
  };
  static const _userSettingKeys = {
    'source',
    'hideVip',
    'playback',
    'tvDecoderSafePresetV1',
    'portraitBlurLandscapeDefaultV1',
    'downloadPreferences',
    'catalogView',
    'recentSearches',
    'playerVolume',
    'danmakuDefaultOffV1',
  };
  static const _plainStringKeys = {
    'activeProfile',
    'displayMode',
    'themeMode',
    'themeAccent',
    'ignoredUpdateVersion',
    'source',
    'playerVolume',
  };
  static const _booleanKeys = {
    'hideVip',
    'forceLogin',
    'checkForUpdatesOnStartup',
    'pureBlackTheme',
    'tvDecoderSafePresetV1',
    'portraitBlurLandscapeDefaultV1',
    'danmakuDefaultOffV1',
  };

  final SharedPreferences preferences;
  final LocalFiles? fileStore;
  Map<String, Object> _values = {};

  static bool owns(String key) =>
      globalKeys.contains(key) ||
      libraryKeys.contains(key) ||
      RegExp(
        r'^profile\.[a-zA-Z0-9_-]{1,64}\.(history|favorites|followStates|followChecks|seriesCandidates|mediaHistory|source|hideVip|playback|downloadPreferences|catalogView|recentSearches|playerVolume|danmakuDefaultOffV1|lanRecords|lanSettings|lanReceipts|tvDecoderSafePresetV1|portraitBlurLandscapeDefaultV1)$',
      ).hasMatch(key);

  Map<String, Object> get values => Map.of(_values);
  String? getString(String key) => _values[key] as String?;
  bool? getBool(String key) => _values[key] as bool?;

  static String _profileKey(String id, String name) =>
      id == 'default' ? name : 'profile.$id.$name';

  static Object? _toDiskValue(String key, Object? value) {
    if (value == null) return null;
    if (_booleanKeys.contains(key.split('.').last) ||
        _plainStringKeys.contains(key.split('.').last)) {
      return value;
    }
    if (key == 'profiles' || key.endsWith('.profiles')) {
      if (value is! String) return value;
      try {
        return jsonDecode(value);
      } catch (_) {
        return value;
      }
    }
    if (value is String) {
      try {
        return jsonDecode(value);
      } catch (_) {
        // Retain malformed category data as a string. Its consumer will
        // apply that category's default rather than locking the whole app.
        return value;
      }
    }
    return value;
  }

  static Object? _fromDiskValue(String key, Object? value) {
    if (value == null) return null;
    final leaf = key.split('.').last;
    if (_booleanKeys.contains(leaf) || _plainStringKeys.contains(leaf)) {
      return value;
    }
    return jsonEncode(value);
  }

  static String _userFile(String id, String category) =>
      'users/$id/$category.json';

  Map<String, Object?> _serializeFiles(Map<String, Object> values) {
    final files = <String, Object?>{};
    final users = <String, Object?>{};
    if (values['profiles'] case final String rawProfiles) {
      try {
        users['profiles'] = jsonDecode(rawProfiles);
      } catch (_) {
        users['profiles'] = rawProfiles;
      }
    }
    if (values['activeProfile'] case final Object active) {
      users['activeProfile'] = active;
    }
    if (values['forceLogin'] case final Object forceLogin) {
      users['forceLogin'] = forceLogin;
    }
    users['formatVersion'] = 1;
    files['users.json'] = users;

    final globalSettings = <String, Object?>{};
    for (final key in [
      'displayMode',
      'themeMode',
      'themeAccent',
      'pureBlackTheme',
      'sourceSwitches',
      'advancedSourceOrder',
      'ignoredUpdateVersion',
      'checkForUpdatesOnStartup',
    ]) {
      if (values.containsKey(key)) {
        globalSettings[key] = _toDiskValue(key, values[key]);
      }
    }
    if (globalSettings.isNotEmpty) {
      files['settings.json'] = globalSettings;
    }

    final ids = <String>{'default'};
    final rawProfiles = values['profiles'];
    if (rawProfiles is String) {
      try {
        final rows = jsonDecode(rawProfiles);
        if (rows is List) {
          for (final row in rows) {
            if (row is Map && row['id'] is String) {
              final id = row['id'] as String;
              if (!RegExp(r'^[a-zA-Z0-9_-]{1,64}$').hasMatch(id)) {
                throw const FormatException('使用者 ID 格式無效');
              }
              ids.add(id);
            }
          }
        }
      } catch (error) {
        if (error is FormatException) rethrow;
        throw const FormatException('使用者清單格式無效');
      }
    }

    for (final id in ids) {
      final settings = <String, Object?>{};
      for (final key in _userSettingKeys) {
        final storageKey = _profileKey(id, key);
        if (values.containsKey(storageKey)) {
          settings[key] = _toDiskValue(storageKey, values[storageKey]);
        }
      }
      if (settings.isNotEmpty) {
        files[_userFile(id, 'settings')] = settings;
      }
      for (final key in _userRecordKeys) {
        final storageKey = _profileKey(id, key);
        if (values.containsKey(storageKey)) {
          files[_userFile(id, key)] =
              _toDiskValue(storageKey, values[storageKey]);
        }
      }
    }
    return files;
  }

  Future<Map<String, Object>> _readFiles(LocalFiles files) async {
    final usersRaw = await files.readJson(LocalFiles.marker);
    if (usersRaw is! Map) {
      throw const FormatException('本機使用者索引無法讀取');
    }
    final users = Map<String, dynamic>.from(usersRaw);
    if (users['formatVersion'] != 1) {
      throw const FormatException('本機使用者索引版本無效');
    }
    final values = <String, Object>{};

    if (users['profiles'] != null) {
      if (users['profiles'] is! List) {
        throw const FormatException('本機使用者清單格式無效');
      }
      values['profiles'] = jsonEncode(users['profiles']);
    }
    if (users['activeProfile'] is String) {
      values['activeProfile'] = users['activeProfile'] as String;
    }
    if (users['forceLogin'] is bool) {
      values['forceLogin'] = users['forceLogin'] as bool;
    }

    final globalSettings =
        await _readOptionalMap(files, 'settings.json');
    for (final key in [
      'displayMode',
      'themeMode',
      'themeAccent',
      'pureBlackTheme',
      'sourceSwitches',
      'advancedSourceOrder',
      'ignoredUpdateVersion',
      'checkForUpdatesOnStartup',
    ]) {
      if (globalSettings.containsKey(key)) {
        final value = _fromDiskValue(key, globalSettings[key]);
        if (value != null) values[key] = value;
      }
    }

    final ids = <String>{'default'};
    final rows = users['profiles'];
    if (rows is List) {
      for (final row in rows) {
        if (row is! Map || row['id'] is! String) {
          throw const FormatException('本機使用者清單格式無效');
        }
        final id = row['id'] as String;
        if (!RegExp(r'^[a-zA-Z0-9_-]{1,64}$').hasMatch(id)) {
          throw const FormatException('本機使用者 ID 格式無效');
        }
        ids.add(id);
      }
    }

    for (final id in ids) {
      final userSettings = await _readOptionalMap(
        files,
        _userFile(id, 'settings'),
      );
      for (final key in _userSettingKeys) {
        if (!userSettings.containsKey(key)) continue;
        final storageKey = _profileKey(id, key);
        final value = _fromDiskValue(storageKey, userSettings[key]);
        if (value != null) values[storageKey] = value;
      }

      for (final key in _userRecordKeys) {
        final fileName = _userFile(id, key);
        final raw = await _readOptionalCategory(files, fileName);
        if (raw == null) continue;
        final storageKey = _profileKey(id, key);
        final value = _fromDiskValue(storageKey, raw);
        if (value != null) values[storageKey] = value;
      }
    }
    _validate(values);
    return values;
  }

  Future<Map<String, dynamic>> _readOptionalMap(
    LocalFiles files,
    String fileName,
  ) async {
    final value = await _readOptionalCategory(files, fileName);
    return value is Map ? Map<String, dynamic>.from(value) : <String, dynamic>{};
  }

  Future<Object?> _readOptionalCategory(
    LocalFiles files,
    String fileName,
  ) async {
    try {
      return await files.readJson(fileName);
    } on FormatException {
      // A damaged settings/history/favourites file affects only that category.
      await files.quarantine(fileName).catchError((Object _) {});
      return null;
    } on FileSystemException {
      return null;
    }
  }

  /// Loads the file-backed store or migrates the existing SharedPreferences
  /// snapshot. Legacy values are removed only after all files are written and
  /// successfully read back.
  static Future<LocalSnapshot> open(
    SharedPreferences preferences,
    LocalFiles files,
  ) async {
    if (await files.exists()) {
      final values = await LocalSnapshot._emptyForRead(
        preferences,
        files,
      )._readFiles(files);
      return LocalSnapshot._fileBacked(preferences, files, values);
    }

    final legacy = LocalSnapshot(preferences);
    final previousValues = legacy.values;
    // The previous player-volume preference was global. Attach it to the
    // currently active user so the new file layout remains per-user.
    final legacyVolume = previousValues.remove('playerVolume');
    if (legacyVolume != null) {
      final activeId = legacy.getString('activeProfile') ?? 'default';
      previousValues.putIfAbsent(
        _profileKey(activeId, 'playerVolume'),
        () => legacyVolume.toString(),
      );
    }
    await files.replaceAll(legacy._serializeFiles(previousValues));
    final verifiedValues = await LocalSnapshot._emptyForRead(
      preferences,
      files,
    )._readFiles(files);

    // Keep the monolithic snapshot until every old preference key has been
    // removed. If cleanup is interrupted, startup can still fall back to it.
    for (final key in preferences.getKeys().where(owns).toList()) {
      await preferences.remove(key);
    }
    await preferences.remove(storageKey);
    return LocalSnapshot._fileBacked(preferences, files, verifiedValues);
  }

  LocalSnapshot._emptyForRead(this.preferences, this.fileStore);

  LocalSnapshot.emptyFileBacked(this.preferences, this.fileStore);

  void reloadValues() {
    final raw = preferences.getString(storageKey);
    if (raw == null) {
      _values = {
        for (final key in preferences.getKeys().where(owns))
          key: preferences.get(key)!,
      };
      _normalizeLegacyVolumeValues(_values);
      _validate(_values);
      return;
    }
    final decoded = jsonDecode(raw) as Map;
    if (decoded['version'] != 1 || decoded['values'] is! Map) {
      throw const FormatException('本地配置快照無效');
    }
    final values = Map<String, Object>.from(decoded['values'] as Map)
      ..removeWhere((key, _) => _retiredKeys.contains(key));
    if (!values.containsKey('profiles')) {
      throw const FormatException('本地配置快照無效');
    }
    _normalizeLegacyVolumeValues(values);
    _validate(values);
    _values = values;
  }

  void _normalizeLegacyVolumeValues(Map<String, Object> values) {
    for (final entry in values.entries.toList()) {
      final parts = entry.key.split('.');
      final isProfileVolume =
          parts.length == 3 &&
          parts[0] == 'profile' &&
          parts[1].isNotEmpty &&
          parts[2] == 'playerVolume';
      if (entry.key == 'playerVolume' || isProfileVolume) {
        if (entry.value is num) {
          final value = (entry.value as num).toDouble();
          values[entry.key] = value.isFinite && value > 0
              ? value.clamp(1.0, 100.0).toString()
              : '100.0';
        }
      }
    }
  }

  void _validate(Map<String, Object> values) {
    for (final entry in values.entries) {
      final boolean = _booleanKeys.contains(entry.key.split('.').last);
      if (!owns(entry.key) ||
          (boolean ? entry.value is! bool : entry.value is! String)) {
        throw const FormatException('本地配置欄位無效');
      }
    }
  }

  Future<bool> _restore(Object? value) => switch (value) {
    null => preferences.remove(storageKey),
    String value => preferences.setString(storageKey, value),
    bool value => preferences.setBool(storageKey, value),
    int value => preferences.setInt(storageKey, value),
    double value => preferences.setDouble(storageKey, value),
    List<String> value => preferences.setStringList(storageKey, value),
    _ => Future.value(false),
  };

  Future<void> commit(
    Map<String, Object> values, {
    bool replace = false,
  }) async {
    if (values.keys.any((key) => !owns(key))) {
      throw StateError('無效的本地配置欄位');
    }
    final content = jsonEncode({'version': 1, 'values': values});
    if (utf8.encode(content).length > 16 * 1024 * 1024) {
      throw StateError('本地記錄超過儲存上限，請先匯出備份並清理記錄');
    }

    final files = fileStore;
    if (files != null) {
      try {
        final nextFiles = _serializeFiles(values);
        if (replace) {
          await files.replaceAll(nextFiles);
        } else {
          await files.updateFiles(
            _serializeFiles(_values),
            nextFiles,
          );
        }
        _values = Map.of(values);
        return;
      } on LocalFilesRecoveryRequired {
        throw const SnapshotRecoveryRequired();
      } on LocalDataTemporarilyUnavailable catch (error) {
        throw StateError(error.toString());
      } catch (_) {
        throw StateError('未能儲存本機檔案，原有紀錄已保留；請檢查儲存空間與權限後重試');
      }
    }

    final previous = preferences.get(storageKey);
    try {
      if (!await preferences.setString(storageKey, content)) {
        throw StateError('本地配置未能儲存');
      }
    } catch (_) {
      try {
        await preferences.reload();
        if (preferences.get(storageKey) != previous) {
          final restored = await _restore(previous);
          if (!restored) throw StateError('無法恢復原配置');
          await preferences.reload();
          if (preferences.get(storageKey) != previous) {
            throw StateError('無法恢復原配置');
          }
        }
      } catch (_) {
        throw const SnapshotRecoveryRequired();
      }
      throw StateError('未能儲存，原有紀錄已保留；請檢查儲存空間與權限後重試');
    }
    _values = Map.of(values);
  }
}

class SnapshotRecoveryRequired implements Exception {
  const SnapshotRecoveryRequired();
  @override
  String toString() => '無法確認本地配置儲存結果，已鎖定訪問；請重新讀取配置或恢復備份';
}
