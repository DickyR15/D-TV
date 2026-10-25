import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';

import 'app_build.dart';
import 'app_layout.dart';
import 'core_bridge.dart';
import 'local_store.dart';
import 'models.dart';
import 'source_status.dart';
import 'ui_localization.dart';

String sourceTimestamp(DateTime? value) {
  if (value == null) return '尚無記錄';
  final local = value.toLocal();
  String two(int number) => number.toString().padLeft(2, '0');
  return '${local.month}/${local.day} ${two(local.hour)}:${two(local.minute)}';
}

class SourcesScreen extends StatefulWidget {
  const SourcesScreen({
    super.key,
    required this.repository,
    required this.store,
    this.initialSource,
    this.drama,
  });

  final AppRepository repository;
  final LocalStore store;
  final String? initialSource;
  final Drama? drama;

  @override
  State<SourcesScreen> createState() => _SourcesScreenState();
}

class _SourcesScreenState extends State<SourcesScreen> {
  final _statuses = <String, SourceStatus>{};
  final _errors = <String, String>{};
  final _pending = <String>{};
  final _revisions = <String, int>{};
  final _expandedHealth = <String>{};
  final _healthScrollControllers = <String, ScrollController>{};
  final _healthFocusNodes = <String, FocusNode>{};
  final _updateFocusNodes = <String, FocusNode>{};
  Timer? _timer;
  bool _initialUpdateFocusRequested = false;
  bool _polling = false;
  int _ticks = 0;

  ScrollController _healthScrollController(String id) =>
      _healthScrollControllers.putIfAbsent(id, ScrollController.new);

  FocusNode _healthFocusNode(String id) => _healthFocusNodes.putIfAbsent(
    id,
    () => FocusNode(debugLabel: 'source-health-$id'),
  );

  FocusNode _updateFocusNode(String id) => _updateFocusNodes.putIfAbsent(
    id,
    () => FocusNode(debugLabel: 'source-update-$id'),
  );

  String? _defaultVisibleUpdateSourceId() {
    final sources = widget.store.permittedSources.toList()
      ..sort((a, b) {
        final aFirst = a.id == widget.initialSource ? 0 : 1;
        final bFirst = b.id == widget.initialSource ? 0 : 1;
        return aFirst.compareTo(bFirst);
      });
    for (final group in SourceGroup.fromSources(sources)) {
      if (group.id == 'huangguo' &&
          !group.sources.any((source) => source.id == widget.initialSource)) {
        continue;
      }
      if (group.sources.isNotEmpty) return group.sources.first.id;
    }
    return null;
  }

  void _requestInitialUpdateFocus() {
    if (!mounted ||
        _initialUpdateFocusRequested ||
        !AppLayout.isTelevision(context)) {
      return;
    }
    final sourceId = _defaultVisibleUpdateSourceId();
    if (sourceId == null) return;
    final node = _updateFocusNodes[sourceId];
    if (node == null || !node.canRequestFocus) return;
    node.requestFocus();
    _initialUpdateFocusRequested = true;
  }

