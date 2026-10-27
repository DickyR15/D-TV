import 'dart:convert';

import 'package:shared_preferences/shared_preferences.dart';

class LocalSnapshot {
  LocalSnapshot(this.preferences) {
    reloadValues();
  }
  LocalSnapshot.empty(this.preferences);

  static const storageKey = 'localSnapshot.v1';
  static const libraryKeys = {
    'history',
    'favorites',
    'followStates',
    'seriesCandidates',
    'mediaHistory',
    'source',
    'hideVip',
    'playback',
    'tvDecoderSafePresetV1',
    'downloadPreferences',
    'catalogView',
    'recentSearches',
    'lanRecords',
    'lanSettings',
    'lanReceipts',
  };
  static const globalKeys = {
    'profiles',
    'activeProfile',
    'displayMode',
    'themeMode',
    'themeAccent',
    'sourceSwitches',
    'forceLogin',
  };
  static const _retiredKeys = {
    'sourceGateEnabled',
    'sourceGateOff',
    'sourceGateSalt',
    'sourceGateHash',
    'autoExport',
    'exportPosters',
  };
  final SharedPreferences preferences;
  Map<String, Object> _values = {};

  static bool owns(String key) =>
      globalKeys.contains(key) ||
      libraryKeys.contains(key) ||
      RegExp(
        r'^profile\.[a-zA-Z0-9_-]{1,64}\.(history|favorites|followStates|seriesCandidates|mediaHistory|source|hideVip|playback|downloadPreferences|catalogView|recentSearches|lanRecords|lanSettings|lanReceipts|tvDecoderSafePresetV1)$',
      ).hasMatch(key);

  Map<String, Object> get values => Map.of(_values);
  String? getString(String key) => _values[key] as String?;
  bool? getBool(String key) => _values[key] as bool?;

  void reloadValues() {
    final raw = preferences.getString(storageKey);
    if (raw == null) {
      _values = {
        for (final key in preferences.getKeys().where(owns))
          key: preferences.get(key)!,
      };
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
    _validate(values);
    _values = values;
  }

  void _validate(Map<String, Object> values) {
    for (final entry in values.entries) {
      final boolean = {
        'hideVip',
        'forceLogin',
        'tvDecoderSafePresetV1',
      }.contains(entry.key.split('.').last);
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

  Future<void> commit(Map<String, Object> values) async {
    if (values.keys.any((key) => !owns(key))) throw StateError('無效的本地配置欄位');
    final content = jsonEncode({'version': 1, 'values': values});
    if (utf8.encode(content).length > 16 * 1024 * 1024) {
      throw StateError('本地記錄超過儲存上限，請先匯出備份並清理記錄');
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
      throw StateError('未能儲存，原有記錄已保留；請檢查儲存空間和許可權後重試');
    }
    _values = Map.of(values);
  }
}

class SnapshotRecoveryRequired implements Exception {
  const SnapshotRecoveryRequired();
  @override
  String toString() => '無法確認本地配置儲存結果，已鎖定訪問；請重新讀取配置或恢復備份';
}
