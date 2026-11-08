import 'package:flutter/material.dart';

import 'core_bridge.dart';
import 'local_store.dart';
import 'resource_settings_screen.dart';

var _hongguoDevicesReady = false;

class DeviceRegistrationGate extends StatefulWidget {
  const DeviceRegistrationGate({
    super.key,
    required this.repository,
    required this.store,
    required this.child,
  });

  final AppRepository repository;
  final LocalStore store;
  final Widget child;

  @override
  State<DeviceRegistrationGate> createState() => _DeviceRegistrationGateState();
}

class _DeviceRegistrationGateState extends State<DeviceRegistrationGate> {
  @override
  void initState() {
    super.initState();
    if (!_hongguoDevicesReady) {
      WidgetsBinding.instance.addPostFrameCallback((_) => _open());
    }
  }

  Future<void> _open() async {
    if (!mounted) return;
    final ready = await widget.repository.hongguoDevices().catchError((_) => false);
    if (!mounted) return;
    if (ready) {
      setState(() => _hongguoDevicesReady = true);
      return;
    }
    await showDialog<bool>(
      context: context,
      barrierDismissible: false,
      builder: (_) => _DeviceRegistrationDialog(
        repository: widget.repository,
        store: widget.store,
      ),
    );
    if (mounted && _hongguoDevicesReady) setState(() {});
  }

  @override
  Widget build(BuildContext context) => _hongguoDevicesReady
      ? widget.child
      : const Scaffold(
          body: Center(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                CircularProgressIndicator(),
                SizedBox(height: 16),
                Text('正在確認紅果主設備與輔助設備註冊狀態…'),
              ],
            ),
          ),
        );
}

class _DeviceRegistrationDialog extends StatefulWidget {
  const _DeviceRegistrationDialog({
    required this.repository,
    required this.store,
  });

  final AppRepository repository;
  final LocalStore store;

  @override
  State<_DeviceRegistrationDialog> createState() =>
      _DeviceRegistrationDialogState();
}

class _DeviceRegistrationDialogState extends State<_DeviceRegistrationDialog> {
  String? _error;

  @override
  void initState() {
    super.initState();
    _register();
  }

  Future<void> _register() async {
    if (_error != null && mounted) setState(() => _error = null);
    try {
      if (await widget.repository.hongguoDevices(register: true)) {
        _hongguoDevicesReady = true;
        if (mounted) Navigator.of(context).pop(true);
        return;
      }
      if (mounted) setState(() => _error = '紅果設備註冊尚未完成，請重試。');
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    }
  }

  Future<void> _openNetworkSettings() async {
    await Navigator.of(context).push<void>(
      MaterialPageRoute<void>(
        builder: (_) => ResourceSettingsScreen(
          repository: widget.repository,
          store: widget.store,
        ),
      ),
    );
    if (mounted) await _register();
  }

  @override
  Widget build(BuildContext context) {
    final error = _error;
    return PopScope(
      canPop: false,
      child: AlertDialog(
        title: Text(error == null ? '正在註冊紅果設備…' : '紅果設備註冊失敗'),
        content: error == null
            ? const Row(
                children: [
                  SizedBox.square(
                    dimension: 24,
                    child: CircularProgressIndicator(strokeWidth: 3),
                  ),
                  SizedBox(width: 16),
                  Expanded(child: Text('首次啟動需要完成主設備與輔助設備註冊，完成後才會載入推薦內容。')),
                ],
              )
            : Text('$error\n\n請檢查網路連線，或開啟「網路與資源」調整代理後重試。'),
        actions: error == null
            ? null
            : [
                TextButton(
                  onPressed: _openNetworkSettings,
                  child: const Text('網路設定'),
                ),
                FilledButton(
                  autofocus: true,
                  onPressed: _register,
                  child: const Text('重試'),
                ),
              ],
      ),
    );
  }
}
