import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'core_bridge.dart';
import 'local_store.dart';
import 'media_library.dart';
import 'models.dart';
import 'player_screen.dart';
import 'settings_screen.dart';
import 'merge_queue_screen.dart';

class LocalMediaScreen extends StatefulWidget {
  const LocalMediaScreen({
    super.key,
    required this.repository,
    required this.store,
  });
  final AppRepository repository;
  final LocalStore store;
  @override
  State<LocalMediaScreen> createState() => _LocalMediaScreenState();
}

class _LocalMediaScreenState extends State<LocalMediaScreen> {
  late final MediaLibrary library =
      MediaLibrary.current ?? MediaLibrary(widget.repository, widget.store);
  List<DownloadJob> _jobs = [];
  bool _loading = true, _merged = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _refresh();
  }

  @override
  void dispose() {
    if (library != MediaLibrary.current && !library.busy) library.dispose();
    super.dispose();
  }

  Future<void> _refresh() async {
    try {
      final jobs = await widget.repository.downloads();
      await library.reload();
      if (mounted) {
        setState(() {
          _jobs = jobs.where((job) => job.completed).toList();
          _error = null;
        });
      }
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _create(bool merge) async {
    if (merge) {
      await Navigator.push(
        context,
        MaterialPageRoute<void>(
          builder: (_) => MergeQueueScreen(
            library: library,
            repository: widget.repository,
            store: widget.store,
            addOnOpen: true,
          ),
        ),
      );
      if (mounted) await _refresh();
      return;
    }
    final groups = <String, List<DownloadJob>>{};
    for (final job in _jobs) {
      (groups[job.drama.id] ??= []).add(job);
    }
    final selected = await showDialog<String>(
      context: context,
      builder: (context) => SimpleDialog(
        title: const Text('匯出哪部劇到 Emby？'),
        children: [
          if (_jobs.isNotEmpty)
            SimpleDialogOption(
              onPressed: () => Navigator.pop(context, '*'),
              child: Text('全部已下載 · ${_jobs.length} 集'),
            ),
          for (final entry in groups.entries)
            SimpleDialogOption(
              onPressed: () => Navigator.pop(context, entry.key),
              child: Text(
                '${entry.value.first.drama.title} · 已下載 ${entry.value.length} 集',
              ),
            ),
          if (groups.isEmpty)
            const Padding(padding: EdgeInsets.all(20), child: Text('請先下載分集')),
        ],
      ),
    );
    if (selected == null || !mounted) return;
    final jobs = selected == '*' ? _jobs : groups[selected]!;
    setState(() {
      _error = null;
      _merged = false;
    });
    try {
      await library.exportJobs(jobs);
      if (mounted) await _refresh();
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    }
  }

  Future<void> _play(LocalMediaItem item) async {
    final watch = widget.store.mediaWatched(item.id);
    final episode = Episode({
      'id': item.id,
      'title': item.merged ? '合併影片' : '第 ${item.episodes.first} 集',
      'currentEpisode': item.episodes.first,
    }, 1);
    await Navigator.push(
      context,
      MaterialPageRoute<void>(
        builder: (_) => PlayerScreen(
          detail: DramaDetail(item.drama, [episode]),
          initialIndex: 0,
          initialPosition: watch == null || watch.finished ? 0 : watch.position,
          repository: _LocalFileRepository(
            widget.repository,
            widget.store,
            item,
            library.fileFor(item),
          ),
          store: widget.store,
          localOnly: true,
          allowOnlineFallback: false,
          mediaId: item.id,
        ),
      ),
    );
    if (mounted) await _refresh();
  }

  Future<void> _remove(LocalMediaItem item) async {
    final yes = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('刪除此媒體檔案？'),
        content: Text(
          '將刪除此成品檔案。已單獨儲存的原分集不受影響。${item.merged || item.special ? '' : '此分集不再自動匯出，需要時可手動重新匯出。'}',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('保留'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('刪除'),
          ),
        ],
      ),
    );
    if (yes != true || !mounted) return;
    try {
      await library.remove(item);
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    }
  }

  Future<void> _exportMerged(LocalMediaItem item) async {
    try {
      await library.exportMerged(item);
      if (mounted) {
        setState(() {
          _merged = false;
          _error = null;
        });
      }
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    }
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: library,
    builder: (_, _) {
      final items = library.items
          .where((item) => item.merged == _merged)
          .toList();
      return Scaffold(
        appBar: AppBar(
          title: const Text('本地媒體'),
          actions: [
            IconButton(
              tooltip: '重新整理',
              onPressed: library.busy ? null : _refresh,
              icon: const Icon(Icons.refresh),
            ),
          ],
        ),
        body: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.all(16),
              child: Wrap(
                spacing: 12,
                runSpacing: 8,
                children: [
                  ChoiceChip(
                    label: const Text('合併成品'),
                    selected: _merged,
                    onSelected: (_) => setState(() => _merged = true),
                  ),
                  ChoiceChip(
                    label: const Text('Emby 匯出'),
                    selected: !_merged,
                    onSelected: (_) => setState(() => _merged = false),
                  ),
                  FilledButton.icon(
                    onPressed: library.busy && !_merged
                        ? null
                        : () => _create(_merged),
                    icon: Icon(_merged ? Icons.merge : Icons.output),
                    label: Text(_merged ? '合併已下載分集' : '匯出已下載分集'),
                  ),
                ],
              ),
            ),
            if (library.busy)
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 20),
                child: Column(
                  children: [
                    LinearProgressIndicator(
                      value: library.progress > 0 ? library.progress : null,
                    ),
                    const SizedBox(height: 8),
                    Row(
                      children: [
                        Expanded(child: Text(library.status)),
                        TextButton(
                          onPressed: () => unawaited(library.cancel()),
                          child: const Text('取消'),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
            if (_error != null || library.error.isNotEmpty)
              Padding(
                padding: const EdgeInsets.all(16),
                child: Text(_error ?? library.error),
              ),
            if (!_merged)
              Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: 20,
                  vertical: 8,
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text(
                      '將 exports 目錄加入 Emby 電視節目庫，可讀取分集名稱和海報後設資料。自動匯出可在設定中開啟。',
                    ),
                    if (library.root != null)
                      TextButton.icon(
                        onPressed: () => Clipboard.setData(
                          ClipboardData(
                            text:
                                '${library.root}${Platform.pathSeparator}exports',
                          ),
                        ),
                        icon: const Icon(Icons.copy),
                        label: const Text('複製 Emby 媒體目錄'),
                      ),
                  ],
                ),
              ),
            Expanded(
              child: _loading
                  ? const Center(child: CircularProgressIndicator())
                  : items.isEmpty
                  ? Center(
                      child: Text(_merged ? '合併成品會顯示在這裡，可直接播放和續播。' : '暫未匯出媒體'),
                    )
                  : ListView.builder(
                      padding: const EdgeInsets.fromLTRB(12, 0, 12, 16),
                      itemCount: items.length,
                      itemBuilder: (context, index) {
                        final item = items[index];
                        return Card(
                          child: Padding(
                            padding: const EdgeInsets.all(12),
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(
                                  item.drama.title,
                                  style: Theme.of(
                                    context,
                                  ).textTheme.titleMedium,
                                ),
                                const SizedBox(height: 6),
                                Text(
                                  '${item.merged
                                      ? '已合併 ${item.episodes.length} 集'
                                      : item.special
                                      ? '特別篇 ${item.specialNumber} · 第 ${item.episodes.first}–${item.episodes.last} 集'
                                      : '第 ${item.episodes.first} 集'} · ${storageSize(item.bytes)}',
                                ),
                                if (item.merged)
                                  Text(
                                    '影片轉碼 ${item.videoTranscodes} 集 · 音軌處理 ${item.audioTranscodes} 集',
                                    style: Theme.of(
                                      context,
                                    ).textTheme.bodySmall,
                                  ),
                                if (item.merged)
                                  TextButton.icon(
                                    onPressed: library.busy
                                        ? null
                                        : () => _exportMerged(item),
                                    icon: const Icon(Icons.output_rounded),
                                    label: const Text('匯出到 Emby 特別篇'),
                                  ),
                                Row(
                                  children: [
                                    TextButton.icon(
                                      onPressed: () => _play(item),
                                      icon: const Icon(Icons.play_arrow),
                                      label: Text(
                                        item.merged &&
                                                item.episodes.length ==
                                                    item.drama.episodes
                                            ? '全集播放'
                                            : '本地播放',
                                      ),
                                    ),
                                    const Spacer(),
                                    IconButton(
                                      tooltip: '複製檔案路徑',
                                      onPressed: () => Clipboard.setData(
                                        ClipboardData(
                                          text: library.fileFor(item),
                                        ),
                                      ),
                                      icon: const Icon(Icons.copy),
                                    ),
                                    IconButton(
                                      tooltip: '刪除成品',
                                      onPressed: library.busy
                                          ? null
                                          : () => _remove(item),
                                      icon: const Icon(Icons.delete_outline),
                                    ),
                                  ],
                                ),
                              ],
                            ),
                          ),
                        );
                      },
                    ),
            ),
          ],
        ),
      );
    },
  );
}

