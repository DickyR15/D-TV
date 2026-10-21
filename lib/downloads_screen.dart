import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'app_layout.dart';
import 'core_bridge.dart';
import 'download_collections.dart';
import 'local_store.dart';
import 'local_media_screen.dart';
import 'models.dart';
import 'player_screen.dart';
import 'remote_widgets.dart';
import 'settings_screen.dart';
import 'widgets.dart';

class DownloadsScreen extends StatefulWidget {
  const DownloadsScreen({
    super.key,
    required this.repository,
    required this.store,
    this.embedded = false,
    this.playerBuilder,
  });
  final AppRepository repository;
  final LocalStore store;
  final bool embedded;
  @visibleForTesting
  final Widget Function(DramaDetail, int, double)? playerBuilder;
  @override
  State<DownloadsScreen> createState() => _DownloadsScreenState();
}

class _DownloadsScreenState extends State<DownloadsScreen> {
  Timer? _timer;
  final _search = TextEditingController();
  final _selected = <String>{};
  final _expanded = <String>{};
  late final DownloadCollectionUpdater _updater;
  late final int _epoch;
  List<DownloadJob> _jobs = [];
  bool _loading = true,
      _refreshing = false,
      _opening = false,
      _selecting = false,
      _busy = false,
      _stopBatch = false;
  String? _error, _retryCommand;
  String _filter = 'all', _release = '', _operation = '';
  bool get _allowed =>
      widget.store.profileEpoch == _epoch && widget.store.canDownload;
  List<DownloadCollection> get _collections => downloadCollections(
    _jobs,
    query: _search.text,
    filter: _filter,
    release: _release,
  );

