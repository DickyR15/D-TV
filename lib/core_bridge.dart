import 'dart:async';
import 'dart:convert';
import 'dart:ffi';
import 'dart:io';
import 'dart:isolate';

import 'package:ffi/ffi.dart';
import 'package:path/path.dart' as path;
import 'package:path_provider/path_provider.dart';

import 'models.dart';
import 'danmaku_models.dart';
import 'background_downloads.dart';
import 'local_store.dart';
import 'app_build.dart';
import 'source_status.dart';
import 'ranking_models.dart';
import 'cover_decoder.dart';
import 'catalog_updates.dart';
import 'download_collections.dart';
import 'resource_settings.dart';
import 'diary_service.dart';

typedef _NativeRequest = Pointer<Utf8> Function(Pointer<Utf8>);
typedef _DartRequest = Pointer<Utf8> Function(Pointer<Utf8>);
typedef _NativeFree = Void Function(Pointer<Utf8>);
typedef _DartFree = void Function(Pointer<Utf8>);

String _nativeRequest(String body) {
  final DynamicLibrary library;
  if (Platform.isAndroid) {
    library = DynamicLibrary.open('libduanju_core.so');
  } else if (Platform.isWindows) {
    library = DynamicLibrary.open(
      path.join(path.dirname(Platform.resolvedExecutable), 'duanju_core.dll'),
    );
  } else if (Platform.isIOS) {
    library = DynamicLibrary.process();
  } else if (Platform.isMacOS) {
    final frameworkLibrary = path.normalize(
      path.join(
        path.dirname(Platform.resolvedExecutable),
        '..',
        'Frameworks',
        'libduanju_core.dylib',
      ),
    );
    library = DynamicLibrary.open(frameworkLibrary);
  } else {
    throw UnsupportedError('目前平台尚未載入本機共用核心');
  }
  final request = library.lookupFunction<_NativeRequest, _DartRequest>(
    'DuanjuRequest',
  );
  final free = library.lookupFunction<_NativeFree, _DartFree>('DuanjuFree');
  final input = body.toNativeUtf8();
  Pointer<Utf8> output = nullptr;
  try {
    output = request(input);
    if (output == nullptr) {
      throw StateError('本機核心沒有傳回結果');
    }
    return output.toDartString();
  } finally {
    malloc.free(input);
    if (output != nullptr) {
      free(output);
    }
  }
}

class AppFailure implements Exception {
  AppFailure(this.message, {this.code = ''});
  final String message;
  final String code;
  @override
  String toString() => message;
}

