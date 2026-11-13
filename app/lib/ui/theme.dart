import 'package:flutter/material.dart';

import 'components.dart' show FocusRings;
import 'tokens.dart';

/// The app theme, built from the tokens. Material widgets pick up Otakase
/// colours, type and shapes from here, so screens do not style them one by
/// one.
///
/// Buttons draw a focus outline only while [FocusRings] show. [focusRings]
/// sets the ink focus colour; the desktop swaps it as the viewer moves
/// between mouse and keyboard.
ThemeData buildOtakaseTheme({required bool isTV, bool focusRings = true}) {
  final buttons = ButtonStyle(
    textStyle: WidgetStatePropertyAll(
      TextStyle(
        fontFamily: 'Inter',
        fontSize: isTV ? 20 : 16,
        fontWeight: FontWeight.w600,
      ),
    ),
    foregroundColor: const WidgetStatePropertyAll(fg),
    overlayColor: WidgetStatePropertyAll(shu.withValues(alpha: .08)),
    shape: const WidgetStatePropertyAll(
      RoundedRectangleBorder(
        borderRadius: BorderRadius.all(Radius.circular(KageRadius.button)),
      ),
    ),
    side: WidgetStateProperty.resolveWith(
      (s) => BorderSide(
        color: FocusRings.show && s.contains(WidgetState.focused)
            ? shu
            : Colors.transparent,
        width: 2,
      ),
    ),
  );
  const inputShape = BorderRadius.all(Radius.circular(KageRadius.input));
  return ThemeData(
    brightness: Brightness.dark,
    colorScheme: const ColorScheme.dark(
      primary: shu,
      onPrimary: bgDeep,
      secondary: shu,
      surface: panel,
      outline: line,
      outlineVariant: line,
      onSurface: fg,
      error: danger,
    ),
    scaffoldBackgroundColor: bg,
    canvasColor: bg,
    fontFamily: 'Inter',
    fontFamilyFallback: const ['NotoSansJP'],
    textTheme: TextTheme(
      bodySmall: TextStyle(fontSize: isTV ? 20 : 15, color: muted, height: 1.4),
      bodyMedium: TextStyle(fontSize: isTV ? 20 : 16, color: fg, height: 1.4),
      bodyLarge: TextStyle(fontSize: isTV ? 22 : 16, color: fg, height: 1.4),
      titleSmall: TextStyle(
        fontSize: isTV ? 20 : 16,
        fontWeight: FontWeight.w600,
      ),
      titleMedium: TextStyle(
        fontSize: isTV ? 22 : 18,
        fontWeight: FontWeight.w600,
      ),
      titleLarge: const TextStyle(fontSize: 26, fontWeight: FontWeight.w700),
      labelLarge: TextStyle(
        fontSize: isTV ? 20 : 16,
        fontWeight: FontWeight.w600,
      ),
      labelMedium: TextStyle(fontSize: isTV ? 20 : 15),
      labelSmall: TextStyle(fontSize: isTV ? 20 : 15),
    ),
    focusColor: focusRings ? line : Colors.transparent,
    dividerColor: line,
    dividerTheme: const DividerThemeData(color: line, thickness: 1),
    iconTheme: IconThemeData(color: fg, size: isTV ? 24 : 22),
    appBarTheme: AppBarTheme(
      backgroundColor: bg,
      surfaceTintColor: Colors.transparent,
      titleTextStyle: TextStyle(
        fontFamily: 'Inter',
        fontSize: isTV ? 22 : 20,
        color: fg,
        fontWeight: FontWeight.w600,
      ),
    ),
    bottomSheetTheme: const BottomSheetThemeData(
      backgroundColor: panel,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(
          top: Radius.circular(KageRadius.sheet),
        ),
      ),
    ),
    dialogTheme: const DialogThemeData(
      backgroundColor: panel,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.all(Radius.circular(KageRadius.dialog)),
      ),
    ),
    textButtonTheme: TextButtonThemeData(style: buttons),
    filledButtonTheme: const FilledButtonThemeData(
      style: ButtonStyle(
        shape: WidgetStatePropertyAll(
          RoundedRectangleBorder(
            borderRadius: BorderRadius.all(Radius.circular(KageRadius.button)),
          ),
        ),
      ),
    ),
    popupMenuTheme: const PopupMenuThemeData(
      color: panel,
      shape: RoundedRectangleBorder(
        side: BorderSide(color: line),
        borderRadius: BorderRadius.all(Radius.circular(KageRadius.menu)),
      ),
    ),
    // Feedback: one quiet floating note. Phones get it full width at the
    // bottom; TVs and computers get a compact one.
    snackBarTheme: SnackBarThemeData(
      behavior: SnackBarBehavior.floating,
      backgroundColor: panelRaised,
      elevation: 0,
      width: isTV ? 440 : null,
      contentTextStyle: TextStyle(
        fontFamily: 'Inter',
        fontSize: isTV ? 20 : 16,
        color: fgBright,
      ),
      actionTextColor: accentText,
      shape: const RoundedRectangleBorder(
        side: BorderSide(color: lineStrong),
        borderRadius: BorderRadius.all(Radius.circular(KageRadius.toast)),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(style: buttons),
    iconButtonTheme: IconButtonThemeData(style: buttons),
    listTileTheme: const ListTileThemeData(textColor: fg, iconColor: muted),
    switchTheme: SwitchThemeData(
      thumbColor: WidgetStateProperty.resolveWith(
        (s) => s.contains(WidgetState.selected) ? fgBright : muted,
      ),
      trackColor: WidgetStateProperty.resolveWith(
        (s) => s.contains(WidgetState.selected) ? shu : line,
      ),
      trackOutlineColor: const WidgetStatePropertyAll(Colors.transparent),
    ),
    inputDecorationTheme: InputDecorationTheme(
      isDense: true,
      hintStyle: TextStyle(
        fontFamily: 'Inter',
        color: muted,
        fontSize: isTV ? 20 : 16,
      ),
      labelStyle: TextStyle(
        fontFamily: 'Inter',
        color: muted,
        fontSize: isTV ? 20 : 15,
      ),
      filled: true,
      fillColor: fgBright.withValues(alpha: .06),
      border: const OutlineInputBorder(
        borderRadius: inputShape,
        borderSide: BorderSide(color: lineStrong),
      ),
      enabledBorder: const OutlineInputBorder(
        borderRadius: inputShape,
        borderSide: BorderSide(color: lineStrong),
      ),
      focusedBorder: const OutlineInputBorder(
        borderRadius: inputShape,
        borderSide: BorderSide(color: shu),
      ),
      contentPadding: const EdgeInsets.symmetric(vertical: 16, horizontal: 16),
    ),
    sliderTheme: SliderThemeData(
      activeTrackColor: shu,
      inactiveTrackColor: line,
      thumbColor: fg,
      trackHeight: 2,
      thumbShape: const RoundSliderThumbShape(enabledThumbRadius: 4),
      overlayShape: const RoundSliderOverlayShape(overlayRadius: 14),
      overlayColor: shu.withValues(alpha: .1),
    ),
  );
}
