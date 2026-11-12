import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';

import 'app_layout.dart';
import 'local_store.dart';
import 'playback_preferences.dart';
import 'portrait_blur_policy.dart';
import 'widgets.dart';

const _androidPlayerEngine = String.fromEnvironment(
  'DTV_ANDROID_PLAYER',
  defaultValue: 'mpv_embed',
);

class PlaybackSettingsScreen extends StatefulWidget {
  const PlaybackSettingsScreen({
    super.key,
    required this.store,
    this.portraitBlurSupported,
  });

  final LocalStore store;
  final bool? portraitBlurSupported;

  @override
  State<PlaybackSettingsScreen> createState() => _PlaybackSettingsScreenState();
}

class _PlaybackSettingsScreenState extends State<PlaybackSettingsScreen> {
  late double _speed = widget.store.playbackPreferences.speed;
  late int _quality = widget.store.playbackPreferences.quality;
  late PlaybackAspectRatio _aspectRatio = widget.store.playbackPreferences.aspectRatio;
  late TelevisionDecoder _decoder =
      widget.store.playbackPreferences.televisionDecoder;
  late bool _portraitBlurBackground =
      widget.store.playbackPreferences.portraitBlurBackground;
  bool get _portraitBlurSupported =>
      widget.portraitBlurSupported ??
      supportsPortraitBlurBackground(
        android: Platform.isAndroid,
        windows: Platform.isWindows,
        androidMpvConfigured:
            _androidPlayerEngine != 'exo' || !AppLayout.isTelevisionDevice(context),
      );
  late bool _preloadEnabled;
  late bool _autoAdvance;
  late bool _autoPictureInPicture;
  bool _pictureInPictureSupported = false;
  bool _playbackValuesInitialized = false;
  bool? _compatibleRendering;
  final FocusNode _speedFocus = FocusNode(debugLabel: 'playback-default-speed');

