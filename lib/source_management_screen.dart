import 'dart:async';

import 'package:flutter/services.dart';

import 'package:flutter/material.dart';

import 'app_layout.dart';
import 'local_store.dart';
import 'models.dart';
import 'profiles_screen.dart';
import 'ui_localization.dart';

String sourceManagementErrorMessage(Object error) => uiTraditional(
  error.toString().replaceFirst(RegExp(r'^(?:Bad state:\s*)+', caseSensitive: false), ''),
);

class SourceManagementScreen extends StatefulWidget {
  const SourceManagementScreen({
    super.key,
    required this.store,
    this.updateSourceEnabled,
  });

  final LocalStore store;
  final Future<void> Function(String sourceId, bool enabled)?
      updateSourceEnabled;

  @override
  State<SourceManagementScreen> createState() => _SourceManagementScreenState();
}

class _SourceManagementScreenState extends State<SourceManagementScreen> {
  final _password = TextEditingController();
  final _firstGeneralFocus = FocusNode(
    debugLabel: 'source-management-first-general-source',
  );
  final _setupPasswordFocus = FocusNode(
    debugLabel: 'source-management-setup-password',
  );
  final _passwordFocus = FocusNode(
    debugLabel: 'source-management-password',
  );
  final _unlockButtonFocus = FocusNode(
    debugLabel: 'source-management-unlock-button',
  );
  final Map<String, FocusNode> _generalFocusNodes = <String, FocusNode>{};
  final Map<String, FocusNode> _advancedFocusNodes = <String, FocusNode>{};
  final Map<String, FocusNode> _advancedSwitchFocusNodes = <String, FocusNode>{};
  Timer? _tvReorderHoldTimer;
  String? _tvPendingReorderSourceId;
  String? _tvSelectedReorderSourceId;
  bool _unlocked = false;
  bool _busy = false;
  bool _ordering = false;
  List<String>? _pendingAdvancedOrder;
  bool _initialFocusScheduled = false;
  final Set<String> _busySourceIds = <String>{};
  String? _error;

  void _onFocusChanged() {
    if (mounted) setState(() {});
  }

  @override
  void initState() {
    super.initState();
    _firstGeneralFocus.addListener(_onFocusChanged);
    _setupPasswordFocus.addListener(_onFocusChanged);
    _passwordFocus.addListener(_onFocusChanged);
    _unlockButtonFocus.addListener(_onFocusChanged);
  }