abstract class AppRepository {
  Future<Map<String, dynamic>> lan(
    String command,
    Map<String, dynamic> payload,
  ) async => throw AppFailure('目前環境不支援裝置互聯');
  Future<PlaybackPlan?> prepareHandoff(
    Drama drama,
    Episode episode, {
    int quality = 0,
  }) => preload(drama, episode, quality: quality);
  Future<void> cancelHandoff() async {}
  Future<ResourceSettings> resourceSettings() async => const ResourceSettings();
  Future<ResourceSettings> saveResourceSettings(
    ResourceSettings settings,
  ) async => throw AppFailure('目前環境不支援資源設定');
  final catalogUpdates = CatalogUpdates();
  Future<void> cancelPreload() async {}
  Future<PlaybackPlan?> preload(
    Drama drama,
    Episode episode, {
    int quality = 0,
    bool online = false,
  }) async => null;
  Future<void> cancelDanmaku() async {}
  Future<DanmakuPage> danmaku(
    PlaybackPlan plan, {
    required int startMs,
    required int durationMs,
  }) async => throw AppFailure('目前環境不支援彈幕');
  Future<void> cancelCatalog() async {}
  Future<void> cancelSuggestions() async {}
  Future<void> cancelRecommendations() async {}
  Future<Drama?> supplementMetadata(Drama drama) async => null;
  Future<Drama> followCheck(Drama drama) async => (await detail(drama)).drama;
  Future<CatalogPage> recommendations(
    String genre, {
    bool more = false,
    bool force = false,
  }) async => throw AppFailure('目前環境不支援紅果推薦');
  Future<List<RankingBoard>> rankingBoards() async => const [];
  Future<RankingPage> rankings(
    String board, {
    int page = 1,
    bool force = false,
  }) async => throw AppFailure('目前環境不支援榜單');
  Future<RankingPage?> cachedRankingTop100(String board) async => null;
  Future<List<CatalogCategory>> categories(
    String source, {
    bool force = false,
  }) async => const [CatalogCategory.all];
  bool get supportsSourceManagement => false;
  Future<SourceStatus> sourceStatus(String source) async =>
      SourceStatus.fromJson({'source': source});
  Future<SourceStatus> startSourceJob(
    String source,
    String operation, {
    Drama? drama,
  }) async => throw AppFailure('目前環境不支援片源管理');
  Future<SourceStatus> startAutomaticSourceCheck(String source) async =>
      startSourceJob(source, 'check');
  Future<SourceStatus> cancelSourceJob(String source) async =>
      throw AppFailure('目前環境不支援片源管理');
  Future<List<String>> suggestions(String query) async => const [];
  Future<Map<String, dynamic>> storage() async => {};
  Future<String> downloadDirectory() async =>
      (await storage())['directory'] as String? ?? '';
  Future<void> moveDownloads(String directory) async =>
      throw AppFailure('目前環境不支援遷移');
  Future<int> workLease(String id, String command) async => 0;
  bool get supportsDownloads => false;
  Future<List<DownloadJob>> downloads() async => [];
  Future<int> enqueueDownloads(
    DramaDetail detail,
    List<Episode> episodes, {
    int quality = 0,
  }) async => throw AppFailure('目前環境不支援下載');
  Future<void> controlDownloads(String command, {String id = ''}) async {}
  Future<DownloadBatchResult> controlDownloadBatch(
    String command,
    List<String> ids, {
    Map<String, String> expectedVersions = const {},
  }) async {
    if (expectedVersions.isNotEmpty) throw AppFailure('目前環境不支援校驗後清理原分集');
    final completed = <String>[], failures = <String, String>{};
    for (final id in ids.toSet()) {
      try {
        await controlDownloads(command, id: id);
        completed.add(id);
      } catch (error) {
        failures[id] = error.toString();
      }
    }
    return DownloadBatchResult(completed: completed, failures: failures);
  }

  Future<int> updateDownloadCollection(
    DramaDetail detail,
    List<Episode> episodes, {
    int quality = 0,
  }) => enqueueDownloads(detail, episodes, quality: quality);
  Future<PlaybackPlan?> localPlayback(Drama drama, Episode episode) async =>
      null;
  Future<PlaybackPlan> resolveOnline(
    Drama drama,
    Episode episode, {
    int quality = 0,
  }) => resolve(drama, episode, quality: quality);
  Future<void> initialize();
  Future<CatalogPage> catalog(
    String source, {
    int page = 1,
    String query = '',
    String category = '',
    bool force = false,
  });
  Future<CatalogPage> cached(String source, {String category = ''});
  /// Fast first-screen snapshot. Implementations without a bounded native
  /// cache read may return null and keep using [cached].
  Future<CatalogPage?> cachedPreview(String source, {String category = ''}) async => null;
  Future<String> cover(Drama drama, {bool force = false});
  bool coverCached(Drama drama) => false;
  Future<DramaDetail> detail(Drama drama);
  Future<PlaybackPlan> resolve(Drama drama, Episode episode, {int quality = 0});
  Future<PlaybackPlan> fallback(PlaybackPlan current);
  Future<void> cancelPlayback();
  Future<void> release(String session);
}

class _DramaDetailCacheEntry {
  _DramaDetailCacheEntry(this.future);
  final Future<DramaDetail> future;
  bool completed = false;
  DateTime expiresAt = DateTime.fromMillisecondsSinceEpoch(0);
}

