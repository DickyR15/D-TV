import 'dart:io' show Platform;

import 'package:flutter/material.dart';

import 'app_layout.dart';
import 'local_store.dart';
import 'models.dart';
import 'profiles_screen.dart';
import 'ui_localization.dart';

String sourceManagementErrorMessage(Object error) => uiTraditional(
  error.toString().replaceFirst(RegExp(r'^Bad state:\s*'), ''),
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
  final _firstAdvancedFocus = FocusNode(
    debugLabel: 'source-management-first-advanced-source',
  );
  bool _unlocked = false;
  bool _busy = false;
  bool _initialFocusScheduled = false;
  final Set<String> _busySourceIds = <String>{};
  String? _error;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_initialFocusScheduled || !AppLayout.isTelevision(context)) return;
    _initialFocusScheduled = true;
    if (widget.store.locked || !widget.store.profile.admin) return;
    final hasAdvancedSources = SourceSite.restrictedValues.any(
      (source) => SourceSite.isAvailable(source.id),
    );
    final target = !hasAdvancedSources
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
    _firstAdvancedFocus.dispose();
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
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && _firstAdvancedFocus.context != null &&
            _firstAdvancedFocus.canRequestFocus) {
          _firstAdvancedFocus.requestFocus();
        }
      });
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
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) {
        setState(() => _busySourceIds.remove(source.id));
      }
    }
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
  }) => SwitchListTile(
    key: ValueKey('source-switch-${source.id}'),
    focusNode: focusNode,
    contentPadding: EdgeInsets.zero,
    title: Text(uiTraditional(source.name)),
    subtitle: Text(uiTraditional(source.description)),
    value: widget.store.sourceEnabled(source.id),
    onChanged: _busy || _busySourceIds.contains(source.id)
        ? null
        : (value) => _toggle(source, value),
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
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Text('管理功能已上鎖'),
            const SizedBox(height: 6),
            const Text('請輸入管理員密碼以繼續。'),
            const SizedBox(height: 12),
            TextField(
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
            const SizedBox(height: 12),
            FilledButton.icon(
              key: const ValueKey('unlock-advanced-sources'),
              onPressed: _busy ? null : _unlock,
              icon: const Icon(Icons.lock_open_rounded),
              label: Text(_busy ? '正在驗證…' : '解鎖'),
            ),
            const SizedBox(height: 8),

          ],
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
    final advanced = SourceSite.restrictedValues
        .where((source) => SourceSite.isAvailable(source.id))
        .toList();
    return AnimatedBuilder(
      animation: widget.store,
      builder: (context, _) {
        final unlocked = _unlocked && widget.store.advancedSourcesUnlocked;
        return Scaffold(
          appBar: AppBar(title: const Text('片源管理')),
          body: Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 720),
              child: ListView(
                padding: const EdgeInsets.all(20),
                children: [
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
                      '預設開啟 ${general.length} 個一般片源：$generalNames。關閉片源只會將其隱藏，不會刪除收藏及觀看紀錄。',
                    ),
                    const SizedBox(height: 8),
                    for (var index = 0; index < general.length; index++)
                      _sourceTile(
                        general[index],
                        focusNode: index == 0 ? _firstGeneralFocus : null,
                      ),
                    if (advanced.isNotEmpty) ...[
                      const Divider(height: 28),
                      if (unlocked) ...[
                        Text(
                          '進階片源',
                          style: Theme.of(context).textTheme.titleLarge,
                        ),
                        const SizedBox(height: 8),
                        for (var index = 0; index < advanced.length; index++)
                          _sourceTile(
                            advanced[index],
                            focusNode: index == 0 ? _firstAdvancedFocus : null,
                          ),
                        Align(
                          alignment: Alignment.centerLeft,
                          child: TextButton.icon(
                            key: const ValueKey('lock-advanced-sources'),
                            onPressed: _lock,
                            icon: const Icon(Icons.lock_outline_rounded),
                            label: const Text('立即上鎖'),
                          ),
                        ),
                      ] else
                        _passwordGate(),
                    ],
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
              ),
            ),
          ),
        );
      },
    );
  }
}
