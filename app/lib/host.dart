import 'dart:io' show Platform;

import 'package:flutter/foundation.dart' show kIsWeb, visibleForTesting;

/// The operating system the app runs on. The website's demo build runs in a
/// browser, where dart:io's [Platform] throws, so it reads as none of them.
abstract final class Host {
  static bool get android => !kIsWeb && Platform.isAndroid;
  static bool get linux => !kIsWeb && Platform.isLinux;
  static bool get macOS => !kIsWeb && Platform.isMacOS;
  static bool get windows => !kIsWeb && Platform.isWindows;
  static String get name => kIsWeb ? 'web' : Platform.operatingSystem;

  /// Set at start-up when the Android host is a TV.
  static bool tv = false;

  /// The system as the Discord bot names it (Go's GOOS): Dart says "macos"
  /// where Go says "darwin".
  static String get goos => name == 'macos' ? 'darwin' : name;
  static Map<String, String> get environment =>
      debugEnvironment ?? (kIsWeb ? const {} : Platform.environment);

  /// Set by tests so the developer's own desktop session (Hyprland, KDE,
  /// Wayland) does not change what the app draws.
  @visibleForTesting
  static Map<String, String>? debugEnvironment;

  /// The short name of the local time zone, like "EET".
  static String get timeZoneName =>
      debugTimeZoneName ?? DateTime.now().timeZoneName;

  /// Set by tests so screens look the same in every time zone.
  @visibleForTesting
  static String? debugTimeZoneName;
  static String get pathSeparator => kIsWeb ? '/' : Platform.pathSeparator;
  static String get executable => kIsWeb ? '' : Platform.resolvedExecutable;
}