  KeyEventResult _scrollHealthWithRemote(
    String id,
    KeyEvent event,
  ) {
    if (event is! KeyDownEvent && event is! KeyRepeatEvent) {
      return KeyEventResult.ignored;
    }
    final key = event.logicalKey;
    if (key != LogicalKeyboardKey.arrowUp &&
        key != LogicalKeyboardKey.arrowDown) {
      return KeyEventResult.ignored;
    }
    final controller = _healthScrollController(id);
    if (!controller.hasClients) return KeyEventResult.handled;
    final position = controller.position;
    final down = key == LogicalKeyboardKey.arrowDown;
    final canScroll = down
        ? position.pixels < position.maxScrollExtent - 1
        : position.pixels > position.minScrollExtent + 1;
    if (canScroll) {
      final target = (position.pixels + (down ? 160 : -160))
          .clamp(position.minScrollExtent, position.maxScrollExtent)
          .toDouble();
      controller.jumpTo(target);
    } else {
      if (down) {
        FocusScope.of(context).nextFocus();
      } else {
        FocusScope.of(context).previousFocus();
      }
    }
    return KeyEventResult.handled;
  }

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _requestInitialUpdateFocus();
    });
    unawaited(_refresh());
    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      _ticks++;
      if (_statuses.values.any((status) => status.retryAt != null)) {
        setState(() {});
      }
      if (_ticks % 2 == 0 &&
          (_statuses.values.any((status) => status.running) ||
              _ticks % 10 == 0)) {
        unawaited(_refresh());
      }
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    for (final controller in _healthScrollControllers.values) {
      controller.dispose();
    }
    for (final node in _healthFocusNodes.values) {
      node.dispose();
    }
    for (final node in _updateFocusNodes.values) {
      node.dispose();
    }
    super.dispose();
  }

  Future<void> _refresh() async {
    if (_polling) return;
    _polling = true;
    final epoch = widget.store.profileEpoch;
    try {
      await Future.wait([
        for (final source in widget.store.sources)
          (() async {
            final revision = _revisions[source.id] ?? 0;
            try {
              final status = await widget.repository.sourceStatus(source.id);
              if (!mounted ||
                  epoch != widget.store.profileEpoch ||
                  revision != (_revisions[source.id] ?? 0)) {
                return;
              }
              setState(() {
                _statuses[source.id] = status;
                _errors.remove(source.id);
              });
            } catch (error) {
              if (mounted &&
                  epoch == widget.store.profileEpoch &&
                  revision == (_revisions[source.id] ?? 0)) {
                setState(() => _errors[source.id] = error.toString());
              }
            }
          })(),
      ]);
    } finally {
      _polling = false;
    }
  }

  Future<void> _run(SourceSite source, String operation) async {
    if (_pending.contains(source.id)) return;
    final epoch = widget.store.profileEpoch;
    setState(() {
      _pending.add(source.id);
      _errors.remove(source.id);
      _revisions[source.id] = (_revisions[source.id] ?? 0) + 1;
      if (operation == 'check' || operation == 'checkCatalog') {
        _expandedHealth.add(source.id);
      }
    });
    try {
      final status = operation == 'cancel'
          ? await widget.repository.cancelSourceJob(source.id)
          : await widget.repository.startSourceJob(
              source.id,
              operation,
              drama: source.id == widget.drama?.source ? widget.drama : null,
            );
      if (mounted && epoch == widget.store.profileEpoch) {
        setState(() => _statuses[source.id] = status);
      }
    } catch (error) {
      if (mounted && epoch == widget.store.profileEpoch) {
        setState(() => _errors[source.id] = error.toString());
      }
    } finally {
      if (mounted) setState(() => _pending.remove(source.id));
    }
  }

  Future<void> _toggle(SourceSite source, bool enabled) async {
    setState(() => _errors.remove(source.id));
    try {
      await widget.store.setSourceEnabled(source.id, enabled);
      if (enabled) unawaited(_refresh());
    } catch (error) {
      if (mounted) setState(() => _errors[source.id] = error.toString());
    }
  }

  String _diagnosticText(SourceSite source, SourceStatus status) {
    final text = StringBuffer('${uiTraditional(source.name)}\n');
    text.writeln(
      '快取 ${status.count} 部，更新 ${sourceTimestamp(status.updatedAt)}',
    );
    final health = status.health;
    if (health != null) {
      text.writeln(
        '${uiTraditional(health.label)} · ${sourceTimestamp(health.checkedAt)}',
      );
      if (health.sample.isNotEmpty) text.writeln('檢測劇集：${health.sample}');
      for (final step in health.steps) {
        text.writeln(
          '${uiTraditional(step.name)}：${uiTraditional(step.message)}',
        );
        text.writeln(
          '${step.host} HTTP ${step.httpStatus} · ${step.elapsedMs} ms',
        );
        if (step.cfRay.isNotEmpty) text.writeln('CF Ray: ${step.cfRay}');
      }
    }
    if (status.error.isNotEmpty) text.writeln(status.error);
    if (status.storageError.isNotEmpty) text.writeln(status.storageError);
    return text.toString().trim();
  }

  Future<void> _copy(SourceSite source, SourceStatus status) async {
    await Clipboard.setData(
      ClipboardData(text: _diagnosticText(source, status)),
    );
    if (mounted) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('診斷資訊已複製')));
    }
  }

  Future<void> _export(SourceSite source, SourceStatus status) async {
    final content = _diagnosticText(source, status);
    if (content.isEmpty) return;
    try {
      String? savedPath;
      if (Platform.isAndroid) {
        final result = await const MethodChannel('duanju/device')
            .invokeMapMethod<String, dynamic>(
          'exportDiagnosticText',
          <String, dynamic>{
            'text': '$content\n',
            'fileNamePrefix': 'D-TV-source-diagnostic-${source.id}',
          },
        );
        savedPath = result?['path'] as String?;
      } else {
        final directory = await getApplicationDocumentsDirectory();
        final stamp = DateTime.now()
            .toIso8601String()
            .replaceAll(':', '-')
            .split('.')
            .first;
        final file = File(
          '${directory.path}${Platform.pathSeparator}D-TV-source-diagnostic-${source.id}_$stamp.txt',
        );
        await file.writeAsString('$content\n', flush: true);
        savedPath = file.path;
      }
      if (!mounted) return;
      final location = savedPath?.isNotEmpty == true
          ? savedPath!
          : 'Download/D-TV/D-TV-source-diagnostic-${source.id}.txt';
      await showDialog<void>(
        context: context,
        builder: (dialogContext) => AlertDialog(
          title: const Text('診斷資訊匯出成功'),
          content: SelectableText(
            '已儲存片源診斷資訊：\n$location\n\n可從檔案管理器開啟 Downloads/D-TV 資料夾，將 TXT 檔傳到電腦後上傳分析。',
          ),
          actions: [
            TextButton(
              onPressed: () {
                Clipboard.setData(ClipboardData(text: location));
                Navigator.of(dialogContext).pop();
              },
              child: const Text('複製檔案位置'),
            ),
            FilledButton(
              onPressed: () => Navigator.of(dialogContext).pop(),
              child: const Text('完成'),
            ),
          ],
        ),
      );
    } catch (error) {
      if (!mounted) return;
      await showDialog<void>(
        context: context,
        builder: (dialogContext) => AlertDialog(
          title: const Text('匯出診斷資訊失敗'),
          content: SelectableText(uiTraditional(error.toString())),
          actions: [
            FilledButton(
              onPressed: () => Navigator.of(dialogContext).pop(),
              child: const Text('關閉'),
            ),
          ],
        ),
      );
    }
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.store,
    builder: (context, _) {
      final sources = widget.store.permittedSources
        ..sort((a, b) {
          final aFirst = a.id == widget.initialSource ? 0 : 1;
          final bFirst = b.id == widget.initialSource ? 0 : 1;
          return aFirst.compareTo(bFirst);
        });
      final viewPaddingBottom = MediaQuery.viewPaddingOf(context).bottom;
      final paddingBottom = MediaQuery.paddingOf(context).bottom;
      final bottomInset = viewPaddingBottom > paddingBottom
          ? viewPaddingBottom
          : paddingBottom;
      return Scaffold(
        appBar: AppBar(title: const Text('片源管理')),
        body: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 960),
            child: RefreshIndicator(
              onRefresh: _refresh,
              child: ListView(
                padding: EdgeInsets.fromLTRB(16, 16, 16, 16 + bottomInset),
                children: [
                  const Padding(
                    padding: EdgeInsets.only(bottom: 16),
                    child: Text(
                      '各片源可分別更新與檢測。更新會查詢新劇、續載一頁歷史內容，並分批補齊資料；紅果會自動接續可處理的批次。離開此頁後工作仍會繼續。',
                    ),
                  ),
                  if (sources.isEmpty)
                    const Padding(
                      padding: EdgeInsets.all(24),
                      child: Text('目前使用者沒有可用片源'),
                    ),
                  for (final group in SourceGroup.fromSources(sources))
                    if (group.id == 'huangguo')
                      Card(
                        clipBehavior: Clip.antiAlias,
                        child: ExpansionTile(
                          key: const PageStorageKey('source-group-huangguo'),
                          initiallyExpanded: group.sources.any(
                            (source) => source.id == widget.initialSource,
                          ),
                          leading: const Icon(Icons.hub_outlined),
                          title: const Text('黃果'),
                          subtitle: Text(
                            '${group.sources.length} 個入口 · ${group.sources.fold<int>(0, (count, source) => count + (_statuses[source.id]?.count ?? 0))} 部',
                          ),
                          childrenPadding: const EdgeInsets.all(8),
                          children: [
                            for (final source in group.sources)
                              _sourceCard(source),
                          ],
                        ),
                      )
                    else
                      for (final source in group.sources) _sourceCard(source),
                ],
              ),
            ),
          ),
        ),
      );
    },
  );

  Widget _sourceCard(SourceSite source) {
    final status = _statuses[source.id];
    final pending = _pending.contains(source.id);
    final busy = pending || status?.running == true;
    final seconds = status?.retrySeconds ?? 0;
    final enabled =
        !busy && seconds == 0 && widget.repository.supportsSourceManagement;
    final error = _errors[source.id] ?? status?.error ?? '';
    final health = status?.health;
    final healthExpanded = _expandedHealth.contains(source.id);
    final colors = Theme.of(context).colorScheme;
    final switchedOn = widget.store.sourceEnabled(source.id);
    final admin = widget.store.profile.admin;
    final header = Row(
      children: [
        const Icon(Icons.dns_outlined),
        const SizedBox(width: 12),
        Expanded(
          child: Text(
            uiTraditional(source.name),
            style: Theme.of(context).textTheme.titleLarge,
          ),
        ),
        if (switchedOn) Text('${status?.count ?? 0} 部'),
        if (allSourcesEnabled) ...[
          const SizedBox(width: 8),
          Switch(
            key: ValueKey('switch-${source.id}'),
            value: switchedOn,
            onChanged: admin ? (value) => _toggle(source, value) : null,
          ),
        ],
      ],
    );
    if (!switchedOn) {
      final closedError = _errors[source.id] ?? '';
      return Card(
        key: PageStorageKey('source-${source.id}'),
        margin: const EdgeInsets.only(bottom: 16),
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              header,
              const SizedBox(height: 8),
              Text(admin ? '已關閉，開啟後在首頁、搜尋和記錄中顯示' : '已關閉，僅管理員可以開啟'),
              if (closedError.isNotEmpty)
                Padding(
                  padding: const EdgeInsets.only(top: 8),
                  child: Text(
                    uiTraditional(closedError),
                    style: TextStyle(color: colors.error),
                  ),
                ),
            ],
          ),
        ),
      );
    }
    return Card(
      key: PageStorageKey('source-${source.id}'),
      margin: const EdgeInsets.only(bottom: 16),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            header,
            const SizedBox(height: 8),
            Text('最近更新：${sourceTimestamp(status?.updatedAt)}'),
            if (status != null && status.count > 0)
              Text(
                '已載入至第 ${status.page} 頁${status.hasMore ? ' · 可繼續載入' : ' · 目前分頁已載入完成'}',
              ),
            const SizedBox(height: 12),
            Wrap(
              spacing: 10,
              runSpacing: 8,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                FilledButton.icon(
                  key: ValueKey('update-${source.id}'),
                  focusNode: _updateFocusNode(source.id),
                  // Match the highly visible focus treatment of the adjacent
                  // outlined diagnostic button. When focused, invert the
                  // filled button to a dark surface with a thick cyan outline.
                  style: ButtonStyle(
                    backgroundColor:
                        WidgetStateProperty.resolveWith<Color?>((states) {
                          if (states.contains(WidgetState.focused) &&
                              !states.contains(WidgetState.disabled)) {
                            return Theme.of(context).colorScheme.surface;
                          }
                          return null;
                        }),
                    foregroundColor:
                        WidgetStateProperty.resolveWith<Color?>((states) {
                          if (states.contains(WidgetState.focused) &&
                              !states.contains(WidgetState.disabled)) {
                            return Theme.of(context).colorScheme.primary;
                          }
                          return null;
                        }),
                    side: WidgetStateProperty.resolveWith<BorderSide?>((states) {
                      if (states.contains(WidgetState.focused) &&
                          !states.contains(WidgetState.disabled)) {
                        return BorderSide(
                          color: Theme.of(context).colorScheme.primary,
                          width: 3,
                        );
                      }
                      return null;
                    }),
                    overlayColor:
                        WidgetStateProperty.resolveWith<Color?>((states) {
                          if (states.contains(WidgetState.focused) &&
                              !states.contains(WidgetState.disabled)) {
                            return Theme.of(context)
                                .colorScheme
                                .primary
                                .withValues(alpha: 0.12);
                          }
                          return null;
                        }),
                  ),
                  onPressed: enabled ? () => _run(source, 'update') : null,
                  icon: const Icon(Icons.sync_rounded),
                  label: const Text('更新'),
                ),
                OutlinedButton.icon(
                  key: ValueKey('check-${source.id}'),
                  onPressed: enabled ? () => _run(source, 'check') : null,
                  icon: const Icon(Icons.network_check),
                  label: const Text('檢測連線與播放'),
                ),
                if (status?.running == true)
                  TextButton(
                    onPressed: pending ? null : () => _run(source, 'cancel'),
                    child: const Text('停止'),
                  ),
                PopupMenuButton<String>(
                  tooltip: '${uiTraditional(source.name)}更多操作',
                  enabled: enabled,
                  onSelected: (operation) => _run(source, operation),
                  itemBuilder: (_) => [
                    PopupMenuItem(
                      value: 'more',
                      enabled: status?.hasMore ?? true,
                      child: const Text('繼續載入一頁'),
                    ),
                    const PopupMenuItem(value: 'metadata', child: Text('補齊資料')),
                    if (source.id == 'huangdou')
                      PopupMenuItem(
                        value: 'vipMetadata',
                        enabled: (status?.unknownVip ?? 0) > 0,
                        child: Text('補齊 VIP 資料（${status?.unknownVip ?? 0} 部）'),
                      ),
                    const PopupMenuItem(
                      value: 'checkCatalog',
                      child: Text('僅檢測目錄'),
                    ),
                  ],
                ),
              ],
            ),
            if (busy) ...[
              const SizedBox(height: 12),
              LinearProgressIndicator(
                value: status != null && status.total > 0
                    ? (status.completed / status.total).clamp(0, 1)
                    : null,
              ),
              const SizedBox(height: 8),
              Text(
                '${uiTraditional(status?.stage ?? '準備中')}${(status?.total ?? 0) > 0 ? ' · ${status!.completed}/${status.total}' : ''}',
              ),
            ] else if (status != null && status.stage.isNotEmpty) ...[
              const SizedBox(height: 12),
              Text(
                '${uiTraditional(status.stage)}'
                '${status.added > 0 ? ' · 新增 ${status.added} 部' : ''}'
                '${source.id == 'hongguo' && (status.operation == 'update' || status.operation == 'metadata') && status.total > 0 ? ' · 已處理 ${status.completed}/${status.total} 部' : ''}',
              ),
            ],
            if (seconds > 0)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(
                  '請在 $seconds 秒後重試',
                  style: TextStyle(color: colors.error),
                ),
              ),
            if (error.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: SelectableText(
                  uiTraditional(error),
                  style: TextStyle(color: colors.error),
                ),
              ),
            if (status != null && status.storageError.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      uiTraditional(status.storageError),
                      style: TextStyle(color: colors.error),
                    ),
                    TextButton.icon(
                      key: ValueKey('save-${source.id}'),
                      onPressed:
                          !busy && widget.repository.supportsSourceManagement
                          ? () => _run(source, 'retrySave')
                          : null,
                      icon: const Icon(Icons.save_outlined),
                      label: const Text('重試儲存'),
                    ),
                  ],
                ),
              ),
            if (health != null) ...[
              const Divider(height: 28),
              Row(
                children: [
                  Expanded(
                    child: Semantics(
                      expanded: healthExpanded,
                      child: Tooltip(
                        message: healthExpanded ? '收起檢測詳情' : '展開檢測詳情',
                        child: TextButton(
                          key: ValueKey('health-toggle-${source.id}'),
                          onPressed: () => setState(() {
                            if (healthExpanded) {
                              _expandedHealth.remove(source.id);
                            } else {
                              _expandedHealth.add(source.id);
                            }
                          }),
                          style: TextButton.styleFrom(
                            foregroundColor: colors.onSurface,
                            padding: const EdgeInsets.symmetric(
                              horizontal: 4,
                              vertical: 8,
                            ),
                          ),
                          child: Row(
                            children: [
                              Expanded(
                                child: Column(
                                  crossAxisAlignment: CrossAxisAlignment.start,
                                  children: [
                                    Text(uiTraditional(health.label)),
                                    Text(
                                      sourceTimestamp(health.checkedAt),
                                      style: Theme.of(
                                        context,
                                      ).textTheme.bodySmall,
                                    ),
                                  ],
                                ),
                              ),
                              const SizedBox(width: 8),
                              Icon(
                                healthExpanded
                                    ? Icons.expand_less_rounded
                                    : Icons.expand_more_rounded,
                              ),
                            ],
                          ),
                        ),
                      ),
                    ),
                  ),
                  IconButton(
                    tooltip: '複製診斷資訊',
                    onPressed: () => _copy(source, status!),
                    icon: const Icon(Icons.copy_rounded),
                  ),
                  IconButton(
                    tooltip: '匯出診斷 TXT',
                    onPressed: () => _export(source, status!),
                    icon: const Icon(Icons.file_download_outlined),
                  ),
                ],
              ),
              if (healthExpanded) ...[
                Focus(
                  focusNode: _healthFocusNode(source.id),
                  onKeyEvent: (node, event) =>
                      _scrollHealthWithRemote(source.id, event),
                  child: ConstrainedBox(
                    constraints: BoxConstraints(
                      maxHeight: MediaQuery.sizeOf(context).height * 0.46,
                    ),
                    child: Scrollbar(
                      controller: _healthScrollController(source.id),
                      thumbVisibility: true,
                      child: SingleChildScrollView(
                        controller: _healthScrollController(source.id),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            if (health.sample.isNotEmpty)
                              Text('檢測劇集：${uiTraditional(health.sample)}'),
                            for (final step in health.steps)
                              Padding(
                                padding: const EdgeInsets.only(top: 10),
                                child: Row(
                                  crossAxisAlignment: CrossAxisAlignment.start,
                                  children: [
                                    Icon(
                                      step.state == 'ok'
                                          ? Icons.check_circle_outline
                                          : Icons.error_outline,
                                      size: 20,
                                      color: step.state == 'ok'
                                          ? colors.primary
                                          : colors.error,
                                    ),
                                    const SizedBox(width: 8),
                                    Expanded(
                                      child: Column(
                                        crossAxisAlignment:
                                            CrossAxisAlignment.start,
                                        children: [
                                          Text(
                                            '${uiTraditional(step.name)}：${uiTraditional(step.message)}',
                                          ),
                                          if (step.host.isNotEmpty ||
                                              step.httpStatus > 0)
                                            Text(
                                              '${step.host}${step.httpStatus > 0 ? ' · HTTP ${step.httpStatus}' : ''} · ${step.elapsedMs} ms',
                                              style: Theme.of(
                                                context,
                                              ).textTheme.bodySmall,
                                            ),
                                        ],
                                      ),
                                    ),
                                  ],
                                ),
                              ),
                          ],
                        ),
                      ),
                    ),
                  ),
                ),
              ],
            ] else ...[
              const SizedBox(height: 12),
              const Text('尚未檢測連線'),
            ],
          ],
        ),
      ),
    );
  }
}

class SourceDiagnosticsButton extends StatelessWidget {
  const SourceDiagnosticsButton({
    super.key,
    required this.repository,
    required this.store,
    required this.drama,
  });
  final AppRepository repository;
  final LocalStore store;
  final Drama drama;

  @override
  Widget build(BuildContext context) => TextButton.icon(
    onPressed: () => Navigator.push<void>(
      context,
      MaterialPageRoute(
        builder: (_) => SourcesScreen(
          repository: repository,
          store: store,
          initialSource: drama.source,
          drama: drama,
        ),
      ),
    ),
    icon: const Icon(Icons.network_check),
    label: const Text('片源診斷'),
  );
}