class NativeRepository extends AppRepository {
  static final _coverDecoder = CoverDecoder();
  static final _coverPaths = <String, String>{};
  NativeRepository({this.background = false});
  final bool background;
  LocalStore? access;
  final _readOwner = DateTime.now().microsecondsSinceEpoch.toString();
  int _readSequence = 0;
  final _activeReads = <String, int>{};
  final _detailCache = <String, _DramaDetailCacheEntry>{};

  @override
  Future<Map<String, dynamic>> lan(
    String command,
    Map<String, dynamic> payload,
  ) => _call({'action': 'lan', 'command': command, 'lan': payload});

  @override
  Future<PlaybackPlan?> prepareHandoff(
    Drama drama,
    Episode episode, {
    int quality = 0,
  }) async {
    final data = await _read('handoff', {
      'action': 'prepareHandoff',
      'drama': drama.toJson(),
      'chapter': episode.raw,
      'index': episode.number,
      'quality': quality,
      'force': access != null && !access!.canDownload,
    });
    return PlaybackPlan.fromJson(data);
  }

  @override
  Future<void> cancelHandoff() => _cancelReads('handoff');

  void _adminPermission() {
    if (access != null && (access!.locked || !access!.profile.admin)) {
      throw AppFailure('僅管理員可修改本機資源設定');
    }
  }

  @override
  Future<ResourceSettings> resourceSettings() async {
    _adminPermission();
    return ResourceSettings.fromJson(
      await _call({'action': 'resourceSettings'}),
    );
  }

  @override
  Future<ResourceSettings> saveResourceSettings(
    ResourceSettings settings,
  ) async {
    _adminPermission();
    return ResourceSettings.fromJson(
      await _call({
        'action': 'saveResourceSettings',
        'settings': settings.toJson(),
      }),
    );
  }

  Future<Map<String, dynamic>> _read(
    String scope,
    Map<String, dynamic> input,
  ) async {
    final sequence = ++_readSequence;
    _activeReads[scope] = sequence;
    try {
      return await _call({
        ...input,
        'session': '$_readOwner:$scope',
        'sequence': sequence,
      });
    } finally {
      if (_activeReads[scope] == sequence) _activeReads.remove(scope);
    }
  }

  Future<void> _cancelReads(String prefix) async {
    final reads = _activeReads.entries
        .where((entry) => entry.key.startsWith(prefix))
        .toList();
    await Future.wait(
      reads.map((entry) async {
        try {
          await _call({
            'action': 'cancelRead',
            'session': '$_readOwner:${entry.key}',
            'sequence': entry.value,
          });
        } catch (_) {}
      }),
    );
  }

  @override
  Future<void> cancelDanmaku() => _cancelReads('danmaku');

  @override
  Future<void> cancelPreload() => _cancelReads('preload');

  @override
  Future<PlaybackPlan?> preload(
    Drama drama,
    Episode episode, {
    int quality = 0,
    bool online = false,
  }) async => PlaybackPlan.fromJson(
    await _read('preload', {
      'action': 'preload',
      'drama': drama.toJson(),
      'chapter': episode.raw,
      'index': episode.number,
      'quality': quality,
      'force': online || access?.canDownload == false,
    }),
  );

  @override
  Future<DanmakuPage> danmaku(
    PlaybackPlan plan, {
    required int startMs,
    required int durationMs,
  }) async {
    _authorize('hongguo');
    if (plan.local || plan.session.isEmpty || plan.danmakuId.isEmpty) {
      throw AppFailure('本集暫不支援彈幕');
    }
    return DanmakuPage.fromJson(
      await _read('danmaku', {
        'action': 'danmaku',
        'playbackSession': plan.session,
        'startMs': startMs,
        'durationMs': durationMs,
      }),
      episodeId: plan.danmakuId,
      startMs: startMs,
      durationMs: durationMs,
    );
  }

