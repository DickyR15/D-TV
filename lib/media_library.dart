import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:path/path.dart' as path;

import 'background_downloads.dart';
import 'core_bridge.dart';
import 'local_store.dart';
import 'media_pipeline.dart';
import 'models.dart';

part 'media_merge_queue.dart';
part 'media_merge.dart';
part 'media_exports.dart';

class LocalMediaItem {
  LocalMediaItem({
    required this.id,
    required this.drama,
    required this.file,
    required this.kind,
    required this.episodes,
    required this.duration,
    required this.bytes,
    required this.created,
    this.videoTranscodes = 0,
    this.audioTranscodes = 0,
    this.jobId = '',
    this.sourceVersion = '',
    this.decodeVerified = false,
    this.specialNumber = 0,
  });
  final String id, file, kind, jobId, sourceVersion;
  final bool decodeVerified;
  final int specialNumber;
  bool get special => kind == 'special';
  final Drama drama;
  final List<int> episodes;
  final double duration;
  final int bytes, videoTranscodes, audioTranscodes;
  final DateTime created;
  bool get merged => kind == 'merged';
  Map<String, dynamic> toJson() => {
    'id': id,
    'drama': drama.toJson(),
    'file': file,
    'kind': kind,
    'episodes': episodes,
    'duration': duration,
    'bytes': bytes,
    'created': created.toIso8601String(),
    'videoTranscodes': videoTranscodes,
    'audioTranscodes': audioTranscodes,
    'jobId': jobId,
    'sourceVersion': sourceVersion,
    'decodeVerified': decodeVerified,
    'specialNumber': specialNumber,
  };
  factory LocalMediaItem.fromJson(Map<String, dynamic> value) {
    final file = value['file'] as String;
    if (path.isAbsolute(file) || file.split(RegExp(r'[/\\]')).contains('..')) {
      throw const FormatException('媒體路徑無效');
    }
    return LocalMediaItem(
      id: value['id'] as String,
      drama: Drama.fromJson(Map<String, dynamic>.from(value['drama'] as Map)),
      file: file,
      kind: value['kind'] as String,
      episodes: (value['episodes'] as List).map(intValue).toList(),
      duration: (value['duration'] as num).toDouble(),
      bytes: intValue(value['bytes']),
      created: DateTime.parse(value['created'] as String),
      videoTranscodes: intValue(value['videoTranscodes']),
      audioTranscodes: intValue(value['audioTranscodes']),
      jobId: value['jobId'] as String? ?? '',
      sourceVersion: value['sourceVersion'] as String? ?? '',
      decodeVerified: value['decodeVerified'] == true,
      specialNumber: intValue(value['specialNumber']),
    );
  }
}

String xmlText(String value) => value
    .replaceAll(RegExp(r'[\x00-\x08\x0B\x0C\x0E-\x1F]'), '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&apos;');

String embyShowNfo(Drama drama, {bool localPoster = false}) {
  final uri = Uri.tryParse(drama.cover);
  final cover = localPoster
      ? 'poster.jpg'
      : uri != null && {'https', 'http'}.contains(uri.scheme)
      ? drama.cover
      : '';
  return '<?xml version="1.0" encoding="utf-8"?>\n<tvshow>'
      '<title>${xmlText(drama.title)}</title><plot>${xmlText(drama.description)}</plot>'
      '<uniqueid type="zhenguojian" default="true">${xmlText(drama.id)}</uniqueid>'
      '${cover.isEmpty ? '' : '<thumb aspect="poster">${xmlText(cover)}</thumb>'}'
      '<season>1</season><episode>${drama.episodes}</episode></tvshow>\n';
}

String embyEpisodeNfo(DownloadJob job) =>
    '<?xml version="1.0" encoding="utf-8"?>\n'
    '<episodedetails><title>${xmlText(job.episode.title)}</title>'
    '<showtitle>${xmlText(job.drama.title)}</showtitle><season>1</season><episode>${job.episode.number}</episode>'
    '<plot>${xmlText(job.drama.description)}</plot>'
    '<uniqueid type="zhenguojian" default="true">${xmlText(job.id)}</uniqueid></episodedetails>\n';

