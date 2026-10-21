import 'dart:async';
import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'lan_controller.dart';
import 'local_store.dart';
import 'widgets.dart';

IconData lanDeviceIcon(String kind) => switch (kind) {
  'tv' => Icons.tv_rounded,
  'computer' => Icons.computer_rounded,
  _ => Icons.smartphone_rounded,
};

Future<T?> _lanPanel<T>(
  BuildContext context,
  Widget child, {
  double maxHeight = 740,
  bool dismissible = true,
}) {
  final size = MediaQuery.sizeOf(context);
  final height = min(size.height * .88, maxHeight);
  if (size.width >= 800) {
    return showDialog<T>(
      context: context,
      barrierDismissible: dismissible,
      builder: (_) => Dialog(
        clipBehavior: Clip.antiAlias,
        child: SizedBox(width: 620, height: height, child: child),
      ),
    );
  }
  return showModalBottomSheet<T>(
    context: context,
    useSafeArea: true,
    isScrollControlled: true,
    isDismissible: dismissible,
    enableDrag: dismissible,
    showDragHandle: true,
    constraints: const BoxConstraints(maxWidth: 720),
    builder: (context) {
      final keyboard = MediaQuery.viewInsetsOf(context).bottom;
      final available = max(
        0.0,
        MediaQuery.sizeOf(context).height -
            keyboard -
            MediaQuery.paddingOf(context).top -
            24,
      );
      return Padding(
        padding: EdgeInsets.only(bottom: keyboard),
        child: SizedBox(height: min(height, available), child: child),
      );
    },
  );
}

Future<LanConnection?> chooseLanDevice(
  BuildContext context,
  LanController controller, {
  bool change = false,
}) async {
  if (!change && controller.connection != null) return controller.connection;
  return _lanPanel<LanConnection>(
    context,
    _LanDevices(controller: controller, change: change),
  );
}

void openLanSync(BuildContext context) {
  final controller = LanController.current;
  if (controller == null) {
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(const SnackBar(content: Text('裝置互聯尚未就緒')));
    return;
  }
  Navigator.push(
    context,
    MaterialPageRoute<void>(
      builder: (_) => LanSyncScreen(controller: controller),
    ),
  );
}

class LanSyncScreen extends StatefulWidget {
  const LanSyncScreen({super.key, required this.controller});
  final LanController controller;
  @override
  State<LanSyncScreen> createState() => _LanSyncScreenState();
}

class _LanSyncScreenState extends State<LanSyncScreen> {
  bool _busy = false;
  bool _cancelling = false;
  String? _error;
  LanController get link => widget.controller;

