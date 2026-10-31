import 'dart:async';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';
import 'package:window_manager/window_manager.dart';

import 'app_layout.dart';
import 'app_orientation.dart';
import 'app_theme.dart';
import 'back_safe_dialog.dart';
import 'core_bridge.dart';
import 'diary_service.dart';
import 'playback_diagnostics.dart';
import 'danmaku_controller.dart';
import 'danmaku_overlay.dart';
import 'local_store.dart';
import 'models.dart';
import 'playback_loader.dart';
import 'playback_preloader.dart';
import 'playback_recovery.dart';
import 'playback_controls_visibility.dart';
import 'playback_preferences.dart';
import 'portrait_blur_policy.dart';
import 'luna_exo_player.dart';
import 'player_controls.dart';
import 'player_interactions.dart';
import 'player_menu.dart';
import 'television_controls.dart';
import 'widgets.dart';
import 'ui_localization.dart';
import 'sources_screen.dart';
import 'lan_controller.dart';
import 'lan_screen.dart';
import 'video_enhancement.dart';
import 'video_output_size.dart';

const _androidPlayerEngine = String.fromEnvironment(
  'DTV_ANDROID_PLAYER',
  // Prefer the Android native Surface route verified on Onn. 4K Pro.
  defaultValue: 'mpv_embed',
);

// Never leave the loading overlay spinning indefinitely if MPV does not
// complete the file-open future for an unavailable or stalled upstream stream.
const _playbackOpenTimeout = Duration(seconds: 15);
const _playbackCleanupTimeout = Duration(seconds: 5);
const _playbackStopTimeout = Duration(seconds: 4);
const _playbackReleaseTimeout = Duration(seconds: 3);
const _playbackPropertyTimeout = Duration(seconds: 3);
const _playbackSpeedTimeout = Duration(seconds: 3);

class PlayerScreen extends StatefulWidget {
  const PlayerScreen({
    super.key,
    required this.detail,
    required this.initialIndex,
    required this.repository,
    required this.store,
    this.initialPosition = 0,
    this.localOnly = false,
    this.allowOnlineFallback = true,
    this.mediaId,
    this.playerFactory,
    this.videoBuilder,
    this.handoff,
    this.recoveryRestarted = false,
  });
  final DramaDetail detail;
  final int initialIndex;
  final double initialPosition;
  final bool localOnly;
  final bool allowOnlineFallback;
  final String? mediaId;
  final AppRepository repository;
  final LocalStore store;
  final LanIncomingPlayback? handoff;
  /// True after the one automatic clean-player restart has already been used.
  final bool recoveryRestarted;
  @visibleForTesting
  final Player Function()? playerFactory;
  @visibleForTesting
  final Widget Function(Widget controls)? videoBuilder;
  @override
  State<PlayerScreen> createState() => _PlayerScreenState();
}