  @override
  Future<void> cancelCatalog() => _cancelReads('catalog-');
  @override
  Future<void> cancelSuggestions() => _cancelReads('suggestions');
  @override
  Future<void> cancelRecommendations() => _cancelReads('recommendations-');

  @override
  Future<CatalogPage> recommendations(
    String genre, {
    bool more = false,
    bool force = false,
  }) async {
    _authorize('hongguo');
    return CatalogPage.fromJson(
      await _read('recommendations-$genre', {
        'action': 'recommendations',
        'category': genre,
        'command': more ? 'more' : '',
        'force': force,
      }),
    );
  }

  @override
  Future<Drama?> supplementMetadata(Drama drama) async {
    if (!(drama.source == 'hongguo' && drama.onlineDate.isEmpty ||
        drama.source == 'huangdou' &&
            (drama.heat.isEmpty || drama.vipStatus == null))) {
      return null;
    }
    final result = await _read('metadata', {
      'action': 'metadata',
      'drama': drama.toJson(),
    });
    return Drama.fromJson(Map<String, dynamic>.from(result['drama'] as Map));
  }

  @override
  Future<Drama> followCheck(Drama drama) async {
    _authorize(drama.source);
    final result = await _call({
      'action': 'followCheck',
      'drama': drama.toJson(),
    });
    return Drama.fromJson(Map<String, dynamic>.from(result['drama'] as Map));
  }

  @override
  bool get supportsSourceManagement => true;

  @override
  Future<List<RankingBoard>> rankingBoards() async {
    final result = await _call({'action': 'rankingBoards'});
    return [
          for (final row in result['items'] as List? ?? [])
            RankingBoard.fromJson(Map<String, dynamic>.from(row as Map)),
        ]
        .where(
          (board) =>
              SourceSite.isAvailable(board.source) &&
              (access?.allowsSource(board.source) ?? true),
        )
        .toList();
  }

  @override
  Future<RankingPage> rankings(
    String board, {
    int page = 1,
    bool force = false,
  }) async => RankingPage.fromJson(
    await _call({
      'action': 'rankings',
      'board': board,
      'page': page,
      'force': force,
    }),
  );

  @override
  Future<RankingPage?> cachedRankingTop100(String board) async =>
      RankingPage.fromJson(
        await _call({
          'action': 'rankings',
          'board': board,
          'page': 0,
          'cacheOnly': true,
        }),
      );

  @override
  Future<SourceStatus> sourceStatus(String source) async =>
      SourceStatus.fromJson(
        await _call({'action': 'sourceStatus', 'source': source}),
      );

  @override
  Future<SourceStatus> startSourceJob(
    String source,
    String operation, {
    Drama? drama,
  }) async {
    _authorize(source);
    if (drama != null && drama.source != source) throw AppFailure('片源與劇集不相符');
    final epoch = access?.profileEpoch;
    await BackgroundDownloads.ensureStarted();
    if (epoch != access?.profileEpoch) throw AppFailure('使用者已切換，請重新操作');
    return SourceStatus.fromJson(
      await _call({
        'action': 'sourceJob',
        'source': source,
        'command': operation,
        if (drama != null) 'drama': drama.toJson(),
      }),
    );
  }

  @override
  Future<SourceStatus> startAutomaticSourceCheck(String source) async {
    _authorize(source);
    // Do not start the foreground download service just to run a lightweight
    // health probe; the native source task is already asynchronous.
    return SourceStatus.fromJson(
      await _call({
        'action': 'sourceJob',
        'source': source,
        'command': 'check',
      }),
    );
  }

  @override
  Future<SourceStatus> cancelSourceJob(String source) async =>
      SourceStatus.fromJson(
        await _call({'action': 'cancelSourceJob', 'source': source}),
      );

  void _authorize(String source, {bool download = false}) {
    if (!SourceSite.isAvailable(source)) {
      throw AppFailure('目前版本不包含此片源');
    }
    if (access == null) return;
    if (access!.locked ||
        !access!.allowsSource(source) ||
        download && !access!.canDownload) {
      throw AppFailure('目前使用者沒有執行此操作的權限');
    }
  }