  Future<void> _perform(Future<void> Function() action) async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await action();
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _rename() async {
    final epoch = link.store.profileEpoch;
    final name = TextEditingController(text: link.deviceName);
    final value = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('裝置名稱'),
        content: TextField(
          controller: name,
          maxLength: 60,
          autofocus: true,
          decoration: const InputDecoration(labelText: '在其他裝置上顯示的名稱'),
          onSubmitted: (_) => Navigator.pop(context, name.text),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, name.text),
            child: const Text('儲存'),
          ),
        ],
      ),
    );
    name.dispose();
    if (mounted &&
        value != null &&
        epoch == link.store.profileEpoch &&
        !link.store.locked) {
      await _perform(() => link.rename(value));
    }
  }

  Future<void> _cancel() async {
    if (_cancelling) return;
    setState(() => _cancelling = true);
    try {
      await link.cancelSync();
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _cancelling = false);
    }
  }

  Future<void> _manual() async {
    final epoch = link.store.profileEpoch;
    await _perform(() async {
      await link.beginManual();
      try {
        if (mounted && epoch == link.store.profileEpoch && !link.store.locked) {
          await _lanPanel<void>(
            context,
            _LanManual(controller: link),
            dismissible: false,
          );
        }
      } finally {
        if (epoch == link.store.profileEpoch) link.endManual();
      }
    });
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: link,
    builder: (context, _) {
      final remote = link.connection;
      int conflicts = 0;
      try {
        conflicts = link.conflictCount;
      } catch (_) {}
      final controlsEnabled =
          !_busy && !_cancelling && !link.syncing && !link.store.locked;
      return Scaffold(
        appBar: AppBar(title: const Text('追劇同步')),
        body: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 720),
            child: ListView(
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 32),
              children: [
                Card(
                  child: Column(
                    children: [
                      SwitchListTile.adaptive(
                        key: const ValueKey('lan-enabled'),
                        title: const Text('裝置互聯'),
                        subtitle: const Text('同一區域網直連，接收端無需點選確認'),
                        value: link.enabled,
                        onChanged: _busy || link.store.locked
                            ? null
                            : (value) => _perform(() => link.setEnabled(value)),
                      ),
                      ListTile(
                        leading: Icon(lanDeviceIcon(link.kind)),
                        title: Text(link.deviceName),
                        subtitle: Text('本機使用者 · ${link.store.profile.name}'),
                        trailing: IconButton(
                          tooltip: '修改裝置名稱',
                          onPressed: _busy ? null : _rename,
                          icon: const Icon(Icons.edit_outlined),
                        ),
                      ),
                    ],
                  ),
                ),
                const SizedBox(height: 12),
                Card(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      ListTile(
                        leading: Icon(
                          remote == null
                              ? Icons.devices_rounded
                              : lanDeviceIcon(remote.peer.kind),
                        ),
                        title: Text(
                          remote?.peer.name ?? link.connectionLabel,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                        ),
                        subtitle: Text(
                          remote == null
                              ? '僅發現一臺裝置時自動連線，多臺時由發起端選擇'
                              : '對方使用者 · ${remote.user}',
                        ),
                        trailing: remote == null
                            ? null
                            : PopupMenuButton<String>(
                                tooltip: '裝置操作',
                                onSelected: (value) => _perform(
                                  () => value == 'forget'
                                      ? link.forget(remote.peer)
                                      : link.disconnect(),
                                ),
                                itemBuilder: (_) => const [
                                  PopupMenuItem(
                                    value: 'disconnect',
                                    child: Text('斷開連線'),
                                  ),
                                  PopupMenuItem(
                                    value: 'forget',
                                    child: Text('忘記配對'),
                                  ),
                                ],
                              ),
                      ),
                      Padding(
                        padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
                        child: OutlinedButton.icon(
                          icon: const Icon(Icons.devices_other_rounded),
                          label: Text(remote == null ? '選擇裝置' : '切換裝置'),
                          onPressed: _busy || link.store.locked
                              ? null
                              : () => chooseLanDevice(
                                  context,
                                  link,
                                  change: remote != null,
                                ),
                        ),
                      ),
                    ],
                  ),
                ),
                const SizedBox(height: 12),
                Card(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      SwitchListTile.adaptive(
                        key: const ValueKey('lan-auto-sync'),
                        title: const Text('自動同步'),
                        subtitle: const Text('同步追劇、觀看狀態和這些劇目的續播進度'),
                        value: link.autoSync,
                        onChanged: controlsEnabled
                            ? (value) => _perform(() => link.setAutoSync(value))
                            : null,
                      ),
                      Padding(
                        padding: const EdgeInsets.symmetric(
                          horizontal: 16,
                          vertical: 8,
                        ),
                        child: Semantics(
                          liveRegion: true,
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              if (link.syncing) const LinearProgressIndicator(),
                              const SizedBox(height: 8),
                              Text(link.syncMessage),
                              if (link.lastSync != null)
                                Text(
                                  '最近同步 · ${MaterialLocalizations.of(context).formatTimeOfDay(TimeOfDay.fromDateTime(link.lastSync!))}',
                                  style: Theme.of(context).textTheme.bodySmall,
                                ),
                              if (link.lastLocalCount != null)
                                Text(
                                  '本機：${link.lastLocalCount!.label}',
                                  style: Theme.of(context).textTheme.bodySmall,
                                ),
                              if (link.lastRemoteCount != null)
                                Text(
                                  '對方：${link.lastRemoteCount!.label}',
                                  style: Theme.of(context).textTheme.bodySmall,
                                ),
                              if (link.skipped > 0)
                                Text(
                                  '保留範圍外記錄：${link.skipped}',
                                  style: Theme.of(context).textTheme.bodySmall,
                                ),
                            ],
                          ),
                        ),
                      ),
                      Padding(
                        padding: const EdgeInsets.all(16),
                        child: FilledButton.icon(
                          icon: Icon(
                            link.syncing
                                ? Icons.close_rounded
                                : Icons.sync_rounded,
                          ),
                          label: Text(link.syncing ? '取消本次同步' : '立即同步'),
                          onPressed: link.syncing
                              ? _cancelling
                                    ? null
                                    : _cancel
                              : remote == null || _busy || _cancelling
                              ? null
                              : () => _perform(() => link.synchronize()),
                        ),
                      ),
                    ],
                  ),
                ),
                if (conflicts > 0)
                  Card(
                    child: ListTile(
                      leading: const Icon(Icons.compare_arrows_rounded),
                      title: Text('$conflicts 項記錄待處理'),
                      subtitle: const Text('兩端同時修改的內容已保留，選擇要繼續使用的記錄'),
                      trailing: const Icon(Icons.chevron_right_rounded),
                      onTap: () => Navigator.push(
                        context,
                        MaterialPageRoute<void>(
                          builder: (_) => _LanConflicts(controller: link),
                        ),
                      ),
                    ),
                  ),
                ListTile(
                  leading: const Icon(Icons.sync_alt_rounded),
                  title: const Text('手動同步'),
                  subtitle: const Text('雙向合併、覆蓋對方或覆蓋本機'),
                  trailing: const Icon(Icons.chevron_right_rounded),
                  enabled: remote != null && controlsEnabled,
                  onTap: remote != null && controlsEnabled ? _manual : null,
                ),
                if (_error ?? link.error case final message?)
                  Padding(
                    padding: const EdgeInsets.all(16),
                    child: Text(
                      message,
                      style: TextStyle(
                        color: Theme.of(context).colorScheme.error,
                      ),
                    ),
                  ),
                if (link.discoveryMessage.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.all(16),
                    child: Text(link.discoveryMessage),
                  ),
                if (link.local != null)
                  ExpansionTile(
                    leading: const Icon(Icons.info_outline_rounded),
                    title: const Text('本機地址與連線幫助'),
                    childrenPadding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
                    children: [
                      const Text(
                        '兩臺裝置都需開啟裝置互聯並保持前臺。發現失敗時，可在另一端輸入下列地址；Windows 需允許本應用透過專用網路防火牆。',
                      ),
                      for (final address in link.local!.addresses)
                        ListTile(
                          title: SelectableText(address),
                          trailing: IconButton(
                            tooltip: '複製地址',
                            icon: const Icon(Icons.copy_rounded),
                            onPressed: () =>
                                Clipboard.setData(ClipboardData(text: address)),
                          ),
                        ),
                      if (link.local!.addresses.isEmpty)
                        const Text('尚未取得區域網地址，請檢查 Wi-Fi 或網線連線。'),
                    ],
                  ),
              ],
            ),
          ),
        ),
      );
    },
  );
}

