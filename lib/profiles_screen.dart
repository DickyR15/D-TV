import 'dart:convert';
import 'dart:typed_data';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';

import 'app_layout.dart';
import 'local_profiles.dart';
import 'local_store.dart';
import 'ui_localization.dart';
import 'models.dart';
import 'app_build.dart';

class ProfilesScreen extends StatefulWidget {
  const ProfilesScreen({super.key, required this.store, this.locked = false});
  final LocalStore store;
  final bool locked;
  @override
  State<ProfilesScreen> createState() => _ProfilesScreenState();
}

class _ProfilesScreenState extends State<ProfilesScreen> {
  bool _busy = false;
  String? _error;
  final FocusNode _forceLoginFocus = FocusNode(
    debugLabel: 'force-login-setting',
  );
  final Map<String, FocusNode> _profileEntryFocusNodes = {};
  final Map<String, FocusNode> _profileEditFocusNodes = {};

  FocusNode _profileEntryFocus(String id) => _profileEntryFocusNodes.putIfAbsent(
    id,
    () => FocusNode(debugLabel: 'profile-entry-$id'),
  );

  FocusNode _profileEditFocus(String id) => _profileEditFocusNodes.putIfAbsent(
    id,
    () => FocusNode(debugLabel: 'profile-edit-$id'),
  );

  @override
  void dispose() {
    _forceLoginFocus.dispose();
    for (final node in _profileEntryFocusNodes.values) {
      node.dispose();
    }
    for (final node in _profileEditFocusNodes.values) {
      node.dispose();
    }
    super.dispose();
  }

