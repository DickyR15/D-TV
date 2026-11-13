import 'dart:convert';
import 'dart:io';

import 'package:path/path.dart' as path;

/// Durable JSON-file storage used by LocalSnapshot. Each user/category is
/// written independently so a watch-history update does not rewrite settings,
/// profiles, favourites, or other users' records.
class LocalFiles {
  LocalFiles(this.root);

  static const marker = 'users.json';

  final Directory root;

  /// Files whose primary or backup copy could not be read due to a transient
  /// filesystem error. Writes to these categories are blocked until reload.
  final Set<String> temporarilyUnavailableFiles = {};

  Directory get _staging => Directory('${root.path}.new');
  Directory get _retired => Directory('${root.path}.old');

  File _file(Directory directory, String name) =>
      File(path.join(directory.path, name));

  Future<bool> _complete(Directory directory) async =>
      await _file(directory, marker).exists() ||
      await File('${_file(directory, marker).path}.bak').exists();

  /// Recovers a directory swap interrupted by process termination or power loss.
  Future<bool> exists() async {
    if (!await _complete(root)) {
      for (final candidate in [_staging, _retired]) {
        if (!await _complete(candidate)) continue;
        if (await root.exists()) await root.delete(recursive: true);
        await candidate.rename(root.path);
        break;
      }
    }
    for (final leftover in [_staging, _retired]) {
      if (await leftover.exists()) {
        await leftover.delete(recursive: true);
      }
    }
    return _complete(root);
  }

  /// Reads the primary file, then its last known-good backup.
  ///
  /// A filesystem read failure is not corruption: retain the original file,
  /// try the backup, and mark the category read-only until a later reload can
  /// read the primary copy successfully.
  Future<Object?> readJson(String name) async {
    final file = _file(root, name);
    final backup = File('${file.path}.bak');
    var found = false;
    FileSystemException? unavailable;
    for (final candidate in [file, backup]) {
      bool exists;
      try {
        exists = await candidate.exists();
      } on FileSystemException catch (error) {
        unavailable ??= error;
        temporarilyUnavailableFiles.add(name);
        continue;
      }
      if (!exists) continue;
      found = true;

      String content;
      try {
        content = await candidate.readAsString();
      } on FileSystemException catch (error) {
        unavailable ??= error;
        temporarilyUnavailableFiles.add(name);
        continue;
      }

      try {
        final value = jsonDecode(content);
        if (candidate.path == file.path) {
          temporarilyUnavailableFiles.remove(name);
        }
        return value;
      } on FormatException {
        // Only malformed JSON is corruption. Keep trying the known-good copy.
      }
    }
    if (unavailable != null) throw unavailable;
    if (found) throw FormatException('$name 無法讀取');
    temporarilyUnavailableFiles.remove(name);
    return null;
  }

  /// Moves only the damaged category aside; other user files remain untouched.
  Future<void> quarantine(String name) async {
    final file = _file(root, name);
    final suffix = '.corrupt.${DateTime.now().microsecondsSinceEpoch}';
    for (final candidate in [file, File('${file.path}.bak')]) {
      if (await candidate.exists()) {
        await candidate.rename('${candidate.path}$suffix');
      }
    }
  }

  Future<void> _persist(File file, Object? value) async {
    await file.parent.create(recursive: true);
    await file.writeAsString(jsonEncode(value), flush: true);
  }

  Future<void> _write(
    Directory directory,
    String name,
    Object? value,
  ) async {
    final file = _file(directory, name);
    await file.parent.create(recursive: true);
    final temporary = File('${file.path}.tmp');
    final backup = File('${file.path}.bak');
    if (await temporary.exists()) await temporary.delete();
    await _persist(temporary, value);
    var movedOriginal = false;
    try {
      if (await file.exists()) {
        if (await backup.exists()) await backup.delete();
        await file.rename(backup.path);
        movedOriginal = true;
      }
      await temporary.rename(file.path);
    } catch (_) {
      if (movedOriginal &&
          !await file.exists() &&
          await backup.exists()) {
        await backup.rename(file.path);
      }
      rethrow;
    } finally {
      if (await temporary.exists()) await temporary.delete();
    }
  }

