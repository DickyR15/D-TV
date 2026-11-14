import 'dart:io';
import 'dart:async';

import 'package:flutter/foundation.dart' show listEquals;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:media_kit/media_kit.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:window_manager/window_manager.dart';
import 'package:screen_retriever/screen_retriever.dart';

import 'desktop_window_state.dart';

import 'core_bridge.dart';
import 'device_registration.dart';
import 'app_layout.dart';
import 'app_orientation.dart';
import 'app_theme.dart';
import 'home_screen.dart';
import 'local_store.dart';
import 'profiles_screen.dart';
import 'media_library.dart';
import 'lan_controller.dart';
import 'player_screen.dart';
import 'video_enhancement_assets.dart';
import 'diary_service.dart';
import 'app_navigation.dart';
import 'ui_localization.dart';

Future<void> main(List<String> args) async {
  WidgetsFlutterBinding.ensureInitialized();
  if (Platform.isAndroid) {
    unawaited(SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge));
    SystemChrome.setSystemUIOverlayStyle(AppTheme.systemBars(Brightness.dark));
  }
  final desktop = Platform.isWindows || Platform.isMacOS;
  if (desktop) {
    await windowManager.ensureInitialized();
  }
  MediaKit.ensureInitialized();
  VideoEnhancementAssets.registerLicenses();

  if (desktop) {
    windowManager.waitUntilReadyToShow(
      const WindowOptions(
        size: Size(1280, 800),
        minimumSize: Size(900, 560),
        center: true,
        title: 'D TV',
      ),
      () async {
        await windowManager.setMinimumSize(const Size(900, 560));
        await windowManager.show();
      },
    );
  }

  // Draw the first Flutter frame before loading and parsing the OpenCC dictionaries.
  runApp(const AppBootstrap());
  WidgetsBinding.instance.addPostFrameCallback((_) {
    unawaited(_initializeUiLocalizationSafely());
  });
}

Future<void> _initializeUiLocalizationSafely() async {
  try {
    await initializeUiLocalization();
  } catch (_) {
    // Keep the built-in fallback dictionary if bundled conversion data cannot load.
  }
}

class AppBootstrap extends StatefulWidget {
  const AppBootstrap({super.key, this.device = const AppDevice()});
  final AppDevice device;
  @override
  State<AppBootstrap> createState() => _AppBootstrapState();
}

