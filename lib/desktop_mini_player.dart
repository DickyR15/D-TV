import 'dart:async';

import 'package:flutter/material.dart';
import 'package:media_kit/media_kit.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:window_manager/window_manager.dart';

import 'app_layout.dart';
import 'app_orientation.dart';
import 'app_theme.dart';
import 'core_bridge.dart';
import 'desktop_mini_player_bridge.dart';
import 'local_store.dart';
import 'player_screen.dart';
import 'ui_localization.dart';

/// A second Flutter engine owns the desktop mini-player. The main window stays
/// paused behind it and resumes at the latest reported episode/position.
class DesktopMiniPlayerApp extends StatefulWidget {
  const DesktopMiniPlayerApp({
    super.key,
    required this.arguments,
  });

  final DesktopMiniPlayerArguments arguments;

  @override
  State<DesktopMiniPlayerApp> createState() => _DesktopMiniPlayerAppState();
}

class _DesktopMiniPlayerAppState extends State<DesktopMiniPlayerApp>
    with WindowListener {
  final NativeRepository _repository = NativeRepository();
  LocalStore? _store;
  Object? _error;
  final DesktopMiniPlayerShutdown _shutdown = DesktopMiniPlayerShutdown();
  final DesktopMiniPlayerStartupGate _startupGate =
      DesktopMiniPlayerStartupGate();
  Completer<void>? _playerDisposed;
  bool _closing = false;
  bool _restoreMainWindowOnClose = true;
  String? _closeError;
  int _episodeIndex = 0;
  double _position = 0;
  DateTime _lastProgressMessage = DateTime.fromMillisecondsSinceEpoch(0);

  @override
  void initState() {
    super.initState();
    _episodeIndex = widget.arguments.initialIndex;
    _position = widget.arguments.initialPosition;
    windowManager.addListener(this);
    unawaited(windowManager.setPreventClose(true));
    unawaited(_start());
  }

  Future<void> _start() async {
    try {
      await _channelReady();
      try {
        await initializeUiLocalization();
      } catch (_) {
        // Keep the Taiwanese fallback translations if OpenCC is unavailable.
      }
      await _initialize();
    } catch (error) {
      if (mounted) setState(() => _error = error);
      try {
        await desktopMiniPlayerChannel.invokeMethod<void>(
          'mini_error',
          {'message': error.toString()},
        );
      } catch (_) {}
    }
  }

  Future<void> _channelReady() async {
    await desktopMiniPlayerChannel.setMethodCallHandler((call) async {
      if (call.method == 'close') {
        final arguments = call.arguments;
        if (arguments is Map && arguments['restoreMain'] == false) {
          _restoreMainWindowOnClose = false;
        }
        unawaited(_closeMiniPlayer().catchError((Object _) {}));
        return true;
      }
      return null;
    });
  }

  Future<void> _initialize() async {
    try {
      final preferences = await SharedPreferences.getInstance();
      await _repository.initialize();
      // Closing can happen while preferences or the native repository are
      // initializing. Never attach a late player after the parent resumes.
      if (!mounted || !_startupGate.canAttachPlayer) return;
      if (!widget.arguments.authorizedSession) {
        throw StateError('主視窗尚未完成使用者驗證');
      }
      final store = LocalStore(
        preferences,
        assumeAuthenticated: widget.arguments.authorizedSession,
      );
      if (!mounted || !_startupGate.canAttachPlayer) {
        store.dispose();
        return;
      }
      _repository.access = store;
      setState(() {
        _store = store;
        _error = null;
      });
      await desktopMiniPlayerChannel.invokeMethod<void>('mini_ready');
    } catch (error) {
      if (!mounted || !_startupGate.canAttachPlayer) return;
      setState(() => _error = error);
      try {
        await desktopMiniPlayerChannel.invokeMethod<void>(
          'mini_error',
          {'message': error.toString()},
        );
      } catch (_) {}
    }
  }

  void _progressChanged(int index, double position) {
    final episodeChanged = index != _episodeIndex;
    _episodeIndex = index;
    if (position.isFinite && position >= 0) _position = position;
    final now = DateTime.now();
    if (!episodeChanged &&
        now.difference(_lastProgressMessage) <
            const Duration(milliseconds: 900)) {
      return;
    }
    _lastProgressMessage = now;
    unawaited(
      desktopMiniPlayerChannel
          .invokeMethod<void>('mini_progress', {
            'index': _episodeIndex,
            'position': _position,
          })
          .catchError((Object _) {}),
    );
  }

  Future<void> _disposePlayerBeforeHandoff() async {
    // Close the startup gate even when PlayerScreen has not been attached yet.
    // Otherwise a slow initialization could create it after mini_closed.
    _startupGate.close();
    if (mounted && !_closing) setState(() => _closing = true);
    if (_store == null) return;

    final disposed = Completer<void>();
    _playerDisposed = disposed;

    // Give the host one frame to unmount PlayerScreen. Its completion callback
    // is fired only after the underlying media_kit MPV player is disposed.
    await WidgetsBinding.instance.endOfFrame;
    if (!disposed.isCompleted) {
      await disposed.future.timeout(const Duration(seconds: 15));
    }
  }

  void _onPlayerDisposed() {
    final disposed = _playerDisposed;
    if (disposed != null && !disposed.isCompleted) {
      disposed.complete();
    }
  }

  Future<void> _notifyMainWindow() async {
    if (!_restoreMainWindowOnClose) return;
    await desktopMiniPlayerChannel.invokeMethod<void>('mini_closed', {
      'index': _episodeIndex,
      'position': _position,
    });
  }

  Future<void> _closeNativeWindow() async {
    await windowManager.setPreventClose(false);
    await windowManager.close();
  }

  Future<void> _closeMiniPlayer() async {
    try {
      await _shutdown.close(
        disposePlayer: _disposePlayerBeforeHandoff,
        notifyMainWindow: _notifyMainWindow,
        closeWindow: _closeNativeWindow,
      );
    } catch (error) {
      if (mounted) {
        setState(() {
          _closing = true;
          _closeError =
              '小窗播放器尚未完成安全釋放，主視窗暫不會啟動第二個播放器。請關閉 D-TV 後重新開啟。';
        });
      }
      rethrow;
    }
  }

  @override
  void onWindowClose() {
    unawaited(_closeMiniPlayer().catchError((Object _) {}));
  }

  @override
  void dispose() {
    windowManager.removeListener(this);
    unawaited(desktopMiniPlayerChannel.setMethodCallHandler(null));
    _store?.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final store = _store;
    return MaterialApp(
      title: 'D TV 小窗',
      debugShowCheckedModeBanner: false,
      theme: AppTheme.dark,
      themeMode: ThemeMode.dark,
      home: Scaffold(
        backgroundColor: Colors.black,
        body: SafeArea(
          child: Column(
            children: [
              SizedBox(
                height: 38,
                child: Row(
                  children: [
                    const SizedBox(width: 12),
                    Expanded(
                      child: Text(
                        uiTraditional(widget.arguments.detail.drama.title),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(fontWeight: FontWeight.w600),
                      ),
                    ),
                    TextButton.icon(
                      onPressed: _closing
                          ? null
                          : () => unawaited(
                                _closeMiniPlayer().catchError((Object _) {}),
                              ),
                      icon: Icon(
                        _closing
                            ? Icons.hourglass_top_rounded
                            : Icons.open_in_full_rounded,
                        size: 16,
                      ),
                      label: Text(_closing ? '正在恢復…' : '恢復主視窗'),
                    ),
                    const SizedBox(width: 6),
                  ],
                ),
              ),
              Expanded(
                child: _closing
                    ? Center(
                        child: Padding(
                          padding: const EdgeInsets.all(18),
                          child: Text(
                            _closeError ?? '正在安全釋放小窗播放器…',
                            textAlign: TextAlign.center,
                          ),
                        ),
                      )
                    : store == null
                    ? Center(
                        child: _error == null
                            ? const CircularProgressIndicator()
                            : Padding(
                                padding: const EdgeInsets.all(18),
                                child: Text(
                                  '小窗播放器初始化失敗：$_error',
                                  textAlign: TextAlign.center,
                                ),
                              ),
                      )
                    : AppLayout(
                        television: false,
                        formFactor: FormFactor.window,
                        child: AppOrientationScope(
                          television: false,
                          child: PlayerScreen(
                            detail: widget.arguments.detail,
                            initialIndex: widget.arguments.initialIndex,
                            initialPosition: widget.arguments.initialPosition,
                            repository: _repository,
                            store: store,
                            desktopMiniPlayer: true,
                            onBackOverride: () => unawaited(
                              _closeMiniPlayer().catchError((Object _) {}),
                            ),
                            onProgress: _progressChanged,
                            onPlaybackDisposed: _onPlayerDisposed,
                          ),
                        ),
                      ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
