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

  test('removed Sorani source is unavailable and stale settings are ignored', () async {
    final store = await create({
      'sourceSwitches': jsonEncode({'sorani': true}),
    });
    expect(SourceSite.isKnown('sorani'), isFalse);
    expect(SourceSite.isAvailable('sorani'), isFalse);
    expect(store.allowsSource('sorani'), isFalse);
    expect(store.sources.map((site) => site.id), isNot(contains('sorani')));
    await expectLater(store.setSourceEnabled('sorani', true), throwsStateError);
  });

  test('Guipian and KANMovie are retired and stale switches are ignored', () async {
    final store = await create({
      'sourceSwitches': jsonEncode({'guipian': true, 'kanmovie': true}),
    });
    expect(SourceSite.isKnown('guipian'), isFalse);
    expect(SourceSite.isAvailable('guipian'), isFalse);
    expect(SourceSite.isKnown('kanmovie'), isFalse);
    expect(store.sourceEnabled('duoduo'), isTrue);
    expect(store.allowsSource('baofeng'), isFalse);
    expect(store.allowsSource('jisu'), isFalse);
    expect(store.allowsSource('360'), isFalse);
    expect(store.allowsSource('huya'), isFalse);
    expect(store.sourceEnabled('guipian'), isFalse);
    expect(store.allowsSource('guipian'), isFalse);
    expect(store.allowsSource('kanmovie'), isFalse);
  });

  test('old aggregate profile permissions migrate to Duoduo', () async {
    final store = await create({
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
          sources: ['guipian', 'kanmovie'],
        ).toJson(),
      ]),
      'activeProfile': 'viewer',
    });
    expect(store.configurationError, isNull);
    expect(store.profile.sources, ['duoduo']);
    expect(store.profile.allows('duoduo'), isTrue);
    expect(store.profile.allows('guipian'), isFalse);
  });

  test('saved KANMovie records are not remapped to unrelated sources', () {
    final drama = Drama.fromJson({
      'id': 'kanmovie:bfzy.tv~12345',
      'source': 'kanmovie',
      'sourceId': 'bfzy.tv~12345',
      'title': '舊收藏影片',
    });
    expect(drama.id, 'kanmovie:bfzy.tv~12345');
    expect(drama.source, 'kanmovie');
    expect(drama.sourceId, 'bfzy.tv~12345');
  });

  test('Heitao is registered as an advanced searchable source', () {
    final heitao = SourceSite.byId('heitao');
    expect(SourceSite.isKnown('heitao'), isTrue);
    expect(SourceSite.isRestricted('heitao'), isTrue);
    expect(heitao.name, '黑桃');
    expect(heitao.pagedSearch, isTrue);
    expect(heitao.onlineSearch, isTrue);
  });

  test('advanced source drag order is password protected and persists', () async {
    if (!allSourcesEnabled) return;
    final store = await create();
    await store.saveProfile(
      id: 'default',
      name: '管理員',
      sources: const [],
      download: true,
      pin: 'source-pass-123',
    );
    await expectLater(
      store.reorderAdvancedSources(store.advancedSources.map((s) => s.id).toList()),
      throwsStateError,
    );
    await store.unlockAdvancedSources('source-pass-123');
    final original = store.advancedSources.map((source) => source.id).toList();
    final reordered = <String>[
      'supjav',
      'missav',
      ...original.where((id) => id != 'supjav' && id != 'missav'),
    ];
    await store.reorderAdvancedSources(reordered);
    expect(store.advancedSources.map((source) => source.id).toList(), reordered);
    await store.setSourceEnabled('supjav', true);
    await store.setSourceEnabled('missav', true);
    expect(
      store.sources.map((source) => source.id).toList().take(5).toList(),
      ['hongguo', 'hanxiaoquan', 'duoduo', 'supjav', 'missav'],
    );
    await expectLater(
      store.reorderAdvancedSources([...reordered, 'missav']),
      throwsStateError,
    );

    final restarted = testStore(store.preferences);
    addTearDown(restarted.dispose);
    expect(restarted.advancedSources.map((source) => source.id).toList(), reordered);
  });

  test('three primary sources start enabled and restricted sources start switched off', () async {
    final store = await create();
    expect(
      store.sources.map((site) => site.id),
      ['hongguo', 'hanxiaoquan', 'duoduo'],
    );
    expect(store.permittedSources.length, SourceSite.values.length);
    expect(store.sourceEnabled('hongguo'), isTrue);
    expect(store.sourceEnabled('hanxiaoquan'), isTrue);
    expect(store.sourceEnabled('duoduo'), isTrue);
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

  test('new-user save is rejected without admin password and preserves existing profiles', () async {
    final store = await create();
    final before = store.profiles
        .map((profile) => jsonEncode(profile.toJson()))
        .toList();
    await expectLater(
      store.saveProfile(
        name: '一般使用者',
        sources: SourceSite.values.map((source) => source.id).toList(),
        download: false,
      ),
      throwsA(isA<StateError>()),
    );
    expect(store.administratorPasswordConfigured, isFalse);
    expect(
      store.profiles.map((profile) => jsonEncode(profile.toJson())).toList(),
      before,
    );
    expect(store.profiles, hasLength(1));
  });

}
