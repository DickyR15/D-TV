import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

abstract final class AppTheme {
  static final light = _theme(Brightness.light);
  static final dark = _theme(Brightness.dark);

  /// D-TV 品牌色取自啟動圖示：珊瑚紅搭配深墨色背景。
  static const brandAccent = Color(0xFFFF526D);

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

  static ThemeData _theme(Brightness brightness) {
    final dark = brightness == Brightness.dark;
    // Softer graphite surfaces are easier to watch for long TV sessions.
    final background = dark
        ? const Color(0xFF171820)
        : const Color(0xFFFBF6F7);
    final scheme = ColorScheme.fromSeed(
      seedColor: brandAccent,
      brightness: brightness,
      primary: dark ? const Color(0xFFD13D55) : const Color(0xFFB51236),
      onPrimary: Colors.white,
      primaryContainer: dark
          ? const Color(0xFF493039)
          : const Color(0xFFFFD9E1),
      onPrimaryContainer: dark
          ? const Color(0xFFFFE4E9)
          : const Color(0xFF510018),
      secondary: dark
          ? const Color(0xFFE5A1B0)
          : const Color(0xFF853247),
      onSecondary: dark
          ? const Color(0xFF310612)
          : Colors.white,
      secondaryContainer: dark
          ? const Color(0xFF403039)
          : const Color(0xFFF4DDE3),
      onSecondaryContainer: dark
          ? const Color(0xFFFFD8E0)
          : const Color(0xFF4F0E24),
      tertiary: dark
          ? const Color(0xFFF6C86B)
          : const Color(0xFF805500),
      surface: dark
          ? const Color(0xFF1D1F28)
          : const Color(0xFFFFFBFC),
      onSurface: dark
          ? const Color(0xFFF2EDF0)
          : const Color(0xFF272027),
      onSurfaceVariant: dark
          ? const Color(0xFFC9BEC5)
          : const Color(0xFF655760),
      outline: dark
          ? const Color(0xFF9C8992)
          : const Color(0xFF807079),
      outlineVariant: dark
          ? const Color(0xFF46404A)
          : const Color(0xFFE8D9DE),
      surfaceContainerLowest: dark
          ? const Color(0xFF191A22)
          : Colors.white,
      surfaceContainerLow: dark
          ? const Color(0xFF20212A)
          : const Color(0xFFFFF3F6),
      surfaceContainer: dark
          ? const Color(0xFF252630)
          : const Color(0xFFF7E9EE),
      surfaceContainerHigh: dark
          ? const Color(0xFF2D2E38)
          : const Color(0xFFF0E0E6),
      surfaceContainerHighest: dark
          ? const Color(0xFF393A45)
          : const Color(0xFFE8D5DD),
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