  void _downloadPermission() {
    if (access != null && (access!.locked || !access!.canDownload)) {
      throw AppFailure('目前使用者僅能線上觀看');
    }
  }

  @override
  Future<List<String>> suggestions(String query) async {
    _authorize('hongguo');
    final result = await _read('suggestions', {
      'action': 'suggestions',
      'query': query,
    });
    return (result['items'] as List? ?? []).whereType<String>().toList();
  }

  @override
  Future<String> downloadDirectory() async {
    _downloadPermission();
    return (await _call({'action': 'downloadDirectory'}))['directory']
            as String? ??
        '';
  }

  @override
  Future<Map<String, dynamic>> storage() async {
    _downloadPermission();
    return _call({'action': 'storage'});
  }

  @override
  Future<void> moveDownloads(String directory) async {
    _downloadPermission();
    if (access != null && !access!.profile.admin) {
      throw AppFailure('只有管理員可以變更下載資料夾');
    }
    await BackgroundDownloads.ensureStarted();
    await workLease('storage', 'start');
    try {
      await _call({'action': 'moveDownloads', 'directory': directory});
    } finally {
      await workLease('storage', 'end');
    }
  }

  @override
  Future<int> workLease(String id, String command) async => intValue(
    (await _call({
      'action': 'workLease',
      'jobId': id,
      'command': command,
    }))['count'],
  );
  int _playbackSequence = DateTime.now().microsecondsSinceEpoch;

  Future<Map<String, dynamic>> _call(Map<String, dynamic> input) async {
    try {
      final action = input['action'] as String;
      final unrestricted =
          {
            'initialize',
            'release',
            'cancelPlayback',
            'cancelRead',
            'updateSystemProxy',
          }.contains(action) ||
          action == 'workLease' && input['command'] == 'end' ||
          action == 'lan' &&
              {
                'stop',
                'respond',
                'cancel',
                'disconnect',
              }.contains(input['command']);
      final epoch = access?.profileEpoch;
      if (!unrestricted && access?.locked == true) throw AppFailure('請先解鎖目前使用者');
      if (action == 'rankings') {
        _authorize(RankingBoard.sourceForID(input['board'] as String));
      }
      if (action == 'recommendations' ||
          action == 'cachedRecommendations' ||
          action == 'suggestions' ||
          action == 'danmaku') {
        _authorize('hongguo');
      }
      if ({
        'catalog',
        'cached',
        'cachedPreview',
        'categories',
        'sourceStatus',
        'sourceJob',
        'cancelSourceJob',
      }.contains(action)) {
        _authorize(input['source'] as String);
      }
      if ({
        'cover',
        'prepareCover',
        'detail',
        'metadata',
        'resolve',
        'preload',
        'prepareHandoff',
        'enqueueDownloads',
        'localPlayback',
      }.contains(action)) {
        _authorize(
          (input['drama'] as Map)['source'] as String,
          download: action == 'enqueueDownloads' || action == 'localPlayback',
        );
      }
      if (!unrestricted &&
          {
            'downloads',
            'controlDownloads',
            'controlDownloadBatch',
            'storage',
            'downloadDirectory',
            'moveDownloads',
            'workLease',
          }.contains(action)) {
        _downloadPermission();
      }
      if (action == 'resolve' && access != null && !access!.canDownload) {
        input['force'] = true;
      }
      final body = jsonEncode(input);
      final response =
          await Isolate.run(
            () => jsonDecode(_nativeRequest(body)) as Map<String, dynamic>,
          ).timeout(
            Duration(
              seconds: action == 'moveDownloads'
                  ? 620
                  : action == 'danmaku'
                  ? 15
                  : action == 'preload'
                  ? 20
                  : 70,
            ),
          );
      if (response['ok'] != true) {
        throw AppFailure(
          response['error'] as String? ?? '讀取失敗，請重試',
          code: response['code'] as String? ?? '',
        );
      }
      final data = response['data'];
      if (!unrestricted && epoch != access?.profileEpoch) {
        if (data is Map && data['session'] is String) {
          await release(data['session'] as String);
        }
        if (action == 'workLease' && input['command'] == 'start') {
          await workLease(input['jobId'] as String, 'end');
        }
        throw AppFailure('使用者已切換，請重新操作');
      }
      return data is Map ? Map<String, dynamic>.from(data) : {};
    } on AppFailure {
      rethrow;
    } on TimeoutException {
      if (input['session'] is String &&
          input['sequence'] is int &&
          {
            'catalog',
            'categories',
            'suggestions',
            'recommendations',
            'metadata',
            'danmaku',
            'preload',
            'prepareHandoff',
          }.contains(input['action'])) {
        unawaited(
          _call({
            'action': 'cancelRead',
            'session': input['session'],
            'sequence': input['sequence'],
          }).catchError((Object _) => <String, dynamic>{}),
        );
      }
      throw AppFailure('片源回應逾時，請重試');
    } catch (_) {
      throw AppFailure('本機核心載入失敗，請使用完整安裝包重新安裝');
    }
  }

