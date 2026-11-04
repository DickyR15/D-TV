import 'dart:async';
import 'dart:convert';

import 'package:desktop_multi_window/desktop_multi_window.dart';
import 'package:flutter/material.dart';
import 'package:media_kit/media_kit.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:window_manager/window_manager.dart';

import 'app_layout.dart';
import 'app_orientation.dart';
import 'app_theme.dart';
import 'core_bridge.dart';
import 'local_store.dart';
import 'models.dart';
import 'player_screen.dart';

const desktopMiniPlayerChannelName = 'dtv.desktop.mini.player';

const desktopMiniPlayerChannel = WindowMethodChannel(
  desktopMiniPlayerChannelName,
);

class DesktopMiniPlayerArguments {
  const DesktopMiniPlayerArguments({
    required this.detail,
    required this.initialIndex,
    required this.initialPosition,
  });

  static const businessId = 'dtv_mini_player';

  final DramaDetail detail;
  final int initialIndex;
  final double initialPosition;

  factory DesktopMiniPlayerArguments.fromJson(Map<String, dynamic> json) {
    final index = intValue(json['initialIndex']);
    final position = (json['initialPosition'] as num?)?.toDouble() ?? 0;
    return DesktopMiniPlayerArguments(
      detail: DramaDetail.fromJson(
        Map<String, dynamic>.from(json['detail'] as Map),
      ),
      initialIndex: index,
      initialPosition: position.isFinite && position > 0 ? position : 0,
    );
  }

  Map<String, dynamic> toJson() => {
    'businessId': businessId,
    'detail': {
      'drama': detail.drama.toJson(),
      'chapters': detail.episodes.map((episode) => episode.raw).toList(),
      'warning': detail.warning,
    },
    'initialIndex': initialIndex,
    'initialPosition': initialPosition,
  };

  static DesktopMiniPlayerArguments? tryParse(String encoded) {
    if (encoded.isEmpty) return null;
    try {
      final json = jsonDecode(encoded);
      if (json is! Map) return null;
      final data = Map<String, dynamic>.from(json);
      if (data['businessId'] != businessId) return null;
      return DesktopMiniPlayerArguments.fromJson(data);
    } catch (_) {
      return null;
    }
  }
}

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
  bool _closing = false;
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
        unawaited(_closeMiniPlayer());
        return true;
      }
      return null;
    });
  }

  Future<void> _initialize() async {
    try {
      final preferences = await SharedPreferences.getInstance();
      await _repository.initialize();
      final store = LocalStore(preferences);
      _repository.access = store;
      if (!mounted) {
        store.dispose();
        return;
      }
      setState(() {
        _store = store;
        _error = null;
      });
      await desktopMiniPlayerChannel.invokeMethod<void>('mini_ready');
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

  void _progressChanged(int index, double position) {
    _episodeIndex = index;
    if (position.isFinite && position >= 0) _position = position;
    final now = DateTime.now();
    if (now.difference(_lastProgressMessage) <
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

  Future<void> _closeMiniPlayer() async {
    if (_closing) return;
    _closing = true;
    try {
      await desktopMiniPlayerChannel.invokeMethod<void>('mini_closed', {
        'index': _episodeIndex,
        'position': _position,
      });
    } catch (_) {}
    try {
      await windowManager.setPreventClose(false);
      await windowManager.close();
    } catch (_) {}
  }

  @override
  void onWindowClose() {
    unawaited(_closeMiniPlayer());
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
                    const Expanded(
                      child: Text(
                        'D TV 小窗播放器',
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(fontWeight: FontWeight.w600),
                      ),
                    ),
                    TextButton.icon(
                      onPressed: _closeMiniPlayer,
                      icon: const Icon(Icons.open_in_full_rounded, size: 16),
                      label: const Text('恢復主視窗'),
                    ),
                    const SizedBox(width: 6),
                  ],
                ),
              ),
              Expanded(
                child: store == null
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
                            onBackOverride: () => unawaited(_closeMiniPlayer()),
                            onProgress: _progressChanged,
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

/// Opens the native second-window player using the exact same episode snapshot
/// and progress. Only Windows and macOS invoke this service.
Future<WindowController> createDesktopMiniPlayerWindow({
  required DramaDetail detail,
  required int index,
  required double position,
}) async {
  final arguments = DesktopMiniPlayerArguments(
    detail: detail,
    initialIndex: index,
    initialPosition: position,
  );
  final controller = await WindowController.create(
    WindowConfiguration(
      arguments: jsonEncode(arguments.toJson()),
      hiddenAtLaunch: true,
    ),
  );
  await controller.show();
  return controller;
}
