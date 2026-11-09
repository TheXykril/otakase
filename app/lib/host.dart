import 'dart:io' show Platform;

import 'package:flutter/foundation.dart' show kIsWeb;

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
      kIsWeb ? const {} : Platform.environment;
  static String get pathSeparator => kIsWeb ? '/' : Platform.pathSeparator;
  static String get executable => kIsWeb ? '' : Platform.resolvedExecutable;
}