  @override
  Future<void> initialize() async {
    final directory = await getApplicationSupportDirectory();
    if (!background) {
      DiaryService.nativeLogPath = path.join(directory.path, 'logs', 'app.log');
    }
    final build = await _call({
      'action': 'initialize',
      'directory': directory.path,
    });
    if (build['allSources'] != allSourcesEnabled) {
      throw AppFailure('應用程式與原生核心的片源版本不一致，請使用完整安裝包重新安裝');
    }
    if (!background) await BackgroundDownloads.prepare();
    if (!background) {
      SystemProxyMonitor.start((value) async {
        await _call({'action': 'updateSystemProxy', 'systemProxy': value});
      });
    }
  }

  @override
  Future<List<CatalogCategory>> categories(
    String source, {
    bool force = false,
  }) async {
    final result = await _read('categories-$source', {
      'action': 'categories',
      'source': source,
      'force': force,
    });
    return [
      for (final row in result['items'] as List? ?? const [])
        CatalogCategory.fromJson(Map<String, dynamic>.from(row as Map)),
    ];
  }

  @override
  Future<CatalogPage> catalog(
    String source, {
    int page = 1,
    String query = '',
    String category = '',
    bool force = false,
  }) async => CatalogPage.fromJson(
    await _read('catalog-$source', {
      'action': 'catalog',
      'source': source,
      'page': page,
      'query': query,
      'category': category,
      'force': force,
    }),
  );
  @override
  Future<CatalogPage> cached(String source, {String category = ''}) async =>
      CatalogPage.fromJson(
        await _call({
          'action': 'cached',
          'source': source,
          'category': category,
        }),
      );

  @override
  Future<CatalogPage?> cachedPreview(
    String source, {
    String category = '',
  }) async => CatalogPage.fromJson(
    await _call({
      'action': 'cachedPreview',
      'source': source,
      'category': category,
    }),
  );

  @override
  bool coverCached(Drama drama) =>
      _coverPaths.containsKey('${drama.id}\u0000${drama.cover}');

  @override
  Future<String> cover(Drama drama, {bool force = false}) async {
    final key = '${drama.id}\u0000${drama.cover}';
    final known = force ? null : _coverPaths[key];
    if (known != null) {
      _authorize(drama.source);
      if (await File(known).exists()) return known;
      _coverPaths.remove(key);
    }
    final epoch = access?.profileEpoch;
    final result = await _call({
      'action': 'cover',
      'drama': drama.toJson(),
      'force': force,
    });
    final file = result['path'] as String? ?? '';
    if (file.isEmpty) throw AppFailure('海報暫不可用');
    if (result['heic'] != true) return _rememberCover(key, file);
    final converted = await _coverDecoder.convert(
      file,
      () => _call({'action': 'prepareCover', 'drama': drama.toJson()}),
      force: force,
    );
    _authorize(drama.source);
    if (epoch != access?.profileEpoch) throw AppFailure('使用者已切換，請重新操作');
    return _rememberCover(key, converted);
  }