class _LanDevices extends StatefulWidget {
  const _LanDevices({required this.controller, required this.change});
  final LanController controller;
  final bool change;
  @override
  State<_LanDevices> createState() => _LanDevicesState();
}

class _LanDevicesState extends State<_LanDevices> {
  final _address = TextEditingController();
  bool _busy = false;
  bool _manual = false;
  bool _selected = false;
  LanPeer? _target;
  bool _closing = false;
  String? _error;
  late final int _epoch;
  LanController get link => widget.controller;

  @override
  void initState() {
    super.initState();
    _epoch = link.store.profileEpoch;
    link.addListener(_changed);
    unawaited(_start());
  }

  Future<void> _start() async {
    try {
      await link.ensureEnabled();
      if (widget.change) await link.rescan();
      _changed();
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    }
  }

  void _changed() {
    if (!mounted) return;
    if (link.store.profileEpoch != _epoch || link.store.locked) {
      if (!_closing) {
        _closing = true;
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted) Navigator.pop(context);
        });
      }
      return;
    }
    final remote = link.connection;
    if (remote != null &&
        (_selected
            ? remote.peer.id == _target?.id && remote.peer.pin == _target?.pin
            : !widget.change) &&
        !_closing) {
      _closing = true;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) Navigator.pop(context, remote);
      });
    }
    setState(() {});
  }

  Future<void> _connect(LanPeer? peer) async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _error = null;
      _selected = true;
      _target = peer;
    });
    try {
      if (_epoch != link.store.profileEpoch || link.store.locked) {
        throw StateError('當前使用者已變更');
      }
      final selected = peer ?? await link.probe(_address.text);
      if (!mounted || _epoch != link.store.profileEpoch || link.store.locked) {
        return;
      }
      _target = selected;
      await link.connect(selected);
      _changed();
    } catch (error) {
      if (mounted) {
        setState(() {
          _error = error.toString();
          _selected = false;
          _target = null;
        });
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _forget(LanPeer peer) async {
    if (_busy || _epoch != link.store.profileEpoch || link.store.locked) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await link.forget(peer);
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  void dispose() {
    link.removeListener(_changed);
    _address.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final peers = [...link.peers];
    final remembered = link.remembered;
    if (remembered != null && !peers.any((peer) => peer.id == remembered.id)) {
      peers.add(remembered);
    }
    return SafeArea(
      top: false,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 8, 12, 8),
            child: Row(
              children: [
                Expanded(
                  child: Text(
                    widget.change ? '切換裝置' : '連線裝置',
                    style: Theme.of(context).textTheme.titleLarge,
                  ),
                ),
                IconButton(
                  tooltip: '重新搜尋',
                  onPressed: _busy ? null : link.rescan,
                  icon: const Icon(Icons.refresh_rounded),
                ),
                IconButton(
                  tooltip: '關閉',
                  onPressed: () => Navigator.pop(context),
                  icon: const Icon(Icons.close_rounded),
                ),
              ],
            ),
          ),
          if (link.connecting || _busy) const LinearProgressIndicator(),
          Expanded(
            child: ListView(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              children: [
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 8),
                  child: Text('只發現一臺裝置時自動連線；多臺時選擇接收裝置，對方無需確認。'),
                ),
                if (peers.isEmpty)
                  Padding(
                    padding: const EdgeInsets.symmetric(vertical: 28),
                    child: Column(
                      children: [
                        const Icon(Icons.wifi_find_rounded, size: 48),
                        const SizedBox(height: 12),
                        Text(link.receiving ? '正在發現同一網路中的裝置' : '正在開啟裝置互聯'),
                        const SizedBox(height: 8),
                        const Text(
                          '請讓另一臺裝置也開啟裝置互聯並保持應用在前臺。',
                          textAlign: TextAlign.center,
                        ),
                      ],
                    ),
                  ),
                for (final peer in peers)
                  Card(
                    child: ListTile(
                      autofocus: peers.length == 1,
                      minVerticalPadding: 16,
                      leading: Icon(lanDeviceIcon(peer.kind)),
                      title: Text(
                        peer.name,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                      ),
                      subtitle: Text(
                        link.connection?.peer.id == peer.id
                            ? '已連線'
                            : link.peers.any((found) => found.id == peer.id)
                            ? '可連線'
                            : '上次連線的裝置',
                      ),
                      trailing: PopupMenuButton<String>(
                        tooltip: '裝置操作',
                        enabled: !_busy && !link.connecting,
                        onSelected: (_) => _forget(peer),
                        itemBuilder: (_) => const [
                          PopupMenuItem(value: 'forget', child: Text('忘記配對')),
                        ],
                      ),
                      onTap: _busy || link.connecting
                          ? null
                          : () => _connect(peer),
                    ),
                  ),
                if (_error ?? link.error case final message?)
                  Padding(
                    padding: const EdgeInsets.all(12),
                    child: Text(
                      message,
                      style: TextStyle(
                        color: Theme.of(context).colorScheme.error,
                      ),
                    ),
                  ),
                if (link.discoveryMessage.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.all(12),
                    child: Text(link.discoveryMessage),
                  ),
                TextButton.icon(
                  onPressed: () => setState(() => _manual = !_manual),
                  icon: const Icon(Icons.keyboard_rounded),
                  label: const Text('輸入地址連線'),
                ),
                if (_manual) ...[
                  TextField(
                    controller: _address,
                    autocorrect: false,
                    enableSuggestions: false,
                    decoration: const InputDecoration(
                      labelText: '對方顯示的本機地址',
                      hintText: '192.168.1.8:53120',
                    ),
                    onSubmitted: (_) => _connect(null),
                  ),
                  const SizedBox(height: 12),
                  FilledButton(
                    onPressed: _busy ? null : () => _connect(null),
                    child: const Text('連線'),
                  ),
                ],
                const SizedBox(height: 24),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _LanManual extends StatefulWidget {
  const _LanManual({required this.controller});
  final LanController controller;
  @override
  State<_LanManual> createState() => _LanManualState();
}

class _LanManualState extends State<_LanManual> {
  LanSyncMode _mode = LanSyncMode.merge;
  LanPreview? _preview;
  bool _busy = false;
  bool _cancelling = false;
  late final int _epoch;
  late final LanConnection? _connection;
  String? _error;
  LanController get link => widget.controller;

  @override
  void initState() {
    super.initState();
    _epoch = link.store.profileEpoch;
    _connection = link.connection;
  }

  Future<void> _cancel() async {
    if (_cancelling) return;
    setState(() => _cancelling = true);
    try {
      await link.cancelSync();
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _cancelling = false);
    }
  }

  @override
  void dispose() {
    if (_busy && _epoch == link.store.profileEpoch) {
      unawaited(link.cancelSync().catchError((Object _) {}));
    }
    super.dispose();
  }

  Future<void> _submit() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      if (_epoch != link.store.profileEpoch ||
          link.store.locked ||
          link.connection != _connection) {
        throw StateError('裝置或使用者已變更，請關閉面板後重新操作');
      }
      if (_preview == null) {
        final preview = await link.preview(_mode);
        if (mounted) setState(() => _preview = preview);
      } else {
        await link.applyPreview(_preview!);
        if (mounted) Navigator.pop(context);
      }
    } catch (error) {
      if (mounted) {
        setState(() {
          _error = error.toString();
          _preview = null;
        });
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final name = link.connection?.peer.name ?? '對方';
    return PopScope<void>(
      canPop: !_busy && !_cancelling,
      child: SafeArea(
        top: false,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 12, 12, 8),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      '手動同步',
                      style: Theme.of(context).textTheme.titleLarge,
                    ),
                  ),
                  IconButton(
                    tooltip: '關閉',
                    onPressed: _busy ? null : () => Navigator.pop(context),
                    icon: const Icon(Icons.close_rounded),
                  ),
                ],
              ),
            ),
            Expanded(
              child: ListView(
                padding: const EdgeInsets.all(16),
                children: [
                  for (final mode in LanSyncMode.values)
                    Card(
                      color: _mode == mode
                          ? Theme.of(context).colorScheme.secondaryContainer
                          : null,
                      child: Semantics(
                        checked: _mode == mode,
                        inMutuallyExclusiveGroup: true,
                        child: ListTile(
                          autofocus: mode == LanSyncMode.merge,
                          minVerticalPadding: 16,
                          leading: Icon(
                            _mode == mode
                                ? Icons.radio_button_checked_rounded
                                : Icons.radio_button_unchecked_rounded,
                          ),
                          title: Text(mode.label),
                          subtitle: Text(switch (mode) {
                            LanSyncMode.merge => '本機 ↔ $name · 合併雙方記錄',
                            LanSyncMode.push => '本機 → $name · 以本機記錄替換對方',
                            LanSyncMode.pull => '$name → 本機 · 以對方記錄替換本機',
                          }),
                          onTap: _busy
                              ? null
                              : () => setState(() {
                                  _mode = mode;
                                  _preview = null;
                                }),
                        ),
                      ),
                    ),
                  if (_busy)
                    const Padding(
                      padding: EdgeInsets.all(16),
                      child: LinearProgressIndicator(),
                    ),
                  if (_preview case final preview?)
                    Card(
                      child: Padding(
                        padding: const EdgeInsets.all(16),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              '同步預覽',
                              style: Theme.of(context).textTheme.titleMedium,
                            ),
                            const SizedBox(height: 12),
                            Text('本機 · ${link.store.profile.name}'),
                            Text(preview.localCount.label),
                            const SizedBox(height: 12),
                            Text('$name · ${preview.connection.user}'),
                            Text(preview.remoteCount.label),
                            if (preview.skipped > 0)
                              Padding(
                                padding: const EdgeInsets.only(top: 12),
                                child: Text('雙方範圍外的 ${preview.skipped} 條記錄會保留'),
                              ),
                            if (_mode != LanSyncMode.merge)
                              Padding(
                                padding: const EdgeInsets.only(top: 16),
                                child: Text(
                                  _mode == LanSyncMode.push
                                      ? '將覆蓋 $name 的當前使用者記錄；移除 ${preview.remoteCount.removed} 部追劇。'
                                      : '將覆蓋本機當前使用者記錄；移除 ${preview.localCount.removed} 部追劇。',
                                ),
                              ),
                          ],
                        ),
                      ),
                    ),
                  if (_error != null)
                    Padding(
                      padding: const EdgeInsets.all(12),
                      child: Text(
                        _error!,
                        style: TextStyle(
                          color: Theme.of(context).colorScheme.error,
                        ),
                      ),
                    ),
                ],
              ),
            ),
            Padding(
              padding: const EdgeInsets.all(16),
              child: _busy || _cancelling
                  ? OutlinedButton.icon(
                      onPressed: _cancelling ? null : _cancel,
                      icon: const Icon(Icons.close_rounded),
                      label: Text(_cancelling ? '正在取消' : '取消本次同步'),
                    )
                  : FilledButton(
                      onPressed: _submit,
                      child: Text(
                        _preview == null
                            ? '檢視預覽'
                            : _mode == LanSyncMode.merge
                            ? '開始同步'
                            : _mode.label,
                      ),
                    ),
            ),
          ],
        ),
      ),
    );
  }
}

