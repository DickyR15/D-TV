import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';

import 'app_layout.dart';
import 'local_store.dart';
import 'playback_preferences.dart';

const _androidPlayerEngine = String.fromEnvironment(
  'DTV_ANDROID_PLAYER',
  defaultValue: 'mpv_embed',
);

class PlaybackSettingsScreen extends StatefulWidget {
  const PlaybackSettingsScreen({super.key, required this.store});

  final LocalStore store;

  @override
  State<PlaybackSettingsScreen> createState() => _PlaybackSettingsScreenState();
}

class _PlaybackSettingsScreenState extends State<PlaybackSettingsScreen> {
  late double _speed = widget.store.playbackPreferences.speed;
  late int _quality = widget.store.playbackPreferences.quality;
  late TelevisionDecoder _decoder =
      widget.store.playbackPreferences.televisionDecoder;
  bool? _compatibleRendering;
  final FocusNode _speedFocus = FocusNode(debugLabel: 'playback-default-speed');

  @override
  void initState() {
    super.initState();
    _loadCompatibleRendering();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && AppLayout.isTelevision(context)) {
        _speedFocus.requestFocus();
      }
    });
  }

  @override
  void dispose() {
    _speedFocus.dispose();
    super.dispose();
  }

  Future<void> _loadCompatibleRendering() async {
    if (!Platform.isAndroid) {
      if (mounted) setState(() => _compatibleRendering = false);
      return;
    }
    final value = await AppDevice.compatibleRendering();
    if (mounted) setState(() => _compatibleRendering = value);
  }

  Future<void> _persistPreferences() async {
    try {
      final current = widget.store.playbackPreferences;
      await widget.store.setPlaybackPreferences(
        current.copyWith(
          speed: _speed,
          quality: _quality,
          televisionDecoder: _decoder,
        ),
      );
    } catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('設定自動儲存失敗：$error')),
        );
      }
    }
  }

  void _updateSpeed(double value) {
    setState(() => _speed = value);
    unawaited(_persistPreferences());
  }

  void _updateQuality(int value) {
    setState(() => _quality = value);
    unawaited(_persistPreferences());
  }

  void _updateDecoder(TelevisionDecoder value) {
    setState(() => _decoder = value);
    unawaited(_persistPreferences());
  }

  Future<void> _setCompatibleRendering(bool enabled) async {
    final accepted = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text(enabled ? '開啟相容渲染模式？' : '關閉相容渲染模式？'),
        content: const Text(
          '需要重新啟動 App 才會生效，播放會中斷。Android TV 預設使用 Skia 相容渲染以避免部分電視盒黑屏；手機預設使用 Impeller。你也可以手動切換後重啟測試。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            autofocus: true,
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('立即重啟'),
          ),
        ],
      ),
    );
    if (accepted != true || !mounted) return;
    final saved = await AppDevice.setCompatibleRendering(enabled);
    if (!saved && mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('相容渲染設定儲存失敗，請重試')),
      );
    }
  }

  List<int> get _qualityOptions {
    final values = <int>{0, 360, 480, 720, 1080, 1440, 2160, 4320, _quality};
    final sorted = values.toList()..sort();
    return sorted;
  }

  String _qualityLabel(int value) => switch (value) {
    0 => '自動（優先高清）',
    _ => '${value}P',
  };

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: const Text('播放'),
    ),
    body: Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 760),
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            DropdownButtonFormField<double>(
              key: const ValueKey('playback-default-speed'),
              focusNode: _speedFocus,
              autofocus: AppLayout.isTelevision(context),
              initialValue: _speed,
              decoration: const InputDecoration(
                labelText: '預設倍速',
                helperText: '開啟影片時套用，可在播放中再調整',
              ),
              items: [
                for (final speed in playbackSpeeds)
                  DropdownMenuItem(
                    value: speed,
                    child: Text('${speed.toStringAsFixed(speed == 1 ? 1 : 2)}x'),
                  ),
              ],
              onChanged: (value) {
                if (value != null) _updateSpeed(value);
              },
            ),
            const SizedBox(height: 20),
            DropdownButtonFormField<int>(
              key: const ValueKey('playback-default-quality'),
              initialValue: _quality,
              decoration: const InputDecoration(
                labelText: '預設清晰度',
                helperText: '自動模式會依片源可用畫質選擇',
              ),
              items: [
                for (final quality in _qualityOptions)
                  DropdownMenuItem(
                    value: quality,
                    child: Text(_qualityLabel(quality)),
                  ),
              ],
              onChanged: (value) {
                if (value != null) _updateQuality(value);
              },
            ),
            const SizedBox(height: 12),
            if (Platform.isAndroid && AppLayout.isTelevision(context)) ...[
              const SizedBox(height: 20),
              DropdownButtonFormField<TelevisionDecoder>(
                key: const ValueKey('playback-tv-decoder'),
                initialValue: _decoder,
                decoration: const InputDecoration(
                  labelText: '電視解碼方式',
                  helperText: '標準 MPV 建置版本會在下一次播放套用',
                ),
                items: const [
                  DropdownMenuItem(
                    value: TelevisionDecoder.hardware,
                    child: Text('硬體解碼（建議）'),
                  ),
                  DropdownMenuItem(
                    value: TelevisionDecoder.optimized,
                    child: Text('硬體解碼（效能最佳化）'),
                  ),
                  DropdownMenuItem(
                    value: TelevisionDecoder.software,
                    child: Text('軟體解碼（相容）'),
                  ),
                ],
                onChanged: _androidPlayerEngine == 'mpv_embed'
                    ? null
                    : (value) {
                        if (value != null) _updateDecoder(value);
                      },
              ),
              if (_androidPlayerEngine == 'mpv_embed')
                const Padding(
                  padding: EdgeInsets.only(top: 8),
                  child: Text(
                    '目前預設建置使用 MPV Embed，解碼器固定為 MediaCodec；解碼方式選單只適用於標準 MPV 測試建置。',
                  ),
                ),
              const SizedBox(height: 20),
              if (_compatibleRendering != null)
                SwitchListTile(
                  key: const ValueKey('playback-compatible-rendering'),
                  contentPadding: EdgeInsets.zero,
                  title: const Text('相容渲染模式'),
                  subtitle: const Text(
                    '使用 Skia 渲染，適合排查電視盒有聲音但沒有畫面或 GPU 相容性問題',
                  ),
                  value: _compatibleRendering!,
                  onChanged: _setCompatibleRendering,
                )
              else
                const ListTile(
                  title: Text('相容渲染模式'),
                  subtitle: Text('正在讀取裝置渲染設定…'),
                ),
            ],
            const SizedBox(height: 16),
            const Text(
              '倍速、清晰度及支援的解碼方式會在變更時自動儲存。相容渲染設定會在重新啟動後生效。',
            ),
          ],
        ),
      ),
    ),
  );
}