String _safeName(String name) {
  var result = name
      .replaceAll(RegExp(r'[<>:"/\\|?*\x00-\x1F]'), '_')
      .trim()
      .replaceAll(RegExp(r'[. ]+$'), '');
  if (result.isEmpty) result = '短劇';
  return String.fromCharCodes(result.runes.take(60));
}

class MediaLibrary extends ChangeNotifier {
  MediaLibrary(
    this.repository,
    this.store, {
    MediaExecutor? executor,
  }) : executor = executor ?? FFmpegExecutor() {
    _observedEpoch = store.profileEpoch;
    store.addListener(_accessChanged);
  }
  final AppRepository repository;
  final LocalStore store;
  final MediaExecutor executor;
  static MediaLibrary? current;
  MergeQueue? _merges;
  MergeQueue get merges => _merges ??= MergeQueue(this);
  int? _taskEpoch;
  late int _observedEpoch;
  Set<String> _taskSources = const {};
  final _showFolders = <String, String>{};
  final _specialNumbers = <String, int>{};

  void _accessChanged() {
    if (_observedEpoch != store.profileEpoch || !store.canDownload) {
      _observedEpoch = store.profileEpoch;
      unawaited(_merges?.pauseUnavailable() ?? Future<void>.value());
      if (busy) unawaited(cancel());
    }
  }

  bool _disposed = false;
  @override
  void notifyListeners() {
    if (!_disposed) super.notifyListeners();
  }

  String? root;
  List<LocalMediaItem> _items = [];
  bool busy = false, _cancelled = false, _suspended = false;
  bool get suspended => _suspended;
  set suspended(bool value) {
    _suspended = value;
    if (value) {
      unawaited(_merges?.pauseUnavailable(all: true) ?? Future<void>.value());
    }
  }

  String status = '', error = '';
  double progress = 0;

  static void attach(AppRepository repository, LocalStore store) {
    current?.dispose();
    current = MediaLibrary(repository, store);
  }

  List<LocalMediaItem> get items =>
      _items
          .where(
            (item) =>
                store.canDownload && store.allowsSource(item.drama.source),
          )
          .toList()
        ..sort((a, b) => b.created.compareTo(a.created));
  String fileFor(LocalMediaItem item) {
    if (root == null) throw AppFailure('媒體目錄尚未就緒');
    final file = path.join(root!, item.file);
    if (!path.isWithin(root!, file)) throw AppFailure('媒體路徑無效');
    return file;
  }

  Future<void> reload({bool duringTask = false}) async {
    if (busy && !duringTask) return;
    final directory = await repository.downloadDirectory();
    if (directory.isEmpty) throw AppFailure('無法讀取下載位置');
    final file = File(path.join(directory, 'media-library.json'));
    var items = <LocalMediaItem>[];
    final folders = <String, String>{},
        numbers = <String, int>{};
    if (await file.exists()) {
      if (await file.length() > 16 * 1024 * 1024) {
        throw AppFailure('本地媒體記錄過大，原檔案已保留');
      }
      final data = jsonDecode(await file.readAsString()) as Map;
      items = (data['items'] as List)
          .map(
            (value) => LocalMediaItem.fromJson(
              Map<String, dynamic>.from(value as Map),
            ),
          )
          .toList();
      if (items.map((item) => item.id).toSet().length != items.length) {
        throw AppFailure('本地媒體記錄重複，原檔案已保留');
      }
      for (final entry in (data['showFolders'] as Map? ?? {}).entries) {
        final folder = entry.value as String;
        if (!_validShowFolder(folder)) throw AppFailure('匯出目錄記錄無效，原檔案已保留');
        folders[entry.key as String] = folder;
      }
      for (final entry in (data['specialNumbers'] as Map? ?? {}).entries) {
        final number = intValue(entry.value);
        if (number < 1 || number > 100000) throw AppFailure('特別篇編號無效，原檔案已保留');
        numbers[entry.key as String] = number;
      }
      for (final item in items.where((item) => !item.merged)) {
        final folder = path.dirname(path.dirname(item.file));
        if (_validShowFolder(folder)) {
          folders.putIfAbsent(item.drama.id, () => folder);
        }
      }
      if (folders.values.toSet().length != folders.length) {
        throw AppFailure('不同劇集的匯出目錄衝突，原檔案已保留');
      }
      for (final item in items.where(
        (item) =>
            item.special &&
            item.specialNumber > 0 &&
            item.id.startsWith('special-'),
      )) {
        numbers.putIfAbsent(
          '${item.drama.id}\u0000${item.id.substring(8)}',
          () => item.specialNumber,
        );
      }
      final uniqueNumbers = <String>{};
      for (final entry in numbers.entries) {
        final show = entry.key.split('\u0000').first;
        if (!uniqueNumbers.add('$show\u0000${entry.value}')) {
          throw AppFailure('特別篇編號重複，原檔案已保留');
        }
      }
    }
    if (busy && !duringTask) return;
    root = directory;
    _items = items;
    _showFolders
      ..clear()
      ..addAll(folders);
    _specialNumbers
      ..clear()
      ..addAll(numbers);
    notifyListeners();
  }

