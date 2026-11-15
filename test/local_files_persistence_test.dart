import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:path/path.dart' as path;
import 'package:shared_preferences/shared_preferences.dart';

import 'package:hgtv/local_snapshot.dart';
import 'package:hgtv/local_store.dart';
import 'package:hgtv/models.dart';
import 'package:hgtv/playback_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  const favorite = Drama(
    id: 'hongguo:70001',
    source: 'hongguo',
    title: '收藏測試',
  );
  const watched = Drama(
    id: 'hongguo:70002',
    source: 'hongguo',
    title: '觀看測試',
  );

  WatchEntry entry(Drama drama, {required int episode, required double position}) =>
      WatchEntry(
        drama: drama,
        episode: episode,
        position: position,
        duration: 120,
        updatedAt: DateTime(2026, 11, 4, 12, episode),
      );

  Future<SharedPreferences> freshPreferences() async {
    SharedPreferences.resetStatic();
    SharedPreferences.setMockInitialValues({});
    return SharedPreferences.getInstance();
  }

  test('legacy snapshot migrates to user/category files and playback rewrites history only', () async {
    final preferences = await freshPreferences();
    final legacy = LocalStore(preferences);
    await legacy.toggleFavorite(favorite);
    await legacy.setThemeMode('light');
    await legacy.setPlaybackPreferences(const PlaybackPreferences());
    await legacy.saveWatch(
      entry(favorite, episode: 1, position: 10),
    );
    legacy.dispose();

    final directory = await Directory.systemTemp.createTemp('dtv-files-migrate-');
    addTearDown(() => directory.delete(recursive: true));
    final store = await LocalStore.open(
      preferences,
      storageDirectory: directory,
    );
    addTearDown(store.dispose);

    expect(store.configurationError, isNull);
    expect(store.favorites.map((item) => item.id), [favorite.id]);
    expect(store.watched(favorite.id)?.position, 10);
    expect(preferences.getString(LocalSnapshot.storageKey), isNull);

    final usersFile = File(path.join(directory.path, 'users.json'));
    final globalSettingsFile = File(path.join(directory.path, 'settings.json'));
    final historyFile = File(
      path.join(directory.path, 'users', 'default', 'history.json'),
    );
    final favoritesFile = File(
      path.join(directory.path, 'users', 'default', 'favorites.json'),
    );
    final followStatesFile = File(
      path.join(directory.path, 'users', 'default', 'followStates.json'),
    );
    final lanRecordsFile = File(
      path.join(directory.path, 'users', 'default', 'lanRecords.json'),
    );
    final userSettingsFile = File(
      path.join(directory.path, 'users', 'default', 'settings.json'),
    );

    for (final file in [
      usersFile,
      globalSettingsFile,
      historyFile,
      favoritesFile,
      followStatesFile,
      lanRecordsFile,
      userSettingsFile,
    ]) {
      expect(await file.exists(), isTrue, reason: file.path);
    }

    final favoritesBefore = await favoritesFile.readAsString();
    final followStatesBefore = await followStatesFile.readAsString();
    final lanRecordsBefore = await lanRecordsFile.readAsString();
    final settingsBefore = await userSettingsFile.readAsString();
    final historyBefore = await historyFile.readAsString();

    // Playback of an already-favourited drama also changes only watch history.
    await store.saveWatch(
      entry(favorite, episode: 2, position: 48),
    );
    expect(await favoritesFile.readAsString(), favoritesBefore);
    expect(await followStatesFile.readAsString(), followStatesBefore);
    expect(await lanRecordsFile.readAsString(), lanRecordsBefore);
    expect(await userSettingsFile.readAsString(), settingsBefore);
    expect(await historyFile.readAsString(), isNot(historyBefore));
    expect(store.following(favorite.id)?.status.name, 'watching');
    expect(store.lanDocument.records[favorite.id]?.watch?.position, 48);

    final historyAfterFavoritePlayback = await historyFile.readAsString();
    await store.saveWatch(
      entry(watched, episode: 2, position: 56),
    );
    expect(await favoritesFile.readAsString(), favoritesBefore);
    expect(await followStatesFile.readAsString(), followStatesBefore);
    expect(await lanRecordsFile.readAsString(), lanRecordsBefore);
    expect(await userSettingsFile.readAsString(), settingsBefore);
    expect(await historyFile.readAsString(), isNot(historyAfterFavoritePlayback));
    expect(store.watched(watched.id)?.position, 56);
  });

  test('failed file migration retains and exports the original snapshot', () async {
    final preferences = await freshPreferences();
    await preferences.setString(LocalSnapshot.storageKey, '{broken-json');
    await preferences.setString('history', '[{"title":"舊資料"}]');

    final directory = await Directory.systemTemp.createTemp('dtv-files-fallback-');
    addTearDown(() => directory.delete(recursive: true));
    final store = await LocalStore.open(
      preferences,
      storageDirectory: directory,
    );
    addTearDown(store.dispose);

    expect(store.configurationError, isNotNull);
    expect(await store.exportRecoveryData(), contains('舊資料'));
    expect(
      await File(path.join(directory.path, 'users.json')).exists(),
      isFalse,
    );
  });

  test('legacy global player volume is migrated to the active user setting', () async {
    final preferences = await freshPreferences();
    await preferences.setString(
      'profiles',
      '[{"id":"default","name":"管理員","admin":true,"sources":[],"download":true}]',
    );
    await preferences.setString('activeProfile', 'default');
    await preferences.setDouble('playerVolume', 38);
    final directory = await Directory.systemTemp.createTemp('dtv-volume-migrate-');
    addTearDown(() => directory.delete(recursive: true));

    final store = await LocalStore.open(
      preferences,
      storageDirectory: directory,
    );
    addTearDown(store.dispose);

    expect(store.playerVolume, 38);
    expect(preferences.getDouble('playerVolume'), isNull);
    expect(
      await File(
        path.join(directory.path, 'users', 'default', 'settings.json'),
      ).exists(),
      isTrue,
    );
  });

  test('a damaged history file is quarantined without locking other categories', () async {
    final preferences = await freshPreferences();
    final legacy = LocalStore(preferences);
    await legacy.toggleFavorite(favorite);
    await legacy.saveWatch(
      entry(favorite, episode: 1, position: 10),
    );
    legacy.dispose();

    final directory = await Directory.systemTemp.createTemp('dtv-files-corrupt-');
    addTearDown(() => directory.delete(recursive: true));
    final first = await LocalStore.open(
      preferences,
      storageDirectory: directory,
    );
    await first.saveWatch(
      entry(watched, episode: 2, position: 32),
    );
    first.dispose();

    final historyFile = File(
      path.join(directory.path, 'users', 'default', 'history.json'),
    );
    final historyBackup = File('${historyFile.path}.bak');
    await historyFile.writeAsString('{broken-json');
    if (await historyBackup.exists()) {
      await historyBackup.writeAsString('{broken-backup');
    }

    final restored = await LocalStore.open(
      preferences,
      storageDirectory: directory,
    );
    addTearDown(restored.dispose);

    expect(restored.configurationError, isNull);
    expect(restored.history, isEmpty);
    expect(restored.favorites.map((item) => item.id), [favorite.id]);
    final entries = await directory
        .list(recursive: true)
        .where((item) => item is File)
        .cast<File>()
        .toList();
    expect(
      entries.any((file) => file.path.contains('history.json.corrupt.')),
      isTrue,
    );
    expect(
      await File(
        path.join(directory.path, 'users', 'default', 'favorites.json'),
      ).exists(),
      isTrue,
    );
  });
  test('temporarily unreadable history is preserved and read-only until reload', () async {
    final preferences = await freshPreferences();
    final legacy = LocalStore(preferences);
    await legacy.toggleFavorite(favorite);
    await legacy.saveWatch(
      entry(favorite, episode: 1, position: 10),
    );
    legacy.dispose();

    final directory = await Directory.systemTemp.createTemp('dtv-files-locked-');
    addTearDown(() => directory.delete(recursive: true));
    final first = await LocalStore.open(
      preferences,
      storageDirectory: directory,
    );
    final historyFile = File(
      path.join(directory.path, 'users', 'default', 'history.json'),
    );
    final lastGood = await historyFile.readAsString();
    first.dispose();

    final backupFile = File('${historyFile.path}.bak');
    await backupFile.writeAsString(lastGood);
    await historyFile.delete();
    await Directory(historyFile.path).create();

    final restored = await LocalStore.open(
      preferences,
      storageDirectory: directory,
    );
    addTearDown(restored.dispose);

    expect(restored.configurationError, isNull);
    expect(restored.watched(favorite.id)?.position, 10);
    expect(restored.hasUnavailableLocalDataFiles, isTrue);
    await expectLater(
      restored.saveWatch(
        entry(favorite, episode: 2, position: 48),
      ),
      throwsA(isA<StateError>()),
    );
    expect(await Directory(historyFile.path).exists(), isTrue);
    expect(await backupFile.readAsString(), lastGood);

    await Directory(historyFile.path).delete(recursive: true);
    await historyFile.writeAsString(lastGood);
    await restored.reload();

    expect(restored.hasUnavailableLocalDataFiles, isFalse);
    expect(restored.watched(favorite.id)?.position, 10);
    await restored.saveWatch(
      entry(favorite, episode: 2, position: 48),
    );
    expect(restored.watched(favorite.id)?.position, 48);
  });

}
