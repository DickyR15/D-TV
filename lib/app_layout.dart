import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:package_info_plus/package_info_plus.dart';

export 'app_build.dart';

const appVersion = '1.0.0+339';

/// Layout family selected in settings or inferred from the active device.
enum FormFactor { phone, pad, window, television }

const _displayModeLabels = <String, String>{
  'auto': '自動識別設備',
  'television': '電視',
  'phone': '手機',
  'pad': '平板',
  'window': '視窗',
};

ThemeData televisionTheme(ThemeData theme) {
  final colors = theme.colorScheme;
  final focusSide = WidgetStateProperty.resolveWith<BorderSide?>(
    (states) => states.contains(WidgetState.focused)
        ? BorderSide(color: colors.primary, width: 3)
        : null,
  );
  // Focus is a navigation state, not a selected state. Keep its fill neutral
  // and use the orange border as a clear focus marker.
  final focusBackground = WidgetStateProperty.resolveWith<Color?>(
    (states) => states.contains(WidgetState.focused)
        ? colors.surfaceContainerHighest
        : null,
  );
  final button = ButtonStyle(
    side: focusSide,
    minimumSize: const WidgetStatePropertyAll(Size(52, 48)),
    textStyle: const WidgetStatePropertyAll(TextStyle(fontSize: 17)),
  );
  return theme.copyWith(
    focusColor: colors.surfaceContainerHighest,
    iconButtonTheme: IconButtonThemeData(
      style: button.copyWith(backgroundColor: focusBackground),
    ),
    filledButtonTheme: FilledButtonThemeData(style: button),
    outlinedButtonTheme: OutlinedButtonThemeData(style: button),
    textButtonTheme: TextButtonThemeData(
      style: button.copyWith(backgroundColor: focusBackground),
    ),
    inputDecorationTheme: theme.inputDecorationTheme.copyWith(
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(16),
        borderSide: BorderSide(color: colors.primary, width: 3),
      ),
    ),
  );
}

class AppDevice {
  const AppDevice({this.television = false, this.version = appVersion});
  final bool television;
  final String version;
  static const channel = MethodChannel('duanju/device');

  static Future<AppDevice> detect({
    AppDevice fallback = const AppDevice(),
  }) async {
    if (defaultTargetPlatform != TargetPlatform.android) {
      try {
        final info = await PackageInfo.fromPlatform();
        return AppDevice(
          version: info.buildNumber.isNotEmpty
              ? '${info.version}+${info.buildNumber}'
              : info.version,
        );
      } catch (_) {
        return fallback;
      }
    }
    try {
      final data = await channel
          .invokeMapMethod<String, dynamic>('deviceInfo')
          .timeout(const Duration(seconds: 2));
      final television = data?['television'];
      if (television is! bool) return fallback;
      final version = data?['version'];
      return AppDevice(
        television: television,
        version: version is String && version.isNotEmpty
            ? version
            : fallback.version,
      );
    } on PlatformException {
      return fallback;
    } on MissingPluginException {
      return fallback;
    } on TimeoutException {
      return fallback;
    }
  }

  static Future<double> getBrightness() async {
    if (defaultTargetPlatform != TargetPlatform.android) return 0.5;
    try {
      final value = await channel.invokeMethod<double>('getBrightness');
      return (value ?? 0.5).clamp(0.0, 1.0);
    } catch (_) {
      return 0.5;
    }
  }

  static Future<void> setBrightness(double brightness) async {
    if (defaultTargetPlatform != TargetPlatform.android) return;
    try {
      await channel.invokeMethod('setBrightness', {
        'brightness': brightness.clamp(0.01, 1.0),
      });
    } catch (_) {}
  }

  static Future<void> resetBrightness() async {
    if (defaultTargetPlatform != TargetPlatform.android) return;
    try {
      await channel.invokeMethod('resetBrightness');
    } catch (_) {}
  }

  static Future<Color?> systemAccentColor() async {
    if (defaultTargetPlatform != TargetPlatform.android) return null;
    try {
      final value = await channel.invokeMethod<int>('systemAccentColor');
      return value == null ? null : Color(value & 0xFFFFFFFF);
    } catch (_) {
      return null;
    }
  }

  static Future<bool> compatibleRendering() async {
    if (defaultTargetPlatform != TargetPlatform.android) return false;
    try {
      return await channel.invokeMethod<bool>('compatibleRendering') ?? false;
    } catch (_) {
      return false;
    }
  }

  static Future<bool> setCompatibleRendering(bool enabled) async {
    if (defaultTargetPlatform != TargetPlatform.android) return false;
    try {
      await channel.invokeMethod('setCompatibleRendering', {
        'enabled': enabled,
      });
      return true;
    } catch (_) {
      return false;
    }
  }
}