  String _rememberCover(String key, String path) {
    _coverPaths.remove(key);
    _coverPaths[key] = path;
    if (_coverPaths.length > 2000) _coverPaths.remove(_coverPaths.keys.first);
    return path;
  }

  @override
  Future<DramaDetail> detail(Drama drama) {
    _authorize(drama.source);
    final epoch = access?.profileEpoch ?? -1;
    final key = '$epoch\u0000${drama.source}\u0000${drama.id}';
    final now = DateTime.now();
    final cached = _detailCache[key];
    if (cached != null &&
        (!cached.completed || now.isBefore(cached.expiresAt))) {
      return cached.future;
    }
    _detailCache.remove(key);

    final completer = Completer<DramaDetail>();
    final entry = _DramaDetailCacheEntry(completer.future);
    _detailCache[key] = entry;
    _pruneDetailCache(keepKey: key);
    unawaited(() async {
      try {
        final value = DramaDetail.fromJson(
          await _call({'action': 'detail', 'drama': drama.toJson()}),
        );
        if ((access?.profileEpoch ?? -1) != epoch) {
          throw AppFailure('使用者已切換，請重新操作');
        }
        entry.completed = true;
        entry.expiresAt = DateTime.now().add(const Duration(seconds: 20));
        completer.complete(value);
      } catch (error, stack) {
        if (identical(_detailCache[key], entry)) _detailCache.remove(key);
        completer.completeError(error, stack);
      }
    }());
    return entry.future;
  }

  void _pruneDetailCache({required String keepKey}) {
    final now = DateTime.now();
    _detailCache.removeWhere(
      (key, entry) =>
          key != keepKey &&
          entry.completed &&
          !now.isBefore(entry.expiresAt),
    );
    while (_detailCache.length > 80) {
      final removable = _detailCache.keys.firstWhere(
        (key) => key != keepKey && (_detailCache[key]?.completed ?? false),
        orElse: () => keepKey,
      );
      if (removable == keepKey) break;
      _detailCache.remove(removable);
    }
  }
  @override
  Future<PlaybackPlan> resolve(
    Drama drama,
    Episode episode, {
    int quality = 0,
  }) async => PlaybackPlan.fromJson(
    await _call({
      'action': 'resolve',
      'drama': drama.toJson(),
      'chapter': episode.raw,
      'index': episode.number,
      'quality': quality,
      'sequence': ++_playbackSequence,
    }),
  );
  @override
  Future<PlaybackPlan> fallback(PlaybackPlan current) async =>
      PlaybackPlan.fromJson(
        await _call({
          'action': 'fallback',
          'session': current.session,
          'sequence': ++_playbackSequence,
        }),
      );
  @override
  Future<void> cancelPlayback() async {
    await _call({'action': 'cancelPlayback', 'sequence': ++_playbackSequence});
  }

  @override
  bool get supportsDownloads => access?.canDownload ?? true;

  @override
  Future<List<DownloadJob>> downloads() async {
    final result = await _call({'action': 'downloads'});
    return (result['jobs'] as List? ?? [])
        .whereType<Map>()
        .map((value) => DownloadJob.fromJson(Map<String, dynamic>.from(value)))
        .where(
          (job) =>
              SourceSite.isAvailable(job.drama.source) &&
              (access == null || access!.allowsSource(job.drama.source)),
        )
        .toList();
  }

