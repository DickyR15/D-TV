import 'package:flutter/material.dart';

import 'core_bridge.dart';
import 'local_store.dart';
import 'resource_settings.dart';
import 'widgets.dart';
import 'ui_localization.dart';

class ResourceSettingsScreen extends StatefulWidget {
  const ResourceSettingsScreen({
    super.key,
    required this.repository,
    required this.store,
  });
  final AppRepository repository;
  final LocalStore store;
  @override
  State<ResourceSettingsScreen> createState() => _ResourceSettingsScreenState();
}

class _ResourceSettingsScreenState extends State<ResourceSettingsScreen> {
  final _proxy = TextEditingController();
  late final int _epoch = widget.store.profileEpoch;
  ResourceSettings? _settings;
  String _mode = 'auto';
  bool _busy = false;
  bool _showProxy = false;
  int _catalog = 3;
  int _interval = 250;

  String? _error;
  bool get _allowed =>
      widget.store.profileEpoch == _epoch &&
      !widget.store.locked &&
      widget.store.profile.admin;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _proxy.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final settings = await widget.repository.resourceSettings();
      if (!mounted || !_allowed) return;
      setState(() {
        _settings = settings;
        _mode = settings.proxyMode;
        _proxy.text = settings.proxyUrl;
        _catalog = settings.catalogConcurrency;
        _interval = settings.catalogIntervalMs;
        _error = null;
      });
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    }
  }

  Future<void> _save() async {
    if (_busy || !_allowed) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final settings = await widget.repository.saveResourceSettings(
        ResourceSettings(
          proxyMode: _mode,
          proxyUrl: _proxy.text.trim(),
          catalogConcurrency: _catalog,
          catalogIntervalMs: _interval,
        ),
      );
      if (!mounted || !_allowed) return;
      setState(() => _settings = settings);
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('設定已儲存，將套用於後續連線與目錄請求')));
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Widget _count(String title, int value, ValueChanged<int> change) =>
      DropdownButtonFormField<int>(
        initialValue: value,
        decoration: InputDecoration(labelText: title),
        items: [
          for (var i = 1; i <= 6; i++)
            DropdownMenuItem(value: i, child: Text('$i')),
        ],
        onChanged: _busy
            ? null
            : (value) {
                if (value != null) setState(() => change(value));
              },
      );

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: const Text('網路與資源'),
      actions: [
        TextButton(
          onPressed: _busy || _settings == null || !_allowed ? null : _save,
          child: const Text('儲存'),
        ),
      ],
    ),
    body: !_allowed
        ? const StatusPanel(title: '需要管理員權限', message: '請返回並切換至管理員帳號。')
        : _settings == null
        ? _error == null
              ? const Center(child: CircularProgressIndicator())
              : StatusPanel(title: '無法讀取設定', message: _error!, onRetry: _load)
        : Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 720),
              child: ListView(
                padding: const EdgeInsets.all(20),
                children: [
                  if (_busy) const LinearProgressIndicator(),
                  if (_settings!.warning.isNotEmpty)
                    Padding(
                      padding: const EdgeInsets.only(bottom: 16),
                      child: Text(uiTraditional(_settings!.warning)),
                    ),
                  DropdownButtonFormField<String>(
                    initialValue: _mode,
                    decoration: const InputDecoration(labelText: '連線方式'),
                    items: const [
                      DropdownMenuItem(value: 'auto', child: Text('自動')),
                      DropdownMenuItem(value: 'direct', child: Text('直連')),
                      DropdownMenuItem(value: 'manual', child: Text('手動代理')),
                    ],
                    onChanged: _busy
                        ? null
                        : (value) => setState(() => _mode = value ?? 'auto'),
                  ),
                  if (_mode == 'auto')
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 12),
                      child: Text(
                        _settings!.systemProxyStatus.isEmpty
                            ? '使用系統靜態代理；未設定時使用環境變數或直連。'
                            : uiTraditional(_settings!.systemProxyStatus),
                      ),
                    ),
                  if (_mode == 'manual')
                    Padding(
                      padding: const EdgeInsets.only(top: 16),
                      child: TextField(
                        controller: _proxy,
                        obscureText: !_showProxy,
                        enableSuggestions: false,
                        autocorrect: false,
                        decoration: InputDecoration(
                          labelText: '代理地址',
                          hintText: 'http://127.0.0.1:7890',
                          helperText: '支援 HTTP、HTTPS、SOCKS5；僅儲存在本機，不會納入設定備份。',
                          helperMaxLines: 3,
                          suffixIcon: IconButton(
                            tooltip: _showProxy ? '隱藏代理地址' : '顯示代理地址',
                            onPressed: () =>
                                setState(() => _showProxy = !_showProxy),
                            icon: Icon(
                              _showProxy
                                  ? Icons.visibility_off_outlined
                                  : Icons.visibility_outlined,
                            ),
                          ),
                        ),
                      ),
                    ),
                  const SizedBox(height: 28),
                  _count('目錄請求併發', _catalog, (value) => _catalog = value),
                  const SizedBox(height: 16),
                  Text('目錄請求間隔 · $_interval 毫秒'),
                  Slider(
                    value: _interval.toDouble(),
                    min: 0,
                    max: 5000,
                    divisions: 100,
                    label: '$_interval 毫秒',
                    onChanged: _busy
                        ? null
                        : (value) => setState(() => _interval = value.round()),
                  ),
                  const SizedBox(height: 16),
                  if (_error != null)
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 12),
                      child: Text(
                        uiTraditional(_error!),
                        style: TextStyle(
                          color: Theme.of(context).colorScheme.error,
                        ),
                      ),
                    ),
                ],
              ),
            ),
          ),
  );
}