  Future<void> writeJson(String name, Object? value) =>
      _write(root, name, value);

  Future<void> _removeFile(String name) async {
    final file = _file(root, name);
    for (final candidate in [file, File('${file.path}.bak'), File('${file.path}.tmp')]) {
      if (await candidate.exists()) await candidate.delete();
    }
  }

  /// Updates only files whose category values changed. If any write fails,
  /// already-updated files are rolled back to their previous content.
  Future<void> updateFiles(
    Map<String, Object?> previous,
    Map<String, Object?> next,
  ) async {
    final names = {...previous.keys, ...next.keys}.toList()..sort();
    final changed = <String>[];
    for (final name in names) {
      if (_canonical(previous[name]) != _canonical(next[name])) {
        changed.add(name);
      }
    }
    final blocked = changed.where(temporarilyUnavailableFiles.contains).toList();
    if (blocked.isNotEmpty) {
      throw LocalDataTemporarilyUnavailable(blocked);
    }
    final applied = <String>[];
    try {
      for (final name in changed) {
        applied.add(name);
        if (!next.containsKey(name)) {
          await _removeFile(name);
        } else {
          await _write(root, name, next[name]);
        }
      }
    } catch (_) {
      var rollbackFailed = false;
      for (final name in applied.reversed) {
        try {
          if (!previous.containsKey(name)) {
            await _removeFile(name);
          } else {
            await _write(root, name, previous[name]);
          }
        } catch (_) {
          rollbackFailed = true;
        }
      }
      if (rollbackFailed) throw const LocalFilesRecoveryRequired();
      rethrow;
    }
  }

  /// Replaces the complete data set transactionally, for validated backup restore.
  Future<void> replaceAll(Map<String, Object?> contents) async {
    if (!contents.containsKey(marker)) {
      throw StateError('本機使用者索引缺失，拒絕替換資料');
    }
    if (await _staging.exists()) {
      await _staging.delete(recursive: true);
    }
    for (final entry in contents.entries) {
      if (entry.key == marker) continue;
      await _write(_staging, entry.key, entry.value);
    }
    await _write(_staging, marker, contents[marker]);
    if (await _retired.exists()) {
      await _retired.delete(recursive: true);
    }
    var movedOld = false;
    try {
      if (await root.exists()) {
        await root.rename(_retired.path);
        movedOld = true;
      }
      await _staging.rename(root.path);
    } catch (_) {
      if (movedOld && !await root.exists() && await _retired.exists()) {
        await _retired.rename(root.path);
      }
      rethrow;
    }
    if (await _retired.exists()) {
      await _retired.delete(recursive: true).catchError((Object _) => _retired);
    }
  }

  Future<Map<String, String>> dump() async {
    final files = <String, String>{};
    if (!await root.exists()) return files;
    await for (final entity in root.list(recursive: true)) {
      if (entity is! File) continue;
      final name = path.relative(entity.path, from: root.path);
      files[name.replaceAll(r'\\', '/')] = await entity
          .readAsString()
          .catchError((Object _) => '');
    }
    return files;
  }

  static String _canonical(Object? value) {
    Object? sort(Object? item) {
      if (item is Map) {
        final keys = item.keys.map((key) => key.toString()).toList()..sort();
        return <String, Object?>{
          for (final key in keys) key: sort(item[key]),
        };
      }
      if (item is List) return item.map(sort).toList();
      return item;
    }

    return jsonEncode(sort(value));
  }
}

class LocalFilesRecoveryRequired implements Exception {
  const LocalFilesRecoveryRequired();
}


class LocalDataTemporarilyUnavailable implements Exception {
  const LocalDataTemporarilyUnavailable(this.files);

  final List<String> files;

  @override
  String toString() =>
      '本機資料暫時無法讀取（${files.join('、')}），原始檔案已保留；請先重新讀取資料，再修改這些項目。';
}