  Future<void> _writeText(File target, String content) async {
    await target.parent.create(recursive: true);
    final temporary = File('${target.path}.writing');
    await temporary.writeAsString(content, flush: true);
    await temporary.rename(target.path);
  }

  Future<void> _save() async {
    final content = jsonEncode({
      'items': _items.map((item) => item.toJson()).toList(),
      'showFolders': _showFolders,
      'specialNumbers': _specialNumbers,
    });
    if (utf8.encode(content).length > 16 * 1024 * 1024) {
      throw AppFailure('本地媒體記錄已滿，請先整理成品記錄');
    }
    await _writeText(File(path.join(root!, 'media-library.json')), content);
  }

  void _check() {
    if (_disposed ||
        _cancelled ||
        suspended ||
        !store.canDownload ||
        _taskEpoch != null && _taskEpoch != store.profileEpoch ||
        _taskSources.any((source) => !store.allowsSource(source))) {
      throw AppFailure('本地媒體處理已停止，原分集和恢復記錄保留');
    }
  }

  Future<T> _task<T>(
    String name,
    Future<T> Function(Directory temporary) action, {
    String? workspace,
    Set<String> sources = const {},
  }) async {
    if (busy) throw AppFailure('已有本地媒體任務正在處理');
    if (!store.canDownload ||
        sources.any((source) => !store.allowsSource(source))) {
      throw AppFailure('當前使用者沒有媒體處理許可權');
    }
    _taskEpoch = store.profileEpoch;
    _taskSources = sources;
    busy = true;
    status = name;
    error = '';
    progress = 0;
    _cancelled = false;
    notifyListeners();
    Directory? temporary;
    bool lease = false;
    try {
      await BackgroundDownloads.ensureStarted();
      await repository.workLease('media', 'start');
      lease = true;
      await reload(duringTask: true);
      for (final entry in Directory(root!).listSync(followLinks: false)) {
        if (entry is Directory &&
            path.basename(entry.path).startsWith('.media-work-')) {
          await entry.delete(recursive: true);
        }
      }
      if (workspace != null) {
        if (!RegExp(r'^[a-zA-Z0-9_-]{1,100}$').hasMatch(workspace)) {
          throw AppFailure('合併任務編號無效');
        }
        temporary = Directory(path.join(root!, '.merge-work', workspace));
        await temporary.create(recursive: true);
      } else {
        temporary = await Directory(root!).createTemp('.media-work-');
      }
      _check();
      final result = await action(temporary);
      progress = 1;
      status = '處理完成';
      return result;
    } catch (failure) {
      error = failure.toString();
      status = _cancelled ? '已取消' : '處理未完成';
      rethrow;
    } finally {
      if (workspace == null && temporary != null && await temporary.exists()) {
        try {
          await temporary.delete(recursive: true);
        } catch (_) {}
      }
      if (lease) {
        try {
          await repository.workLease('media', 'end');
        } catch (_) {}
      }
      busy = false;
      _taskEpoch = null;
      _taskSources = const {};
      notifyListeners();
    }
  }

  Future<void> cancel() async {
    _cancelled = true;
    await executor.cancel();
  }