class AppLayout extends InheritedWidget {
  const AppLayout({
    super.key,
    required this.television,
    this.formFactor,
    this.version = appVersion,
    required super.child,
  });

  /// Kept for older call sites and tests. New app entry points should pass
  /// [formFactor] so all screens use the same explicit layout decision.
  final bool television;
  final FormFactor? formFactor;
  final String version;

  static bool get isDesktopPlatform => switch (defaultTargetPlatform) {
    TargetPlatform.windows ||
    TargetPlatform.macOS ||
    TargetPlatform.linux => true,
    _ => false,
  };

  static Map<String, String> availableDisplayModes({bool? desktopPlatform}) {
    final desktop = desktopPlatform ?? isDesktopPlatform;
    return {
      for (final entry in _displayModeLabels.entries)
        if (entry.key != 'window' || desktop) entry.key: entry.value,
    };
  }

  static FormFactor _inferFromSize(Size size, {required bool desktop}) {
    if (desktop) return size.width >= 600 ? FormFactor.window : FormFactor.phone;
    return size.shortestSide >= 600 ? FormFactor.pad : FormFactor.phone;
  }

  /// Mirrors the upstream app's device mode rules while preserving D-TV's TV
  /// remote navigation. Legacy "standard" explicitly suppresses TV UI.
  static FormFactor resolveFormFactor({
    required String mode,
    required bool detectedTelevision,
    required Size screenSize,
    bool? desktopPlatform,
  }) {
    final desktop = desktopPlatform ?? isDesktopPlatform;
    switch (mode) {
      case 'television':
        return FormFactor.television;
      case 'phone':
        return FormFactor.phone;
      case 'pad':
        return FormFactor.pad;
      case 'window':
        return desktop ? FormFactor.window : FormFactor.pad;
      case 'standard':
        return _inferFromSize(screenSize, desktop: desktop);
      case 'auto':
      default:
        if (detectedTelevision) return FormFactor.television;
        return _inferFromSize(screenSize, desktop: desktop);
    }
  }

  static FormFactor formFactorOf(BuildContext context) {
    final layout = context.dependOnInheritedWidgetOfExactType<AppLayout>();
    if (layout != null) {
      return layout.formFactor ??
          (layout.television ? FormFactor.television : FormFactor.phone);
    }
    final size = MediaQuery.maybeOf(context)?.size ?? Size.zero;
    return resolveFormFactor(
      mode: 'auto',
      detectedTelevision: false,
      screenSize: size,
    );
  }

  static bool isTelevision(BuildContext context) =>
      formFactorOf(context) == FormFactor.television;
  static bool isPhone(BuildContext context) =>
      formFactorOf(context) == FormFactor.phone;
  static bool isPad(BuildContext context) =>
      formFactorOf(context) == FormFactor.pad;
  static bool isWindow(BuildContext context) =>
      formFactorOf(context) == FormFactor.window;
  static bool isWide(BuildContext context) =>
      formFactorOf(context) == FormFactor.pad ||
      formFactorOf(context) == FormFactor.window;

  static String displayModeLabel(
    String mode, {
    FormFactor? currentFactor,
  }) {
    final key = mode == 'standard'
        ? currentFactor?.name ?? 'auto'
        : mode == 'window' && !isDesktopPlatform
        ? 'pad'
        : mode;
    return _displayModeLabels[key] ?? _displayModeLabels['auto']!;
  }

  static Future<String?> chooseDisplayMode(
    BuildContext context, {
    required String currentMode,
  }) {
    final options = availableDisplayModes();
    var selected = currentMode;
    if (selected == 'standard' ||
        selected == 'window' && !isDesktopPlatform) {
      selected = formFactorOf(context).name;
    }
    if (!options.containsKey(selected)) selected = 'auto';

    return showDialog<String>(
      context: context,
      builder: (dialogContext) => SimpleDialog(
        title: const Text('介面模式'),
        children: [
          for (final entry in options.entries)
            ListTile(
              autofocus: entry.key == selected,
              selected: entry.key == selected,
              title: Text(entry.value),
              trailing: entry.key == selected
                  ? const Icon(Icons.check_rounded)
                  : null,
              onTap: () => Navigator.of(dialogContext).pop(entry.key),
            ),
        ],
      ),
    );
  }

  static String versionOf(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<AppLayout>()?.version ??
      appVersion;

  @override
  bool updateShouldNotify(AppLayout oldWidget) =>
      television != oldWidget.television ||
      formFactor != oldWidget.formFactor ||
      version != oldWidget.version;
}