  Future<void> _recover({bool export = false, bool reload = false}) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      if (reload) {
        await widget.store.reload();
      } else if (export) {
        await FilePicker.saveFile(
          fileName: '$appSlug-recovery.json',
          bytes: Uint8List.fromList(
            utf8.encode(widget.store.exportRecoveryData()),
          ),
          mimeType: 'application/json',
        );
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
        widget.store.validateBackup(content);
        if (!mounted) return;
        final accepted = await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
            title: const Text('從備份還原使用者與記錄？'),
            content: const Text(
              '這份備份會取代使用者設定、收藏清單、觀看紀錄與偏好。建議先匯出目前設定。',
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(context, false),
                child: const Text('取消'),
              ),
              FilledButton(
                onPressed: () => Navigator.pop(context, true),
                child: const Text('繼續恢復'),
              ),
            ],
          ),
        );
        if (accepted != true || !mounted) return;
        final pin = await showDialog<String>(
          context: context,
          builder: (_) => const _PasswordDialog(name: '管理員（原設定或備份中的密碼）'),
        );
        if (pin == null || !mounted) return;
        await widget.store.recoverBackup(content, pin: pin);
      }
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _switch(LocalProfile profile) async {
    String pin = '';
    if (profile.protected) {
      final value = await showDialog<String>(
        context: context,
        builder: (_) => _PasswordDialog(name: uiTraditional(profile.name)),
      );
      if (value == null || !mounted) return;
      pin = value;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await widget.store.switchProfile(profile.id, pin: pin);
      if (mounted) Navigator.of(context).popUntil((route) => route.isFirst);
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _edit([LocalProfile? profile]) async {
    await Navigator.push(
      context,
      MaterialPageRoute<void>(
        builder: (_) => ProfileEditor(store: widget.store, profile: profile),
      ),
    );
    if (mounted) setState(() {});
  }

  Future<void> _setForceLogin(bool value) async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await widget.store.setForceLogin(value);
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) {
        setState(() => _busy = false);
        // Settings writes rebuild the list. Restore the remote-control focus
        // to this tile after the frame whether the switch was enabled or not.
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted && _forceLoginFocus.canRequestFocus) {
            _forceLoginFocus.requestFocus();
          }
        });
      }
    }
  }

  String _permissionsLabel(LocalProfile profile) {
    if (profile.admin) return '管理員 · 所有權限';
    final sources = profile.sources
        .where(SourceSite.isAvailable)
        .map((id) => uiTraditional(SourceSite.byId(id).name))
        .join(' / ');
    return sources.isEmpty ? '尚未開放片源' : '可用片源：$sources';
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.store,
    builder: (_, _) => Scaffold(
      appBar: AppBar(title: Text(widget.locked ? '解鎖$appName' : '使用者管理')),
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 720),
          child: ListView(
            padding: const EdgeInsets.all(20),
            children: [
              if (widget.store.configurationError != null) ...[
                Text('需要還原本機設定', style: Theme.of(context).textTheme.titleLarge),
                const SizedBox(height: 12),
                Text(uiTraditional(widget.store.configurationError!)),
                const SizedBox(height: 12),
                FilledButton.icon(
                  onPressed: _busy ? null : () => _recover(),
                  icon: const Icon(Icons.restore_rounded),
                  label: const Text('從備份恢復'),
                ),
                OutlinedButton(
                  onPressed: _busy ? null : () => _recover(export: true),
                  child: const Text('匯出原始設定'),
                ),
                TextButton(
                  onPressed: _busy ? null : () => _recover(reload: true),
                  child: const Text('重新讀取設定'),
                ),
              ] else ...[
                Text(
                  widget.locked
                      ? '選擇使用者並輸入密碼'
                      : '目前使用者：${uiTraditional(widget.store.profile.name)}',
                  style: Theme.of(context).textTheme.titleLarge,
                ),
                const SizedBox(height: 8),
                const Text('各使用者的收藏清單與觀看紀錄獨立儲存。'),
                if (!widget.store.locked && widget.store.profile.admin) ...[
                  const SizedBox(height: 12),
                  SwitchListTile(
                    key: const ValueKey('force-login-setting'),
                    autofocus: AppLayout.isTelevision(context),
                    focusNode: _forceLoginFocus,
                    contentPadding: EdgeInsets.zero,
                    title: const Text('啟動時需要登入'),
                    subtitle: Text(
                      widget.store.forceLogin
                          ? '每次開啟應用先解鎖當前受保護使用者'
                          : '保留密碼，僅切換使用者或手動鎖定時驗證',
                    ),
                    value: widget.store.forceLogin,
                    // Keep the tile focusable during the asynchronous write.
                    // The handler ignores additional presses while busy.
                    onChanged: _setForceLogin,
                  ),
                ],
              ],
              if (_busy)
                const Padding(
                  padding: EdgeInsets.all(16),
                  child: LinearProgressIndicator(),
                ),
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
              const SizedBox(height: 16),
              for (final profile
                  in widget.store.configurationError == null
                      ? widget.store.profiles
                      : <LocalProfile>[])
                Card(
                  child: FocusTraversalGroup(
                    policy: OrderedTraversalPolicy(),
                    child: Row(
                      children: [
                        Expanded(
                          child: FocusTraversalOrder(
                            order: const NumericFocusOrder(1),
                            child: InkWell(
                              key: ValueKey('profile-open-${profile.id}'),
                              focusNode: _profileEntryFocus(profile.id),
                              borderRadius: BorderRadius.circular(12),
                              onTap: _busy ? null : () => _switch(profile),
                              child: Padding(
                                padding: const EdgeInsets.fromLTRB(16, 14, 8, 14),
                                child: Row(
                                  children: [
                                    Icon(
                                      profile.admin
                                          ? Icons.admin_panel_settings_outlined
                                          : Icons.person_outline,
                                    ),
                                    const SizedBox(width: 16),
                                    Expanded(
                                      child: Column(
                                        crossAxisAlignment: CrossAxisAlignment.start,
                                        children: [
                                          Text(uiTraditional(profile.name)),
                                          const SizedBox(height: 4),
                                          Text(
                                            _permissionsLabel(profile),
                                            style: Theme.of(context).textTheme.bodySmall,
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
                        if (!widget.store.locked && widget.store.profile.admin)
                          Padding(
                            padding: const EdgeInsets.only(right: 8),
                            child: FocusTraversalOrder(
                              order: const NumericFocusOrder(2),
                              child: IconButton(
                                key: ValueKey('profile-edit-${profile.id}'),
                                focusNode: _profileEditFocus(profile.id),
                                tooltip: '編輯使用者',
                                onPressed: _busy ? null : () => _edit(profile),
                                icon: const Icon(Icons.edit_outlined),
                              ),
                            ),
                          )
                        else
                          Padding(
                            padding: const EdgeInsets.only(right: 16),
                            child: Icon(
                              profile.protected
                                  ? Icons.lock_outline
                                  : Icons.chevron_right,
                            ),
                          ),
                      ],
                    ),
                  ),
                ),
              if (!widget.store.locked && widget.store.profile.admin) ...[
                const SizedBox(height: 16),
                FilledButton.icon(
                  onPressed: _busy ? null : () => _edit(),
                  icon: const Icon(Icons.person_add_outlined),
                  label: const Text('新增使用者'),
                ),
              ],
              if (!widget.store.locked && widget.store.profile.protected)
                TextButton.icon(
                  onPressed: () {
                    Navigator.of(context).popUntil((route) => route.isFirst);
                    widget.store.lock();
                  },
                  icon: const Icon(Icons.lock_outline),
                  label: const Text('鎖定當前使用者'),
                ),
            ],
          ),
        ),
      ),
    ),
  );
}

class _PasswordDialog extends StatefulWidget {
  const _PasswordDialog({required this.name});
  final String name;
  @override
  State<_PasswordDialog> createState() => _PasswordDialogState();
}

class _PasswordDialogState extends State<_PasswordDialog> {
  final _pin = TextEditingController();
  @override
  void dispose() {
    _pin.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text('登入 ${widget.name}'),
    content: TextField(
      controller: _pin,
      autofocus: true,
      obscureText: true,
      decoration: const InputDecoration(labelText: '密碼'),
      onSubmitted: (value) => Navigator.pop(context, value),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('取消'),
      ),
      FilledButton(
        onPressed: () => Navigator.pop(context, _pin.text),
        child: const Text('登入'),
      ),
    ],
  );
}

class ProfileEditor extends StatefulWidget {
  const ProfileEditor({super.key, required this.store, this.profile});
  final LocalStore store;
  final LocalProfile? profile;
  @override
  State<ProfileEditor> createState() => _ProfileEditorState();
}

class _ProfileEditorState extends State<ProfileEditor> {
  late final _name = TextEditingController(text: widget.profile?.name ?? '');
  final _pin = TextEditingController(), _confirm = TextEditingController();
  late final _sources =
      (widget.profile?.sources ??
              widget.store.sources.map((s) => s.id).toList())
          .toSet();
  late bool _download = widget.profile?.download ?? false;
  bool _clearPin = false, _busy = false;
  String? _error;
  @override
  void dispose() {
    _name.dispose();
    _pin.dispose();
    _confirm.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    setState(() {
      _error = null;
      _busy = true;
    });
    try {
      if (_pin.text != _confirm.text) throw StateError('兩次輸入的密碼不一致');
      await widget.store.saveProfile(
        id: widget.profile?.id,
        name: _name.text,
        sources: _sources.toList(),
        download: _download,
        pin: _clearPin
            ? ''
            : _pin.text.isEmpty
            ? null
            : _pin.text,
      );
      if (mounted) Navigator.pop(context);
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _delete() async {
    final yes = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('刪除此使用者？'),
        content: const Text('會同時刪除該使用者的收藏清單與觀看紀錄。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('刪除'),
          ),
        ],
      ),
    );
    if (yes != true || !mounted) return;
    try {
      await widget.store.deleteProfile(widget.profile!.id);
      if (mounted) Navigator.pop(context);
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: Text(widget.profile == null ? '新增使用者' : '編輯使用者')),
    body: Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 640),
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            TextField(
              key: const ValueKey('profile-editor-name'),
              controller: _name,
              maxLength: 40,
              textInputAction: TextInputAction.next,
              decoration: const InputDecoration(labelText: '使用者名稱'),
            ),
            const SizedBox(height: 12),
            TextField(
              key: const ValueKey('profile-editor-pin'),
              controller: _pin,
              obscureText: true,
              textInputAction: TextInputAction.next,
              decoration: const InputDecoration(
                labelText: '設定密碼（至少 6 位）',
                helperText: '留空保留原密碼',
              ),
            ),
            const SizedBox(height: 12),
            TextField(
              key: const ValueKey('profile-editor-confirm-pin'),
              controller: _confirm,
              obscureText: true,
              textInputAction: TextInputAction.done,
              onSubmitted: (_) {
                if (!_busy) _save();
              },
              decoration: const InputDecoration(labelText: '再次輸入密碼'),
            ),
            if (widget.profile?.protected == true &&
                widget.profile?.admin != true)
              CheckboxListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('取消此使用者密碼'),
                value: _clearPin,
                onChanged: (v) => setState(() => _clearPin = v!),
              ),
            const SizedBox(height: 20),
            if (widget.profile?.admin != true) ...[
              Text('允許使用的片源', style: Theme.of(context).textTheme.titleMedium),
              for (final source in SourceSite.values)
                CheckboxListTile(
                  contentPadding: EdgeInsets.zero,
                  controlAffinity: ListTileControlAffinity.leading,
                  title: Text(uiTraditional(source.name)),
                  value: _sources.contains(source.id),
                  onChanged: (value) {
                    setState(() {
                      if (value == true) {
                        _sources.add(source.id);
                      } else {
                        _sources.remove(source.id);
                      }
                    });
                  },
                ),
            ] else
              const Text('管理員可存取所有片源及功能。建立其他使用者前，請先設定管理員密碼。'),
            if (_error != null)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 12),
                child: Text(
                  uiTraditional(_error!),
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
              ),
            const SizedBox(height: 24),
            FilledButton(
              key: const ValueKey('profile-editor-save'),
              autofocus: true,
              onPressed: _busy ? null : _save,
              child: Text(_busy ? '正在儲存…' : '儲存'),
            ),
            if (widget.profile != null && !widget.profile!.admin)
              TextButton(
                onPressed: _busy ? null : _delete,
                child: const Text('刪除使用者'),
              ),
          ],
        ),
      ),
    ),
  );
}