  Future<MediaProbe> _prepare(
    DownloadJob job,
    String destination,
    double base,
    double weight,
  ) async {
    _check();
    final plan = await repository.localPlayback(job.drama, job.episode);
    _check();
    if (plan == null || !plan.local) {
      throw AppFailure('第 ${job.episode.number} 集尚未完整下載');
    }
    status = '讀取第 ${job.episode.number} 集';
    notifyListeners();
    final input = <String>[];
    if (plan.decryptionKey.isNotEmpty) {
      input.addAll(['-decryption_key', plan.decryptionKey]);
    }
    if (path.extension(plan.url).toLowerCase() == '.m3u8') {
      input.addAll(['-allowed_extensions', 'ALL', '-extension_picky', '0']);
    }
    await executor.run([
      ...input,
      '-i',
      plan.url,
      '-map',
      '0:v:0',
      '-map',
      '0:a:0?',
      '-c',
      'copy',
      '-map_metadata',
      '-1',
      '-avoid_negative_ts',
      'make_zero',
      destination,
    ]);
    _check();
    final probe = await executor.probe(destination);
    _check();
    verifyMediaDuration(probe, 0);
    progress = base + weight;
    notifyListeners();
    return probe;
  }

  String _sourceVersion(DownloadJob job) =>
      '${job.created}-${job.bytes}-${job.actualQuality}'
      '${job.revision > 0 ? '-${job.revision}' : ''}';

  Future<void> _exportMetadata(DownloadJob job, File target) async {
    await _writeShowMetadata(job.drama, target.parent.parent);
    _check();
    await _writeText(
      File(path.setExtension(target.path, '.nfo')),
      embyEpisodeNfo(job),
    );
  }

  Future<void> exportJobs(List<DownloadJob> selected) async {
    final jobs = selected.where((job) => job.completed).toList()
      ..sort((a, b) => a.episode.number.compareTo(b.episode.number));
    if (jobs.isEmpty) throw AppFailure('沒有已下載的分集');
    await _task('準備匯出 Emby', (temporary) async {
      for (var i = 0; i < jobs.length; i++) {
        _check();
        final job = jobs[i], id = 'export-${jobs[i].id}';
        final existing = _items.where((item) => item.id == id).firstOrNull;
        if (existing?.sourceVersion == _sourceVersion(job) &&
            await File(fileFor(existing!)).exists()) {
          await _exportMetadata(job, File(fileFor(existing)));
          progress = (i + 1) / jobs.length;
          notifyListeners();
          continue;
        }
        final intermediate = path.join(temporary.path, 'export-$i.mkv');
        final probe = await _prepare(
          job,
          intermediate,
          i / jobs.length,
          .8 / jobs.length,
        );
        _check();
        status = '匯出第 ${job.episode.number} 集';
        notifyListeners();
        final show = await _showDirectory(job.drama);
        final relative = path.join(
          show,
          'Season 01',
          'S01E${job.episode.number.toString().padLeft(3, '0')}.mkv',
        );
        final target = File(path.join(root!, relative));
        await target.parent.create(recursive: true);
        await File(intermediate).rename(target.path);
        await _exportMetadata(job, target);
        _items.removeWhere((item) => item.id == id);
        _items.add(
          LocalMediaItem(
            id: id,
            drama: job.drama,
            file: relative,
            kind: 'export',
            episodes: [job.episode.number],
            duration: probe.duration,
            bytes: await target.length(),
            created: DateTime.now(),
            jobId: job.id,
            sourceVersion: _sourceVersion(job),
          ),
        );
        await _save();
        progress = (i + 1) / jobs.length;
        notifyListeners();
      }
    }, sources: jobs.map((job) => job.drama.source).toSet());
  }

  Future<void> remove(LocalMediaItem selected) async {
    await _task('刪除本地媒體', (_) async {
      final item = _items.where((item) => item.id == selected.id).firstOrNull;
      if (item == null) return;
      final file = File(fileFor(item));
      if (item.merged) {
        if (await file.parent.exists()) {
          await file.parent.delete(recursive: true);
        }
      } else {
        if (await file.exists()) await file.delete();
        final metadata = File(path.setExtension(file.path, '.nfo'));
        if (await metadata.exists()) await metadata.delete();
      }
      _items.removeWhere((entry) => entry.id == item.id);
      await _save();
    }, sources: {selected.drama.source});
  }

  @override
  void dispose() {
    _disposed = true;
    _cancelled = true;
    _merges?.dispose();
    store.removeListener(_accessChanged);
    unawaited(executor.cancel());
    super.dispose();
  }
}