  FocusNode _generalSourceFocus(String sourceId) {
    if (sourceId == SourceSite.primaryValues.first.id) return _firstGeneralFocus;
    return _generalFocusNodes.putIfAbsent(sourceId, () {
      final node = FocusNode(debugLabel: 'source-general-$sourceId');
      node.addListener(_onFocusChanged);
      return node;
    });
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_initialFocusScheduled || !AppLayout.isTelevision(context)) return;
    _initialFocusScheduled = true;
    if (widget.store.locked || !widget.store.profile.admin) return;
    final firstAdvanced = widget.store.advancedSourceOrder
        .where(SourceSite.isAvailable)
        .firstOrNull;
    final target = firstAdvanced == null
        ? _firstGeneralFocus
        : widget.store.administratorPasswordConfigured
        ? _passwordFocus
        : _setupPasswordFocus;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && target.context != null && target.canRequestFocus) {
        target.requestFocus();
      }
    });
  }

  @override
  void dispose() {
    widget.store.lockAdvancedSources();
    _password.dispose();
    _firstGeneralFocus.dispose();
    _setupPasswordFocus.dispose();
    _passwordFocus.dispose();
    _unlockButtonFocus.dispose();
    for (final node in _generalFocusNodes.values) {
      node.dispose();
    }
    _tvReorderHoldTimer?.cancel();
    for (final node in _advancedFocusNodes.values) {
      node.dispose();
    }
    for (final node in _advancedSwitchFocusNodes.values) {
      node.dispose();
    }
    super.dispose();
  }

  Future<void> _unlock() async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await widget.store.unlockAdvancedSources(_password.text);
      if (!mounted) return;
      setState(() {
        _unlocked = true;
        _password.clear();
      });
      final firstAdvanced = _visibleAdvancedSourceIds.firstOrNull;
      if (firstAdvanced != null) {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          final node = _advancedSourceFocus(firstAdvanced);
          if (mounted && node.context != null && node.canRequestFocus) {
            node.requestFocus();
          }
        });
      }
    } catch (error) {
      if (mounted) setState(() => _error = sourceManagementErrorMessage(error));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _setupPassword() async {
    final admin = widget.store.profiles.firstWhere((profile) => profile.admin);
    await Navigator.push<void>(
      context,
      MaterialPageRoute<void>(
        builder: (_) => ProfileEditor(store: widget.store, profile: admin),
      ),
    );
    if (mounted) {
      setState(() => _error = null);
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && _passwordFocus.context != null &&
            _passwordFocus.canRequestFocus) {
          _passwordFocus.requestFocus();
        }
      });
    }
  }

  Future<void> _toggle(SourceSite source, bool enabled) async {
    if (_busy || _busySourceIds.contains(source.id)) return;
    setState(() {
      _busySourceIds.add(source.id);
      _error = null;
    });
    try {
      final update = widget.updateSourceEnabled ?? widget.store.setSourceEnabled;
      await update(source.id, enabled);
    } catch (error) {
      if (mounted) setState(() => _error = sourceManagementErrorMessage(error));
    } finally {
      if (mounted) {
        setState(() => _busySourceIds.remove(source.id));
      }
    }
  }

  List<String> get _visibleAdvancedSourceIds =>
      (_pendingAdvancedOrder ?? widget.store.advancedSourceOrder)
          .where(SourceSite.isAvailable)
          .toList();

  Future<void> _saveAdvancedOrder(List<String> order) async {
    if (_busy || _ordering || !widget.store.advancedSourcesUnlocked) return;
    setState(() {
      _ordering = true;
      _pendingAdvancedOrder = List<String>.of(order);
      _error = null;
    });
    try {
      await widget.store.setAdvancedSourceOrder(order);
    } catch (error) {
      if (mounted) setState(() => _error = sourceManagementErrorMessage(error));
    } finally {
      if (mounted) {
        setState(() {
          _ordering = false;
          _pendingAdvancedOrder = null;
        });
      }
    }
  }

  void _reorderAdvanced(int oldIndex, int newIndex) {
    if (_ordering || oldIndex == newIndex) return;
    final order = _visibleAdvancedSourceIds;
    if (oldIndex < 0 || oldIndex >= order.length) return;
    if (newIndex > oldIndex) newIndex--;
    final target = newIndex.clamp(0, order.length - 1).toInt();
    if (target == oldIndex) return;
    final moved = order.removeAt(oldIndex);
    order.insert(target, moved);
    _saveAdvancedOrder(order);
  }

  void _moveAdvancedSource(int index, int delta) {
    if (_ordering) return;
    final order = _visibleAdvancedSourceIds;
    final target = index + delta;
    if (index < 0 || index >= order.length ||
        target < 0 || target >= order.length) {
      return;
    }
    final moved = order.removeAt(index);
    order.insert(target, moved);
    _saveAdvancedOrder(order);
  }

  FocusNode _advancedSourceFocus(String sourceId) =>
      _advancedFocusNodes.putIfAbsent(sourceId, () {
        final node = FocusNode(debugLabel: 'source-management-$sourceId');
        node.addListener(_onFocusChanged);
        return node;
      });

  FocusNode _advancedSwitchFocus(String sourceId) =>
      _advancedSwitchFocusNodes.putIfAbsent(sourceId, () {
        final node = FocusNode(debugLabel: 'source-switch-$sourceId');
        node.addListener(_onFocusChanged);
        return node;
      });

  bool _isTvConfirmKey(LogicalKeyboardKey key) => {
    LogicalKeyboardKey.select,
    LogicalKeyboardKey.enter,
    LogicalKeyboardKey.numpadEnter,
    LogicalKeyboardKey.gameButtonA,
  }.contains(key);

  KeyEventResult _advancedSourceKey(
    SourceSite source,
    int index,
    KeyEvent event,
  ) {
    if (!AppLayout.isTelevision(context)) return KeyEventResult.ignored;
    final key = event.logicalKey;
    final selectedId = _tvSelectedReorderSourceId;
    if (selectedId != null) {
      if ((key == LogicalKeyboardKey.escape ||
              key == LogicalKeyboardKey.goBack) &&
          event is KeyDownEvent) {
        setState(() => _tvSelectedReorderSourceId = null);
        return KeyEventResult.handled;
      }
      if (_isTvConfirmKey(key) && event is KeyDownEvent) {
        setState(() => _tvSelectedReorderSourceId = null);
        return KeyEventResult.handled;
      }
      if ((key == LogicalKeyboardKey.arrowUp ||
              key == LogicalKeyboardKey.arrowDown) &&
          (event is KeyDownEvent || event is KeyRepeatEvent)) {
        if (selectedId == source.id) {
          _moveAdvancedSource(
            index,
            key == LogicalKeyboardKey.arrowUp ? -1 : 1,
          );
        }
        return KeyEventResult.handled;
      }
      if (_isTvConfirmKey(key) && event is KeyRepeatEvent) {
        return KeyEventResult.handled;
      }
      return KeyEventResult.ignored;
    }

    if (!_isTvConfirmKey(key)) return KeyEventResult.ignored;
    if (event is KeyDownEvent) {
      if (_busy || _ordering || _busySourceIds.contains(source.id)) {
        return KeyEventResult.handled;
      }
      _tvReorderHoldTimer?.cancel();
      _tvPendingReorderSourceId = source.id;
      _tvReorderHoldTimer = Timer(const Duration(milliseconds: 500), () {
        if (!mounted || _tvPendingReorderSourceId != source.id) return;
        _tvPendingReorderSourceId = null;
        setState(() => _tvSelectedReorderSourceId = source.id);
      });
      return KeyEventResult.handled;
    }
    if (event is KeyRepeatEvent) return KeyEventResult.handled;
    if (event is KeyUpEvent) {
      final wasShortPress = _tvPendingReorderSourceId == source.id;
      _tvPendingReorderSourceId = null;
      _tvReorderHoldTimer?.cancel();
      if (wasShortPress &&
          (_advancedSourceFocus(source.id).hasFocus ||
              _advancedSwitchFocus(source.id).hasFocus)) {
        unawaited(_toggle(source, !widget.store.sourceEnabled(source.id)));
      }
      return KeyEventResult.handled;
    }
    return KeyEventResult.ignored;
  }

  Widget _advancedSourceTile(SourceSite source, {required int index}) {
    final isTelevision = AppLayout.isTelevision(context);
    final selectedForReorder = _tvSelectedReorderSourceId == source.id;
    final rowFocus = isTelevision ? _advancedSourceFocus(source.id) : null;
    final switchFocus = isTelevision ? _advancedSwitchFocus(source.id) : null;
    final hasFocus = isTelevision &&
        (rowFocus!.hasFocus || switchFocus!.hasFocus);
    final colors = Theme.of(context).colorScheme;
    final enabled = widget.store.sourceEnabled(source.id);
    final subtitle = selectedForReorder
        ? '已選取排序：使用遙控器方向鍵移動，按確認完成'
        : hasFocus
        ? '目前焦點：${uiTraditional(source.name)}｜按確認切換，長按可排序'
        : uiTraditional(source.description);
    return ReorderableDelayedDragStartListener(
      key: ValueKey('reorderable-source-${source.id}'),
      index: index,
      child: Focus(
        key: ValueKey('source-row-focus-${source.id}'),
        focusNode: rowFocus,
        onKeyEvent: (node, event) => _advancedSourceKey(source, index, event),
        child: AnimatedContainer(
          duration: const Duration(milliseconds: 140),
          decoration: BoxDecoration(
            border: selectedForReorder
                ? Border.all(color: colors.primary, width: 2.5)
                : hasFocus
                ? Border.all(color: colors.primary, width: 1.8)
                : null,
            color: hasFocus
                ? colors.primary.withValues(alpha: 0.08)
                : null,
            borderRadius: BorderRadius.circular(12),
          ),
          child: ListTile(
            contentPadding: const EdgeInsets.symmetric(horizontal: 8),
            title: Text(uiTraditional(source.name)),
            subtitle: Wrap(
              spacing: 8,
              runSpacing: 4,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                Container(
                  key: ValueKey('restricted-badge-${source.id}'),
                  padding: const EdgeInsets.symmetric(
                    horizontal: 7,
                    vertical: 3,
                  ),
                  decoration: BoxDecoration(
                    color: Theme.of(context).colorScheme.errorContainer,
                    borderRadius: BorderRadius.circular(6),
                  ),
                  child: Text(
                    '限制級',
                    style: Theme.of(context).textTheme.labelSmall?.copyWith(
                      color: Theme.of(context).colorScheme.onErrorContainer,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
                Text(subtitle),
              ],
            ),
            trailing: Switch(
              key: ValueKey('source-switch-${source.id}'),
              focusNode: switchFocus,
              value: enabled,
              onChanged: _busy || _ordering || _busySourceIds.contains(source.id)
                  ? null
                  : (value) => _toggle(source, value),
            ),
          ),
        ),
      ),
    );
  }

  void _lock() {
    widget.store.lockAdvancedSources();
    _password.clear();
    setState(() {
      _unlocked = false;
      _error = null;
    });
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && _passwordFocus.context != null &&
          _passwordFocus.canRequestFocus) {
        _passwordFocus.requestFocus();
      }
    });
  }

  Widget _sourceTile(
    SourceSite source, {
    FocusNode? focusNode,
  }) => AnimatedContainer(
    duration: const Duration(milliseconds: 140),
    margin: const EdgeInsets.symmetric(vertical: 2),
    padding: const EdgeInsets.symmetric(horizontal: 6),
    decoration: BoxDecoration(
      color: focusNode?.hasFocus == true
          ? Theme.of(context).colorScheme.primary.withValues(alpha: 0.08)
          : null,
      border: focusNode?.hasFocus == true
          ? Border.all(color: Theme.of(context).colorScheme.primary, width: 1.8)
          : null,
      borderRadius: BorderRadius.circular(10),
    ),
    child: SwitchListTile(
      key: ValueKey('source-switch-${source.id}'),
      focusNode: focusNode,
      contentPadding: EdgeInsets.zero,
      title: Text(uiTraditional(source.name)),
      subtitle: Text(source.id == 'duoduo' ? source.description : uiTraditional(source.description)),
      value: widget.store.sourceEnabled(source.id),
      onChanged: _busy || _busySourceIds.contains(source.id)
          ? null
          : (value) => _toggle(source, value),
    ),
  );

  Widget _passwordGate() {
    if (!widget.store.administratorPasswordConfigured) {
      return Card(
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const Text('尚未設定管理員密碼'),
              const SizedBox(height: 6),
              const Text('請先設定管理員密碼，才能繼續管理片源。'),
              const SizedBox(height: 12),
              OutlinedButton.icon(
                key: const ValueKey('configure-admin-source-password'),
                focusNode: _setupPasswordFocus,
                autofocus: AppLayout.isTelevision(context),
                onPressed: _busy ? null : _setupPassword,
                icon: const Icon(Icons.admin_panel_settings_outlined),
                label: const Text('設定管理員密碼'),
              ),
            ],
          ),
        ),
      );
    }
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: FocusTraversalGroup(
          child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Text('管理功能已上鎖'),
            const SizedBox(height: 6),
            const Text('請輸入管理員密碼以繼續。'),
            const SizedBox(height: 12),
            FocusTraversalOrder(
              order: const NumericFocusOrder(1),
              child: Shortcuts(
                shortcuts: <ShortcutActivator, Intent>{
                  SingleActivator(LogicalKeyboardKey.arrowDown):
                      const NextFocusIntent(),
                  SingleActivator(LogicalKeyboardKey.arrowUp):
                      const PreviousFocusIntent(),
                },
                child: TextField(
                  key: const ValueKey('advanced-source-password'),
                  controller: _password,
                  focusNode: _passwordFocus,
                  autofocus: AppLayout.isTelevision(context),
                  enabled: !_busy,
                  obscureText: true,
                  textInputAction: TextInputAction.done,
                  onSubmitted: (_) => _unlock(),
                  decoration: const InputDecoration(
                    labelText: '管理員密碼',
                    border: OutlineInputBorder(),
                  ),
                ),
              ),
            ),
            const SizedBox(height: 12),
            FocusTraversalOrder(
              order: const NumericFocusOrder(2),
              child: FilledButton.icon(
                key: const ValueKey('unlock-advanced-sources'),
                focusNode: _unlockButtonFocus,
                onPressed: _busy ? null : _unlock,
                icon: const Icon(Icons.lock_open_rounded),
                label: Text(_busy ? '正在驗證…' : '解鎖'),
              ),
            ),
            const SizedBox(height: 8),

          ],
        ),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final general = SourceSite.primaryValues
        .where((source) => SourceSite.isAvailable(source.id))
        .toList();
    final generalNames = general.map((source) => source.name).join('、');
    return AnimatedBuilder(
      animation: widget.store,
      builder: (context, _) {
        final unlocked = _unlocked && widget.store.advancedSourcesUnlocked;
        final advancedOrder = unlocked
            ? _visibleAdvancedSourceIds
            : <String>[];
        final advanced = [
          for (final id in advancedOrder) SourceSite.byId(id),
        ];
        final hasAdvancedSources = SourceSite.restrictedValues.any(
          (source) => SourceSite.isAvailable(source.id),
        );
        final mainContent = <Widget>[
          if (widget.store.locked || !widget.store.profile.admin) ...[
            const Icon(Icons.lock_outline_rounded, size: 40),
            const SizedBox(height: 12),
            const Text('只有已登入的管理員可以管理片源。'),
          ] else ...[
            Text(
              '一般片源',
              style: Theme.of(context).textTheme.titleLarge,
            ),
            const SizedBox(height: 4),
            Text(
              '預設開啟 ${general.length} 個一般片源：${generalNames}。關閉片源只會將其隱藏，不會刪除收藏及觀看紀錄。',
            ),
            const SizedBox(height: 8),
            for (var index = 0; index < general.length; index++)
              _sourceTile(
                general[index],
                focusNode: AppLayout.isTelevision(context)
                    ? _generalSourceFocus(general[index].id)
                    : null,
              ),
            if (hasAdvancedSources) ...[
              const Divider(height: 28),
              if (unlocked) ...[
                Text(
                  '進階片源',
                  style: Theme.of(context).textTheme.titleLarge,
                ),
                const SizedBox(height: 4),
                Text(
                  AppLayout.isTelevision(context)
                      ? '長按整個片源列選取，再使用遙控器方向鍵移動；短按確認可切換開關。'
                      : '長按整個片源列即可拖曳移動；短按右側開關可切換片源。',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
                const SizedBox(height: 8),
              ] else ...[
                _passwordGate(),
                if (_error != null) ...[
                  const SizedBox(height: 12),
                  Text(
                    uiTraditional(_error!),
                    style: TextStyle(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                ],
              ],
            ],
          ],
        ];
        final isAdmin = !widget.store.locked && widget.store.profile.admin;
        return Scaffold(
          appBar: AppBar(title: const Text('片源管理')),
          body: Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 720),
              child: CustomScrollView(
                slivers: [
                  SliverPadding(
                    padding: const EdgeInsets.all(20),
                    sliver: SliverList(
                      delegate: SliverChildListDelegate(mainContent),
                    ),
                  ),
                  if (isAdmin && unlocked && advanced.isNotEmpty)
                    SliverPadding(
                      padding: const EdgeInsets.symmetric(horizontal: 20),
                      sliver: SliverReorderableList(
                        itemCount: advanced.length,
                        onReorder: _ordering ? (_, __) {} : _reorderAdvanced,
                        proxyDecorator: (child, index, animation) =>
                            AnimatedBuilder(
                              animation: animation,
                              builder: (context, _) => Material(
                                type: MaterialType.transparency,
                                color: Colors.transparent,
                                elevation: 0,
                                child: DecoratedBox(
                                  decoration: BoxDecoration(
                                    border: Border.all(
                                      color: Theme.of(context)
                                          .colorScheme
                                          .primary
                                          .withValues(alpha: 0.8),
                                    ),
                                    borderRadius: BorderRadius.circular(12),
                                  ),
                                  child: child,
                                ),
                              ),
                            ),
                        itemBuilder: (context, index) => _advancedSourceTile(
                          advanced[index],
                          index: index,
                        ),
                      ),
                    ),
                  if (isAdmin && unlocked)
                    SliverPadding(
                      padding: const EdgeInsets.fromLTRB(20, 0, 20, 20),
                      sliver: SliverToBoxAdapter(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            if (_ordering)
                              const Padding(
                                padding: EdgeInsets.symmetric(vertical: 8),
                                child: LinearProgressIndicator(),
                              ),
                            Align(
                              alignment: Alignment.centerLeft,
                              child: TextButton.icon(
                                key: const ValueKey('lock-advanced-sources'),
                                onPressed: _ordering ? null : _lock,
                                icon: const Icon(Icons.lock_outline_rounded),
                                label: const Text('立即上鎖'),
                              ),
                            ),
                            if (_error != null) ...[
                              const SizedBox(height: 12),
                              Text(
                                uiTraditional(_error!),
                                style: TextStyle(
                                  color: Theme.of(context).colorScheme.error,
                                ),
                              ),
                            ],
                          ],
                        ),
                      ),
                    ),
                ],
              ),
            ),
          ),
        );
      },
    );
  }
}