class _LocalFileRepository extends AppRepository {
  _LocalFileRepository(this.parent, this.store, this.item, this.file);
  final AppRepository parent;
  final LocalStore store;
  final LocalMediaItem item;
  final String file;
  Future<PlaybackPlan> _plan() async {
    if (!store.canDownload || !store.allowsSource(item.drama.source)) {
      throw AppFailure('當前使用者無權讀取此影片');
    }
    if (!await File(file).exists() || await File(file).length() == 0) {
      throw AppFailure('本地成品檔案缺失，請重新生成。', code: 'local_media');
    }
    return PlaybackPlan(url: file, local: true);
  }

  @override
  Future<void> initialize() async {}
  @override
  Future<CatalogPage> catalog(
    String source, {
    int page = 1,
    String query = '',
    String category = '',
    bool force = false,
  }) => parent.catalog(
    source,
    page: page,
    query: query,
    category: category,
    force: force,
  );
  @override
  Future<CatalogPage> cached(String source, {String category = ''}) =>
      parent.cached(source, category: category);
  @override
  Future<List<CatalogCategory>> categories(
    String source, {
    bool force = false,
  }) => parent.categories(source, force: force);
  @override
  Future<String> cover(Drama drama, {bool force = false}) =>
      parent.cover(drama, force: force);
  @override
  Future<DramaDetail> detail(Drama drama) => parent.detail(drama);
  @override
  Future<PlaybackPlan> resolve(
    Drama drama,
    Episode episode, {
    int quality = 0,
  }) => _plan();
  @override
  Future<PlaybackPlan?> localPlayback(Drama drama, Episode episode) => _plan();
  @override
  Future<PlaybackPlan> resolveOnline(
    Drama drama,
    Episode episode, {
    int quality = 0,
  }) => throw AppFailure('此成品僅支援本地播放');
  @override
  Future<PlaybackPlan> fallback(PlaybackPlan current) => _plan();
  @override
  Future<void> cancelPlayback() async {}
  @override
  Future<void> release(String session) async {}
}