  @override
  void initState() {
    super.initState();
    _loadCompatibleRendering();
    _loadPictureInPictureSupport();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && AppLayout.isTelevision(context)) {
        _speedFocus.requestFocus();
      }
    });
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final size = MediaQuery.maybeOf(context)?.size ?? Size.zero;
    final landscape = size.width > size.height;
    if (landscape &&
        _portraitBlurSupported &&
        widget.store.needsPortraitBlurLandscapeDefaultMigration) {
      _portraitBlurBackground = true;
      unawaited(widget.store.migratePortraitBlurLandscapeDefault());
    }
    if (_playbackValuesInitialized) return;
    final preferences = widget.store.playbackPreferences;
    _preloadEnabled = preferences.preloads(
      television: AppLayout.isTelevision(context),
    );
    _autoAdvance = preferences.autoAdvance;
    _autoPictureInPicture = preferences.autoPictureInPicture;
    _playbackValuesInitialized = true;
  }

  @override
  void dispose() {
    _speedFocus.dispose();
    super.dispose();
  }

  Future<void> _loadPictureInPictureSupport() async {
    if (!Platform.isAndroid) return;
    try {
      final status = await AppDevice.channel.invokeMapMethod<String, dynamic>(
        'pictureInPictureStatus',
      );
      if (mounted) {
        setState(() => _pictureInPictureSupported = status?['supported'] == true);
      }
    } catch (_) {
      if (mounted) setState(() => _pictureInPictureSupported = false);
    }
  }

  Future<void> _loadCompatibleRendering() async {
    if (!Platform.isAndroid) {
      if (mounted) setState(() => _compatibleRendering = false);
      return;
    }
    final value = await AppDevice.compatibleRendering();
    if (mounted) setState(() => _compatibleRendering = value);
  }

  Future<void> _persistPreferences({VoidCallback? onSaved}) async {
    try {
      final current = widget.store.playbackPreferences;
      await widget.store.setPlaybackPreferences(
        current.copyWith(
          speed: _speed,
          quality: _quality,
          televisionDecoder: _decoder,
          aspectRatio: _aspectRatio,
          portraitBlurBackground: _portraitBlurBackground,
          preload: _preloadEnabled,
          autoAdvance: _autoAdvance,
          autoPictureInPicture: _autoPictureInPicture,
        ),
      );
      if (mounted) onSaved?.call();
    } catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('設定自動儲存失敗：$error')),
        );
      }
    }
  }

  void _updateAspectRatio(PlaybackAspectRatio value) {
    setState(() => _aspectRatio = value);
    unawaited(_persistPreferences());
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
    unawaited(
      _persistPreferences(
        onSaved: () {
          showDtvToast(
            context,
            '解碼方式已儲存；重新進入播放頁面即可套用，無須重啟 D TV。',
          );
        },
      ),
    );
  }

  void _updatePortraitBlurBackground(bool value) {
    setState(() => _portraitBlurBackground = value);
    unawaited(_persistPreferences());
  }

  void _updatePreload(bool value) {
    setState(() => _preloadEnabled = value);
    unawaited(_persistPreferences());
  }

  void _updateAutoAdvance(bool value) {
    setState(() => _autoAdvance = value);
    unawaited(_persistPreferences());
  }

  void _updateAutoPictureInPicture(bool value) {
    setState(() => _autoPictureInPicture = value);
    unawaited(_persistPreferences());
  }

  Future<void> _setCompatibleRendering(bool enabled) async {
    final accepted = await showDialog<bool>(
      context: context,
      builder: (_) => CompatibleRenderingRestartDialog(enabled: enabled),
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
            const SizedBox(height: 20),
            DropdownButtonFormField<PlaybackAspectRatio>(
              key: const ValueKey('playback-aspect-ratio'),
              initialValue: _aspectRatio,
              decoration: const InputDecoration(
                labelText: '畫面比例',
                helperText: '決定影片在播放區域的顯示方式，不會改變來源清晰度',
              ),
              items: [
                for (final value in PlaybackAspectRatio.values)
                  DropdownMenuItem(
                    value: value,
                    child: Text(playbackAspectRatioLabel(value)),
                  ),
              ],
              onChanged: (value) {
                if (value != null) _updateAspectRatio(value);
              },
            ),
            const SizedBox(height: 20),
            DropdownButtonFormField<TelevisionDecoder>(
              key: const ValueKey('playback-decoder'),
              initialValue: _decoder,
              decoration: const InputDecoration(
                labelText: '解碼方式',
                helperText: '自動依裝置選擇解碼方式；也可手動指定硬體解碼或軟體解碼。變更後重新進入播放頁面生效，無須重啟 D TV。',
              ),
              items: const [
                DropdownMenuItem(
                  value: TelevisionDecoder.automatic,
                  child: Text('自動'),
                ),
                DropdownMenuItem(
                  value: TelevisionDecoder.hardware,
                  child: Text('硬體解碼'),
                ),
                DropdownMenuItem(
                  value: TelevisionDecoder.software,
                  child: Text('軟體解碼'),
                ),
              ],
              onChanged: Platform.isAndroid &&
                      AppLayout.isTelevisionDevice(context) &&
                      _androidPlayerEngine == 'exo'
                  ? null
                  : (value) {
                      if (value != null) _updateDecoder(value);
                    },
            ),
            if (Platform.isAndroid &&
                AppLayout.isTelevisionDevice(context) &&
                _androidPlayerEngine == 'exo')
              const Padding(
                padding: EdgeInsets.only(top: 8),
                child: Text('目前使用 ExoPlayer，解碼方式由 Android 管理，無法手動切換。'),
              ),
            const SizedBox(height: 8),
            SwitchListTile(
              key: const ValueKey('playback-portrait-blur-background'),
              contentPadding: EdgeInsets.zero,
              title: const Text('直式影片模糊背景'),
              subtitle: Text(
                _portraitBlurSupported
                    ? '橫式畫面播放直式影片時，以同一個播放器影格合成左右模糊背景與清晰前景，不另開解碼器。'
                    : 'Android 需使用 MPV 播放引擎；目前平台或 ExoPlayer 不支援。',
              ),
              value: _portraitBlurSupported && _portraitBlurBackground,
              onChanged: _portraitBlurSupported
                  ? _updatePortraitBlurBackground
                  : null,
            ),
            SwitchListTile(
              key: const ValueKey('playback-preopen-next-episode'),
              contentPadding: EdgeInsets.zero,
              title: const Text('下一集預載入'),
              subtitle: const Text('提前取得下一集的播放資料與網址，不會下載整集影片'),
              value: _preloadEnabled,
              onChanged: _updatePreload,
            ),
            SwitchListTile(
              key: const ValueKey('playback-auto-advance'),
              contentPadding: EdgeInsets.zero,
              title: const Text('自動連播'),
              subtitle: const Text('播放結束後自動播放下一集'),
              value: _autoAdvance,
              onChanged: _updateAutoAdvance,
            ),
            if (supportsPictureInPicture(
              android: Platform.isAndroid,
              systemSupported: _pictureInPictureSupported,
              television: AppLayout.isTelevisionDevice(context),
            ))
              SwitchListTile(
                key: const ValueKey('playback-auto-picture-in-picture'),
                contentPadding: EdgeInsets.zero,
                title: const Text('離開應用程式時自動進入子母畫面'),
                subtitle: const Text('播放中離開應用程式或切換其他應用時進入系統子母畫面；也可在播放控制列手動開啟'),
                value: _autoPictureInPicture,
                onChanged: _updateAutoPictureInPicture,
              ),
            const SizedBox(height: 20),
            if (Platform.isAndroid && AppLayout.isTelevision(context)) ...[
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
              '倍速、清晰度、畫面比例、解碼方式、直式影片模糊背景、下一集預載入及自動連播會在變更時自動儲存；模糊背景使用同一個即時影格，不會啟動第二個解碼器。',
            ),
          ],
        ),
      ),
    ),
  );
}


