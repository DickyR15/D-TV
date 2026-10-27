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
    duration: const Duration(milliseconds: 1250),
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
      return Transform.scale(
        // Keep the logo at the native splash size; only a very subtle pulse.
        scale: 0.99 + 0.02 * t,
        child: SizedBox(
          width: 128,
          height: 128,
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
    final side = size.width < size.height ? size.width : size.height;
    final dx = (size.width - side) / 2;
    final dy = (size.height - side) / 2;
    canvas.save();
    canvas.translate(dx, dy);
    canvas.scale(side / 192.0);
    final rect = const Rect.fromLTWH(0, 0, 192, 192);

    // Keep the same vector geometry as Android's native dtv_mark.xml.
    final outer = Path()
      ..moveTo(34, 21)
      ..lineTo(96, 21)
      ..cubicTo(137, 21, 158, 49, 158, 96)
      ..cubicTo(158, 143, 137, 171, 96, 171)
      ..lineTo(34, 171)
      ..close();

    canvas.drawPath(
      outer,
      Paint()
        ..color = const Color(0xFF087EFF).withValues(alpha: 0.25 + 0.12 * phase)
        ..maskFilter = MaskFilter.blur(BlurStyle.normal, 4.0 + 2.0 * phase),
    );

    canvas.saveLayer(rect, Paint());
    canvas.drawPath(
      outer,
      Paint()
        ..shader = const LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [
            Color(0xFFEAF1F7),
            Color(0xFFE0EAF2),
            Color(0xFF718CA4),
            Color(0xFFB5CDE0),
          ],
          stops: [0.0, 0.28, 0.65, 1.0],
        ).createShader(rect),
    );

    final highlight = Path()
      ..moveTo(37, 21)
      ..lineTo(93, 21)
      ..cubicTo(108, 21, 120, 25, 130, 34)
      ..lineTo(111, 47)
      ..cubicTo(101, 40, 91, 38, 77, 38)
      ..lineTo(37, 38)
      ..close();
    canvas.drawPath(highlight, Paint()..color = Colors.white.withValues(alpha: 0.92));

    final lowerShade = Path()
      ..moveTo(35, 147)
      ..lineTo(65, 135)
      ..lineTo(96, 135)
      ..cubicTo(119, 135, 134, 124, 143, 106)
      ..lineTo(152, 132)
      ..cubicTo(141, 159, 122, 173, 96, 173)
      ..lineTo(35, 173)
      ..close();
    canvas.drawPath(lowerShade, Paint()..color = const Color(0xFF718CA4));

    final inner = Path()
      ..moveTo(65, 51)
      ..lineTo(96, 51)
      ..cubicTo(121, 51, 136, 68, 136, 96)
      ..cubicTo(136, 124, 121, 141, 96, 141)
      ..lineTo(65, 141)
      ..close();
    canvas.drawPath(inner, Paint()..color = const Color(0xFF071322));

    final play = Path()
      ..moveTo(77, 70)
      ..lineTo(77, 122)
      ..lineTo(119, 96)
      ..close();
    canvas.drawPath(
      play,
      Paint()
        ..color = const Color(0xFF087EFF)
        ..maskFilter = MaskFilter.blur(BlurStyle.normal, 3.0 + 1.5 * phase),
    );
    canvas.drawPath(
      play,
      Paint()
        ..shader = const LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [Color(0xFF53F3FF), Color(0xFF00CFFF), Color(0xFF2787FF)],
        ).createShader(rect),
    );

    final playHighlight = Path()
      ..moveTo(80, 73)
      ..lineTo(80, 95)
      ..lineTo(99, 84)
      ..close();
    canvas.drawPath(playHighlight, Paint()..color = const Color(0xFF53F3FF));

    final shine = Path()
      ..moveTo(38, 23)
      ..lineTo(92, 23)
      ..cubicTo(109, 23, 121, 28, 132, 38);
    canvas.drawPath(
      shine,
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1.1
        ..strokeCap = StrokeCap.round
        ..color = Colors.white.withValues(alpha: 0.36 + 0.24 * phase),
    );

    canvas.restore();
    canvas.restore();
  }

  @override
  bool shouldRepaint(_DtvMarkPainter oldDelegate) => oldDelegate.phase != phase;
}