  @override
  void initState() {
    super.initState();
    _epoch = widget.store.profileEpoch;
    _updater = DownloadCollectionUpdater(widget.repository, widget.store)
      ..addListener(_changed);
    widget.store.addListener(_changed);
    _refresh();
    _timer = Timer.periodic(const Duration(seconds: 2), (_) => _refresh());
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  @override
  void dispose() {
    _stopBatch = true;
    _timer?.cancel();
    _search.dispose();
    widget.store.removeListener(_changed);
    _updater.removeListener(_changed);
    _updater.dispose();
    super.dispose();
  }

  Future<void> _refresh() async {
    if (_refreshing || !_allowed) return;
    _refreshing = true;
    try {
      final jobs = await widget.repository.downloads();
      if (mounted && _allowed) {
        setState(() {
          _jobs = jobs;
          _selected.retainAll(jobs.map((job) => job.id));
          _error = null;
        });
      }
    } catch (error) {
      if (mounted && _allowed) setState(() => _error = error.toString());
    } finally {
      _refreshing = false;
      if (mounted) setState(() => _loading = false);
    }
  }

  void _message(String message) {
    if (mounted) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(message)));
    }
  }

  Future<void> _batch(String command, Iterable<DownloadJob> source) async {
    if (_busy || !_allowed) return;
    final jobs = source
        .where(
          (job) => switch (command) {
            'pause' => job.active,
            'resume' => job.resumable,
            'archive' => job.completed && !job.archived,
            'restore' => job.completed && job.archived,
            'remove' => true,
            _ => false,
          },
        )
        .toList();
    if (jobs.isEmpty) {
      _message('沒有適合此操作的任務');
      return;
    }
    if (command == 'remove') {
      final completed = jobs.where((job) => job.completed).length;
      final accepted = await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          title: Text(completed > 0 ? '刪除所選影片？' : '取消所選下載？'),
          content: Text(
            '共 ${jobs.length} 項${completed > 0 ? '，包含 $completed 個已下載影片' : ''}，檔案也會刪除。\n需要保留影片時，請選擇“清理任務，保留影片”。',
          ),
          actions: [
            TextButton(
              autofocus: AppLayout.isTelevision(context),
              onPressed: () => Navigator.pop(context, false),
              child: const Text('保留'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, true),
              child: const Text('確認刪除'),
            ),
          ],
        ),
      );
      if (accepted != true || !mounted || !_allowed) return;
    }
    setState(() {
      _busy = true;
      _stopBatch = false;
      _retryCommand = null;
    });
    final failures = <String, String>{};
    var completed = 0;
    try {
      for (var offset = 0; offset < jobs.length; offset += 500) {
        if (!mounted || !_allowed || _stopBatch) break;
        setState(
          () => _operation =
              '正在處理 ${math.min(offset + 500, jobs.length)}/${jobs.length} 項',
        );
        final ids = jobs
            .sublist(offset, math.min(offset + 500, jobs.length))
            .map((job) => job.id)
            .toList();
        try {
          final result = await widget.repository.controlDownloadBatch(
            command,
            ids,
          );
          if (!mounted || !_allowed) return;
          completed += result.completed.length;
          _selected.removeAll(result.completed);
          failures.addAll(result.failures);
        } catch (error) {
          failures.addAll({for (final id in ids) id: error.toString()});
        }
      }
      if (!mounted || !_allowed) return;
      if (failures.isNotEmpty) {
        _selected.addAll(failures.keys);
        _selecting = true;
        _retryCommand = command;
        _message(
          '已處理 $completed 項，${failures.length} 項失敗：${failures.values.first}',
        );
      } else {
        _message(
          _stopBatch
              ? '已停止，已處理 $completed 項'
              : command == 'archive'
              ? '已清理 $completed 項任務，影片仍可本地播放'
              : '已處理 $completed 項',
        );
      }
      await _refresh();
    } finally {
      if (mounted) {
        setState(() {
          _busy = false;
          _operation = '';
        });
      }
    }
  }

  Future<void> _update(Drama drama) async {
    await _updater.update(drama);
    if (mounted && _allowed) {
      _message(_updater.error ?? _updater.status);
      await _refresh();
    }
  }

  Future<void> _play(DownloadJob job) async {
    if (_opening || !_allowed) return;
    _opening = true;
    final completed =
        _jobs
            .where((entry) => entry.completed && entry.drama.id == job.drama.id)
            .toList()
          ..sort((a, b) => a.episode.number.compareTo(b.episode.number));
    final index = completed.indexWhere((entry) => entry.id == job.id);
    if (index < 0) {
      _opening = false;
      return;
    }
    final detail = DramaDetail(
      job.drama,
      completed.map((entry) => entry.episode).toList(),
    );
    final watch = widget.store.watched(job.drama.id);
    final position = watch?.episode == job.episode.number && !watch!.finished
        ? watch.position
        : 0.0;
    try {
      await Navigator.of(context).push(
        MaterialPageRoute<void>(
          builder: (_) =>
              widget.playerBuilder?.call(detail, index, position) ??
              PlayerScreen(
                detail: detail,
                initialIndex: index,
                initialPosition: position,
                repository: widget.repository,
                store: widget.store,
                localOnly: true,
              ),
        ),
      );
    } finally {
      _opening = false;
      if (mounted) await _refresh();
    }
  }

  void _select(Iterable<DownloadJob> jobs) {
    final ids = jobs.map((job) => job.id).toSet();
    setState(() {
      _selecting = true;
      if (ids.every(_selected.contains)) {
        _selected.removeAll(ids);
      } else {
        _selected.addAll(ids);
      }
    });
  }

  Future<void> _filters() async {
    var state = _filter, release = _release;
    final result = await showDialog<bool>(
      context: context,
      builder: (context) => StatefulBuilder(
        builder: (context, change) => AlertDialog(
          title: const Text('篩選下載合集'),
          content: SizedBox(
            width: 420,
            child: SingleChildScrollView(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text('任務狀態'),
                  const SizedBox(height: 8),
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: [
                      for (final entry in const {
                        'all': '全部任務',
                        'pending': '未完成',
                        'active': '進行中',
                        'paused': '已暫停',
                        'failed': '失敗',
                        'completed': '已下載',
                        'archived': '已保留影片',
                      }.entries)
                        ChoiceChip(
                          label: Text(entry.value),
                          selected: state == entry.key,
                          onSelected: (_) => change(() => state = entry.key),
                        ),
                    ],
                  ),
                  const SizedBox(height: 20),
                  const Text('劇集狀態'),
                  const SizedBox(height: 8),
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: [
                      for (final entry in const {
                        '': '全部',
                        'ongoing': '連載中',
                        'finished': '已完結',
                      }.entries)
                        ChoiceChip(
                          label: Text(entry.value),
                          selected: release == entry.key,
                          onSelected: (_) => change(() => release = entry.key),
                        ),
                    ],
                  ),
                ],
              ),
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, true),
              child: const Text('應用'),
            ),
          ],
        ),
      ),
    );
    if (result == true && mounted) {
      setState(() {
        _filter = state;
        _release = release;
      });
    }
  }

  List<Widget> _titleActions(List<DownloadCollection> collections) => [
    if (_selecting) ...[
      IconButton(
        tooltip: '全選當前',
        onPressed: _busy
            ? null
            : () => _select(collections.expand((c) => c.jobs)),
        icon: const Icon(Icons.select_all_rounded),
      ),
      IconButton(
        tooltip: '退出多選',
        onPressed: _busy
            ? null
            : () => setState(() {
                _selecting = false;
                _selected.clear();
                _retryCommand = null;
              }),
        icon: const Icon(Icons.close_rounded),
      ),
    ] else ...[
      IconButton(
        tooltip: '篩選下載合集',
        onPressed: _filters,
        icon: Badge(
          isLabelVisible: _filter != 'all' || _release.isNotEmpty,
          child: const Icon(Icons.filter_list_rounded),
        ),
      ),
      IconButton(
        tooltip: '多選下載任務',
        onPressed: () => setState(() => _selecting = true),
        icon: const Icon(Icons.checklist_rounded),
      ),
      IconButton(
        key: const ValueKey('download-local-media'),
        tooltip: '本地媒體與合併',
        onPressed: () => Navigator.push(
          context,
          MaterialPageRoute<void>(
            builder: (_) => LocalMediaScreen(
              repository: widget.repository,
              store: widget.store,
            ),
          ),
        ),
        icon: const Icon(Icons.video_library_outlined),
      ),
    ],
    PopupMenuButton<String>(
      key: const ValueKey('download-queue-actions'),
      tooltip: '佇列操作',
      enabled: !_busy,
      onSelected: (value) {
        if (value == 'refresh') {
          _refresh();
        } else {
          _batch(value, _jobs);
        }
      },
      itemBuilder: (_) => const [
        PopupMenuItem(value: 'pause', child: Text('全部暫停')),
        PopupMenuItem(value: 'resume', child: Text('全部繼續')),
        PopupMenuItem(value: 'archive', child: Text('清理已完成任務，保留影片')),
        PopupMenuItem(value: 'refresh', child: Text('重新整理記錄')),
      ],
    ),
  ];

  Widget _collection(DownloadCollection collection) {
    final count = collection.jobs
        .where((job) => _selected.contains(job.id))
        .length;
    final expanded = _expanded.contains(collection.drama.id);
    return Card(
      child: ListTile(
        leading: _selecting
            ? Checkbox(
                tristate: true,
                value: count == 0
                    ? false
                    : count == collection.jobs.length
                    ? true
                    : null,
                onChanged: _busy ? null : (_) => _select(collection.jobs),
              )
            : Icon(expanded ? Icons.folder_open_rounded : Icons.folder_rounded),
        title: Text(
          collection.drama.title,
          maxLines: 2,
          overflow: TextOverflow.ellipsis,
          style: const TextStyle(fontWeight: FontWeight.w600),
        ),
        subtitle: Text(
          '${collection.completed}/${collection.jobs.length} 集已下載 · ${storageSize(collection.bytes)}'
          '${collection.active > 0 ? ' · ${collection.active} 項進行中' : ''}${collection.failed > 0 ? ' · ${collection.failed} 項失敗' : ''}',
        ),
        onTap: () => setState(() {
          if (!_expanded.remove(collection.drama.id)) {
            _expanded.add(collection.drama.id);
          }
        }),
        trailing: PopupMenuButton<String>(
          tooltip: '${collection.drama.title} · 合集操作',
          enabled: !_busy,
          onSelected: (value) {
            if (value == 'update') {
              _update(collection.drama);
            } else if (value == 'select') {
              _select(collection.jobs);
            } else {
              _batch(value, collection.jobs);
            }
          },
          itemBuilder: (_) => [
            PopupMenuItem(
              value: 'update',
              enabled: !_updater.busy,
              child: const Text('更新本劇 · 補充新增與缺失'),
            ),
            const PopupMenuItem(value: 'select', child: Text('選擇本合集')),
            const PopupMenuItem(value: 'pause', child: Text('暫停本合集')),
            const PopupMenuItem(value: 'resume', child: Text('繼續 / 重試本合集')),
            const PopupMenuItem(value: 'archive', child: Text('清理已完成任務，保留影片')),
            if (_filter == 'archived')
              const PopupMenuItem(value: 'restore', child: Text('恢復到任務列表')),
            const PopupMenuItem(value: 'remove', child: Text('取消任務並刪除影片')),
          ],
        ),
      ),
    );
  }

  Widget _episode(DownloadJob job) => Padding(
    padding: const EdgeInsets.only(left: 12),
    child: Card(
      key: ValueKey('download-task-${job.id}'),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          ListTile(
            leading: _selecting
                ? Checkbox(
                    value: _selected.contains(job.id),
                    onChanged: _busy ? null : (_) => _select([job]),
                  )
                : Icon(
                    job.completed
                        ? Icons.offline_pin_outlined
                        : job.state == 'failed'
                        ? Icons.error_outline
                        : Icons.downloading_rounded,
                  ),
            title: Text(
              '第 ${job.episode.number} 集${job.episode.vip ? ' · VIP' : ''}',
            ),
            subtitle: Text(
              '${job.archived ? '已保留影片' : job.stateLabel} · ${storageSize(job.bytes)}'
              '${job.actualQuality > 0 ? ' · ${job.actualQuality}P' : ''}${job.error.isNotEmpty ? '\n${job.error}' : ''}',
            ),
            onTap: _busy
                ? null
                : _selecting
                ? () => _select([job])
                : job.completed
                ? () => _play(job)
                : null,
            trailing: _selecting
                ? null
                : PopupMenuButton<String>(
                    enabled: !_busy,
                    tooltip: '分集操作',
                    onSelected: (value) {
                      if (value == 'play') {
                        _play(job);
                      } else {
                        _batch(value, [job]);
                      }
                    },
                    itemBuilder: (_) => [
                      if (job.completed)
                        const PopupMenuItem(value: 'play', child: Text('本地播放')),
                      if (job.active)
                        const PopupMenuItem(value: 'pause', child: Text('暫停')),
                      if (job.resumable)
                        const PopupMenuItem(
                          value: 'resume',
                          child: Text('繼續 / 重試'),
                        ),
                      if (job.completed)
                        PopupMenuItem(
                          value: job.archived ? 'restore' : 'archive',
                          child: Text(job.archived ? '恢復任務' : '清理任務，保留影片'),
                        ),
                      PopupMenuItem(
                        value: 'remove',
                        child: Text(job.completed ? '刪除影片' : '取消下載'),
                      ),
                    ],
                  ),
          ),
          if (!job.completed)
            LinearProgressIndicator(
              value: job.progress > 0 || !job.active ? job.progress : null,
              minHeight: 2,
            ),
        ],
      ),
    ),
  );

  Widget _batchBar() => Material(
    color: Theme.of(context).colorScheme.surfaceContainerHighest,
    child: SafeArea(
      top: false,
      child: Padding(
        padding: const EdgeInsets.all(8),
        child: Wrap(
          spacing: 6,
          runSpacing: 4,
          alignment: WrapAlignment.center,
          children: [
            if (_busy)
              TextButton(
                onPressed: () => setState(() => _stopBatch = true),
                child: const Text('停止批次操作'),
              )
            else ...[
              for (final entry in const {
                'pause': '暫停',
                'resume': '繼續 / 重試',
                'archive': '保留影片',
                'remove': '刪除',
              }.entries)
                TextButton(
                  onPressed: _selected.isEmpty
                      ? null
                      : () => _batch(
                          entry.key,
                          _jobs.where((job) => _selected.contains(job.id)),
                        ),
                  child: Text(entry.value),
                ),
              if (_retryCommand != null)
                FilledButton(
                  onPressed: () => _batch(
                    _retryCommand!,
                    _jobs.where((job) => _selected.contains(job.id)),
                  ),
                  child: const Text('重試失敗項'),
                ),
            ],
          ],
        ),
      ),
    ),
  );

  @override
  Widget build(BuildContext context) {
    if (!_allowed) {
      return const StatusPanel(title: '當前使用者無權訪問下載', message: '請返回後重新操作。');
    }
    final collections = _collections;
    final rows = <Object>[
      for (final collection in collections) ...[
        collection,
        if (_expanded.contains(collection.drama.id)) ...collection.jobs,
      ],
    ];
    final title = _selecting ? '已選 ${_selected.length} 項' : '下載合集';
    final actions = _titleActions(collections);
    final body = Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (widget.embedded)
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 4, 4, 0),
            child: Row(
              children: [
                Expanded(
                  child: Text(
                    title,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.titleLarge,
                  ),
                ),
                ...actions,
              ],
            ),
          ),
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
          child: RemoteTextInput(
            child: TextField(
              controller: _search,
              onChanged: (_) => setState(() {}),
              decoration: InputDecoration(
                hintText: '搜尋劇名、站源或分類',
                prefixIcon: const Icon(Icons.search_rounded),
                suffixIcon: _search.text.isEmpty
                    ? null
                    : IconButton(
                        tooltip: '清空搜尋',
                        onPressed: () => setState(_search.clear),
                        icon: const Icon(Icons.close_rounded),
                      ),
              ),
            ),
          ),
        ),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 4),
          child: Text(
            '${collections.length} 部 · ${collections.fold<int>(0, (sum, c) => sum + c.jobs.length)} 集'
            '${_filter == 'archived' ? ' · 已清理任務，影片仍保留' : ''}',
            style: Theme.of(context).textTheme.bodySmall,
          ),
        ),
        if (_updater.busy || _busy) LinearProgressIndicator(minHeight: 2),
        if (_updater.busy || _busy)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: Row(
              children: [
                Expanded(
                  child: Text(
                    _busy ? _operation : _updater.status,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                if (_updater.busy)
                  TextButton(
                    onPressed: _updater.cancel,
                    child: const Text('停止'),
                  ),
              ],
            ),
          ),
        if (_error != null && _jobs.isNotEmpty)
          Padding(padding: const EdgeInsets.all(12), child: Text(_error!)),
        Expanded(
          child: _loading
              ? const Center(child: CircularProgressIndicator())
              : _error != null && _jobs.isEmpty
              ? StatusPanel(
                  title: '無法讀取下載記錄',
                  message: _error!,
                  onRetry: _refresh,
                )
              : rows.isEmpty
              ? StatusPanel(
                  title: _jobs.isEmpty ? '還沒有下載任務' : '暫無符合條件的任務',
                  message: '在劇集詳情選擇下載，或調整篩選檢視已保留的影片。',
                  icon: Icons.download_outlined,
                )
              : ListView.builder(
                  padding: const EdgeInsets.fromLTRB(12, 4, 12, 20),
                  itemCount: rows.length,
                  itemBuilder: (_, index) => switch (rows[index]) {
                    DownloadCollection collection => _collection(collection),
                    DownloadJob job => _episode(job),
                    _ => const SizedBox.shrink(),
                  },
                ),
        ),
        if (_selecting) _batchBar(),
      ],
    );
    if (widget.embedded) return body;
    return CallbackShortcuts(
      bindings: {
        const SingleActivator(LogicalKeyboardKey.escape): () =>
            Navigator.of(context).maybePop(),
        const SingleActivator(LogicalKeyboardKey.goBack): () =>
            Navigator.of(context).maybePop(),
      },
      child: Scaffold(
        appBar: AppBar(title: Text(title), actions: actions),
        body: SafeArea(top: false, child: body),
      ),
    );
  }
}