  @override
  Future<int> enqueueDownloads(
    DramaDetail detail,
    List<Episode> episodes, {
    int quality = 0,
  }) async {
    _authorize(detail.drama.source, download: true);
    final epoch = access?.profileEpoch;
    await BackgroundDownloads.ensureStarted();
    if (epoch != access?.profileEpoch) throw AppFailure('使用者已切換，請重新操作');
    final result = await _call({
      'action': 'enqueueDownloads',
      'drama': detail.drama.toJson(),
      'quality': quality,
      'entries': episodes
          .map((episode) => {'chapter': episode.raw, 'index': episode.number})
          .toList(),
    });
    return intValue(result['added']);
  }

  @override
  Future<int> updateDownloadCollection(
    DramaDetail detail,
    List<Episode> episodes, {
    int quality = 0,
  }) async {
    _authorize(detail.drama.source, download: true);
    final epoch = access?.profileEpoch;
    await BackgroundDownloads.ensureStarted();
    if (epoch != access?.profileEpoch) throw AppFailure('使用者已切換，請重新操作');
    final result = await _call({
      'action': 'enqueueDownloads',
      'force': true,
      'drama': detail.drama.toJson(),
      'quality': quality,
      'entries': episodes
          .map((episode) => {'chapter': episode.raw, 'index': episode.number})
          .toList(),
    });
    return intValue(result['added']);
  }

  @override
  Future<DownloadBatchResult> controlDownloadBatch(
    String command,
    List<String> ids, {
    Map<String, String> expectedVersions = const {},
  }) async {
    _downloadPermission();
    final epoch = access?.profileEpoch;
    if (ids.isEmpty || ids.length > 500) throw AppFailure('每批請選擇 1 至 500 個任務');
    final visible = (await downloads()).map((job) => job.id).toSet();
    if (ids.any((id) => !visible.contains(id))) {
      throw AppFailure('部分任務已刪除或目前使用者無權操作，請重新整理');
    }
    if (command == 'resume') await BackgroundDownloads.ensureStarted();
    if (epoch != access?.profileEpoch) throw AppFailure('使用者已切換，請重新操作');
    return DownloadBatchResult.fromJson(
      await _call({
        'action': 'controlDownloadBatch',
        'command': command,
        'jobIds': ids,
        if (expectedVersions.isNotEmpty) 'expectedVersions': expectedVersions,
      }),
    );
  }

  @override
  Future<void> controlDownloads(String command, {String id = ''}) async {
    _downloadPermission();
    if (command == 'resume' || command == 'resumeAll') {
      await BackgroundDownloads.ensureStarted();
    }
    if (access != null && !access!.profile.admin) {
      final visible = await downloads();
      if (command == 'pauseAll' || command == 'resumeAll') {
        for (final job in visible.where(
          (job) => command == 'pauseAll' ? job.active : job.resumable,
        )) {
          await _call({
            'action': 'controlDownloads',
            'command': command == 'pauseAll' ? 'pause' : 'resume',
            'jobId': job.id,
          });
        }
        return;
      }
      if (!visible.any((job) => job.id == id)) {
        throw AppFailure('目前使用者沒有此下載任務的權限');
      }
    }
    await _call({
      'action': 'controlDownloads',
      'command': command,
      'jobId': id,
    });
  }

  @override
  Future<PlaybackPlan?> localPlayback(Drama drama, Episode episode) async {
    final result = await _call({
      'action': 'localPlayback',
      'drama': drama.toJson(),
      'index': episode.number,
    });
    if ((result['url'] as String? ?? '').isEmpty) return null;
    return PlaybackPlan.fromJson(result);
  }

  @override
  Future<PlaybackPlan> resolveOnline(
    Drama drama,
    Episode episode, {
    int quality = 0,
  }) async => PlaybackPlan.fromJson(
    await _call({
      'action': 'resolve',
      'drama': drama.toJson(),
      'chapter': episode.raw,
      'index': episode.number,
      'quality': quality,
      'force': true,
      'sequence': ++_playbackSequence,
    }),
  );

  @override
  Future<void> release(String session) async {
    if (session.isEmpty) {
      return;
    }
    try {
      await _call({'action': 'release', 'session': session});
    } catch (_) {}
  }
}
