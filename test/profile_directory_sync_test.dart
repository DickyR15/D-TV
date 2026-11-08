import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:hgtv/local_profiles.dart';
import 'package:hgtv/local_store.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('profile directory sync merges PIN hashes and propagates deletions', () async {
    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    await store.saveProfile(
      id: 'default',
      name: '管理員',
      sources: const [],
      download: true,
      pin: 'admin-pin-123',
    );

    const guestId = 'guest-profile-123';
    const salt1 = '11111111111111111111111111111111';
    final hash1 = await hashProfilePin('guest-pin-123', salt1);
    final firstUpdatedAt = DateTime.now().toUtc().millisecondsSinceEpoch;
    final guest1 = LocalProfile(
      id: guestId,
      name: '客廳',
      sources: const ['hongguo'],
      download: false,
      salt: salt1,
      pinHash: hash1,
      updatedAt: firstUpdatedAt,
    );
    final admin = store.profiles.single;
    final firstSnapshot = {
      'schema': 1,
      'profiles': [admin.toJson(), guest1.toJson()],
      'tombstones': <String, int>{},
    };

    await store.mergeProfileSyncSnapshot(firstSnapshot);
    final merged1 = await store.exportProfileSyncSnapshot();
    final storedGuest1 = store.profiles.singleWhere((p) => p.id == guestId);
    expect(storedGuest1.pinHash, hash1);
    expect(storedGuest1.salt, salt1);
    expect(await checkProfilePin(storedGuest1, 'guest-pin-123'), isTrue);
    expect(jsonEncode(merged1), isNot(contains('guest-pin-123')));

    const salt2 = '22222222222222222222222222222222';
    final hash2 = await hashProfilePin('new-guest-pin', salt2);
    final guest2 = LocalProfile(
      id: guestId,
      name: '客廳',
      sources: const ['hongguo'],
      download: false,
      salt: salt2,
      pinHash: hash2,
      updatedAt: firstUpdatedAt + 2,
    );
    final changedSnapshot = {
      'schema': 1,
      'profiles': [store.profiles.first.toJson(), guest2.toJson()],
      'tombstones': <String, int>{},
    };
    await store.mergeProfileSyncSnapshot(changedSnapshot);
    final storedGuest2 = store.profiles.singleWhere((p) => p.id == guestId);
    expect(storedGuest2.pinHash, hash2);
    expect(storedGuest2.salt, salt2);
    expect(await checkProfilePin(storedGuest2, 'new-guest-pin'), isTrue);
    expect(await checkProfilePin(storedGuest2, 'guest-pin-123'), isFalse);

    final deleteSnapshot = {
      'schema': 1,
      'profiles': [store.profiles.first.toJson()],
      'tombstones': {guestId: guest2.updatedAt + 1},
    };
    await store.mergeProfileSyncSnapshot(deleteSnapshot);
    final finalSnapshot = await store.exportProfileSyncSnapshot();
    expect(store.profiles.any((p) => p.id == guestId), isFalse);
    expect((finalSnapshot['tombstones'] as Map)[guestId], guest2.updatedAt + 1);
  });

  test('conflicting simultaneous profile edits are not silently overwritten', () async {
    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    await store.saveProfile(
      id: 'default',
      name: '管理員',
      sources: const [],
      download: true,
      pin: 'admin-pin-123',
    );

    const salt = '33333333333333333333333333333333';
    final pinHash = await hashProfilePin('guest-pin-123', salt);
    final updatedAt = DateTime.now().toUtc().millisecondsSinceEpoch;
    final guest = LocalProfile(
      id: 'guest-profile-456',
      name: '臥室',
      sources: const ['hongguo'],
      download: false,
      salt: salt,
      pinHash: pinHash,
      updatedAt: updatedAt,
    );
    final admin = store.profiles.single;
    await store.mergeProfileSyncSnapshot({
      'schema': 1,
      'profiles': [admin.toJson(), guest.toJson()],
      'tombstones': <String, int>{},
    });

    final conflicting = LocalProfile(
      id: guest.id,
      name: '客房',
      sources: guest.sources,
      download: guest.download,
      salt: guest.salt,
      pinHash: guest.pinHash,
      updatedAt: guest.updatedAt,
    );
    await expectLater(
      store.mergeProfileSyncSnapshot({
        'schema': 1,
        'profiles': [store.profiles.first.toJson(), conflicting.toJson()],
        'tombstones': <String, int>{},
      }),
      throwsStateError,
    );
    expect(store.profiles.singleWhere((p) => p.id == guest.id).name, '臥室');
  });
}
