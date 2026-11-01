import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';

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
  final _revisions = <String, int>{};
  final _expandedHealth = <String>{};
  final _healthScrollControllers = <String, ScrollController>{};
  final _healthFocusNodes = <String, FocusNode>{};
  Timer? _timer;
  bool _polling = false;
  int _ticks = 0;

  ScrollController _healthScrollController(String id) =>
      _healthScrollControllers.putIfAbsent(id, ScrollController.new);

  FocusNode _healthFocusNode(String id) => _healthFocusNodes.putIfAbsent(
    id,
    () => FocusNode(debugLabel: 'source-health-$id'),
  );

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
    unawaited(_refresh());
    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      _ticks++;
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
            'fileNamePrefix': 'DTV-片源診斷-${source.id}',
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
          '${directory.path}${Platform.pathSeparator}DTV-片源診斷-${source.id}_$stamp.txt',
        );
        await file.writeAsString('$content\n', flush: true);
        savedPath = file.path;
      }
      if (!mounted) return;
      final location = savedPath?.isNotEmpty == true
          ? savedPath!
          : 'Download/D-TV/DTV-片源診斷-${source.id}.txt';
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
      final sourceGroups = SourceGroup.fromSources(sources);
      final viewPaddingBottom = MediaQuery.viewPaddingOf(context).bottom;
      final paddingBottom = MediaQuery.paddingOf(context).bottom;
      final bottomInset = viewPaddingBottom > paddingBottom
          ? viewPaddingBottom
          : paddingBottom;
      return Scaffold(
        appBar: AppBar(title: const Text('片源診斷')),
        body: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 960),
            child: ListView(
              padding: EdgeInsets.fromLTRB(16, 16, 16, 16 + bottomInset),
              children: [
                const Padding(
                  padding: EdgeInsets.only(bottom: 16),
                  child: Text('顯示目前可用片源的狀態與最近一次自動檢查紀錄。'),
                ),
                if (sources.isEmpty)
                  const Padding(
                    padding: EdgeInsets.all(24),
                    child: Text('目前使用者沒有可診斷的片源'),
                  ),
                for (final group in sourceGroups)
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
                          for (final source in group.sources) _sourceCard(source),
                        ],
                      ),
                    )
                  else
                    for (final source in group.sources) _sourceCard(source),
              ],
            ),
          ),
        ),
      );
    },
  );

  Widget _sourceCard(SourceSite source) {
    final status = _statuses[source.id];
    final error = _errors[source.id] ?? status?.error ?? '';
    final health = status?.health;
    final healthExpanded = _expandedHealth.contains(source.id);
    final colors = Theme.of(context).colorScheme;
    return Card(
      key: PageStorageKey('source-${source.id}'),
      margin: const EdgeInsets.only(bottom: 16),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(Icons.dns_outlined),
                const SizedBox(width: 12),
                Expanded(
                  child: Text(
                    uiTraditional(source.name),
                    style: Theme.of(context).textTheme.titleLarge,
                  ),
                ),
                Text('${status?.count ?? 0} 部'),
              ],
            ),
            if (status?.updatedAt != null)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text('目錄資料時間：${sourceTimestamp(status?.updatedAt)}'),
              ),
            if (status?.running == true) ...[
              const SizedBox(height: 12),
              const LinearProgressIndicator(),
              const SizedBox(height: 8),
              Text(uiTraditional(status?.stage ?? '自動檢查中')),
            ],
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
                child: Text(
                  uiTraditional(status.storageError),
                  style: TextStyle(color: colors.error),
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
                        message: healthExpanded ? '收起診斷詳情' : '展開診斷詳情',
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
                                      style: Theme.of(context).textTheme.bodySmall,
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
              if (healthExpanded)
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
                                              style: Theme.of(context)
                                                  .textTheme
                                                  .bodySmall,
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
            ] else ...[
              const SizedBox(height: 12),
              const Text('尚無自動檢查記錄'),
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
