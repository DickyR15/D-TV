import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

abstract final class AppTheme {
  static final light = _theme(Brightness.light);
  static final dark = _theme(Brightness.dark);

  /// D TV 品牌色：低飽和青藍搭配石墨灰與銀白色。
  static const brandAccent = Color(0xFF55BDCA);

  static const accentOptions = <String>[
    'system', 'warmOrange', 'roseRed', 'amber', 'teal', 'ocean', 'indigo',
  ];

  static String accentLabel(String value) => switch (value) {
    'system' => '跟隨系統色彩',
    'warmOrange' => '暖橙',
    'roseRed' => '玫紅',
    'amber' => '琥珀',
    'teal' => '青綠',
    'ocean' => '海藍',
    'indigo' => '藍紫',
    _ => '跟隨系統色彩',
  };

  static String systemAccentDescription({
    TargetPlatform? platform,
  }) => switch (platform ?? defaultTargetPlatform) {
    TargetPlatform.android =>
      'Android 12 以上使用系統桌布動態色彩；較舊版本使用 D TV 品牌色。',
    TargetPlatform.iOS =>
      '使用 Apple 系統藍色，並依淺色／深色外觀自動調整。',
    TargetPlatform.windows =>
      '使用 Windows 個人化設定中的系統強調色。',
    TargetPlatform.macOS =>
      '使用 macOS 系統設定中的強調色。',
    _ => '此平台目前使用 D TV 品牌色。',
  };

  static Color? accentFor(String value, {Color? systemAccent}) => switch (value) {
    'system' => systemAccent,
    'warmOrange' => const Color(0xFFFFA45B),
    'roseRed' => const Color(0xFFE8799C),
    'amber' => const Color(0xFFBE8B47),
    'teal' => const Color(0xFF4F9E96),
    'ocean' => const Color(0xFF578BC8),
    'indigo' => const Color(0xFF7774C8),
    _ => null,
  };

  static Color accentPreview(String value) => switch (value) {
    'system' => const Color(0xFF8992A6),
    _ => accentFor(value) ?? const Color(0xFF8992A6),
  };

  static ThemeData lightFor(Color? accent) =>
      accent == null ? light : _theme(Brightness.light, accent: accent);

  static ThemeData darkFor(Color? accent) =>
      accent == null ? dark : _theme(Brightness.dark, accent: accent);

  static ThemeMode mode(String preference) => switch (preference) {
    'light' => ThemeMode.light,
    'dark' => ThemeMode.dark,
    _ => ThemeMode.system,
  };

  static String label(String preference) => switch (preference) {
    'light' => '淺色',
    'dark' => '深色',
    _ => '跟隨系統',
  };

  static SystemUiOverlayStyle systemBars(Brightness brightness) {
    final icons = brightness == Brightness.dark
        ? Brightness.light
        : Brightness.dark;
    return SystemUiOverlayStyle(
      statusBarColor: Colors.transparent,
      statusBarIconBrightness: icons,
      statusBarBrightness: brightness,
      systemStatusBarContrastEnforced: false,
      systemNavigationBarColor: Colors.transparent,
      systemNavigationBarDividerColor: Colors.transparent,
      systemNavigationBarIconBrightness: icons,
      systemNavigationBarContrastEnforced: false,
    );
  }

  static ButtonStyle _filledButtonStyle(ColorScheme colors) => ButtonStyle(
    animationDuration: const Duration(milliseconds: 120),
    minimumSize: WidgetStateProperty.all(const Size(48, 48)),
    padding: WidgetStateProperty.all(
      const EdgeInsets.symmetric(horizontal: 18, vertical: 11),
    ),
    shape: WidgetStateProperty.all(
      RoundedRectangleBorder(borderRadius: BorderRadius.circular(13)),
    ),
    backgroundColor: WidgetStateProperty.resolveWith<Color?>((states) {
      if (states.contains(WidgetState.disabled)) {
        return colors.surfaceContainerHighest;
      }
      return colors.primary;
    }),
    foregroundColor: WidgetStateProperty.resolveWith<Color?>((states) {
      if (states.contains(WidgetState.disabled)) {
        return colors.onSurface.withValues(alpha: 0.38);
      }
      return colors.onPrimary;
    }),
    side: WidgetStateProperty.resolveWith<BorderSide?>((states) {
      if (states.contains(WidgetState.focused) &&
          !states.contains(WidgetState.disabled)) {
        return BorderSide(color: colors.onSurface, width: 2.5);
      }
      return BorderSide.none;
    }),
    overlayColor: WidgetStateProperty.resolveWith<Color?>((states) {
      if (states.contains(WidgetState.pressed)) {
        return Colors.white.withValues(alpha: 0.16);
      }
      if (states.contains(WidgetState.focused)) {
        return Colors.white.withValues(alpha: 0.08);
      }
      return null;
    }),
    elevation: WidgetStateProperty.resolveWith<double?>((states) {
      if (states.contains(WidgetState.pressed)) return 0;
      return 1;
    }),
  );

