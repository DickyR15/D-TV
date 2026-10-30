import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'core_bridge.dart';
import 'app_theme.dart';
import 'app_layout.dart';
import 'local_store.dart';
import 'profiles_screen.dart';
import 'source_management_screen.dart';
import 'app_build.dart';
import 'ui_localization.dart';
import 'widgets.dart';
import 'resource_settings_screen.dart';
import 'lan_screen.dart';
import 'diary_service.dart';
import 'playback_settings_screen.dart';

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
  final FocusNode _deviceInterconnectFocus =
      FocusNode(debugLabel: 'settings-device-interconnect');
  bool _initialFocusScheduled = false;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_initialFocusScheduled || !AppLayout.isTelevision(context)) return;
    _initialFocusScheduled = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && _deviceInterconnectFocus.canRequestFocus) {
        _deviceInterconnectFocus.requestFocus();
      }
    });
  }

  @override
  void dispose() {
    _deviceInterconnectFocus.dispose();
    super.dispose();
  }
  Future<void> _chooseDisplayMode() async {
    final selection = await AppLayout.chooseDisplayMode(
      context,
      currentMode: widget.store.displayMode,
    );
    if (selection == null || !mounted) return;
    await saveUserChange(
      context,
      () => widget.store.setDisplayMode(selection),
    );
    if (mounted) setState(() => _message = null);
  }


  Future<void> _chooseTheme() async {
    final current = widget.store.themeMode;
    final selected = await showDialog<String>(
      context: context,
      builder: (dialogContext) => SimpleDialog(
        title: const Text('外觀主題'),
        children: [
          for (final mode in ['light', 'dark', 'system'])
            ListTile(
              autofocus: mode == current && AppLayout.isTelevision(context),
              selected: mode == current,
              leading: Icon(
                mode == current
                    ? Icons.radio_button_checked_rounded
                    : Icons.radio_button_off_rounded,
              ),
              title: Text(AppTheme.label(mode)),
              subtitle: mode == 'system'
                  ? const Text('隨裝置的深色模式自動切換')
                  : null,
              onTap: () => Navigator.of(dialogContext).pop(mode),
            ),
        ],
      ),
    );
    if (selected == null || !mounted || selected == widget.store.themeMode) {
      return;
    }
    try {
      await widget.store.setThemeMode(selected);
      if (mounted) setState(() => _message = null);
    } catch (error) {
      if (mounted) setState(() => _message = '外觀主題切換失敗：$error');
    }
  }

  Future<void> _chooseThemeAccent() async {
    final selected = await showDialog<String>(
      context: context,
      builder: (dialogContext) => SimpleDialog(
        title: const Text('介面配色'),
        children: [
          for (final tone in AppTheme.accentOptions)
            ListTile(
              autofocus: tone == widget.store.themeAccent &&
                  AppLayout.isTelevision(context),
              selected: tone == widget.store.themeAccent,
              leading: CircleAvatar(
                radius: 13,
                backgroundColor: AppTheme.accentPreview(tone),
                child: tone == widget.store.themeAccent
                    ? const Icon(Icons.check_rounded, size: 16, color: Colors.white)
                    : null,
              ),
              title: Text(AppTheme.accentLabel(tone)),
              subtitle: tone == 'system'
                  ? const Text('Android 12 以上支援系統動態色彩')
                  : null,
              onTap: () => Navigator.of(dialogContext).pop(tone),
            ),
        ],
      ),
    );
    if (selected == null || !mounted || selected == widget.store.themeAccent) {
      return;
    }
    try {
      await widget.store.setThemeAccent(selected);
      if (mounted) setState(() => _message = null);
    } catch (error) {
      if (mounted) setState(() => _message = '介面配色切換失敗：$error');
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
        if (Platform.isAndroid) {
          // Android TV images may not include a document-picker activity.
          // Save directly to Download/D-TV through the native MediaStore API.
          final saved = await const MethodChannel('duanju/device')
              .invokeMapMethod<String, dynamic>(
            'exportSettingsBackup',
            <String, dynamic>{'content': content},
          );
          if (mounted) {
            final path = saved?['path'] as String? ?? 'Download/D-TV';
            setState(() => _message = '備份已儲存：$path');
            await showDialog<void>(
              context: context,
              builder: (dialogContext) => AlertDialog(
                title: const Text('設定備份匯出成功'),
                content: SelectableText(
                  '已儲存設定備份：\n$path\n\n'
                  '請從檔案管理器開啟 Download/D-TV 資料夾，'
                  '將 JSON 備份檔複製到電腦保存。',
                ),
                actions: [
                  TextButton(
                    onPressed: () {
                      Clipboard.setData(ClipboardData(text: path));
                      Navigator.of(dialogContext).pop();
                    },
                    child: const Text('複製檔案位置'),
                  ),
                  FilledButton(
                    autofocus: true,
                    onPressed: () => Navigator.of(dialogContext).pop(),
                    child: const Text('完成'),
                  ),
                ],
              ),
            );
          }
        } else {
          final saved = await FilePicker.saveFile(
            fileName:
                '$appSlug-backup-${DateTime.now().toIso8601String().substring(0, 10)}.json',
            bytes: Uint8List.fromList(utf8.encode(content)),
            mimeType: 'application/json',
          );
          if (saved != null && mounted) setState(() => _message = '備份已儲存');
        }
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
              '包含 ${(data['profiles'] as List).length} 個使用者。還原會取代本機使用者、收藏清單、觀看紀錄與偏好設定。完成後請使用備份內的管理員密碼登入。',
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
      if (mounted) {
        setState(() => _message = error.toString());
        await showDialog<void>(
          context: context,
          builder: (dialogContext) => AlertDialog(
            title: Text(restore ? '還原設定備份失敗' : '匯出設定備份失敗'),
            content: SelectableText(error.toString()),
            actions: [
              FilledButton(
                autofocus: true,
                onPressed: () => Navigator.of(dialogContext).pop(),
                child: const Text('確定'),
              ),
            ],
          ),
        );
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    // Pin the author row to the bottom in TV mode even when SettingsScreen is
    // embedded inside the home shell. In the embedded TV route, an inert row
    // at the end of ListView cannot reliably receive D-pad focus to scroll into
    // view, so it must not depend on list navigation.
    final pinAuthor = AppLayout.isTelevision(context);
    final settings = AnimatedBuilder(
    animation: widget.store,
    builder: (_, _) => Scaffold(
      appBar: widget.embedded ? null : AppBar(title: const Text('設定與備份')),
      bottomNavigationBar: pinAuthor
          ? SafeArea(
              top: false,
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 16),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    const Divider(height: 1),
                    ListTile(
                      key: const ValueKey('app-author'),
                      dense: true,
                      visualDensity: VisualDensity.compact,
                      leading: const Icon(Icons.person_outline_rounded),
                      title: const Text('作者'),
                      subtitle: Text(appAuthor),
                    ),
                  ],
                ),
              ),
            )
          : null,
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 720),
          child: ListView(
            padding: const EdgeInsets.all(16),
            children: [
              ListTile(
                key: const ValueKey('lan-settings'),
                focusNode: _deviceInterconnectFocus,
                leading: const Icon(Icons.devices_rounded),
                title: const Text('裝置互聯'),
                subtitle: const Text('區域網自動同步收藏與推送播放'),
                trailing: const Icon(Icons.chevron_right_rounded),
                onTap: () => openLanSync(context),
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
                key: const ValueKey('theme-accent-setting'),
                leading: const Icon(Icons.color_lens_outlined),
                title: const Text('介面配色'),
                subtitle: Text(AppTheme.accentLabel(widget.store.themeAccent)),
                trailing: const Icon(Icons.chevron_right_rounded),
                onTap: _chooseThemeAccent,
              ),
              ListTile(
                key: const ValueKey('display-mode-setting'),
                leading: const Icon(Icons.devices_rounded),
                title: const Text('介面模式'),
                subtitle: Text(
                  AppLayout.displayModeLabel(
                    widget.store.displayMode,
                    currentFactor: AppLayout.formFactorOf(context),
                  ),
                ),
                trailing: const Icon(Icons.chevron_right_rounded),
                onTap: _chooseDisplayMode,
              ),
              ListTile(
                key: const ValueKey('playback-setting'),
                leading: const Icon(Icons.tune_rounded),
                title: const Text('播放'),
                subtitle: const Text('預設倍速、清晰度、解碼方式與相容渲染'),
                trailing: const Icon(Icons.chevron_right_rounded),
                onTap: () => Navigator.push(
                  context,
                  MaterialPageRoute<void>(
                    builder: (_) => PlaybackSettingsScreen(store: widget.store),
                  ),
                ),
              ),
              if (widget.store.profile.admin)
                ListTile(
                  key: const ValueKey('source-management-setting'),
                  leading: const Icon(Icons.source_outlined),
                  title: const Text('片源管理'),
                  subtitle: const Text('管理一般片源；進階片源需輸入管理員密碼解鎖'),
                  trailing: const Icon(Icons.chevron_right_rounded),
                  onTap: () => Navigator.push(
                    context,
                    MaterialPageRoute<void>(
                      builder: (_) => SourceManagementScreen(store: widget.store),
                    ),
                  ),
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
                title: const Text('播放除錯紀錄'),
                subtitle: const Text('檢視並複製應用執行與播放器的詳細事件紀錄'),
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
                ListTile(
                  leading: const Icon(Icons.backup_outlined),
                  title: const Text('匯出設定備份'),
                  subtitle: const Text('包含本機使用者、收藏清單、觀看紀錄與設定'),
                  onTap: _busy ? null : () => _backup(false),
                ),
                ListTile(
                  key: const ValueKey('restore-settings-backup'),
                  leading: const Icon(Icons.restore),
                  title: const Text('還原設定備份'),
                  subtitle: const Text(
                    '取代本機使用者、收藏清單、觀看紀錄與偏好設定',
                  ),
                  onTap: _busy ? null : () => _backup(true),
                ),
              ],
              if (_busy) const LinearProgressIndicator(),
              if (_message != null)
                Padding(
                  padding: const EdgeInsets.all(16),
                  child: SelectableText(uiTraditional(_message!)),
                ),
              if (!pinAuthor) ...[
                const Divider(height: 24),
                ListTile(
                  key: const ValueKey('app-author'),
                  leading: const Icon(Icons.person_outline_rounded),
                  title: const Text('作者'),
                  subtitle: Text(appAuthor),
                ),
              ],
            ],
          ),
        ),
      ),
    ),
  );
    if (widget.embedded || !AppLayout.isTelevision(context)) {
      return settings;
    }
    void closeSettings() {
      final navigator = Navigator.of(context);
      if (navigator.canPop()) navigator.pop();
    }
    return CallbackShortcuts(
      bindings: {
        const SingleActivator(LogicalKeyboardKey.goBack): closeSettings,
        const SingleActivator(LogicalKeyboardKey.escape): closeSettings,
      },
      child: settings,
    );
  }
}