class CompatibleRenderingRestartDialog extends StatefulWidget {
  const CompatibleRenderingRestartDialog({super.key, required this.enabled});

  final bool enabled;

  @override
  State<CompatibleRenderingRestartDialog> createState() =>
      _CompatibleRenderingRestartDialogState();
}

class _CompatibleRenderingRestartDialogState
    extends State<CompatibleRenderingRestartDialog> {
  final FocusNode _cancelFocus =
      FocusNode(debugLabel: 'compatible-rendering-cancel');
  final FocusNode _restartFocus =
      FocusNode(debugLabel: 'compatible-rendering-restart');
  bool _initialFocusRequested = false;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_initialFocusRequested) return;
    _initialFocusRequested = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) _cancelFocus.requestFocus();
    });
  }

  @override
  void dispose() {
    _cancelFocus.dispose();
    _restartFocus.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => FocusTraversalGroup(
        policy: OrderedTraversalPolicy(),
        child: AlertDialog(
          title: Text(
            widget.enabled ? '開啟相容渲染模式？' : '關閉相容渲染模式？',
          ),
          content: const Text(
            '需要重新啟動 App 才會生效，播放會中斷。Android TV 預設使用 Skia 相容渲染以避免部分電視盒黑屏；手機預設使用 Impeller。你也可以手動切換後重啟測試。',
          ),
          actions: [
            FocusTraversalOrder(
              order: const NumericFocusOrder(1),
              child: TextButton(
                key: const ValueKey('compatible-rendering-cancel'),
                focusNode: _cancelFocus,
                autofocus: true,
                onPressed: () => Navigator.of(context).pop(false),
                child: const Text('取消'),
              ),
            ),
            FocusTraversalOrder(
              order: const NumericFocusOrder(2),
              child: FilledButton(
                key: const ValueKey('compatible-rendering-restart'),
                focusNode: _restartFocus,
                onPressed: () => Navigator.of(context).pop(true),
                child: const Text('立即重啟'),
              ),
            ),
          ],
        ),
      );
}