  static ButtonStyle _outlinedButtonStyle(ColorScheme colors) => ButtonStyle(
    animationDuration: const Duration(milliseconds: 120),
    minimumSize: WidgetStateProperty.all(const Size(48, 48)),
    padding: WidgetStateProperty.all(
      const EdgeInsets.symmetric(horizontal: 18, vertical: 11),
    ),
    shape: WidgetStateProperty.all(
      RoundedRectangleBorder(borderRadius: BorderRadius.circular(13)),
    ),
    backgroundColor: WidgetStateProperty.resolveWith<Color?>((states) {
      if (states.contains(WidgetState.disabled)) return Colors.transparent;
      if (states.contains(WidgetState.focused)) return colors.primaryContainer;
      return Colors.transparent;
    }),
    foregroundColor: WidgetStateProperty.resolveWith<Color?>((states) {
      if (states.contains(WidgetState.disabled)) {
        return colors.onSurface.withValues(alpha: 0.38);
      }
      if (states.contains(WidgetState.focused)) {
        return colors.onPrimaryContainer;
      }
      return colors.primary;
    }),
    side: WidgetStateProperty.resolveWith<BorderSide?>((states) {
      if (states.contains(WidgetState.focused) &&
          !states.contains(WidgetState.disabled)) {
        return BorderSide(color: colors.primary, width: 2.5);
      }
      return BorderSide(color: colors.outlineVariant, width: 1);
    }),
    overlayColor: WidgetStateProperty.resolveWith<Color?>((states) {
      if (states.contains(WidgetState.pressed) ||
          states.contains(WidgetState.focused)) {
        return colors.primary.withValues(alpha: 0.12);
      }
      return null;
    }),
  );

  static ButtonStyle _textButtonStyle(ColorScheme colors) => ButtonStyle(
    animationDuration: const Duration(milliseconds: 120),
    minimumSize: WidgetStateProperty.all(const Size(48, 44)),
    padding: WidgetStateProperty.all(
      const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
    ),
    shape: WidgetStateProperty.all(
      RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
    ),
    backgroundColor: WidgetStateProperty.resolveWith<Color?>((states) {
      if (states.contains(WidgetState.focused) &&
          !states.contains(WidgetState.disabled)) {
        return colors.primaryContainer;
      }
      return Colors.transparent;
    }),
    foregroundColor: WidgetStateProperty.resolveWith<Color?>((states) {
      if (states.contains(WidgetState.disabled)) {
        return colors.onSurface.withValues(alpha: 0.38);
      }
      if (states.contains(WidgetState.focused)) {
        return colors.onPrimaryContainer;
      }
      return colors.primary;
    }),
    side: WidgetStateProperty.resolveWith<BorderSide?>((states) {
      if (states.contains(WidgetState.focused) &&
          !states.contains(WidgetState.disabled)) {
        return BorderSide(color: colors.primary, width: 2);
      }
      return BorderSide.none;
    }),
    overlayColor: WidgetStateProperty.resolveWith<Color?>((states) {
      if (states.contains(WidgetState.pressed) ||
          states.contains(WidgetState.focused)) {
        return colors.primary.withValues(alpha: 0.10);
      }
      return null;
    }),
  );