class _LanConflicts extends StatelessWidget {
  const _LanConflicts({required this.controller});
  final LanController controller;

  String _value(String field, Object? value) {
    if (field == 'member') return value == true ? '保留追劇' : '取消追劇';
    if (value == null) return '清空續播進度';
    final row = lanMap(value);
    if (field == 'status') {
      return switch (row['status']) {
        'planned' => '想看',
        'watching' => '在看',
        _ => row['manual'] == true ? '已看 · 手動標記' : '已看',
      };
    }
    return '第 ${row['episode']} 集 · ${formatPosition((row['position'] as num).toDouble())}';
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: controller.store,
    builder: (context, _) {
      final records = controller.store.locked
          ? <LanRecord>[]
          : controller.store.lanDocument.records.values
                .where(
                  (record) =>
                      record.conflicts > 0 &&
                      controller.store.allowsSource(record.drama.source),
                )
                .toList();
      return Scaffold(
        appBar: AppBar(title: const Text('處理記錄衝突')),
        body: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 720),
            child: records.isEmpty
                ? const Center(child: Text('沒有待處理的衝突'))
                : ListView.builder(
                    padding: const EdgeInsets.all(16),
                    itemCount: records.length,
                    itemBuilder: (context, index) {
                      final record = records[index];
                      return Card(
                        child: Padding(
                          padding: const EdgeInsets.all(16),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                record.drama.title,
                                style: Theme.of(context).textTheme.titleMedium,
                              ),
                              for (final field in record.fields.entries.where(
                                (field) => field.value.conflict,
                              )) ...[
                                Padding(
                                  padding: const EdgeInsets.only(
                                    top: 16,
                                    bottom: 4,
                                  ),
                                  child: Text(switch (field.key) {
                                    'member' => '追劇列表',
                                    'status' => '觀看狀態',
                                    _ => '續播進度',
                                  }),
                                ),
                                for (final candidate in field.value.values)
                                  ListTile(
                                    leading: const Icon(
                                      Icons.radio_button_unchecked_rounded,
                                    ),
                                    title: Text(
                                      _value(field.key, candidate.value),
                                    ),
                                    subtitle: const Text('使用這條記錄'),
                                    onTap: () =>
                                        saveUserChange(context, () async {
                                          await controller.store
                                              .resolveLanConflict(
                                                record.id,
                                                field.key,
                                                candidate,
                                                record.hash,
                                              );
                                          controller.flush();
                                        }),
                                  ),
                              ],
                            ],
                          ),
                        ),
                      );
                    },
                  ),
          ),
        ),
      );
    },
  );
}

