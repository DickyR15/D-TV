import 'dart:io';
import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:media_kit/media_kit.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:window_manager/window_manager.dart';

import 'core_bridge.dart';
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

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  if (Platform.isAndroid) {
    unawaited(SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge));
    SystemChrome.setSystemUIOverlayStyle(AppTheme.systemBars(Brightness.dark));
  }
  if (Platform.isWindows) {
    await windowManager.ensureInitialized();
  }
  MediaKit.ensureInitialized();
  VideoEnhancementAssets.registerLicenses();

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
  final repository = NativeRepository();
  final navigator = GlobalKey<NavigatorState>();
  LocalStore? store;
  Object? error;
  Color? systemAccentColor;
  late AppDevice device = widget.device;

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
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
    _initialize();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed && Platform.isAndroid) {
      unawaited(_refreshDevice());
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

  Future<void> _refreshDevice() async {
    final detected = await AppDevice.detect(fallback: device);
    if (!mounted ||
        (device.television == detected.television &&
            device.version == detected.version)) {
      return;
    }
    setState(() => device = detected);
  }

  Future<void> _logStartupRendering() async {
    try {
      final rendering = Platform.isAndroid
          ? ', 渲染: ${await AppDevice.compatibleRendering() ? 'skia' : 'impeller'}'
          : '';
      DiaryService.add(
        '[App] 應用啟動，版本: ${device.version}, '
        '平臺: ${Platform.operatingSystem}$rendering',
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
      await _refreshDevice();
      final accent = await AppDevice.systemAccentColor();
      unawaited(_logStartupRendering());
      await repository.initialize();
      if (mounted) {
        setState(() {
          systemAccentColor = accent;
          store = LocalStore(preferences);
          repository.access = store;
          MediaLibrary.attach(repository, store!);
          LanController.current?.dispose();
          final link = LanController(
            repository,
            store!,
            kind: device.television
                ? 'tv'
                : Platform.isWindows
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
              throw StateError('當前使用者不能接收播放');
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
    version: device.version,
    navigatorKey: navigator,
    systemAccentColor: systemAccentColor,
  );
}

class DuanjuApp extends StatelessWidget {
  const DuanjuApp({
    super.key,
    required this.repository,
    this.store,
    this.bootstrapError,
    this.onRetry,
    this.television = false,
    this.version = appVersion,
    this.navigatorKey,
    this.systemAccentColor,
  });
  final AppRepository repository;
  final LocalStore? store;
  final String? bootstrapError;
  final VoidCallback? onRetry;
  final bool television;
  final String version;
  final GlobalKey<NavigatorState>? navigatorKey;
  final Color? systemAccentColor;

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: Listenable.merge([store]),
    builder: (_, _) => _application(),
  );

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
        systemAccent: systemAccentColor,
      ),
    ),
    darkTheme: AppTheme.darkFor(
      AppTheme.accentFor(
        store?.themeAccent ?? 'system',
        systemAccent: systemAccentColor,
      ),
    ),
    themeMode: store == null ? ThemeMode.dark : AppTheme.mode(store?.themeMode ?? 'system'),
    builder: (context, child) {
      final mode = store?.displayMode ?? 'auto';
      final tv = mode == 'television' || mode == 'auto' && television;
      WidgetsBinding.instance.addPostFrameCallback(
        (_) => FocusManager.instance.highlightStrategy = tv
            ? FocusHighlightStrategy.alwaysTraditional
            : FocusHighlightStrategy.automatic,
      );
      final theme = Theme.of(context);
      return AnnotatedRegion<SystemUiOverlayStyle>(
        value: AppTheme.systemBars(theme.brightness),
        child: ColoredBox(
          color: theme.scaffoldBackgroundColor,
          child: AppOrientationScope(
            television: tv,
            child: AppLayout(
              television: tv,
              version: version,
              child: Theme(
                data: tv ? televisionTheme(theme) : theme,
                child: Shortcuts(
                  shortcuts: const {
                    SingleActivator(
                      LogicalKeyboardKey.select,
                      includeRepeats: false,
                    ): ActivateIntent(),
                    SingleActivator(
                      LogicalKeyboardKey.gameButtonA,
                      includeRepeats: false,
                    ): ActivateIntent(),
                    SingleActivator(LogicalKeyboardKey.goBack): DismissIntent(),
                  },
                  child: FocusTraversalGroup(child: child!),
                ),
              ),
            ),
          ),
        ),
      );
    },
    home: store != null
        ? store!.locked
              ? ProfilesScreen(store: store!, locked: true)
              : HomeScreen(
                  key: ValueKey(
                    'profile-${store!.profile.id}-${store!.profileEpoch}',
                  ),
                  repository: repository,
                  store: store!,
                )
        : Scaffold(
            body: Center(
              child: bootstrapError == null
                  ? const DtvLaunchAnimation()
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
  );
}


/// A short, looping brand motion used only while the app finishes bootstrapping.
/// It deliberately has no "starting" label or progress spinner.
class DtvLaunchAnimation extends StatefulWidget {
  const DtvLaunchAnimation({super.key});

  @override
  State<DtvLaunchAnimation> createState() => _DtvLaunchAnimationState();
}

class _DtvLaunchAnimationState extends State<DtvLaunchAnimation>
    with SingleTickerProviderStateMixin {
  late final AnimationController _controller = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 1050),
  )..repeat(reverse: true);

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: _controller,
    builder: (context, _) {
      final t = Curves.easeInOut.transform(_controller.value);
      final glow = 0.12 + 0.16 * t;
      return Transform.scale(
        scale: 0.96 + 0.045 * t,
        child: Container(
          width: 144,
          height: 144,
          decoration: BoxDecoration(
            color: const Color(0xFF0B0D12),
            borderRadius: BorderRadius.circular(32),
            boxShadow: [
              BoxShadow(
                color: const Color(0xFF55C7D3).withValues(alpha: glow),
                blurRadius: 24 + 20 * t,
                spreadRadius: 1 + 2 * t,
              ),
              BoxShadow(
                color: const Color(0xFFAFC4D0).withValues(alpha: glow * 0.42),
                blurRadius: 30 + 14 * t,
                spreadRadius: t,
              ),
            ],
          ),
          child: CustomPaint(
            painter: _DtvMarkPainter(phase: t),
          ),
        ),
      );
    },
  );
}

class _DtvMarkPainter extends CustomPainter {
  const _DtvMarkPainter({required this.phase});

  final double phase;

  @override
  void paint(Canvas canvas, Size size) {
    final rect = Offset.zero & size;
    final outer = Path()
      ..moveTo(size.width * 0.20, size.height * 0.15)
      ..lineTo(size.width * 0.51, size.height * 0.15)
      ..cubicTo(
        size.width * 0.81, size.height * 0.15,
        size.width * 0.89, size.height * 0.35,
        size.width * 0.89, size.height * 0.50,
      )
      ..cubicTo(
        size.width * 0.89, size.height * 0.68,
        size.width * 0.80, size.height * 0.85,
        size.width * 0.51, size.height * 0.85,
      )
      ..lineTo(size.width * 0.20, size.height * 0.85)
      ..close();
    final bodyPaint = Paint()
      ..shader = const LinearGradient(
        begin: Alignment.topLeft,
        end: Alignment.bottomRight,
        colors: [
          Color(0xFFF5F8FA),
          Color(0xFFCBD5DC),
          Color(0xFF7B8D9B),
          Color(0xFFE5EBEF),
        ],
        stops: [0.0, 0.34, 0.68, 1.0],
      ).createShader(rect);
    canvas.drawPath(outer, bodyPaint);

    // Hollow out the D and place the play triangle inside the negative space.
    final inner = Path()
      ..moveTo(size.width * 0.36, size.height * 0.30)
      ..lineTo(size.width * 0.49, size.height * 0.30)
      ..cubicTo(
        size.width * 0.64, size.height * 0.30,
        size.width * 0.70, size.height * 0.39,
        size.width * 0.70, size.height * 0.50,
      )
      ..cubicTo(
        size.width * 0.70, size.height * 0.62,
        size.width * 0.63, size.height * 0.70,
        size.width * 0.49, size.height * 0.70,
      )
      ..lineTo(size.width * 0.36, size.height * 0.70)
      ..close();
    canvas.drawPath(inner, Paint()..color = const Color(0xFF0B0D12));

    final play = Path()
      ..moveTo(size.width * 0.42, size.height * 0.38)
      ..lineTo(size.width * 0.42, size.height * 0.62)
      ..lineTo(size.width * 0.61, size.height * 0.50)
      ..close();
    canvas.drawPath(
      play,
      Paint()
        ..shader = const LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [Color(0xFFA7EDF1), Color(0xFF55C8D3), Color(0xFF218A9A)],
        ).createShader(rect),
    );

    final highlight = Path()
      ..moveTo(size.width * 0.22, size.height * 0.17)
      ..lineTo(size.width * 0.50, size.height * 0.17)
      ..cubicTo(
        size.width * 0.67, size.height * 0.17,
        size.width * 0.78, size.height * 0.23,
        size.width * 0.83, size.height * 0.35,
      );
    canvas.drawPath(
      highlight,
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = size.width * 0.012
        ..strokeCap = StrokeCap.round
        ..color = Colors.white.withValues(alpha: 0.14 + 0.12 * phase),
    );
  }

  @override
  bool shouldRepaint(_DtvMarkPainter oldDelegate) =>
      oldDelegate.phase != phase;
}