  static ButtonStyle _iconButtonStyle(ColorScheme colors) => ButtonStyle(
    animationDuration: const Duration(milliseconds: 120),
    minimumSize: WidgetStateProperty.all(const Size(48, 48)),
    padding: WidgetStateProperty.all(const EdgeInsets.all(10)),
    shape: WidgetStateProperty.all(
      RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
    ),
    backgroundColor: WidgetStateProperty.resolveWith<Color?>((states) {
      if (states.contains(WidgetState.focused) &&
          !states.contains(WidgetState.disabled)) {
        return colors.primaryContainer;
      }
      return Colors.transparent;
    }),
    foregroundColor: WidgetStateProperty.resolveWith<Color?>((states) {
      if (states.contains(WidgetState.disabled)) {
        return colors.onSurface.withValues(alpha: 0.38);
      }
      if (states.contains(WidgetState.focused)) {
        return colors.onPrimaryContainer;
      }
      return colors.onSurfaceVariant;
    }),
    side: WidgetStateProperty.resolveWith<BorderSide?>((states) {
      if (states.contains(WidgetState.focused) &&
          !states.contains(WidgetState.disabled)) {
        return BorderSide(color: colors.primary, width: 2);
      }
      return BorderSide.none;
    }),
    overlayColor: WidgetStateProperty.resolveWith<Color?>((states) {
      if (states.contains(WidgetState.pressed) ||
          states.contains(WidgetState.focused)) {
        return colors.primary.withValues(alpha: 0.10);
      }
      return null;
    }),
  );