class _AppBootstrapState extends State<AppBootstrap>
    with WidgetsBindingObserver {
  static const _desktopWindowSaveDelay = Duration(milliseconds: 320);
  SharedPreferences? _desktopPreferences;
  Timer? _desktopWindowSaveTimer;
  bool _restoringDesktopWindow = false;
  late final _desktopWindowListener = _DesktopWindowListener(
    onGeometryChanged: _scheduleDesktopWindowSave,
    onClose: () => unawaited(_saveDesktopWindowState()),
  );

  bool get _isDesktopWindow =>
      Platform.isWindows || Platform.isMacOS;

  void _scheduleDesktopWindowSave() {
    if (!_isDesktopWindow ||
        _desktopPreferences == null ||
        _restoringDesktopWindow) {
      return;
    }
    _desktopWindowSaveTimer?.cancel();
    _desktopWindowSaveTimer = Timer(_desktopWindowSaveDelay, () {
      _desktopWindowSaveTimer = null;
      unawaited(_saveDesktopWindowState());
    });
  }

  Future<bool> _isOnConnectedDisplay(Rect bounds) async {
    try {
      final displays = await ScreenRetriever.instance.getAllDisplays();
      final workAreas = displays.map((display) {
        final position = display.visiblePosition ?? Offset.zero;
        final size = display.visibleSize ?? display.size;
        return Rect.fromLTWH(
          position.dx,
          position.dy,
          size.width,
          size.height,
        );
      });
      return desktopWindowOverlapsDisplay(bounds, workAreas);
    } catch (_) {
      // If display enumeration itself fails, retain saved geometry.
      return true;
    }
  }

  Future<void> _showDefaultDesktopWindow() async {
    await windowManager.setSize(const Size(1280, 800));
    await windowManager.center();
  }

  Future<void> _restoreDesktopWindowState(
    SharedPreferences preferences,
  ) async {
    if (!_isDesktopWindow) return;
    _desktopPreferences = preferences;
    final saved = DesktopWindowState.tryParse(
      preferences.getString(DesktopWindowState.preferencesKey),
    );
    _restoringDesktopWindow = true;
    try {
      if (await windowManager.isFullScreen()) {
        await windowManager.setFullScreen(false);
      }
      if (await windowManager.isMaximized()) {
        await windowManager.unmaximize();
      }
      if (saved != null && await _isOnConnectedDisplay(saved.bounds)) {
        await windowManager.setBounds(saved.bounds);
      } else {
        await _showDefaultDesktopWindow();
      }
    } catch (_) {
      await _showDefaultDesktopWindow();
    } finally {
      _restoringDesktopWindow = false;
    }
  }

  Future<void> _saveDesktopWindowState() async {
    final preferences = _desktopPreferences;
    if (!_isDesktopWindow || preferences == null || _restoringDesktopWindow) {
      return;
    }
    try {
      final fullScreen = await windowManager.isFullScreen();
      final maximized = await windowManager.isMaximized();
      final minimized = await windowManager.isMinimized();
      if (fullScreen || maximized || minimized) return;
      final bounds = await windowManager.getBounds();
      // The original desktop window has a 900x560 minimum. A smaller frame
      // is the in-place mini-player and must never replace the saved main size.
      if (bounds.width < 900 || bounds.height < 560) return;
      final current = DesktopWindowState.fromRect(bounds);
      if (current != null) {
        await preferences.setString(
          DesktopWindowState.preferencesKey,
          current.encode(),
        );
      }
    } catch (_) {
      // Window-state persistence must never interfere with playback or exit.
    }
  }
  final repository = NativeRepository();
  final navigator = GlobalKey<NavigatorState>();
  LocalStore? store;
  Object? error;
  SystemAccentColors systemAccentColors = (light: null, dark: null);
  late AppDevice device = widget.device;

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    if (_isDesktopWindow) {
      windowManager.removeListener(_desktopWindowListener);
      _desktopWindowSaveTimer?.cancel();
      unawaited(_saveDesktopWindowState());
    }
    LanController.current?.dispose();
    LanController.current = null;
    MediaLibrary.current?.dispose();
    MediaLibrary.current = null;
    store?.dispose();
    super.dispose();
  }

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    if (_isDesktopWindow) windowManager.addListener(_desktopWindowListener);
    _initialize();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      if (Platform.isAndroid) unawaited(_refreshDevice());
      if (Platform.isAndroid ||
          Platform.isIOS ||
          Platform.isWindows ||
          Platform.isMacOS) {
        unawaited(_refreshSystemAccentColors());
      }
    }
    if (!Platform.isIOS) return;
    final library = MediaLibrary.current;
    if (state == AppLifecycleState.paused ||
        state == AppLifecycleState.detached) {
      if (library != null) {
        library.suspended = true;
        unawaited(library.cancel());
      }
      unawaited(
        NativeRepository(
          background: true,
        ).controlDownloads('pauseAll').catchError((Object _) {}),
      );
    } else if (state == AppLifecycleState.resumed) {
      if (library != null) library.suspended = false;
    }
  }

  Future<void> _refreshSystemAccentColors() async {
    final colors = await AppDevice.systemAccentColors();
    if (!mounted || colors == systemAccentColors) return;
    setState(() => systemAccentColors = colors);
  }

  Future<void> _refreshDevice() async {
    final detected = await AppDevice.detect(fallback: device);
    if (!mounted ||
        (device.television == detected.television &&
            device.emulator == detected.emulator &&
            listEquals(device.emulatorIndicators, detected.emulatorIndicators) &&
            device.version == detected.version)) {
      return;
    }
    setState(() => device = detected);
    DiaryService.add(
      '[Device] television=${detected.television}, '
      'emulator=${detected.emulator}, '
      'signals=${detected.emulatorIndicators.join("|")}',
    );
  }

  Future<void> _logStartupRendering() async {
    try {
      final rendering = Platform.isAndroid
          ? ', 渲染: ${await AppDevice.compatibleRendering() ? 'skia' : 'impeller'}'
          : '';
      DiaryService.add(
        '[App] 應用啟動，版本: ${device.version}, '
        '平臺: ${Platform.operatingSystem}, '
        'TV=${device.television}, emulator=${device.emulator}, '
        'signals=${device.emulatorIndicators.join("|")}$rendering',
      );
    } catch (_) {
      // Diagnostics must never delay application startup.
    }
  }

  Future<void> _initialize() async {
    setState(() {
      error = null;
    });
    try {
      final preferences = await SharedPreferences.getInstance();
      await _restoreDesktopWindowState(preferences);
      await _refreshDevice();
      final accents = await AppDevice.systemAccentColors();
      unawaited(_logStartupRendering());
      await repository.initialize();
      final localStore = await LocalStore.open(preferences);
      if (mounted) {
        setState(() {
          systemAccentColors = accents;
          store = localStore;
          repository.access = store;
          MediaLibrary.attach(repository, store!);
          LanController.current?.dispose();
          final link = LanController(
            repository,
            store!,
            kind: (device.television || device.emulator)
                ? 'tv'
                : Platform.isWindows || Platform.isMacOS
                ? 'computer'
                : 'phone',
          );
          LanController.current = link;
          link.openPlayback = (request) async {
            final epoch = request.profileEpoch;
            if (request.cancelled ||
                !mounted ||
                store!.locked ||
                !link.receiving ||
                store!.profileEpoch != epoch ||
                !store!.allowsSource(request.detail.drama.source)) {
              throw StateError('目前使用者無法接收播放內容');
            }
            await link.playbackHost?.stop();
            if (request.cancelled ||
                !mounted ||
                store!.locked ||
                store!.profileEpoch != epoch ||
                !link.receiving) {
              throw StateError('播放接收已取消');
            }
            final navigation = navigator.currentState;
            if (navigation == null) throw StateError('接收裝置介面尚未就緒');
            unawaited(
              navigation.pushAndRemoveUntil<void>(
                MaterialPageRoute<void>(
                  builder: (_) => PlayerScreen(
                    detail: request.detail,
                    initialIndex: request.index,
                    initialPosition: request.position,
                    repository: repository,
                    store: store!,
                    handoff: request,
                  ),
                ),
                (route) => route.isFirst,
              ),
            );
          };
        });
      }
    } catch (failure) {
      if (mounted) {
        setState(() {
          error = failure;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) => DuanjuApp(
    repository: repository,
    store: store,
    bootstrapError: error?.toString(),
    onRetry: _initialize,
    television: device.television,
    emulator: device.emulator,
    version: device.version,
    navigatorKey: navigator,
    systemAccentColors: systemAccentColors,
  );
}


class _DesktopWindowListener with WindowListener {
  _DesktopWindowListener({
    required this.onGeometryChanged,
    required this.onClose,
  });

  final VoidCallback onGeometryChanged;
  final VoidCallback onClose;

  @override
  void onWindowMoved() => onGeometryChanged();

  @override
  void onWindowResized() => onGeometryChanged();

  @override
  void onWindowMaximize() {}

  @override
  void onWindowUnmaximize() => onGeometryChanged();

  @override
  void onWindowLeaveFullScreen() => onGeometryChanged();

  @override
  void onWindowClose() => onClose();
}

class DuanjuApp extends StatelessWidget {
  const DuanjuApp({
    super.key,
    required this.repository,
    this.store,
    this.bootstrapError,
    this.onRetry,
    this.television = false,
    this.emulator = false,
    this.version = appVersion,
    this.navigatorKey,
    this.systemAccentColors = (light: null, dark: null),
  });
  final AppRepository repository;
  final LocalStore? store;
  final String? bootstrapError;
  final VoidCallback? onRetry;
  final bool television;
  final bool emulator;
  final String version;
  final GlobalKey<NavigatorState>? navigatorKey;
  final SystemAccentColors systemAccentColors;

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: Listenable.merge([store]),
    builder: (_, _) => _application(),
  );

  Future<void> _toggleDesktopFullscreen() async {
    if (!Platform.isWindows && !Platform.isMacOS) return;
    try {
      final fullScreen = await windowManager.isFullScreen();
      await windowManager.setFullScreen(!fullScreen);
    } catch (_) {
      // Unsupported window managers should leave the current window unchanged.
    }
  }

  Widget _application() => MaterialApp(
    navigatorKey: navigatorKey,
    navigatorObservers: [appNavigationObserver],
    title: appName,
    debugShowCheckedModeBanner: false,
    locale: const Locale('zh', 'TW'),
    supportedLocales: const [Locale('zh', 'TW')],
    localizationsDelegates: GlobalMaterialLocalizations.delegates,
    theme: AppTheme.lightFor(
      AppTheme.accentFor(
        store?.themeAccent ?? 'system',
        systemAccent: systemAccentColors.light,
      ),
    ),
    darkTheme: AppTheme.darkFor(
      AppTheme.accentFor(
        store?.themeAccent ?? 'system',
        systemAccent: systemAccentColors.dark,
      ),
      pureBlack: store?.pureBlackTheme ?? false,
    ),
    themeMode: store == null ? ThemeMode.dark : AppTheme.mode(store?.themeMode ?? 'system'),
    builder: (context, child) {
      final mode = store?.displayMode ?? 'auto';
      final screenSize = MediaQuery.maybeOf(context)?.size ?? Size.zero;
      final formFactor = AppLayout.resolveFormFactor(
        mode: mode,
        detectedTelevision: television,
        detectedEmulator: emulator,
        screenSize: screenSize,
      );
      final tv = formFactor == FormFactor.television;
      WidgetsBinding.instance.addPostFrameCallback(
        (_) => FocusManager.instance.highlightStrategy = tv
            ? FocusHighlightStrategy.alwaysTraditional
            : FocusHighlightStrategy.automatic,
      );
      final theme = Theme.of(context);
      return Stack(
        fit: StackFit.expand,
        children: [
          AnnotatedRegion<SystemUiOverlayStyle>(
        value: AppTheme.systemBars(theme.brightness),
        child: ColoredBox(
          color: theme.scaffoldBackgroundColor,
          child: AppOrientationScope(
            television: tv,
            phone: formFactor == FormFactor.phone,
            child: AppLayout(
              television: tv,
              detectedTelevision: television,
              formFactor: formFactor,
              version: version,
              child: Theme(
                data: tv ? televisionTheme(theme) : theme,
                child: Actions(
                  actions: {
                    ToggleDesktopFullscreenIntent:
                        CallbackAction<ToggleDesktopFullscreenIntent>(
                          onInvoke: (_) {
                            unawaited(_toggleDesktopFullscreen());
                            return null;
                          },
                        ),
                  },
                  child: Shortcuts(
                    shortcuts: {
                      ...desktopRootShortcuts,
                      const SingleActivator(
                        LogicalKeyboardKey.select,
                        includeRepeats: false,
                      ): const ActivateIntent(),
                      const SingleActivator(
                        LogicalKeyboardKey.gameButtonA,
                        includeRepeats: false,
                      ): const ActivateIntent(),
                      const SingleActivator(
                        LogicalKeyboardKey.goBack,
                      ): const DismissIntent(),
                    },
                    child: FocusTraversalGroup(child: child!),
                  ),
                ),
              ),
            ),
          ),
        ),
          ),
        ],
      );
    },
    home: AnimatedSwitcher(
      duration: const Duration(milliseconds: 580),
      switchInCurve: Curves.easeOutCubic,
      switchOutCurve: Curves.easeInCubic,
      transitionBuilder: (child, animation) => FadeTransition(
        opacity: animation,
        child: ScaleTransition(
          scale: Tween<double>(begin: 0.985, end: 1.0).animate(
            CurvedAnimation(parent: animation, curve: Curves.easeOutCubic),
          ),
          child: child,
        ),
      ),
      child: store != null
          ? store!.locked
                ? KeyedSubtree(
                    key: ValueKey('locked-${store!.profileEpoch}'),
                    child: ProfilesScreen(store: store!, locked: true),
                  )
                : DeviceRegistrationGate(
                    repository: repository,
                    store: store!,
                    child: HomeScreen(
                      key: ValueKey(
                        'profile-${store!.profile.id}-${store!.profileEpoch}',
                      ),
                      repository: repository,
                      store: store!,
                    ),
                  )
          : KeyedSubtree(
              key: ValueKey<String>(
                bootstrapError == null ? 'bootstrap-loading' : 'bootstrap-error',
              ),
              child: Scaffold(
                backgroundColor: const Color(0xFF0B0D12),
                body: Center(
                  child: bootstrapError == null
                      ? const CircularProgressIndicator(strokeWidth: 2)
                      : Padding(
                          padding: const EdgeInsets.all(32),
                          child: Column(
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              Text(
                                bootstrapError!,
                                textAlign: TextAlign.center,
                                style: const TextStyle(fontSize: 16),
                              ),
                              const SizedBox(height: 20),
                              FilledButton(
                                onPressed: onRetry,
                                child: const Text('重新開啟'),
                              ),
                            ],
                          ),
                        ),
                ),
              ),
            ),
    ),
  );
}
