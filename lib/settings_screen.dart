import 'dart:convert';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';

import 'core_bridge.dart';
import 'app_theme.dart';
import 'local_store.dart';
import 'profiles_screen.dart';
import 'app_build.dart';
import 'sources_screen.dart';
import 'ui_localization.dart';
import 'widgets.dart';
import 'resource_settings_screen.dart';
import 'lan_screen.dart';
import 'diary_service.dart';

class SettingsScreen extends StatefulWidget {
  const SettingsScreen({
    super.key,
    required this.repository,
    required this.store,
    this.embedded = false,
  });
  final AppRepository repository;
  final LocalStore store;
  final bool embedded;
  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  bool _busy = false;
  String? _message;

  Future<void> _chooseTheme() async {
    final selected = await showDialog<String>(
      context: context,
      builder: (context) => SimpleDialog(
        title: const Text('外觀主題'),
        children: [
          RadioGroup<String>(
            groupValue: widget.store.themeMode,
            onChanged: (value) => Navigator.pop(context, value),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                for (final mode in ['light', 'dark', 'system'])
                  RadioListTile<String>(
                    value: mode,
                    title: Text(AppTheme.label(mode)),
                    subtitle: mode == 'system'
                        ? const Text('隨裝置的深色模式自動切換')
                        : null,
                  ),
              ],
            ),
          ),
        ],
      ),
    );
    if (selected != null && mounted) {
      await saveUserChange(context, () => widget.store.setThemeMode(selected));
    }
  }

  Future<void> _backup(bool restore) async {
    setState(() {
      _busy = true;
      _message = null;
    });
    try {
      if (!restore) {
        final content = await widget.store.exportBackup();
        final saved = await FilePicker.saveFile(
          fileName:
              '$appSlug-backup-${DateTime.now().toIso8601String().substring(0, 10)}.json',
          bytes: Uint8List.fromList(utf8.encode(content)),
          mimeType: 'application/json',
        );
        if (saved != null && mounted) setState(() => _message = '備份已儲存');
      } else {
        final file = await FilePicker.pickFile(
          type: FileType.custom,
          allowedExtensions: ['json'],
        );
        if (file == null || !mounted) return;
        final size = await file.length();
        if (size == null || size > 8 * 1024 * 1024) {
          throw const FormatException('備份檔案過大或無法讀取');
        }
        final content = utf8.decode(await file.readAsBytes());
        final data = widget.store.validateBackup(content);
        if (!mounted) return;
        final accepted = await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
            title: const Text('還原備份？'),
            content: Text(
              '包含 ${(data['profiles'] as List).length} 個使用者。還原會取代本機使用者、追劇清單、觀看紀錄與偏好設定；其他影片檔案不會一併處理。完成後請使用備份內的管理員密碼登入。',
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(context, false),
                child: const Text('取消'),
              ),
              FilledButton(
                onPressed: () => Navigator.pop(context, true),
                child: const Text('恢復'),
              ),
            ],
          ),
        );
        if (accepted != true || !mounted) return;
        await widget.store.importBackup(content);
        if (mounted) Navigator.of(context).popUntil((route) => route.isFirst);
      }
    } catch (error) {
      if (mounted) setState(() => _message = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.store,
    builder: (_, _) => Scaffold(
      appBar: widget.embedded ? null : AppBar(title: const Text('設定與備份')),
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 720),
          child: ListView(
            padding: const EdgeInsets.all(16),
            children: [
              ListTile(
                key: const ValueKey('lan-settings'),
                leading: const Icon(Icons.devices_rounded),
                title: const Text('裝置互聯'),
                subtitle: const Text('區域網自動同步追劇與推送播放'),
                trailing: const Icon(Icons.chevron_right_rounded),
                onTap: () => openLanSync(context),
              ),
              if (widget.repository.supportsSourceManagement)
                ListTile(
                  leading: const Icon(Icons.dns_outlined),
                  title: const Text('片源管理'),
                  subtitle: const Text('獨立更新、連線與播放檢測'),
                  trailing: const Icon(Icons.chevron_right_rounded),
                  onTap: () => Navigator.push(
                    context,
                    MaterialPageRoute<void>(
                      builder: (_) => SourcesScreen(
                        repository: widget.repository,
                        store: widget.store,
                      ),
                    ),
                  ),
                ),
              ListTile(
                key: const ValueKey('theme-setting'),
                leading: const Icon(Icons.palette_outlined),
                title: const Text('外觀主題'),
                subtitle: Text(AppTheme.label(widget.store.themeMode)),
                trailing: const Icon(Icons.chevron_right_rounded),
                onTap: _chooseTheme,
              ),
              ListTile(
                leading: const Icon(Icons.people_outline),
                title: const Text('使用者管理'),
                subtitle: Text('目前：${uiTraditional(widget.store.profile.name)}'),
                onTap: () => Navigator.push(
                  context,
                  MaterialPageRoute<void>(
                    builder: (_) => ProfilesScreen(store: widget.store),
                  ),
                ),
              ),
              ListTile(
                key: const ValueKey('diary-setting'),
                leading: const Icon(Icons.receipt_long_rounded),
                title: const Text('播放除錯日記'),
                subtitle: const Text('檢視並複製應用執行與播放器的詳細事件日誌'),
                trailing: const Icon(Icons.chevron_right_rounded),
                onTap: () => DiaryService.showDiaryDialog(context),
              ),
              if (widget.store.profile.admin) ...[
                ListTile(
                  leading: const Icon(Icons.settings_ethernet_rounded),
                  title: const Text('網路與資源'),
                  subtitle: const Text('代理、目錄請求間隔與片源目錄'),
                  trailing: const Icon(Icons.chevron_right_rounded),
                  onTap: () => Navigator.push(
                    context,
                    MaterialPageRoute<void>(
                      builder: (_) => ResourceSettingsScreen(
                        repository: widget.repository,
                        store: widget.store,
                      ),
                    ),
                  ),
                ),
                SwitchListTile(
                  value: widget.store.exportPosters,
                  title: const Text('同時匯出海報檔案'),
                  subtitle: const Text('預設只寫海報 URL。源站海報需要解密或外部讀取失敗時可開啟。'),
                  onChanged: _busy
                      ? null
                      : (value) => saveUserChange(
                          context,
                          () => widget.store.setExportPosters(value),
                        ),
                ),
                ListTile(
                  leading: const Icon(Icons.backup_outlined),
                  title: const Text('匯出設定備份'),
                  subtitle: const Text('包含本機使用者、追劇清單、觀看紀錄與設定，不含影片檔案'),
                  onTap: _busy ? null : () => _backup(false),
                ),
                ListTile(
                  leading: const Icon(Icons.restore),
                  title: const Text('還原設定備份'),
                  onTap: _busy ? null : () => _backup(true),
                ),
              ],
              if (_busy) const LinearProgressIndicator(),
              if (_message != null)
                Padding(
                  padding: const EdgeInsets.all(16),
                  child: SelectableText(uiTraditional(_message!)),
                ),
            ],
          ),
        ),
      ),
    ),
  );
}

