import 'dart:io' show Platform;

import 'package:flutter/material.dart';

import 'local_store.dart';
import 'models.dart';
import 'profiles_screen.dart';
import 'ui_localization.dart';

class SourceManagementScreen extends StatefulWidget {
  const SourceManagementScreen({super.key, required this.store});

  final LocalStore store;

  @override
  State<SourceManagementScreen> createState() => _SourceManagementScreenState();
}

class _SourceManagementScreenState extends State<SourceManagementScreen> {
  final _password = TextEditingController();
  bool _unlocked = false;
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    widget.store.lockAdvancedSources();
    _password.dispose();
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
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
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
    if (mounted) setState(() => _error = null);
  }

  Future<void> _toggle(SourceSite source, bool enabled) async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await widget.store.setSourceEnabled(source.id, enabled);
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  void _lock() {
    widget.store.lockAdvancedSources();
    _password.clear();
    setState(() {
      _unlocked = false;
      _error = null;
    });
  }

  Widget _sourceTile(SourceSite source) => SwitchListTile(
    key: ValueKey('source-switch-${source.id}'),
    contentPadding: EdgeInsets.zero,
    title: Text(uiTraditional(source.name)),
    subtitle: Text(uiTraditional(source.description)),
    value: widget.store.sourceEnabled(source.id),
    onChanged: _busy ? null : (value) => _toggle(source, value),
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
              const Text('請先設定管理員密碼，才能解鎖七個進階片源。'),
              const SizedBox(height: 12),
              OutlinedButton.icon(
                key: const ValueKey('configure-admin-source-password'),
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
            const Text('進階片源已上鎖'),
            const SizedBox(height: 6),
            const Text('輸入管理員密碼後，才會顯示七個進階片源及其開關。'),
            const SizedBox(height: 12),
            TextField(
              key: const ValueKey('advanced-source-password'),
              controller: _password,
              autofocus: true,
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
              label: Text(_busy ? '正在驗證…' : '解鎖進階片源'),
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
                    const Text(
                      '四個一般片源預設開啟。關閉片源只會將其隱藏，不會刪除收藏及觀看紀錄。',
                    ),
                    const SizedBox(height: 8),
                    for (final source in general) _sourceTile(source),
                    const Divider(height: 28),
                    Text(
                      '進階片源',
                      style: Theme.of(context).textTheme.titleLarge,
                    ),
                    const SizedBox(height: 4),
                    if (advanced.isEmpty)
                      const Text('目前這個版本未包含進階片源，請使用完整片源版 D TV。')
                    else if (unlocked) ...[
                      const Text('已解鎖。離開此頁面後，進階片源管理會自動重新上鎖。'),
                      const SizedBox(height: 8),
                      for (final source in advanced) _sourceTile(source),
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
