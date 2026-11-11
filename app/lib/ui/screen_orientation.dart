import 'dart:async';

import 'package:flutter/services.dart';

import '../host.dart';

/// Which way a phone may turn. The phone remote stays upright, the player
/// turns sideways, and everything else follows the phone.
abstract final class ScreenOrientation {
  static int _upright = 0;

  /// Whether a page that locks upright is open.
  static bool get upright => _upright > 0;

  /// Keeps the screen upright until [release]; only phones lock, a TV or
  /// computer never turns.
  static void lockUpright() {
    _upright++;
    restore();
  }

  static void release() {
    if (_upright > 0) _upright--;
    restore();
  }

  /// Back to what the open pages want, after the player turned sideways.
  static void restore() {
    if (!Host.android || Host.tv) return;
    unawaited(
      SystemChrome.setPreferredOrientations(
        upright ? const [DeviceOrientation.portraitUp] : const [],
      ),
    );
  }
}