class LanPushButton extends StatelessWidget {
  const LanPushButton({super.key, required this.onPressed});
  final VoidCallback? onPressed;
  @override
  Widget build(BuildContext context) {
    final link = LanController.current;
    if (link == null) return const SizedBox.shrink();
    return AnimatedBuilder(
      animation: link,
      builder: (context, _) => IconButton(
        tooltip: link.connection == null
            ? '推送'
            : '推送到 ${link.connection!.peer.name}',
        onPressed: onPressed,
        constraints: const BoxConstraints.tightFor(width: 36, height: 36),
        padding: EdgeInsets.zero,
        iconSize: 22,
        icon: link.pushing
            ? const SizedBox(
                width: 22,
                height: 22,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            : Icon(
                link.connection == null
                    ? Icons.cast_rounded
                    : Icons.cast_connected_rounded,
              ),
      ),
    );
  }
}

Future<void> showLanPush(
  BuildContext context,
  LanController link, {
  required LanPlaybackIntent Function() snapshot,
  required bool Function() stillCurrent,
  required Future<void> Function() onAccepted,
}) async {
  final remote = await chooseLanDevice(context, link);
  if (remote == null || !context.mounted || !stillCurrent()) return;
  await _lanPanel<void>(
    context,
    _LanPushPanel(
      controller: link,
      snapshot: snapshot,
      stillCurrent: stillCurrent,
      onAccepted: onAccepted,
    ),
    maxHeight: 560,
  );
  if (link.pushing) await link.cancelPush();
}

class _LanPushPanel extends StatefulWidget {
  const _LanPushPanel({
    required this.controller,
    required this.snapshot,
    required this.stillCurrent,
    required this.onAccepted,
  });
  final LanController controller;
  final LanPlaybackIntent Function() snapshot;
  final bool Function() stillCurrent;
  final Future<void> Function() onAccepted;
  @override
  State<_LanPushPanel> createState() => _LanPushPanelState();
}

class _LanPushPanelState extends State<_LanPushPanel> {
  bool _done = false;
  bool _success = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _start());
  }

  Future<void> _start() async {
    if (!mounted) return;
    try {
      await widget.controller.pushPlayback(
        snapshot: widget.snapshot,
        stillCurrent: () => mounted && widget.stillCurrent(),
        onAccepted: widget.onAccepted,
        confirmReplace: (title) async {
          if (!mounted) return false;
          return await showDialog<bool>(
                context: context,
                builder: (context) => AlertDialog(
                  title: const Text('切換對方正在播放的內容？'),
                  content: Text('對方正在播放“$title”，繼續後將接續本機當前分集。'),
                  actions: [
                    TextButton(
                      onPressed: () => Navigator.pop(context, false),
                      child: const Text('取消'),
                    ),
                    FilledButton(
                      onPressed: () => Navigator.pop(context, true),
                      child: const Text('推送播放'),
                    ),
                  ],
                ),
              ) ??
              false;
        },
      );
      _success = true;
    } catch (_) {
    } finally {
      if (mounted) setState(() => _done = true);
    }
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.controller,
    builder: (context, _) => SafeArea(
      top: false,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.all(20),
            child: Text('推送播放', style: Theme.of(context).textTheme.titleLarge),
          ),
          Expanded(
            child: SingleChildScrollView(
              padding: const EdgeInsets.all(24),
              child: Column(
                children: [
                  Icon(
                    _success
                        ? Icons.cast_connected_rounded
                        : Icons.cast_rounded,
                    size: 64,
                  ),
                  const SizedBox(height: 24),
                  Text(
                    widget.snapshot().drama.title,
                    textAlign: TextAlign.center,
                    style: Theme.of(context).textTheme.titleLarge,
                  ),
                  const SizedBox(height: 12),
                  Text(
                    '接收裝置 · ${widget.controller.connection?.peer.name ?? '連線已斷開'}',
                    textAlign: TextAlign.center,
                  ),
                  const SizedBox(height: 24),
                  if (!_done) const LinearProgressIndicator(),
                  const SizedBox(height: 16),
                  Semantics(
                    liveRegion: true,
                    child: Text(
                      widget.controller.pushMessage,
                      textAlign: TextAlign.center,
                    ),
                  ),
                  if (!_done)
                    const Padding(
                      padding: EdgeInsets.only(top: 16),
                      child: Text(
                        '對方實際開播後，本機自動暫停。',
                        textAlign: TextAlign.center,
                      ),
                    ),
                ],
              ),
            ),
          ),
          Padding(
            padding: const EdgeInsets.all(16),
            child: FilledButton(
              onPressed: () async {
                if (!_done) await widget.controller.cancelPush();
                if (context.mounted) Navigator.pop(context);
              },
              child: Text(_done ? '完成' : '取消推送'),
            ),
          ),
        ],
      ),
    ),
  );
}