class _PlayerScreenState extends State<PlayerScreen>
    with WidgetsBindingObserver {
  late final Player _player;
  late final VideoController? _video;
  late final VideoEnhancementController _enhancement;
  late final PlaybackLoader _loader;
  late final PlaybackPreloader _preloader;
  bool _preloadEnabled = true;
  late final DanmakuController _danmaku;
  int _seekSequence = 0;
  bool _danmakuEnabled = true;
  late final PlayerInteractions _interactions;
  final _playerFocus = FocusNode(debugLabel: 'player-surface');
  final _videoPaneKey = GlobalKey();
  final GlobalKey<TelevisionControlsState> _televisionControlsKey =
      GlobalKey<TelevisionControlsState>();
  final _menuRevision = ValueNotifier<int>(0);
  final List<StreamSubscription<dynamic>> _subscriptions = [];
  final _recovery = PlaybackRecovery();
  final _recoveryStability = PlaybackRecoveryStability();
  bool _recoveryBudgetPendingReset = false;
  Object _lanIdentity = Object();
  bool _handoffOwned = true;
  double? _lanFirstPosition;
  String? _savedProgressKey;
  final _health = PlaybackHealth();
  Timer? _saveTimer;
  Timer? _healthTimer;
  Timer? _syncDiagnosticTimer;
  Timer? _loadingOverlayTimer;
  bool _showLoadingOverlay = false;
  Timer? _errorTimer;
  Timer? _pictureInPictureExitTimer;
  Future<void> _operations = Future<void>.value();
  late int _index;
  late final int _profileEpoch;
  int _openedIndex = -1;
  int _generation = 0;
  int _requestedQuality = 0;
  bool _loading = true;
  bool _forceOnline = false;
  bool _localFailure = false;
  bool _fullscreen = false;
  bool _automaticFullscreenSuppressed = false;
  bool _panelOpen = false;
  DateTime? _lastNestedRoutePopAt;
  int _mobileTab = 0;
  bool _autoAdvance = true;
  bool? _systemFullscreen;
  Orientation? _lastOrientation;
  bool _closed = false;
  bool _acceptErrors = false;
  bool _foreground = true;
  bool _playIntent = true;
  bool _showControlsOnPlaybackReady = true;
  bool _pendingError = false;
  bool _pictureInPictureSupported = false;
  bool _pictureInPictureActive = false;
  bool _pictureInPictureRequested = false;
  bool _pictureInPictureHandlerInstalled = false;
  String? _pictureInPictureStateSignature;
  AppLifecycleState _lifecycleState = AppLifecycleState.resumed;
  String _loadingMessage = '正在準備播放';
  String? _error;
  String? _saveWarning;
  PlaybackPlan? _plan;
  double _speed = 1;
  bool _portraitBlurBackground = false;
  double _aspectRatio = 9 / 16;
  double _resumePosition = 0;
  bool _rotating = false;
  bool _television = false;
  FormFactor _formFactor = FormFactor.phone;
  TelevisionDecoder _televisionDecoder = TelevisionDecoder.automatic;
  late final bool _useMpvEmbedForThisPlayer;
  bool _tvDecoderPolicyMigrated = false;
  bool _portraitBlurDefaultMigrationRequested = false;
  // Android MPV Embed renders MediaCodec frames into the Flutter Texture
  // Registry surface. Portrait blur can composite that same texture twice
  // without switching to mediacodec-copy or creating another decoder.
  // ExoPlayer does not expose MPV decoder properties.
  bool get _mpvEmbedBackend => _useMpvEmbedForThisPlayer;
  bool get _televisionDecoderApplies =>
      Platform.isAndroid && _television && _player.platform is NativePlayer;
  bool get _televisionRenderingApplies =>
      Platform.isAndroid && _television && _player.platform is NativePlayer;
  AppOrientationController? _orientationController;
  bool get _pictureInPictureVisible =>
      _pictureInPictureActive || _pictureInPictureRequested;
  bool get _canUsePictureInPicture =>
      !_television &&
      defaultTargetPlatform == TargetPlatform.android &&
      _pictureInPictureSupported;
  bool get _mobile => _formFactor == FormFactor.phone;
  VideoEnhancementController? get _enhancementForUi =>
      _enhancement.supported ? _enhancement : null;
  PlaybackPreferences get _preferences => PlaybackPreferences(
    speed: _speed,
    quality: _requestedQuality,
    autoAdvance: _autoAdvance,
    danmaku: _danmakuEnabled,
    preload: _preloadEnabled,
    enhancement: _enhancement.preferences,
    portraitBlurBackground: _portraitBlurBackground,
    televisionDecoder: _televisionDecoder,
    autoPictureInPicture: widget.store.playbackPreferences.autoPictureInPicture,
  );
  String get _qualityLabel => _plan?.local == true
      ? '本機原畫'
      : _requestedQuality == 0
      ? '自動'
      : '${_requestedQuality}P';
  String get _session => _plan?.session ?? '';
  double get _currentPosition =>
      _openedIndex == _index && _player.state.position.inMilliseconds > 0
      ? _player.state.position.inMilliseconds / 1000
      : _resumePosition;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _index = widget.initialIndex;
    _profileEpoch = widget.handoff?.profileEpoch ?? widget.store.profileEpoch;
    final preferences = widget.store.playbackPreferences;
    final migrateDanmakuDefault =
        widget.store.needsDanmakuDefaultOffMigration;
    if (migrateDanmakuDefault) {
      unawaited(widget.store.migrateDanmakuDefaultOff());
    }
    _speed = preferences.speed;
    _requestedQuality = preferences.quality;
    _autoAdvance = preferences.autoAdvance;
    _danmakuEnabled = migrateDanmakuDefault ? false : preferences.danmaku;
    _preloadEnabled = preferences.preloads(television: false);
    _televisionDecoder = preferences.televisionDecoder;
    // Android TV uses the previously validated MPV Embed + MediaCodec
    // surface path for automatic and hardware modes. Software mode stays on
    // the regular GPU path; phones/tablets use the regular MPV view.
    final inheritedLayout = context.getInheritedWidgetOfExactType<AppLayout>();
    final isTelevisionLayout =
        inheritedLayout?.formFactor == FormFactor.television ||
        (inheritedLayout?.formFactor == null &&
            inheritedLayout?.television == true);
    // Keep MPV Embed + MediaCodec for Android TV even when blur is enabled.
    // The blur is composed from the same Flutter Texture Registry output;
    // it must never force the mediacodec-copy path that caused TV A/V drift.
    _portraitBlurBackground = preferences.portraitBlurBackground;
    _useMpvEmbedForThisPlayer = shouldUseMpvEmbedForPlayback(
      android: Platform.isAndroid,
      television: isTelevisionLayout,
      mpvEmbedConfigured: _androidPlayerEngine == 'mpv_embed',
      softwareDecoderSelected:
          preferences.televisionDecoder == TelevisionDecoder.software,
      customPlayer: widget.playerFactory != null || widget.videoBuilder != null,
    );
    _loader = PlaybackLoader(widget.repository);
    _preloader = PlaybackPreloader(widget.repository);
    _danmaku = DanmakuController(widget.repository)
      ..setEnabled(_danmakuEnabled);
    widget.store.addListener(_accessChanged);
    final useMpvEmbed = _mpvEmbedBackend;
    final useExoPlayer =
        Platform.isAndroid &&
        _androidPlayerEngine == 'exo' &&
        widget.videoBuilder == null &&
        widget.playerFactory == null &&
        !useMpvEmbed;
    _player =
        widget.playerFactory?.call() ??
        (useExoPlayer
            ? LunaExoPlayer()
            : Player(
                configuration: const PlayerConfiguration(
                  bufferSize: 32 * 1024 * 1024,
                  logLevel: MPVLogLevel.error,
                ),
              ));
    final exoPlayer = _player is LunaExoPlayer;
    DiaryService.add(
      '[Player] backend=${exoPlayer
          ? 'video_player/ExoPlayer'
          : useMpvEmbed
          ? 'media_kit/MPV (mediacodec_embed)'
          : 'media_kit/MPV'} '
      'android-surface-after-video-params=${Platform.isAndroid ? false : 'default'}',
    );
    _video = widget.videoBuilder == null && !exoPlayer
        ? VideoController(
            _player,
            configuration: VideoControllerConfiguration(
              enableHardwareAcceleration: !Platform.isIOS,
              vo: useMpvEmbed ? 'mediacodec_embed' : null,
              hwdec: useMpvEmbed ? 'mediacodec' : null,
              // Prevent Android video decoding from racing a late Surface.
              // The real-device log showed the codec start before Surface availability.
              androidAttachSurfaceAfterVideoParameters:
                  Platform.isAndroid ? false : null,
            ),
          )
        : null;
    _enhancement = VideoEnhancementController(
      player: _player,
      video: _video,
      preferences: preferences.enhancement,
      category: widget.detail.drama.category,
      tags: widget.detail.drama.tags,
    );
    _interactions = PlayerInteractions(
      player: _player,
      available: () =>
          !_closed &&
          widget.store.profileEpoch == _profileEpoch &&
          !_loading &&
          _error == null &&
          _foreground &&
          !_panelOpen,
      baseSpeed: () => _speed,
      onTogglePlayback: _togglePlayback,
      onSeek: _seekTo,
      onFullscreen: _rotate,
      onEpisode: (direction) {
        final next = _index + direction;
        if (next < 0) return '已經是第一集';
        if (next >= widget.detail.episodes.length) return '已經是最後一集';
        unawaited(_play(next));
        return '第 ${widget.detail.episodes[next].number} 集';
      },
    );
    _playerFocus.addListener(() {
      if (!_playerFocus.hasPrimaryFocus && !_closed) _interactions.cancel();
    });
    _configurePictureInPicture();
    _subscriptions.add(
      _player.stream.error.listen((error) {
        DiaryService.add(
          '[Recovery] player_error error=${playbackDiagnosticError(error)} generation=$_generation accepted=$_acceptErrors',
        );
        if (_enhancement.handlePlaybackError(error)) return;
        if (!_closed && _acceptErrors && mounted && error.trim().isNotEmpty) {
          _queueRecovery(reason: 'player_error');
        }
      }),
    );
    _subscriptions.add(
      _player.stream.log.listen((log) {
        if (_closed ||
            !playbackDiagnosticLogRelevant(
              level: log.level,
              prefix: log.prefix,
              text: log.text,
            )) {
          return;
        }
        final text = playbackDiagnosticLogText(log.text);
        if (text.isEmpty) return;
        DiaryService.add(
          '[MPV] level=${log.level} prefix=${playbackDiagnosticLogText(log.prefix)} generation=$_generation text=$text',
        );
        if (_acceptErrors &&
            mounted &&
            playbackDiagnosticLogRequiresRecovery(
              level: log.level,
              prefix: log.prefix,
              text: log.text,
            )) {
          DiaryService.add(
            '[Recovery] demuxer_open_failure generation=$_generation',
          );
          _queueRecovery(reason: 'demuxer_open_error');
        }
      }),
    );
    _subscriptions.add(
      _player.stream.completed.listen((completed) {
        if (completed &&
            !_loading &&
            !_closed &&
            _acceptErrors &&
            _error == null) {
          final duration = _player.state.duration;
          if (duration <= Duration.zero ||
              _player.state.position < duration - const Duration(seconds: 2)) {
            _queueRecovery(reason: 'early_completion');
          } else if (_autoAdvance &&
              _foreground &&
              !_panelOpen &&
              _index + 1 < widget.detail.episodes.length) {
            _play(_index + 1, showControlsOnReady: false);
          } else {
            _playIntent = false;
            _interactions.cancel();
            unawaited(_player.pause());
            unawaited(_saveProgress(flush: true));
          }
        }
      }),
    );
    _subscriptions.add(
      _player.stream.position.listen((position) {
        if (!_closed && _openedIndex == _index && position > Duration.zero) {
          _resumePosition = position.inMilliseconds / 1000;
        }
        _syncDanmaku();
        _syncPreload();
        _acknowledgeHandoff();
      }),
    );
    for (final stream in [
      _player.stream.duration,
      _player.stream.buffer,
      _player.stream.playing,
      _player.stream.buffering,
      _player.stream.rate,
      _player.stream.completed,
    ]) {
      _subscriptions.add(
        stream.listen((_) {
          _syncDanmaku();
          _syncPreload();
          _acknowledgeHandoff();
        }),
      );
    }
    _subscriptions.add(
      _player.stream.videoParams.listen((parameters) {
        final size = videoDisplaySize(parameters);
        if (size != null &&
            size.width > 0 &&
            size.height > 0 &&
            mounted &&
            !_closed) {
          setState(() {
            _aspectRatio = size.width / size.height;
          });
          _scheduleSystemUi();
        }
        unawaited(_logVideoOutput(parameters));
      }),
    );
    _saveTimer = Timer.periodic(
      const Duration(seconds: 5),
      (_) => _saveProgress(),
    );
    _healthTimer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (_closed || !_acceptErrors || _loading || _error != null) return;
      final state = _player.state;
      final now = DateTime.now();
      if (_recoveryBudgetPendingReset &&
          _recoveryStability.observe(
            position: state.position,
            playing: state.playing,
            buffering: state.buffering,
            intendedToPlay: _playIntent && !state.completed,
            foreground: _foreground,
            completed: state.completed,
            now: now,
          )) {
        _recovery.reset();
        _recoveryBudgetPendingReset = false;
        _recoveryStability.reset();
        DiaryService.add(
          '[Recovery] budget_reset_after_stable_playback generation=$_generation stable_seconds=${PlaybackRecoveryStability.stablePlaybackDuration.inSeconds}',
        );
      }
      final routeIndex = _plan?.routeIndex ?? 0;
      if (_health.stalled(
        position: state.position,
        duration: state.duration,
        buffer: state.buffer,
        // Use the user's playback intent, not MPV's transient playing /
        // buffering flags. Buffer and duration changes are separately counted
        // as progress. A new route gets a shorter startup window after prior
        // candidates have already failed.
        intendedToPlay: _playIntent && !state.completed,
        foreground: _foreground,
        now: now,
        routeIndex: routeIndex,
      )) {
        final startup = state.position == Duration.zero &&
            state.duration == Duration.zero;
        final startupSeconds =
            PlaybackHealth.startupTimeoutForRoute(routeIndex).inSeconds;
        unawaited(
          _recover(
            reason: startup
                ? 'startup_stalled_${startupSeconds}s'
                : 'position_stalled_32s',
          ),
        );
      }
    });
    final handoff = widget.handoff;
    if (handoff != null) {
      handoff.consumed = true;
      handoff.stop = () async {
        if (_handoffOwned && !_closed) await _stopForLan();
      };
    }
    if (_profileEpoch != widget.store.profileEpoch ||
        handoff?.cancelled == true) {
      handoff?.fail('接收使用者已變更，推送已取消');
      if (handoff != null) {
        unawaited(_releasePlayback(handoff.plan.session, 'handoff_discarded'));
      }
      _loading = false;
      _error = '播放接收已取消';
    } else {
      _play(
        _index,
        position: widget.initialPosition,
        handoffPlan: handoff?.plan,
        // A clean-player restart is part of automatic recovery; don't flash
        // the control overlay while the replacement stream begins.
        showControlsOnReady: !widget.recoveryRestarted,
      );
    }
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && !_closed) {
        widget.repository.catalogUpdates.publish(
          widget.repository.catalogUpdates.current(widget.detail.drama),
          retryCover: true,
        );
      }
    });
  }

  void _accessChanged() {
    if (!_closed && mounted) {
      final blurEnabled =
          widget.store.playbackPreferences.portraitBlurBackground;
      if (blurEnabled != _portraitBlurBackground) {
        setState(() => _portraitBlurBackground = blurEnabled);
      }
    }
    if (!_closed &&
        (widget.store.profileEpoch != _profileEpoch || widget.store.locked)) {
      _preloader.clear();
      _enhancement.suspend();
      _handoffOwned = false;
      _playIntent = false;
      widget.handoff?.fail('接收端使用者已變更');
      unawaited(_player.pause());
    }
    if (!_closed &&
        (widget.store.profileEpoch != _profileEpoch ||
            widget.store.locked ||
            !widget.store.allowsSource('hongguo'))) {
      _danmaku.setPlan(null);
    }
  }

  void _configurePictureInPicture() {
    if (defaultTargetPlatform != TargetPlatform.android) return;
    _pictureInPictureHandlerInstalled = true;
    AppDevice.channel.setMethodCallHandler((call) async {
      if (call.method == 'pictureInPictureChanged') {
        final arguments = call.arguments;
        final active = arguments is Map && arguments['active'] == true;
        _setPictureInPictureStatus(active: active, delayHiddenPause: !active);
      } else if (call.method == 'pictureInPicturePending') {
        _handlePictureInPicturePending();
      } else if (call.method == 'pictureInPictureAction') {
        final arguments = call.arguments;
        final action = arguments is Map ? arguments['action'] : null;
        switch (action) {
          case 'toggle':
            _togglePlayback();
            break;
          case 'previous':
            if (_index > 0) unawaited(_play(_index - 1));
            break;
          case 'next':
            if (_index + 1 < widget.detail.episodes.length) {
              unawaited(_play(_index + 1));
            }
            break;
        }
      }
    });
    unawaited(_refreshPictureInPictureStatus());
  }

  void _handlePictureInPicturePending() {
    _setPictureInPictureStatus(requested: true);
    _pictureInPictureExitTimer?.cancel();
    _pictureInPictureExitTimer = Timer(const Duration(seconds: 2), () {
      if (!_closed && _pictureInPictureRequested && !_pictureInPictureActive) {
        _setPictureInPictureStatus(requested: false, delayHiddenPause: true);
      }
    });
  }

  void _syncPictureInPicture() {
    if (defaultTargetPlatform != TargetPlatform.android ||
        !_pictureInPictureSupported) {
      return;
    }
    final ratio = _aspectRatio.isFinite && _aspectRatio > 0
        ? _aspectRatio.clamp(1 / 2.39, 2.39).toDouble()
        : 16 / 9;
    final width = ratio >= 1 ? (1000 * ratio).round() : 1000;
    final height = ratio >= 1 ? 1000 : (1000 / ratio).round();
    final playing = !_loading && _error == null && _player.state.playing;
    final hasPrevious = _index > 0;
    final hasNext = _index + 1 < widget.detail.episodes.length;
    final autoEnter = playing && !_television &&
        widget.store.playbackPreferences.autoPictureInPicture;
    final signature = '$width:$height:$playing:$hasPrevious:$hasNext:$autoEnter';
    if (_pictureInPictureStateSignature == signature) return;
    _pictureInPictureStateSignature = signature;
    unawaited(
      AppDevice.channel.invokeMethod<void>('updatePictureInPicture', {
        'width': width,
        'height': height,
        'playing': playing,
        'hasPrevious': hasPrevious,
        'hasNext': hasNext,
        'autoEnter': autoEnter,
      }).catchError((Object _) {
        _pictureInPictureStateSignature = null;
      }),
    );
  }

  Future<void> _refreshPictureInPictureStatus() async {
    if (defaultTargetPlatform != TargetPlatform.android) return;
    try {
      final status = await AppDevice.channel.invokeMapMethod<String, dynamic>(
        'pictureInPictureStatus',
      );
      if (!mounted || _closed) return;
      _setPictureInPictureStatus(
        supported: status?['supported'] == true,
        active: status?['active'] == true,
      );
      _syncPictureInPicture();
    } on PlatformException {
      if (mounted && !_closed) {
        _setPictureInPictureStatus(supported: false, active: false);
      }
    } on MissingPluginException {
      if (mounted && !_closed) {
        _setPictureInPictureStatus(supported: false, active: false);
      }
    }
  }

  void _setPictureInPictureStatus({
    bool? supported,
    bool? active,
    bool? requested,
    bool delayHiddenPause = false,
  }) {
    final nextSupported = supported ?? _pictureInPictureSupported;
    final nextActive = active ?? _pictureInPictureActive;
    final nextRequested =
        requested ?? (nextActive ? false : _pictureInPictureRequested);
    void assign() {
      _pictureInPictureSupported = nextSupported;
      _pictureInPictureActive = nextActive;
      _pictureInPictureRequested = nextRequested;
    }

    if (mounted && !_closed) {
      setState(assign);
    } else {
      assign();
    }
    if (_pictureInPictureVisible ||
        _lifecycleState == AppLifecycleState.resumed) {
      _pictureInPictureExitTimer?.cancel();
      _applyLifecycleVisibility(pauseWhenHidden: false);
      return;
    }
    _pictureInPictureExitTimer?.cancel();
    _applyLifecycleVisibility(pauseWhenHidden: !delayHiddenPause);
    if (delayHiddenPause) {
      _pictureInPictureExitTimer = Timer(const Duration(milliseconds: 700), () {
        if (!_closed &&
            !_pictureInPictureVisible &&
            _lifecycleState != AppLifecycleState.resumed) {
          _applyLifecycleVisibility();
        }
      });
    }
  }

  void _applyLifecycleVisibility({bool pauseWhenHidden = true}) {
    final treatAsVisible = Platform.isWindows
        ? (AppLifecycleState.resumed == _lifecycleState ||
              AppLifecycleState.inactive == _lifecycleState)
        : _lifecycleState == AppLifecycleState.resumed;
    final visible = treatAsVisible || _pictureInPictureVisible;
    _foreground = visible;
    _enhancement.setForeground(_foreground);
    _syncDanmaku();
    _syncPreload();
    _health.reset();
    if (!visible) _interactions.cancel();
    if (pauseWhenHidden &&
        !visible &&
        (_lifecycleState == AppLifecycleState.paused ||
            (!Platform.isWindows &&
                _lifecycleState == AppLifecycleState.inactive) ||
            _lifecycleState == AppLifecycleState.hidden)) {
      _playIntent = false;
      unawaited(_player.pause());
      unawaited(_saveProgress(flush: true));
    }
    if (visible && _pendingError) {
      _queueRecovery(reason: 'foreground_pending_error');
    }
  }

  Future<void> _enterPictureInPicture() async {
    if (!_canUsePictureInPicture ||
        _closed ||
        _loading ||
        _error != null ||
        _panelOpen) {
      return;
    }
    _interactions.cancel();
    _syncPictureInPicture();
    final rawRatio = _aspectRatio.isFinite && _aspectRatio > 0
        ? _aspectRatio
        : 16 / 9;
    final ratio = rawRatio.clamp(1 / 2.39, 2.39).toDouble();
    final width = ratio >= 1 ? (1000 * ratio).round() : 1000;
    final height = ratio >= 1 ? 1000 : (1000 / ratio).round();
    _setPictureInPictureStatus(requested: true);
    await WidgetsBinding.instance.endOfFrame;
    if (!mounted || _closed) return;
    final sourceRect = _pictureInPictureSourceRect();
    final arguments = <String, int>{'width': width, 'height': height};
    if (sourceRect != null) arguments.addAll(sourceRect);
    try {
      final status = await AppDevice.channel.invokeMapMethod<String, dynamic>(
        'enterPictureInPicture',
        arguments,
      );
      if (!mounted || _closed) return;
      final supported = status?['supported'] == true;
      final active = status?['active'] == true;
      final requested = status?['requested'] == true;
      _setPictureInPictureStatus(
        supported: supported,
        active: active,
        requested: requested && !active,
      );
      if (!supported || (!active && !requested)) {
        _notice('目前裝置不支援子母畫面');
      } else if (requested && !active) {
        _pictureInPictureExitTimer?.cancel();
        _pictureInPictureExitTimer = Timer(const Duration(seconds: 2), () {
          if (!_closed &&
              _pictureInPictureRequested &&
              !_pictureInPictureActive) {
            _setPictureInPictureStatus(requested: false);
          }
        });
      }
    } on PlatformException {
      _setPictureInPictureStatus(requested: false);
      _notice('無法進入子母畫面，請檢查系統權限');
    } on MissingPluginException {
      _setPictureInPictureStatus(supported: false, requested: false);
      _notice('目前平台不支援子母畫面');
    }
  }

  Map<String, int>? _pictureInPictureSourceRect() {
    final paneContext = _videoPaneKey.currentContext;
    final renderObject = paneContext?.findRenderObject();
    if (paneContext == null ||
        renderObject is! RenderBox ||
        !renderObject.hasSize) {
      return null;
    }
    final topLeft = renderObject.localToGlobal(Offset.zero);
    final size = renderObject.size;
    final ratio =
        MediaQuery.maybeOf(paneContext)?.devicePixelRatio ??
        View.of(paneContext).devicePixelRatio;
    final left = (topLeft.dx * ratio).round().clamp(0, 100000);
    final top = (topLeft.dy * ratio).round().clamp(0, 100000);
    final right = ((topLeft.dx + size.width) * ratio).round().clamp(0, 100000);
    final bottom = ((topLeft.dy + size.height) * ratio).round().clamp(
      0,
      100000,
    );
    if (right <= left || bottom <= top) return null;
    return {'left': left, 'top': top, 'right': right, 'bottom': bottom};
  }

  void _attachLanPlayback() {
    LanController.current?.attachPlayback(
      LanPlaybackHost(
        identity: _lanIdentity,
        title:
            '${uiTraditional(widget.detail.drama.title)} · 第 ${widget.detail.episodes[_index].number} 集',
        stop: _stopForLan,
      ),
    );
  }

  void _acknowledgeHandoff() {
    final handoff = widget.handoff;
    if (!_handoffOwned ||
        handoff == null ||
        handoff.cancelled ||
        handoff.started.isCompleted ||
        _closed ||
        _loading ||
        _error != null ||
        _openedIndex != _index ||
        _index != widget.initialIndex ||
        widget.store.profileEpoch != _profileEpoch) {
      return;
    }
    final state = _player.state;
    final position = state.position.inMilliseconds / 1000;
    final duration = state.duration.inMilliseconds / 1000;
    if (duration > 0 && handoff.position > duration + 2) {
      handoff.fail('續播位置超過接收端分集時長');
      return;
    }
    if (!state.playing ||
        state.buffering ||
        (state.width ?? 0) <= 0 ||
        position < handoff.position - .5 ||
        position > handoff.position + 20) {
      return;
    }
    _lanFirstPosition ??= position;
    if (position >= _lanFirstPosition! + .15) handoff.acknowledge(position);
  }

  Future<void> _stopForLan() async {
    final generation = _generation;
    await _serialize(() async {
      if (_closed ||
          generation != _generation ||
          widget.store.profileEpoch != _profileEpoch) {
        return;
      }
      _playIntent = false;
      _interactions.cancel();
      _enhancement.suspend();
      _health.reset();
      await _player.pause().timeout(
        _playbackStopTimeout,
        onTimeout: () => throw TimeoutException(
          '暫停播放器逾時（區域播放交接）',
          _playbackStopTimeout,
        ),
      );
      await _saveProgressForPlayback(
        flush: true,
        reason: 'lan_handoff',
      );
    });
  }

  Future<void> _pushToDevice() async {
    final link = LanController.current;
    if (link == null ||
        _panelOpen ||
        _closed ||
        _loading ||
        _error != null ||
        widget.mediaId != null) {
      return;
    }
    final generation = _generation;
    final index = _index;
    bool current() =>
        mounted &&
        !_closed &&
        generation == _generation &&
        index == _index &&
        widget.store.profileEpoch == _profileEpoch;
    _interactions.cancel();
    setState(() => _panelOpen = true);
    try {
      await showLanPush(
        context,
        link,
        snapshot: () => LanPlaybackIntent(
          drama: widget.detail.drama,
          episodeID: widget.detail.episodes[index].id,
          episode: widget.detail.episodes[index].number,
          position: _currentPosition,
        ),
        stillCurrent: current,
        onAccepted: () async {
          if (!current()) throw StateError('本機播放內容已變更');
          await _stopForLan();
          if (!current()) throw StateError('本機播放內容已變更');
        },
      );
    } catch (error) {
      if (mounted && !_closed) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(uiTraditional(error.toString()))));
      }
    } finally {
      if (mounted && !_closed) setState(() => _panelOpen = false);
    }
  }

  void _syncPreload() {
    if (_closed) return;
    _syncPictureInPicture();
    if (!_preloadEnabled ||
        !_foreground ||
        widget.localOnly ||
        _plan?.local == true ||
        _loading ||
        _error != null ||
        _openedIndex != _index ||
        widget.store.profileEpoch != _profileEpoch ||
        widget.store.locked ||
        _index + 1 >= widget.detail.episodes.length) {
      _preloader.clear();
      return;
    }
    final state = _player.state;
    if (!state.playing || state.buffering || !_playIntent) {
      _preloader.pause();
      return;
    }
    final duration = state.duration.inMilliseconds;
    final position = state.position.inMilliseconds;
    // Prepare the next episode once playback is stable and has buffer headroom,
    // instead of waiting until the final 45 seconds of the current episode.
    if (duration <= 0 ||
        position < 5000 ||
        state.buffer.inMilliseconds - position < 8000) {
      return;
    }
    _preloader.prepare(
      widget.detail.drama,
      widget.detail.episodes[_index + 1],
      quality: _requestedQuality,
    );
  }

  void _syncDanmaku({bool discontinuity = false}) {
    if (_closed) return;
    final state = _player.state;
    _danmaku.update(
      position: state.position,
      duration: state.duration,
      speed: state.rate,
      playing: state.playing && _playIntent && !state.completed,
      buffering: state.buffering,
      foreground: _foreground,
      available:
          !_loading &&
          _error == null &&
          _openedIndex == _index &&
          widget.store.profileEpoch == _profileEpoch &&
          !widget.store.locked &&
          widget.store.allowsSource('hongguo'),
      discontinuity: discontinuity,
    );
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final formFactor = AppLayout.formFactorOf(context);
    final television = formFactor == FormFactor.television;
    if (_formFactor != formFactor) {
      _fullscreen = false;
      _automaticFullscreenSuppressed = false;
      _interactions.cancel();
    }
    _formFactor = formFactor;
    _television = television;
    if (widget.store.playbackPreferences.preload == null) {
      _preloadEnabled = widget.store.playbackPreferences.preloads(
        television: television,
      );
    }
    // Migrate the aggressive legacy default once on Android TV while preserving
    // an intentional optimized/software choice made after this migration.
    if (Platform.isAndroid &&
        television &&
        !_tvDecoderPolicyMigrated &&
        !_mpvEmbedBackend &&
        widget.store.needsTvDecoderSafePresetMigration) {
      _tvDecoderPolicyMigrated = true;
      if (_televisionDecoder == TelevisionDecoder.optimized) {
        _televisionDecoder = TelevisionDecoder.hardware;
      }
      unawaited(
        widget.store
            .migrateTvDecoderSafePreset()
            .then<void>(
              (_) => DiaryService.add(
                '[Sync] completed legacy TV decoder preset migration',
              ),
            )
            .catchError((Object error) {
              DiaryService.add(
                '[Sync] decoder preset migration failed: ${playbackDiagnosticError(error)}',
              );
            }),
      );
    } else if (television) {
      _tvDecoderPolicyMigrated = true;
    }
    _orientationController = AppOrientationScope.maybeOf(context);
    final orientation = MediaQuery.orientationOf(context);
    if ((Platform.isWindows ||
            (Platform.isAndroid && _androidPlayerEngine != 'exo')) &&
        orientation == Orientation.landscape &&
        !_portraitBlurDefaultMigrationRequested &&
        widget.store.needsPortraitBlurLandscapeDefaultMigration) {
      _portraitBlurDefaultMigrationRequested = true;
      _portraitBlurBackground = true;
      unawaited(widget.store.migratePortraitBlurLandscapeDefault());
    }
    if (_lastOrientation != null && _lastOrientation != orientation) {
      _automaticFullscreenSuppressed = false;
      _interactions.cancel();
    }
    _lastOrientation = orientation;
    _scheduleSystemUi();
  }

  void _scheduleSystemUi() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || _closed || (!_mobile && !_television)) return;
      final fullscreen = _showFullscreen;
      if (_systemFullscreen == fullscreen) return;
      _systemFullscreen = fullscreen;
      unawaited(
        SystemChrome.setEnabledSystemUIMode(
          fullscreen ? SystemUiMode.immersiveSticky : SystemUiMode.edgeToEdge,
        ),
      );
    });
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _lifecycleState = state;
    _applyLifecycleVisibility();
  }

  Future<void> _releasePlayback(String session, String reason) async {
    DiaryService.add(
      '[Recovery] release reason=$reason generation=$_generation session=${playbackDiagnosticId(session)}',
    );
    try {
      await widget.repository
          .release(session)
          .timeout(_playbackReleaseTimeout);
    } on TimeoutException {
      // Session cleanup must not block switching/reloading the next episode.
      DiaryService.add(
        '[Recovery] release_timeout reason=$reason generation=$_generation session=${playbackDiagnosticId(session)}',
      );
    } catch (error) {
      DiaryService.add(
        '[Recovery] release_error reason=$reason generation=$_generation error=${playbackDiagnosticError(error)}',
      );
    }
  }

  Future<void> _saveProgressForPlayback({
    required bool flush,
    required String reason,
  }) async {
    try {
      await _saveProgress(flush: flush).timeout(
        _playbackCleanupTimeout,
        onTimeout: () => throw TimeoutException(
          '保存觀看進度逾時（$reason）',
          _playbackCleanupTimeout,
        ),
      );
    } catch (error) {
      DiaryService.add(
        '[Play] cleanup_failure stage=save_progress reason=$reason error=${playbackDiagnosticError(error)}',
      );
      rethrow;
    }
  }

  Future<void> _beforeMediaForPlayback({required String reason}) async {
    try {
      await _enhancement.beforeMedia().timeout(
        _playbackCleanupTimeout,
        onTimeout: () => throw TimeoutException(
          '清理畫質增強逾時（$reason）',
          _playbackCleanupTimeout,
        ),
      );
    } catch (error) {
      DiaryService.add(
        '[Play] cleanup_failure stage=enhancement reason=$reason error=${playbackDiagnosticError(error)}',
      );
      rethrow;
    }
  }

  Future<void> _stopPlayerForPlayback({required String reason}) async {
    try {
      await _player.stop().timeout(
        _playbackStopTimeout,
        onTimeout: () => throw TimeoutException(
          '停止舊播放器逾時（$reason）',
          _playbackStopTimeout,
        ),
      );
    } catch (error) {
      DiaryService.add(
        '[Play] cleanup_failure stage=player_stop reason=$reason error=${playbackDiagnosticError(error)}',
      );
      rethrow;
    }
  }

  Future<void> _setPlaybackProperty(
    NativePlayer platform,
    String property,
    String value,
  ) async {
    try {
      await platform.setProperty(property, value).timeout(
        _playbackPropertyTimeout,
        onTimeout: () => throw TimeoutException(
          '設定播放器參數逾時：$property',
          _playbackPropertyTimeout,
        ),
      );
    } catch (error) {
      // Do not log property values; some properties include signed/decryption
      // data. Only the safe setting name is needed for diagnostics.
      DiaryService.add(
        '[Play] setup_property_failure property=$property error=${playbackDiagnosticError(error)}',
      );
      rethrow;
    }
  }

  void _queueRecovery({required String reason}) {
    if (_closed || !_acceptErrors || _error != null) {
      return;
    }
    _pendingError = true;
    if (!_foreground || (_errorTimer?.isActive ?? false)) {
      return;
    }
    final ticket = _generation;
    final position = _player.state.position;
    DiaryService.add('[Recovery] queued reason=$reason generation=$ticket');
    _errorTimer = Timer(const Duration(milliseconds: 900), () {
      if (_closed || ticket != _generation || !_foreground || !_acceptErrors) {
        return;
      }
      _pendingError = false;
      final state = _player.state;
      if (state.playing &&
          !state.buffering &&
          (state.width ?? 0) > 0 &&
          state.position > position + const Duration(milliseconds: 300)) {
        return;
      }
      unawaited(_recover(reason: reason));
    });
  }

  Future<void> _recover({required String reason}) async {
    final current = _plan;
    if (_closed ||
        !_acceptErrors ||
        !_foreground ||
        current == null ||
        _error != null) {
      return;
    }
    _acceptErrors = false;
    _danmaku.setPlan(null);
    _errorTimer?.cancel();
    _pendingError = false;
    final position = _currentPosition;
    final action = current.local
        ? PlaybackRecoveryAction.stop
        : _recovery.next(
            current,
            allowPlayerRecreate:
                !widget.recoveryRestarted &&
                widget.handoff == null &&
                widget.playerFactory == null &&
                widget.videoBuilder == null,
          );
    DiaryService.add(
      '[Recovery] start reason=$reason action=${action.name} generation=$_generation route=${current.routeIndex}/${current.routeCount} session=${playbackDiagnosticId(current.session)} position=${_player.state.position.inMilliseconds} duration=${_player.state.duration.inMilliseconds} buffering=${_player.state.buffering}',
    );
    if (action == PlaybackRecoveryAction.recreatePlayer) {
      await _recreatePlayerForRecovery(current, position);
      return;
    }
    if (action == PlaybackRecoveryAction.stop) {
      _resumePosition = position;
      final ticket = _generation;
      try {
        await _serialize(() async {
          if (_closed || ticket != _generation) {
            return;
          }
          try {
            await _saveProgressForPlayback(
              flush: false,
              reason: 'recovery_stop',
            );
            _openedIndex = -1;
            await _stopPlayerForPlayback(reason: 'recovery_stop');
          } finally {
            await _releasePlayback(current.session, 'recovery_stop');
          }
        });
      } catch (_) {}
      if (mounted && !_closed && ticket == _generation) {
        _loadingOverlayTimer?.cancel();
        setState(() {
          _loading = false;
          _showLoadingOverlay = false;
          _localFailure = current.local;
          _error = current.local
              ? widget.allowOnlineFallback
                    ? '本機影片讀取失敗，請重試；也可以手動改為線上播放。'
                    : '本機影片讀取失敗，請重試或改為線上播放。'
              : '自動恢復未成功，請檢查網路後重試，也可換一集或選擇其他畫質選項。';
        });
      }
      return;
    }
    await _play(
      _index,
      position: position,
      recoveryAction: action,
      // Route fallback is automatic: keep the viewing surface unobstructed.
      showControlsOnReady: false,
    );
  }

  Future<void> _recreatePlayerForRecovery(
    PlaybackPlan current,
    double position,
  ) async {
    final ticket = _generation;
    try {
      await _serialize(() async {
        if (_closed || ticket != _generation) return;
        await _saveProgressForPlayback(
          flush: true,
          reason: 'recreate_player',
        );
        await _beforeMediaForPlayback(reason: 'recreate_player');
        if (_closed || ticket != _generation) return;
        _openedIndex = -1;
        await _stopPlayerForPlayback(reason: 'recreate_player');
        if (identical(_plan, current)) _plan = null;
        _danmaku.setPlan(null);
        await _releasePlayback(current.session, 'recovery_recreate_player');
      });
    } catch (error) {
      DiaryService.add(
        '[Recovery] recreate_player_prepare_failed error=${playbackDiagnosticError(error)}',
      );
      if (mounted && !_closed && ticket == _generation) {
        setState(() {
          _loading = false;
          _showLoadingOverlay = false;
          _error = '備用線路已耗盡，重新啟動播放器失敗；請退出播放後重新進入。';
        });
      }
      return;
    }
    if (!mounted || _closed || ticket != _generation) return;
    DiaryService.add(
      '[Recovery] recreate_player_screen generation=$ticket position_ms=${(position * 1000).round()} restarted=false',
    );
    final route = PageRouteBuilder<void>(
      settings: const RouteSettings(name: 'DTVPlaybackRecoveryRestart'),
      transitionDuration: Duration.zero,
      reverseTransitionDuration: Duration.zero,
      pageBuilder: (context, animation, secondaryAnimation) => PlayerScreen(
        detail: widget.detail,
        initialIndex: _index,
        initialPosition: position,
        localOnly: widget.localOnly,
        allowOnlineFallback: widget.allowOnlineFallback,
        mediaId: widget.mediaId,
        repository: widget.repository,
        store: widget.store,
        recoveryRestarted: true,
      ),
      transitionsBuilder: (context, animation, secondaryAnimation, child) =>
          child,
    );
    unawaited(Navigator.of(context).pushReplacement<void, void>(route));
  }

  void _togglePlayback() {
    if (_closed || _loading || _error != null) return;
    _handoffOwned = false;
    widget.handoff?.fail('接收端已操作播放');
    _interactions.cancel();
    _playIntent = !_player.state.playing;
    if (_playIntent) _enhancement.mediaReady();
    _health.reset();
    if (_playIntent && _player.state.completed) {
      unawaited(_play(_index));
      return;
    }
    unawaited(_player.playOrPause());
    if (!_playIntent) unawaited(_saveProgress(flush: true));
    _syncDanmaku();
  }

  Future<void> _saveProgress({bool flush = false}) async {
    if (_openedIndex < 0 || widget.store.profileEpoch != _profileEpoch) {
      return;
    }
    final position = _player.state.position.inMilliseconds / 1000;
    final duration = _player.state.duration.inMilliseconds / 1000;
    if (position < .1) {
      return;
    }
    final store = widget.store;
    final progressKey =
        '${widget.mediaId ?? widget.detail.drama.id}:$_openedIndex:$position:$duration';
    if (_savedProgressKey == progressKey && _saveWarning == null) {
      if (flush && widget.mediaId == null) LanController.current?.flush();
      return;
    }
    final entry = WatchEntry(
      drama: widget.repository.catalogUpdates.current(widget.detail.drama),
      episode: widget.detail.episodes[_openedIndex].number,
      position: position,
      duration: duration,
      updatedAt: DateTime.now(),
    );
    try {
      await Future<void>.value();
      if (store.profileEpoch != _profileEpoch) return;
      if (widget.mediaId == null) {
        await store.saveWatch(entry);
        if (flush) LanController.current?.flush();
      } else {
        await store.saveMediaWatch(widget.mediaId!, entry);
      }
      _savedProgressKey = progressKey;
      if (mounted && !_closed && _saveWarning != null) {
        setState(() => _saveWarning = null);
      }
    } catch (_) {
      if (mounted && !_closed && _saveWarning == null) {
        setState(() => _saveWarning = '觀看進度尚未儲存，請檢查儲存空間後重試。');
      }
    }
  }

  Future<void> _serialize(Future<void> Function() operation) {
    final next = _operations.catchError((Object _) {}).then((_) => operation());
    _operations = next;
    return next;
  }

  Future<void> _play(
    int index, {
    double position = 0,
    PlaybackRecoveryAction? recoveryAction,
    bool playWhenReady = true,
    PlaybackPlan? handoffPlan,
    bool showControlsOnReady = true,
  }) async {
    if (_closed ||
        widget.store.profileEpoch != _profileEpoch ||
        index < 0 ||
        index >= widget.detail.episodes.length) {
      return;
    }
    final playClock = Stopwatch()..start();
    var cleanupMs = 0;
    var resolveMs = 0;
    var setupMs = 0;
    var openMs = 0;
    _interactions.cancel();
    if (widget.handoff != null &&
        handoffPlan == null &&
        recoveryAction == null) {
      _handoffOwned = false;
      widget.handoff?.fail('接收端已更換播放內容');
    }
    if (index != _index) _forceOnline = false;
    final warmed =
        handoffPlan ??
        (recoveryAction == null && _preloadEnabled && !widget.localOnly
            ? _preloader.take(
                widget.detail.drama,
                widget.detail.episodes[index],
                quality: _requestedQuality,
                online: _forceOnline,
              )
            : null);
    _preloader.clear();
    final ticket = ++_generation;
    _enhancement.suspend();
    _seekSequence++;
    _danmaku.setPlan(null);
    _acceptErrors = false;
    _syncDiagnosticTimer?.cancel();
    _syncDiagnosticTimer = null;
    _pendingError = false;
    _errorTimer?.cancel();
    _loadingOverlayTimer?.cancel();
    _showLoadingOverlay = false;
    _health.reset();
    _recoveryStability.reset();
    _recoveryBudgetPendingReset = recoveryAction != null;
    if (recoveryAction == null) {
      _recovery.reset();
      _recoveryBudgetPendingReset = false;
      _playIntent = playWhenReady;
    }
    _resumePosition = position;
    _showControlsOnPlaybackReady = showControlsOnReady;
    setState(() {
      _index = index;
      _loading = true;
      _showLoadingOverlay = false;
      _error = null;
      _localFailure = false;
      _loadingMessage = switch (recoveryAction) {
        PlaybackRecoveryAction.alternative => '正在切換備用線路',
        PlaybackRecoveryAction.refresh => '正在重新獲取播放地址',
        _ => '正在準備播放',
      };
    });
    // Don't flash a spinner for streams that start quickly.
    _loadingOverlayTimer = Timer(const Duration(milliseconds: 350), () {
      if (mounted &&
          !_closed &&
          ticket == _generation &&
          _loading &&
          _error == null) {
        setState(() => _showLoadingOverlay = true);
      }
    });
    LanController.current?.detachPlayback(_lanIdentity);
    _lanIdentity = Object();
    _attachLanPlayback();
    PlaybackPlan? prepared;
    PlaybackPlan? retained;
    bool installed = false;
    var openTimedOut = false;
    try {
      final cleanupClock = Stopwatch()..start();
      await _serialize(() async {
        if (_closed || ticket != _generation) {
          return;
        }
        final hasPreviousMedia = _openedIndex >= 0 || _plan != null;
        if (hasPreviousMedia) {
          await _saveProgressForPlayback(
            flush: true,
            reason: 'replace_or_refresh',
          );
          await _beforeMediaForPlayback(reason: 'replace_or_refresh');
          if (_closed || ticket != _generation) return;
          _openedIndex = -1;
          await _stopPlayerForPlayback(reason: 'replace_or_refresh');
          final previous = _plan;
          _plan = null;
          if (recoveryAction == PlaybackRecoveryAction.alternative) {
            retained = previous;
          } else if (previous != null) {
            await _releasePlayback(previous.session, 'replace_or_refresh');
          }
        } else {
          // A new PlayerScreen has no old decoder, effects, or media to tear down.
          _openedIndex = -1;
          _plan = null;
        }
      });
      cleanupMs = cleanupClock.elapsedMilliseconds;
      if (_closed || ticket != _generation) {
        return;
      }
      final resolveClock = Stopwatch()..start();
      if (retained != null) {
        prepared = await _loader.fallback(retained!);
      } else if (warmed != null) {
        prepared = await _loader.use(warmed);
      } else {
        final forceRefresh = recoveryAction == PlaybackRecoveryAction.refresh;
        if (forceRefresh) {
          DiaryService.add(
            '[Recovery] force_refresh_resolve generation=$ticket episode=${widget.detail.episodes[index].number}',
          );
        }
        prepared = await _loader.load(
          widget.detail.drama,
          widget.detail.episodes[index],
          quality: _requestedQuality,
          localOnly: widget.localOnly,
          // A route list exhausted or returned invalid MP4 headers. Resolve
          // this episode again with force=true instead of reusing the old plan.
          online: _forceOnline || forceRefresh,
        );
      }
      resolveMs = resolveClock.elapsedMilliseconds;
      if (prepared == null) {
        // A superseded request should stay silent, but a null result for the
        // current request must not leave the loading overlay spinning forever.
        if (mounted && !_closed && ticket == _generation) {
          DiaryService.add(
            '[Play] empty_plan generation=$ticket resolveMs=$resolveMs',
          );
          _loadingOverlayTimer?.cancel();
          setState(() {
            _loading = false;
            _showLoadingOverlay = false;
            _error = '沒有取得可播放的影片網址，請重試或更換片源。';
          });
        }
        return;
      }
      final plan = prepared;
      await _serialize(() async {
        if (_closed || ticket != _generation) {
          await _releasePlayback(plan.session, 'stale_generation');
          return;
        }
        if (plan.url.isEmpty) {
          throw AppFailure('片源未提供播放網址，請重試');
        }
        final setupClock = Stopwatch()..start();
        final platform = _player.platform;
        if (platform is NativePlayer) {
          final hwdec = playbackHwdecFor(
            decoder: _televisionDecoder,
            android: Platform.isAndroid,
            ios: Platform.isIOS,
            macos: Platform.isMacOS,
            windows: Platform.isWindows,
            mpvEmbed: _mpvEmbedBackend,
          );
          if (hwdec != null) {
            await _setPlaybackProperty(platform, 'hwdec', hwdec);
          }
          if (Platform.isAndroid) {
            if (_television) {
              final optimized =
                  _televisionDecoder == TelevisionDecoder.optimized;
              await _setPlaybackProperty(platform, 'hwdec-codecs', 'all');
              // MPV auto-safe falls back to software when no supported HW
              // decoder can be initialized; its decoder-failure fallback is
              // also enabled by default (hwdec-software-fallback=3).
              if (_mpvEmbedBackend) {
                // Onn. 4K Pro logs repeatedly report "Failed to create file
                // cache." Keep demuxer cache in RAM instead of creating a
                // temporary on-disk cache in the embedded MPV sandbox.
                try {
                  await _setPlaybackProperty(platform, 'cache-on-disk', 'no');
                  DiaryService.add('[MPV] cache_on_disk=no');
                } catch (error) {
                  DiaryService.add(
                    '[MPV] cache_on_disk_setting_failed=${error.runtimeType}',
                  );
                }
                await _setPlaybackProperty(platform, 'vd-lavc-threads', '4');
              } else {
                await _setPlaybackProperty(
                  platform,
                  'opengl-pbo',
                  optimized ? 'yes' : 'no',
                );
                await _setPlaybackProperty(
                  platform,
                  'video-latency-hacks',
                  optimized ? 'yes' : 'no',
                );
                await _setPlaybackProperty(platform, 'scale', 'bilinear');
                await _setPlaybackProperty(platform, 'cscale', 'bilinear');
                await _setPlaybackProperty(platform, 'dscale', 'bilinear');
                await _setPlaybackProperty(platform, 'correct-downscaling', 'no');
                await _setPlaybackProperty(
                  platform,
                  'vd-lavc-skiploopfilter',
                  optimized ? 'all' : 'default',
                );
                await _setPlaybackProperty(
                  platform,
                  'vd-lavc-skipidct',
                  optimized ? 'all' : 'default',
                );
                await _setPlaybackProperty(platform, 'vd-lavc-threads', '2');
              }
              // Embed's original 4 MiB cap made the active demuxer cache
              // much smaller than PlayerConfiguration.bufferSize (32 MiB).
              // Increase available headroom without waiting for the cache to fill.
              await _setPlaybackProperty(
                platform,
                'demuxer-max-bytes',
                '${(_mpvEmbedBackend ? 16 : 4) * 1024 * 1024}',
              );
              await _setPlaybackProperty(
                platform,
                'demuxer-max-back-bytes',
                '${(_mpvEmbedBackend ? 4 : 1) * 1024 * 1024}',
              );
              await _setPlaybackProperty(
                platform,
                'demuxer-readahead-secs',
                _mpvEmbedBackend ? '8' : '5',
              );
            } else {
              await _setPlaybackProperty(platform, 'hwdec-codecs', 'all');
              if (!_mpvEmbedBackend) {
                await _setPlaybackProperty(platform, 'vd-lavc-skiploopfilter', 'nonkey');
              }
              await _setPlaybackProperty(platform, 'vd-lavc-threads', '4');
            }
            await _setPlaybackProperty(platform, 'vd-lavc-fast', 'yes');
            await _setPlaybackProperty(platform, 'video-sync', 'audio');
            // The Onn. 4K Pro log showed VO frame drops but zero decoder
            // drops while video lagged audio by nearly four seconds.
            // Drop late TV frames at the decoder stage to reduce GPU work.
            await _setPlaybackProperty(
              platform,
              'framedrop',
              _television ? 'decoder' : 'vo',
            );
          }
          await _setPlaybackProperty(
            platform,
            'demuxer-lavf-o',
            [
              'seg_max_retry=3',
              'strict=experimental',
              'allowed_extensions=ALL',
              plan.local
                  ? 'protocol_whitelist=[file,crypto,data]'
                  : 'protocol_whitelist=[http,https,tcp,tls,crypto,data,file]',
              if (plan.decryptionKey.isNotEmpty)
                'decryption_key=${plan.decryptionKey}',
            ].join(','),
          );
          await _setPlaybackProperty(platform, 'network-timeout', '20');
        }
        setupMs = setupClock.elapsedMilliseconds;
        _plan = plan;
        installed = true;
        _acceptErrors = true;
        DiaryService.add(
          '[Play] open generation=$ticket route=${plan.routeIndex}/${plan.routeCount} session=${playbackDiagnosticId(plan.session)} url=${playbackDiagnosticUrl(plan.url)} headers=${plan.headers.keys.toList()}',
        );
        final openClock = Stopwatch()..start();
        await _player
            .open(
              Media(
                plan.url,
                httpHeaders: plan.headers,
                start: position > 0
                    ? Duration(milliseconds: (position * 1000).round())
                    : null,
              ),
              play: _foreground && _playIntent,
            )
            .timeout(
              _playbackOpenTimeout,
              onTimeout: () async {
                openTimedOut = true;
                // Stop the pending native load before the existing recovery
                // pipeline switches routes or recreates the player.
                try {
                  await _player
                      .stop()
                      .timeout(const Duration(seconds: 2));
                } catch (_) {}
                throw TimeoutException(
                  'MPV 開啟串流超過 ${_playbackOpenTimeout.inSeconds} 秒',
                );
              },
            );
        openMs = openClock.elapsedMilliseconds;
        if (_closed || ticket != _generation) {
          return;
        }
        DiaryService.add(
          '[PlayTiming] generation=$ticket cleanupMs=$cleanupMs resolveMs=$resolveMs setupMs=$setupMs openMs=$openMs totalMs=${playClock.elapsedMilliseconds} backend=${_mpvEmbedBackend ? 'mpv_embed' : 'standard'}',
        );
        _openedIndex = index;
        _startSyncDiagnostics();
        _enhancement.mediaReady();
        _attachLanPlayback();
        _health.reset();
        try {
          await _interactions.applySpeed().timeout(
            _playbackSpeedTimeout,
            onTimeout: () => throw TimeoutException(
              '套用播放速度逾時',
              _playbackSpeedTimeout,
            ),
          );
        } on TimeoutException {
          // Rate application should not keep the loading overlay on top of a
          // stream that has already opened successfully.
          DiaryService.add(
            '[Play] speed_apply_timeout generation=$ticket',
          );
        }
        if (mounted && !_closed && ticket == _generation) {
          _loadingOverlayTimer?.cancel();
          setState(() {
            _loading = false;
            _showLoadingOverlay = false;
          });
          _danmaku.setPlan(plan);
          _syncDanmaku();
          _acknowledgeHandoff();
          _menuRevision.value++;
        }
      });
    } catch (error) {
      DiaryService.add(
        '[Play] flow_error generation=$ticket error=${playbackDiagnosticError(error)}',
      );
      if (!_closed && mounted && ticket == _generation) {
        if (prepared != null && identical(_plan, prepared)) {
          _acceptErrors = true;
          if (openTimedOut) {
            DiaryService.add(
              '[Recovery] open_timeout generation=$ticket timeoutMs=${_playbackOpenTimeout.inMilliseconds}',
            );
            // A confirmed open stall should immediately use the existing
            // alternate-route/player-recreation flow.
            unawaited(_recover(reason: 'open_timeout'));
          } else {
            _queueRecovery(reason: 'open_exception');
          }
        } else {
          if (prepared != null) {
            await _releasePlayback(prepared.session, 'failed_preparation');
          }
          if (mounted && !_closed && ticket == _generation) {
            _loadingOverlayTimer?.cancel();
            setState(() {
              _loading = false;
              _showLoadingOverlay = false;
              _localFailure =
                  (error is AppFailure && error.code == 'local_media') ||
                  (widget.localOnly && !_forceOnline);
              _error = error is AppFailure
                  ? error.message
                  : error is TimeoutException
                  ? (error.message ?? '播放載入逾時，請重試或更換片源。')
                  : '無法播放這一集，請重試或換一集。';
              widget.handoff?.fail(_error!);
            });
          }
        }
      } else if (prepared != null && !installed) {
        await _releasePlayback(prepared.session, 'failed_preparation');
      }
    } finally {
      if (warmed != null && !installed && !identical(prepared, warmed)) {
        await _releasePlayback(warmed.session, 'unused_preload');
      }
      if (retained != null) {
        await _releasePlayback(retained!.session, 'fallback_previous');
      }
    }
  }

  void _startSyncDiagnostics() {
    _syncDiagnosticTimer?.cancel();
    if (!_televisionDecoderApplies || _player.platform is! NativePlayer) {
      return;
    }
    unawaited(_logPlaybackSyncHealth());
    _syncDiagnosticTimer = Timer.periodic(const Duration(seconds: 15), (_) {
      if (!_closed && _acceptErrors && _foreground && _player.state.playing) {
        unawaited(_logPlaybackSyncHealth());
      }
    });
  }

  Future<void> _logPlaybackSyncHealth() async {
    final platform = _player.platform;
    if (_closed || !_televisionDecoderApplies || platform is! NativePlayer) {
      return;
    }
    const properties = <String>[
      'avsync',
      'total-avsync-change',
      'decoder-frame-drop-count',
      'frame-drop-count',
      'mistimed-frame-count',
      'video-sync',
      'framedrop',
      'hwdec',
      'hwdec-current',
      'current-vo',
      'opengl-pbo',
      'video-latency-hacks',
      'demuxer-readahead-secs',
      'container-fps',
    ];
    final values = <String>[];
    for (final property in properties) {
      try {
        final value = await platform.getProperty(property);
        values.add('$property=$value');
      } catch (_) {
        values.add('$property=n/a');
      }
    }
    if (_closed) return;
    DiaryService.add(
      '[Sync] generation=$_generation decoder=${_televisionDecoder.name} '
      '${values.join(' ')}',
    );
  }

  Future<void> _logVideoOutput(VideoParams parameters) async {
    final platform = _player.platform;
    if (_closed ||
        parameters.pixelformat == null ||
        platform is! NativePlayer) {
      return;
    }
    final generation = _generation;
    try {
      final hwdec = await platform.getProperty('hwdec-current');
      final vo = await platform.getProperty('current-vo');
      if (_closed) return;
      DiaryService.add(
        '[Video] generation=$generation television=$_television decoder=${_televisionDecoderApplies ? _televisionDecoder.name : 'n/a'} hwdec=$hwdec vo=$vo format=${parameters.pixelformat} hw_format=${parameters.hwPixelformat} size=${parameters.w}x${parameters.h}',
      );
    } catch (_) {}
  }

  Future<void> _switchOnline() async {
    _forceOnline = true;
    await _retry();
  }

  Future<void> _retry({int? quality}) async {
    final position = _currentPosition;
    if (quality != null) {
      _requestedQuality = quality;
    }
    await _play(
      _index,
      position: position,
      playWhenReady: quality == null || _error != null || _player.state.playing,
    );
  }

  bool get _showFullscreen =>
      _television ||
      _fullscreen ||
      (_mobile &&
          !_automaticFullscreenSuppressed &&
          _aspectRatio >= 1 &&
          MediaQuery.orientationOf(context) == Orientation.landscape);

  Future<void> _rotate() async {
    if (_rotating || _television) {
      return;
    }
    final fullscreen = !_showFullscreen;
    final previous = _fullscreen;
    final previousSuppressed = _automaticFullscreenSuppressed;
    _interactions.cancel();
    _rotating = true;
    setState(() {
      _fullscreen = fullscreen;
      _automaticFullscreenSuppressed = !fullscreen;
    });
    try {
      if (Platform.isWindows || Platform.isMacOS) {
        await windowManager.setFullScreen(fullscreen);
      } else if (_mobile || _formFactor == FormFactor.pad) {
        await (_orientationController?.setPlayback(
              this,
              fullscreen: fullscreen,
              aspectRatio: _aspectRatio,
            ) ??
            SystemChrome.setPreferredOrientations(
              AppOrientationController.orientations(
                television: _television,
                phone: _mobile,
                fullscreen: fullscreen,
                aspectRatio: _aspectRatio,
              ),
            ));
      }
    } catch (_) {
      if (mounted && !_closed) {
        setState(() {
          _fullscreen = previous;
          _automaticFullscreenSuppressed = previousSuppressed;
        });
        _notice('無法切換全屏，請重試');
      }
    } finally {
      _rotating = false;
      if (mounted && !_closed) _scheduleSystemUi();
    }
  }

  void _notice(String message) {
    if (!mounted || _closed) return;
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(SnackBar(content: Text(message)));
  }

  Future<void> _setPreferences(PlaybackPreferences preferences) async {
    if (_closed || widget.store.profileEpoch != _profileEpoch) {
      throw StateError('目前使用者已變更');
    }
    _interactions.cancel();
    final nextPreferences = preferences;
    await widget.store.setPlaybackPreferences(nextPreferences);
    if (!mounted || _closed || widget.store.profileEpoch != _profileEpoch) {
      return;
    }
    final qualityChanged = nextPreferences.quality != _requestedQuality;
    final decoderChanged =
        nextPreferences.televisionDecoder != _televisionDecoder;
    _enhancement.setPreferences(nextPreferences.enhancement);
    setState(() {
      _speed = nextPreferences.speed;
      _requestedQuality = nextPreferences.quality;
      _autoAdvance = nextPreferences.autoAdvance;
      _danmakuEnabled = nextPreferences.danmaku;
      _preloadEnabled = nextPreferences.preload ?? _preloadEnabled;
      _portraitBlurBackground = nextPreferences.portraitBlurBackground;
      _televisionDecoder = nextPreferences.televisionDecoder;
    });
    _danmaku.setEnabled(_danmakuEnabled);
    _syncDanmaku();
    if (qualityChanged) _preloader.clear();
    _syncPreload();
    _menuRevision.value++;
    _syncPictureInPicture();
    await _interactions.applySpeed();
    if (qualityChanged && _plan?.local != true) {
      await _retry(quality: nextPreferences.quality);
    } else if (decoderChanged && _televisionDecoderApplies) {
      await _retry();
    }
  }

  Future<void> _toggleFavorite() async {
    try {
      await widget.store.toggleFavorite(widget.detail.drama);
      if (mounted && !_closed) setState(() {});
    } catch (_) {
      _notice('收藏記錄未能儲存，請檢查儲存空間後重試');
    }
  }

  Future<void> _toggleDanmaku() async {
    if (widget.detail.drama.source != 'hongguo') return;
    try {
      await _setPreferences(_preferences.copyWith(danmaku: !_danmakuEnabled));
    } catch (_) {
      _notice('彈幕偏好未能儲存，請重試');
    }
  }

  Future<void> _retryDanmakuFromControls() async {
    try {
      _danmaku.retry();
      _menuRevision.value++;
    } catch (_) {
      _notice('彈幕重試失敗，請稍後再試');
    }
  }

  void _recordNestedRoutePop() {
    _lastNestedRoutePopAt = DateTime.now();
  }

  Widget _guardPlayerDialog(Widget child) => BackSafeDialog(
    onDismissed: _recordNestedRoutePop,
    child: child,
  );

  Future<void> _openPanel(PlayerMenuSection section) async {
    if (_panelOpen || _closed) return;
    _interactions.cancel();
    setState(() => _panelOpen = true);
    final menuTheme = _showFullscreen ? AppTheme.dark : Theme.of(context);
    try {
      final index = await showDialog<int>(
        context: context,
        builder: (menuContext) => _guardPlayerDialog(
          AnimatedBuilder(
          animation: Listenable.merge([
            _menuRevision,
            widget.store,
            _danmaku,
            _preloader,
          ]),
          builder: (_, _) => Theme(
            data: menuTheme,
            child: PlayerMenu(
              section: section,
              episodes: widget.detail.episodes,
              currentIndex: _index,
              preferences: _preferences,
              qualities: _plan?.qualities ?? [],
              actualQuality: _plan?.quality ?? 0,
              local: _plan?.local == true,
              favorite: widget.store.isFavorite(widget.detail.drama.id),
              mobile: _mobile,
              onEpisode: (index) => Navigator.pop(menuContext, index),
              onPreferences: _setPreferences,
              showDanmaku: widget.detail.drama.source == 'hongguo',
              danmakuStatus: _danmaku.status,
              onRetryDanmaku: _danmaku.canRetry ? _danmaku.retry : null,
              preloadStatus: _preloader.status,
              enhancement: _enhancementForUi,
              onCompareEnhancement: () {
                unawaited(_enhancement.toggleCompare());
                Navigator.pop(menuContext);
              },
              onFavorite: () =>
                  widget.store.toggleFavorite(widget.detail.drama),
            ),
          ),
        ),
        ),
      );
      if (mounted && !_closed && index != null && index != _index) {
        await _play(index);
      }
    } finally {
      if (mounted && !_closed) {
        setState(() => _panelOpen = false);
        _playerFocus.requestFocus();
      }
    }
  }

  Future<void> _seekTo(Duration target) async {
    if (_closed || _loading || _error != null) return;
    _handoffOwned = false;
    widget.handoff?.fail('接收端已調整播放位置');
    final ticket = ++_seekSequence;
    final generation = _generation;
    _danmaku.beginSeek();
    _enhancement.ignorePerformance();
    var succeeded = false;
    try {
      await _player.seek(target);
      succeeded = true;
    } catch (_) {
      _notice('跳轉失敗，請重試');
    } finally {
      if (!_closed && ticket == _seekSequence && generation == _generation) {
        _danmaku.endSeek(succeeded ? target : _player.state.position);
      }
    }
  }

  void _seek(int seconds) {
    final desired = _player.state.position + Duration(seconds: seconds);
    final maxDuration = _player.state.duration;
    final target = desired < Duration.zero
        ? Duration.zero
        : maxDuration > Duration.zero && desired > maxDuration
        ? maxDuration
        : desired;
    unawaited(_seekTo(target));
  }

  Future<void> _televisionEpisodes(BuildContext context) async {
    if (_panelOpen || _closed) return;
    setState(() => _panelOpen = true);
    int? index;
    try {
      index = await showDialog<int>(
        context: context,
        builder: (_) => _guardPlayerDialog(
          Theme(
          data: televisionTheme(AppTheme.dark),
          child: TelevisionEpisodeDialog(
            episodes: widget.detail.episodes,
            currentIndex: _index,
          ),
        ),
        ),
      );
    } finally {
      if (mounted && !_closed) setState(() => _panelOpen = false);
    }
    if (index != null && mounted && !_closed && index != _index) {
      await _play(index);
    }
  }

  Future<void> _televisionSettings(BuildContext context) async {
    if (_panelOpen || _closed) return;
    setState(() => _panelOpen = true);
    TelevisionPlaybackSetting? selection;
    try {
      final compatibleRendering = _televisionRenderingApplies
          ? await AppDevice.compatibleRendering()
          : null;
      if (!context.mounted || _closed) return;
      selection = await showDialog<TelevisionPlaybackSetting>(
        context: context,
        builder: (menuContext) => _guardPlayerDialog(
          AnimatedBuilder(
          animation: Listenable.merge([_danmaku, _preloader]),
          builder: (_, _) => Theme(
            data: televisionTheme(AppTheme.dark),
            child: TelevisionSettingsDialog(
              speed: _speed,
              quality: _requestedQuality,
              qualities: _plan?.qualities ?? [],
              favorite: widget.store.isFavorite(widget.detail.drama.id),
              onFavorite: _toggleFavorite,
              autoAdvance: _autoAdvance,
              danmaku: _danmakuEnabled,
              showDanmaku: widget.detail.drama.source == 'hongguo',
              danmakuStatus: _danmaku.status,
              onRetryDanmaku: _danmaku.canRetry ? _danmaku.retry : null,
              preload: _preloadEnabled,
              preloadStatus: _preloader.status,
              enhancement: _enhancementForUi,
              onCompareEnhancement: () {
                unawaited(_enhancement.toggleCompare());
                Navigator.pop(menuContext);
              },
              decoder: _televisionDecoderApplies && !_mpvEmbedBackend
                  ? _televisionDecoder
                  : null,
              compatibleRendering: compatibleRendering,
            ),
          ),
        ),
        ),
      );
    } finally {
      if (mounted && !_closed) setState(() => _panelOpen = false);
    }
    if (selection == null || !mounted || _closed) return;
    final compatibleRendering = selection.compatibleRendering;
    if (compatibleRendering != null) {
      await _switchCompatibleRendering(compatibleRendering);
      return;
    }
    try {
      await _setPreferences(
        _preferences.copyWith(
          speed: selection.speed,
          quality: selection.quality,
          autoAdvance: selection.autoAdvance ?? _autoAdvance,
          danmaku: selection.danmaku,
          preload: selection.preload ?? _preloadEnabled,
          enhancement: selection.enhancement,
          televisionDecoder: selection.decoder,
        ),
      );
    } catch (_) {
      _notice('播放偏好未能儲存，請重試');
    }
  }

  Future<void> _switchCompatibleRendering(bool enabled) async {
    final accepted = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => _guardPlayerDialog(
        Theme(
        data: televisionTheme(AppTheme.dark),
        child: AlertDialog(
          title: Text(enabled ? '開啟相容渲染模式？' : '關閉相容渲染模式？'),
          content: const Text('需要重新啟動 App 才會生效，播放會中斷。'),
          actions: [
            TextButton(
              autofocus: true,
              onPressed: () => Navigator.pop(dialogContext, false),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(dialogContext, true),
              child: const Text('立即重啟'),
            ),
          ],
        ),
      ),
      ),
    );
    if (accepted != true || !mounted || _closed) return;
    if (!await AppDevice.setCompatibleRendering(enabled)) {
      _notice('相容渲染模式未能儲存，請重試');
    }
  }

  void _handlePlaybackBack() {
    // The TV controls are disabled while the playback-error panel is visible.
    // PopScope still intercepts remote Back, so bypass the disabled controls
    // and apply the normal route/dialog back behavior directly.
    if (_error != null) {
      _back();
      return;
    }
    if (_television) {
      _televisionControlsKey.currentState?.handleBack();
    } else {
      _back();
    }
  }

  void _back() {
    final now = DateTime.now();
    final lastNestedPop = _lastNestedRoutePopAt;
    if (lastNestedPop != null &&
        now.difference(lastNestedPop) < const Duration(milliseconds: 350)) {
      return;
    }

    final routeIsCurrent = ModalRoute.of(context)?.isCurrent ?? true;
    if (!routeIsCurrent) {
      _recordNestedRoutePop();
      Navigator.of(context).maybePop();
      return;
    }
    // While an asynchronous dialog is opening, don't pop the video route.
    if (_panelOpen) return;

    if (_television) {
      // The visible control-row action is an explicit exit; physical remote
      // Back is handled separately by _handlePlaybackBack().
      Navigator.of(context).pop();
    } else if (_showFullscreen) {
      _rotate();
    } else {
      Navigator.of(context).maybePop();
    }
  }

  @override
  void dispose() {
    _closed = true;
    final enhancementClosed = _enhancement.close();
    LanController.current?.detachPlayback(_lanIdentity);
    widget.handoff?.fail('接收端已退出播放');
    widget.store.removeListener(_accessChanged);
    _danmaku.dispose();
    _preloader.dispose();
    _generation++;
    WidgetsBinding.instance.removeObserver(this);
    _saveTimer?.cancel();
    _healthTimer?.cancel();
    _loadingOverlayTimer?.cancel();
    _syncDiagnosticTimer?.cancel();
    _errorTimer?.cancel();
    _pictureInPictureExitTimer?.cancel();
    if (_pictureInPictureHandlerInstalled) {
      unawaited(
        AppDevice.channel.invokeMethod<void>('updatePictureInPicture', null)
            .catchError((Object _) {}),
      );
      AppDevice.channel.setMethodCallHandler(null);
    }
    _interactions.dispose();
    _playerFocus.dispose();
    _menuRevision.dispose();
    unawaited(_saveProgress(flush: true));
    for (final subscription in _subscriptions) {
      subscription.cancel();
    }
    unawaited(_releasePlayback(_session, 'screen_dispose'));
    unawaited(_loader.close().catchError((Object _) {}));
    unawaited(
      _operations.catchError((Object _) {}).then((_) async {
        await _interactions.pendingRates.catchError((Object _) {});
        await enhancementClosed.catchError((Object _) {});
        await _player.dispose();
      }),
    );
    if (Platform.isWindows || Platform.isMacOS) {
      unawaited(windowManager.setFullScreen(false));
    } else if (_mobile ||
        _formFactor == FormFactor.pad ||
        _television && Platform.isAndroid) {
      unawaited(
        (_orientationController?.releasePlayback(this) ??
                SystemChrome.setPreferredOrientations(
                  AppOrientationController.orientations(
                    television: _television,
                    phone: _mobile,
                  ),
                ))
            .catchError((Object _) {}),
      );
      unawaited(SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge));
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final inherited = Theme.of(context);
    final theme = _television ? televisionTheme(inherited) : inherited;
    return Theme(
      data: theme,
      child: Builder(
        builder: (context) {
          final fullscreen = _showFullscreen;
          final overlayBrightness = fullscreen
              ? Brightness.dark
              : Theme.of(context).brightness;
          return AnnotatedRegion<SystemUiOverlayStyle>(
            value: AppTheme.systemBars(overlayBrightness),
            child: _buildPlayer(context),
          );
        },
      ),
    );
  }

  Widget _buildPlayer(BuildContext context) {
    final theme = Theme.of(context);
    final title = uiTraditional(widget.detail.drama.title);
    final fullscreen = _showFullscreen;
    final pictureInPicture = _pictureInPictureVisible;
    return PopScope(
      // Android TV Back is consumed by the playback controls, never the video route.
      canPop: !_television && !fullscreen,
      onPopInvokedWithResult: (didPop, result) {
        if (didPop) return;
        if (_television) {
          _handlePlaybackBack();
        } else if (fullscreen) {
          _rotate();
        }
      },
      child: CallbackShortcuts(
        bindings: {
          const SingleActivator(LogicalKeyboardKey.escape): _handlePlaybackBack,
          const SingleActivator(LogicalKeyboardKey.goBack): _handlePlaybackBack,
        },
        child: Focus(
          focusNode: _playerFocus,
          onKeyEvent: (_, event) {
            if (_television ||
                _panelOpen ||
                !_playerFocus.hasPrimaryFocus ||
                !(ModalRoute.of(context)?.isCurrent ?? true)) {
              return KeyEventResult.ignored;
            }
            return _interactions.key(event);
          },
          autofocus: !_television,
          canRequestFocus: !_television,
          skipTraversal: _television,
          child: Scaffold(
            backgroundColor: fullscreen || pictureInPicture
                ? Colors.black
                : theme.scaffoldBackgroundColor,
            appBar: pictureInPicture || fullscreen || _mobile
                ? null
                : AppBar(
                    title: Text(
                      title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                    actions: [
                      IconButton(
                        tooltip: '旋轉與全屏',
                        onPressed: _rotate,
                        icon: const Icon(Icons.screen_rotation_alt_rounded),
                      ),
                    ],
                  ),
            body: pictureInPicture
                ? _videoPane(context)
                : SafeArea(
                    top: !_television && (fullscreen || _mobile),
                    bottom: !fullscreen,
                    child: LayoutBuilder(
                      builder: (context, constraints) {
                        final desktop =
                            (_formFactor == FormFactor.pad ||
                                _formFactor == FormFactor.window) &&
                            constraints.maxWidth >= 840;
                        if (fullscreen) {
                          return _videoPane(context);
                        }
                        if (desktop ||
                            constraints.maxWidth >
                                constraints.maxHeight * 1.3) {
                          return Row(
                            children: [
                              Expanded(
                                child: Column(
                                  children: [
                                    Expanded(child: _videoPane(context)),
                                  ],
                                ),
                              ),
                              SizedBox(
                                width: desktop ? 312 : 210,
                                child: _episodePanel(),
                              ),
                            ],
                          );
                        }
                        if (_mobile) {
                          final height = (constraints.maxWidth / _aspectRatio)
                              .clamp(0.0, constraints.maxHeight * .56);
                          return Column(
                            children: [
                              SizedBox(
                                height: height,
                                width: double.infinity,
                                child: _videoPane(context),
                              ),
                              Expanded(child: _mobilePlaybackPanel()),
                            ],
                          );
                        }
                        final height = (constraints.maxWidth / _aspectRatio)
                            .clamp(0.0, constraints.maxHeight * .64);
                        return Column(
                          children: [
                            SizedBox(
                              height: height,
                              width: double.infinity,
                              child: _videoPane(context),
                            ),
                            Expanded(child: _episodePanel()),
                          ],
                        );
                      },
                    ),
                  ),
          ),
        ),
      ),
    );
  }

  Widget _videoOutput({required BoxFit fit, required Widget controls}) {
    if (_player is LunaExoPlayer) {
      return LunaExoVideoView(
        player: _player as LunaExoPlayer,
        fit: fit,
        controls: (_) => controls,
      );
    }
    final textureRendererAvailable =
        (Platform.isWindows ||
            (Platform.isAndroid && _androidPlayerEngine != 'exo')) &&
        _player.platform is NativePlayer;
    final portraitBlurActive = shouldUsePortraitBlurBackground(
      enabled: _portraitBlurBackground,
      textureRendererAvailable: textureRendererAvailable,
      loading: _loading,
      hasError: _error != null,
      aspectRatio: _aspectRatio,
    );
    if (!portraitBlurActive) {
      return Video(
        controller: _video!,
        fit: fit,
        fill: Colors.black,
        controls: (_) => controls,
      );
    }

    // Both Video widgets subscribe to the same VideoController texture ID.
    // MPV decodes once; Flutter composites that one live texture twice:
    // cover + blur behind, contain + controls in front.
    return Stack(
      fit: StackFit.expand,
      children: [
        Positioned.fill(
          child: ClipRect(
            child: ImageFiltered(
              imageFilter: ui.ImageFilter.blur(sigmaX: 26, sigmaY: 26),
              child: Transform.scale(
                scale: 1.08,
                child: Video(
                  controller: _video!,
                  fit: BoxFit.cover,
                  fill: Colors.black,
                  controls: null,
                  subtitleViewConfiguration:
                      const SubtitleViewConfiguration(visible: false),
                  wakelock: false,
                  pauseUponEnteringBackgroundMode: false,
                ),
              ),
            ),
          ),
        ),
        Positioned.fill(
          child: ColoredBox(color: Colors.black.withValues(alpha: .24)),
        ),
        Positioned.fill(
          child: Video(
            controller: _video!,
            fit: fit,
            fill: Colors.transparent,
            controls: (_) => controls,
          ),
        ),
      ],
    );
  }

  Widget _videoPane(BuildContext context) {
    final videoTheme = _television
        ? televisionTheme(AppTheme.dark)
        : AppTheme.dark;
    final title = uiTraditional(
      '${uiTraditional(widget.detail.drama.title)} · 第 ${widget.detail.episodes[_index].number} 集'
      '${_plan?.local == true ? ' · 本機' : ''}'
      '${widget.detail.episodes[_index].vip ? ' · VIP 試看' : ''}',
    );
    final hideOverlayForPictureInPicture = _pictureInPictureVisible;
    final Widget controls = hideOverlayForPictureInPicture
        ? const SizedBox.shrink()
        : _television
        ? TelevisionControls(
            key: _televisionControlsKey,
            player: _player,
            playbackGeneration: _generation,
            title: title,
            enabled: !_loading && _error == null,
            // On TV, start with a clean video. The user can reveal controls
            // by pressing OK/Select; loading/playing events must not flash them.
            showOnPlaybackReady: playbackControlsVisibleOnReady(
              television: true,
              requested: _showControlsOnPlaybackReady,
            ),
            enhancement: _enhancementForUi,
            onTogglePlayback: _togglePlayback,
            onSeek: _seek,
            onPrevious: _index > 0 ? () => _play(_index - 1) : null,
            onNext: _index + 1 < widget.detail.episodes.length
                ? () => _play(_index + 1)
                : null,
            onEpisodes: () => _televisionEpisodes(context),
            onSettings: () => _televisionSettings(context),
            onBack: _back,
          )
        : PlayerControls(
            player: _player,
            playbackGeneration: _generation,
            interactions: _interactions,
            enabled: !_loading && _error == null,
            enhancement: _enhancementForUi,
            panelOpen: _panelOpen,
            fullscreen: _showFullscreen,
            showOnPlaybackReady: _showControlsOnPlaybackReady,
            title: title,
            onTogglePlayback: _togglePlayback,
            swipeEnabled: _mobile,
            onFullscreen: _rotate,
            onBack: _back,
            onFocusSurface: _playerFocus.requestFocus,
            onSeek: _seekTo,
            speed: _speed,
            qualityLabel: _qualityLabel,
            showDanmaku: widget.detail.drama.source == 'hongguo',
            danmakuEnabled: _danmakuEnabled,
            danmakuStatus: _danmaku.status,
            onEpisodes: () => _openPanel(PlayerMenuSection.episodes),
            onSpeed: () => _openPanel(PlayerMenuSection.speed),
            onQuality: () => _openPanel(PlayerMenuSection.quality),
            onDanmaku: widget.detail.drama.source == 'hongguo'
                ? _toggleDanmaku
                : null,
            onRetryDanmaku: _danmaku.canRetry
                ? _retryDanmakuFromControls
                : null,
            onPush: widget.mediaId == null ? _pushToDevice : null,
            onPictureInPicture: _canUsePictureInPicture
                ? _enterPictureInPicture
                : null,
            onPrevious: _index > 0 ? () => _play(_index - 1) : null,
            onNext: _index + 1 < widget.detail.episodes.length
                ? () => _play(_index + 1)
                : null,
          );
    final layeredControls = Stack(
      fit: StackFit.expand,
      children: [
        if (!hideOverlayForPictureInPicture)
          DanmakuOverlay(controller: _danmaku, aspectRatio: _aspectRatio),
        controls,
      ],
    );
    return Theme(
      data: videoTheme,
      child: LayoutBuilder(
        builder: (context, constraints) {
          final ratio = MediaQuery.devicePixelRatioOf(context);
          final pixels = Size(
            constraints.maxWidth * ratio,
            constraints.maxHeight * ratio,
          );
          WidgetsBinding.instance.addPostFrameCallback((_) {
            if (mounted && !_closed) {
              _enhancement.setViewport(pixels, television: _television);
            }
          });
          return Stack(
            key: _videoPaneKey,
            fit: StackFit.expand,
            children: [
              if (widget.videoBuilder != null)
                widget.videoBuilder!(layeredControls)
              else
                _videoOutput(
                  fit: BoxFit.contain,
                  controls: layeredControls,
                ),
              if (_loading &&
                  _showLoadingOverlay &&
                  !hideOverlayForPictureInPicture)
                ColoredBox(
                  color: Colors.black.withValues(alpha: .78),
                  child: Center(
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        const CircularProgressIndicator(),
                        const SizedBox(height: 16),
                        Text(_loadingMessage),
                      ],
                    ),
                  ),
                ),
              if (_error != null && !hideOverlayForPictureInPicture)
                ColoredBox(
                  color: Colors.black.withValues(alpha: .9),
                  child: StatusPanel(
                    title: '暫時無法播放',
                    message: uiTraditional(_error!),
                    onRetry: () => _retry(),
                    action: _localFailure ? '重試本機播放' : '重試播放',
                    secondaryAction: Wrap(
                      spacing: 12,
                      runSpacing: 10,
                      alignment: WrapAlignment.center,
                      crossAxisAlignment: WrapCrossAlignment.center,
                      children: [
                        FilledButton.tonalIcon(
                          onPressed: () => DiaryService.showDiaryDialog(
                            context,
                            onDialogDismissed: _recordNestedRoutePop,
                          ),
                          icon: const Icon(Icons.receipt_long_rounded),
                          label: const Text('檢視播放紀錄'),
                        ),
                        if (_localFailure && widget.allowOnlineFallback)
                          TextButton.icon(
                            onPressed: _switchOnline,
                            icon: const Icon(Icons.cloud_outlined),
                            label: const Text('改為線上播放'),
                          )
                        else if (!_localFailure &&
                            !widget.localOnly &&
                            widget.repository.supportsSourceManagement)
                          SourceDiagnosticsButton(
                            repository: widget.repository,
                            store: widget.store,
                            drama: widget.detail.drama,
                          ),
                      ],
                    ),
                    icon: Icons.play_disabled_rounded,
                  ),
                ),
              if ((_loading || _error != null) &&
                  !hideOverlayForPictureInPicture &&
                  _showFullscreen &&
                  !_television)
                SafeArea(
                  child: Align(
                    alignment: Alignment.topCenter,
                    child: Row(
                      children: [
                        IconButton(
                          tooltip: '退出全屏',
                          onPressed: _rotate,
                          icon: const Icon(Icons.arrow_back_rounded),
                        ),
                        const Spacer(),
                        TextButton(
                          onPressed: () =>
                              _openPanel(PlayerMenuSection.episodes),
                          child: const Text('選集'),
                        ),
                      ],
                    ),
                  ),
                ),
              if (_saveWarning != null && !hideOverlayForPictureInPicture)
                Positioned(
                  top: 52,
                  left: 12,
                  right: 12,
                  child: SafeArea(
                    bottom: false,
                    child: Material(
                      color: const Color(0xE6322424),
                      borderRadius: BorderRadius.circular(8),
                      child: Padding(
                        padding: const EdgeInsets.all(8),
                        child: Wrap(
                          crossAxisAlignment: WrapCrossAlignment.center,
                          children: [
                            Text(
                              _saveWarning!,
                              style: const TextStyle(fontSize: 12),
                            ),
                            TextButton(
                              onPressed: _saveProgress,
                              child: const Text('重試儲存'),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                ),
            ],
          );
        },
      ),
    );
  }

  Widget _mobilePlaybackPanel() {
    final colors = Theme.of(context).colorScheme;
    return ColoredBox(
      color: Theme.of(context).scaffoldBackgroundColor,
      child: SafeArea(
        top: false,
        child: Column(
          children: [
            _mobileTabs(colors),
            Expanded(
              child: Padding(
                padding: const EdgeInsets.fromLTRB(8, 6, 8, 8),
                child: ClipRRect(
                  borderRadius: BorderRadius.circular(8),
                  child: _mobileTab == 0
                      ? _episodePanel(compact: true)
                      : _mobileSynopsis(),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _mobileTabs(ColorScheme colors) => Padding(
    padding: const EdgeInsets.fromLTRB(8, 2, 8, 0),
    child: SizedBox(
      height: 42,
      child: Row(
        children: [
          _mobileTabButton(0, '選集'),
          _mobileTabButton(1, '簡介'),
        ],
      ),
    ),
  );

  Widget _mobileTabButton(int value, String label) {
    final selected = _mobileTab == value;
    final colors = Theme.of(context).colorScheme;
    return Expanded(
      child: InkWell(
        onTap: () => setState(() => _mobileTab = value),
        child: AnimatedContainer(
          duration: const Duration(milliseconds: 120),
          alignment: Alignment.center,
          decoration: BoxDecoration(
            border: Border(
              bottom: BorderSide(
                color: selected ? colors.primary : Colors.transparent,
                width: 2,
              ),
            ),
          ),
          child: Text(
            label,
            style: TextStyle(
              color: selected ? colors.onSurface : colors.onSurfaceVariant,
              fontSize: 13,
              fontWeight: selected ? FontWeight.w700 : FontWeight.w500,
            ),
          ),
        ),
      ),
    );
  }

  Widget _mobileSynopsis() {
    final drama = widget.detail.drama;
    final colors = Theme.of(context).colorScheme;
    final meta = [
      SourceSite.byId(drama.source).name,
      if (drama.episodes > 0) '共 ${drama.episodes} 集',
      if (drama.releaseStatus.isNotEmpty && drama.releaseStatus != 'unknown')
        drama.releaseLabel,
      if (drama.category.isNotEmpty) uiTraditional(drama.category),
    ];
    final body = drama.description.trim().isEmpty ? '暫無簡介' : uiTraditional(drama.description);
    return ColoredBox(
      color: colors.surface,
      child: ListView(
        padding: const EdgeInsets.all(12),
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              SizedBox(
                width: 82,
                height: 123,
                child: DramaCover(drama: drama, repository: widget.repository),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      uiTraditional(drama.title),
                      style: Theme.of(context).textTheme.titleMedium?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    if (meta.isNotEmpty) ...[
                      const SizedBox(height: 6),
                      Text(
                        meta.join(' · '),
                        style: TextStyle(color: colors.onSurfaceVariant),
                      ),
                    ],
                    if (drama.onlineDate.isNotEmpty ||
                        drama.heat.isNotEmpty ||
                        drama.views.isNotEmpty) ...[
                      const SizedBox(height: 8),
                      Text(
                        [
                          if (drama.onlineDate.isNotEmpty)
                            '${drama.onlineDate} 上線',
                          if (drama.heat.isNotEmpty) '熱度 ${drama.heat}',
                          if (drama.views.isNotEmpty) '播放 ${drama.views}',
                        ].join(' · '),
                        style: TextStyle(color: colors.onSurfaceVariant),
                      ),
                    ],
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 10),
          _mobileFollowControl(drama),
          if (drama.tags.isNotEmpty) ...[
            const SizedBox(height: 10),
            Wrap(
              spacing: 6,
              runSpacing: 6,
              children: [
                for (final tag in drama.tags.take(12))
                  Chip(label: Text(uiTraditional(tag)), visualDensity: VisualDensity.compact),
              ],
            ),
          ],
          const SizedBox(height: 12),
          Text(body, style: const TextStyle(height: 1.55)),
          if (widget.detail.warning.isNotEmpty) ...[
            const SizedBox(height: 12),
            Text(uiTraditional(widget.detail.warning), style: TextStyle(color: colors.error)),
          ],
        ],
      ),
    );
  }

  Widget _mobileFollowControl(Drama drama) {
    final following = widget.store.following(drama.id);
    return FilledButton.tonalIcon(
      key: const ValueKey('player-favorite-toggle'),
      onPressed: () async {
        if (_profileEpoch != widget.store.profileEpoch) return;
        await saveUserChange(
          context,
          () => widget.store.toggleFavorite(drama),
        );
        if (mounted && !_closed) setState(() {});
      },
      icon: Icon(
        following == null
            ? Icons.bookmark_add_outlined
            : Icons.bookmark_remove_outlined,
      ),
      label: Text(following == null ? '加入收藏' : '取消收藏'),
    );
  }

  Widget _episodePanel({bool compact = false}) => ColoredBox(
    color: Theme.of(context).colorScheme.surface,
    child: PlayerEpisodeGrid(
      episodes: widget.detail.episodes,
      currentIndex: _index,
      compact: compact,
      title: compact ? '劇集' : '選集',
      note:
          widget.detail.drama.releaseStatus.isNotEmpty &&
              widget.detail.drama.releaseStatus != 'unknown'
          ? widget.detail.drama.releaseLabel
          : '',
      onSelected: (index) => _play(index),
    ),
  );
}
