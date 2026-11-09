import 'package:flutter/services.dart';

import '../host.dart';

/// Keeps the Android screen on while the app is busy, and the app running
/// while it downloads, so an update or a saved episode does not stop when
/// the phone goes to sleep. A playing video keeps the screen on by itself.
class StayAwake {
  StayAwake({Future<void> Function(bool screen, String? work)? apply})
    : _apply = apply ?? _android;

  /// The one the app uses; profiles come and go, the device stays.
  static final instance = StayAwake();

  final Future<void> Function(bool screen, String? work) _apply;
  final Map<String, String> _work = {};
  bool _always = false;
  (bool, String?)? _sent;

  /// The screen stays on whenever the app is open (the KeepScreenOn setting).
  set always(bool value) {
    _always = value;
    _sync();
  }

  bool get screen => _always || _work.isNotEmpty;

  /// What the background notification says, or null with nothing running.
  String? get work => _work.isEmpty ? null : _work.values.last;

  /// Marks [reason] busy with [text] shown while it runs, or done when
  /// [text] is null.
  void busy(String reason, String? text) {
    if (text == null) {
      _work.remove(reason);
    } else {
      _work[reason] = text;
    }
    _sync();
  }

  void _sync() {
    final next = (screen, work);
    if (next == _sent) return;
    _sent = next;
    _apply(next.$1, next.$2).catchError((_) {});
  }

  static Future<void> _android(bool screen, String? work) async {
    if (!Host.android) return;
    await const MethodChannel('otakase/device')
        .invokeMethod<void>('keepAwake', {'screen': screen, 'work': work});
  }
}
