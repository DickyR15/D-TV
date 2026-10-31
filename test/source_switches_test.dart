import 'dart:convert';

import 'package:hgtv/app_build.dart';
import 'package:hgtv/local_profiles.dart';
import 'package:hgtv/local_snapshot.dart';
import 'package:hgtv/local_store.dart';
import 'package:hgtv/models.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Future<LocalStore> create([Map<String, Object> initial = const {}]) async {
    SharedPreferences.setMockInitialValues(Map.of(initial));
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    return store;
  }

  test('restricted sources start switched off', () async {
    final store = await create();
    expect(
      store.sources.map((site) => site.id),
      allSourcesEnabled
          ? ['hongguo', 'hanxiaoquan', 'guipian', 'sorani']
          : ['hongguo'],
    );
    expect(store.permittedSources.length, SourceSite.values.length);
    expect(store.sourceEnabled('hongguo'), isTrue);
    expect(store.sourceEnabled('huangdou'), isFalse);
    expect(store.allowsSource('huangdou'), isFalse);
  });

  test('switches persist across restarts', () async {
    final store = await create();
    await store.setSourceEnabled('hongguo', false);
    expect(store.allowsSource('hongguo'), isFalse);
    if (allSourcesEnabled) {
      await store.saveProfile(
        id: 'default',
        name: '管理員',
        sources: const [],
        download: true,
        pin: 'source-pass-123',
      );
      await store.unlockAdvancedSources('source-pass-123');
      await store.setSourceEnabled('huangdou', true);
      expect(store.allowsSource('huangdou'), isTrue);
      store.lockAdvancedSources();
      await store.setForceLogin(false);
    }
    final restarted = testStore(store.preferences);
    addTearDown(restarted.dispose);
    expect(restarted.sourceEnabled('hongguo'), isFalse);
    expect(restarted.allowsSource('huangdou'), allSourcesEnabled);
  });

  test('advanced source switches require an unlocked administrator password', () async {
    if (!allSourcesEnabled) return;
    final store = await create();
    await expectLater(
      store.setSourceEnabled('huangdou', true),
      throwsStateError,
    );
    await expectLater(
      store.unlockAdvancedSources('source-pass-123'),
      throwsStateError,
    );
    await store.saveProfile(
      id: 'default',
      name: '管理員',
      sources: const [],
      download: true,
      pin: 'source-pass-123',
    );
    await expectLater(
      store.unlockAdvancedSources('incorrect-password'),
      throwsStateError,
    );
    expect(store.advancedSourcesUnlocked, isFalse);
    await store.unlockAdvancedSources('source-pass-123');
    expect(store.advancedSourcesUnlocked, isTrue);
    await store.setSourceEnabled('huangdou', true);
    expect(store.sourceEnabled('huangdou'), isTrue);
    store.lockAdvancedSources();
    expect(store.advancedSourcesUnlocked, isFalse);
    await expectLater(
      store.setSourceEnabled('huangdou', false),
      throwsStateError,
    );
  });

  test('only the administrator can switch sources', () async {
    final store = await create({
      'sourceSwitches': jsonEncode({'huangdou': true}),
      'profiles': jsonEncode([
        LocalProfile(
          id: 'default',
          name: '管理員',
          admin: true,
          salt: '0' * 32,
          pinHash: '1' * 64,
        ).toJson(),
        const LocalProfile(
          id: 'viewer',
          name: '普通使用者',
          sources: ['hongguo', 'huangdou'],
        ).toJson(),
      ]),
      'activeProfile': 'viewer',
    });
    expect(store.allowsSource('huangdou'), allSourcesEnabled);
    await expectLater(
      store.setSourceEnabled('hongguo', false),
      throwsStateError,
    );
    expect(store.sourceEnabled('hongguo'), isTrue);
  });

  test('retired password gate records are dropped on load', () async {
    final store = await create({
      LocalSnapshot.storageKey: jsonEncode({
        'version': 1,
        'values': {
          'profiles': jsonEncode([
            const LocalProfile(
              id: 'default',
              name: '管理員',
              admin: true,
            ).toJson(),
          ]),
          'sourceGateEnabled': false,
          'sourceGateOff': true,
          'sourceGateSalt': '',
          'sourceGateHash': '',
        },
      }),
    });
    expect(store.configurationError, isNull);
    expect(store.allowsSource('huangdou'), isFalse);
    await store.setSourceEnabled('hongguo', false);
    expect(
      store.preferences.getString(LocalSnapshot.storageKey),
      isNot(contains('sourceGate')),
    );
  });

  test('a damaged switch record falls back to defaults', () async {
    final store = await create({'sourceSwitches': 'not json'});
    expect(store.configurationError, isNull);
    expect(store.sourceEnabled('hongguo'), isTrue);
    expect(store.sourceEnabled('huangdou'), isFalse);
  });
  test('Huangguo providers remain independent source groups', () {
    const sources = [
      SourceSite('huangguo-video', '黃果影片', '影片與劇集'),
      SourceSite('huangguoai', '黃果 AI', 'AI 短劇'),
      SourceSite('cloudfront', '黃果舊版', '舊版 API 片庫'),
    ];

    final groups = SourceGroup.fromSources(sources);
    expect(groups.map((group) => group.id).toList(), [
      'huangguo-video',
      'huangguoai',
      'cloudfront',
    ]);
    expect(groups.map((group) => group.name).toList(), [
      '黃果影片',
      '黃果 AI',
      '黃果舊版',
    ]);
    expect(sources.map((source) => source.hasRankings).toList(), [
      false,
      true,
      false,
    ]);
  });

}