  static ThemeData _theme(Brightness brightness, {Color? accent}) {
    final dark = brightness == Brightness.dark;
    // Neutral graphite surfaces keep the TV interface calm during long sessions.
    final background = dark
        ? const Color(0xFF101318)
        : const Color(0xFFF3F6F8);
    final scheme = ColorScheme.fromSeed(
      seedColor: accent ?? brandAccent,
      brightness: brightness,
      primary: accent == null
          ? (dark ? const Color(0xFF76D6E0) : const Color(0xFF087987))
          : null,
      onPrimary: accent == null ? (dark ? const Color(0xFF062E34) : Colors.white) : null,
      primaryContainer: accent == null
          ? (dark ? const Color(0xFF193B43) : const Color(0xFFD1F2F5))
          : null,
      onPrimaryContainer: accent == null
          ? (dark ? const Color(0xFFDDFBFD) : const Color(0xFF073F46))
          : null,
      secondary: accent == null
          ? (dark ? const Color(0xFFA8C4CD) : const Color(0xFF315B66))
          : null,
      onSecondary: accent == null
          ? (dark ? const Color(0xFF14282E) : Colors.white)
          : null,
      secondaryContainer: accent == null
          ? (dark ? const Color(0xFF2A4149) : const Color(0xFFDBEDF1))
          : null,
      onSecondaryContainer: accent == null
          ? (dark ? const Color(0xFFDEEDF1) : const Color(0xFF18363D))
          : null,
      tertiary: accent == null
          ? (dark ? const Color(0xFFA9B4CB) : const Color(0xFF495B7D))
          : null,
      surface: dark
          ? const Color(0xFF191D23)
          : Colors.white,
      onSurface: dark
          ? const Color(0xFFEEF2F5)
          : const Color(0xFF202830),
      onSurfaceVariant: dark
          ? const Color(0xFFB1BCC5)
          : const Color(0xFF55636E),
      outline: dark
          ? const Color(0xFF8797A4)
          : const Color(0xFF6D7C87),
      outlineVariant: dark
          ? const Color(0xFF3A454F)
          : const Color(0xFFD5DEE4),
      surfaceContainerLowest: dark
          ? const Color(0xFF14181D)
          : Colors.white,
      surfaceContainerLow: dark
          ? const Color(0xFF1A2026)
          : const Color(0xFFF5F8FA),
      surfaceContainer: dark
          ? const Color(0xFF222A31)
          : const Color(0xFFECF1F4),
      surfaceContainerHigh: dark
          ? const Color(0xFF2B343D)
          : const Color(0xFFE3EAEF),
      surfaceContainerHighest: dark
          ? const Color(0xFF343E48)
          : const Color(0xFFD8E2E8),
      surfaceTint: Colors.transparent,
    );

    return ThemeData(
      useMaterial3: true,
      brightness: brightness,
      colorScheme: scheme,
      scaffoldBackgroundColor: background,
      appBarTheme: AppBarTheme(
        backgroundColor: background,
        foregroundColor: scheme.onSurface,
        scrolledUnderElevation: 0,
        elevation: 0,
        centerTitle: false,
        systemOverlayStyle: systemBars(brightness),
      ),
      filledButtonTheme: FilledButtonThemeData(
        style: _filledButtonStyle(scheme),
      ),
      elevatedButtonTheme: ElevatedButtonThemeData(
        style: _filledButtonStyle(scheme),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: _outlinedButtonStyle(scheme),
      ),
      textButtonTheme: TextButtonThemeData(
        style: _textButtonStyle(scheme),
      ),
      iconButtonTheme: IconButtonThemeData(
        style: _iconButtonStyle(scheme),
      ),
      dividerTheme: DividerThemeData(color: scheme.outlineVariant),
      chipTheme: ChipThemeData(
        backgroundColor: scheme.surfaceContainer,
        selectedColor: scheme.primaryContainer,
        labelStyle: TextStyle(color: scheme.onSurface),
        secondaryLabelStyle: TextStyle(
          color: scheme.onPrimaryContainer,
          fontWeight: FontWeight.w700,
        ),
        checkmarkColor: scheme.onPrimaryContainer,
        side: BorderSide(color: scheme.outlineVariant),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(12),
        ),
      ),
      navigationRailTheme: NavigationRailThemeData(
        backgroundColor: background,
        useIndicator: false,
        selectedIconTheme: IconThemeData(color: scheme.primary),
        unselectedIconTheme: IconThemeData(color: scheme.onSurfaceVariant),
        selectedLabelTextStyle: TextStyle(
          color: scheme.primary,
          fontWeight: FontWeight.w700,
        ),
        unselectedLabelTextStyle: TextStyle(color: scheme.onSurfaceVariant),
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: scheme.surfaceContainer,
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(14),
          borderSide: BorderSide(color: scheme.outlineVariant),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(14),
          borderSide: BorderSide(color: scheme.outlineVariant),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(14),
          borderSide: BorderSide(color: scheme.primary, width: 2),
        ),
        errorBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(14),
          borderSide: BorderSide(color: scheme.error),
        ),
        focusedErrorBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(14),
          borderSide: BorderSide(color: scheme.error, width: 2),
        ),
      ),
      popupMenuTheme: PopupMenuThemeData(
        color: scheme.surfaceContainerHigh,
        textStyle: TextStyle(color: scheme.onSurface),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(14),
        ),
      ),
      snackBarTheme: SnackBarThemeData(
        behavior: SnackBarBehavior.floating,
        backgroundColor: scheme.surfaceContainerHighest,
        contentTextStyle: TextStyle(color: scheme.onSurface),
        actionTextColor: scheme.primary,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(14),
        ),
      ),
      progressIndicatorTheme: ProgressIndicatorThemeData(
        color: scheme.primary,
        linearTrackColor: scheme.surfaceContainerHighest,
        circularTrackColor: scheme.surfaceContainerHighest,
      ),
      floatingActionButtonTheme: FloatingActionButtonThemeData(
        backgroundColor: scheme.primary,
        foregroundColor: scheme.onPrimary,
        focusColor: scheme.primaryContainer,
        hoverColor: scheme.primaryContainer,
      ),
      switchTheme: SwitchThemeData(
        thumbColor: WidgetStateProperty.resolveWith<Color?>((states) {
          if (states.contains(WidgetState.selected)) return scheme.primary;
          return scheme.onSurfaceVariant;
        }),
        trackColor: WidgetStateProperty.resolveWith<Color?>((states) {
          if (states.contains(WidgetState.selected)) {
            return scheme.primary.withValues(alpha: 0.32);
          }
          return scheme.surfaceContainerHighest;
        }),
      ),
      checkboxTheme: CheckboxThemeData(
        fillColor: WidgetStateProperty.resolveWith<Color?>((states) {
          if (states.contains(WidgetState.selected)) return scheme.primary;
          return null;
        }),
        checkColor: WidgetStateProperty.all(scheme.onPrimary),
      ),
      radioTheme: RadioThemeData(
        fillColor: WidgetStateProperty.resolveWith<Color?>((states) {
          if (states.contains(WidgetState.selected)) return scheme.primary;
          return scheme.onSurfaceVariant;
        }),
      ),
    );
  }
}
