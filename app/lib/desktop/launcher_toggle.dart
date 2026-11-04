import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';

/// A shortcut invocation either owns the popup or asks its owner to close.
class LauncherToggle {
  LauncherToggle._(this._lock, this._signal, this._initialLength);

  final RandomAccessFile _lock;
  final File _signal;
  final int _initialLength;
  Timer? _timer;
  bool _closed = false;

  static Future<LauncherToggle?> open(
    Directory directory, {
    Map<String, String>? environment,
  }) async {
    final env = environment ?? Platform.environment;
    final session = [
      for (final key in [
        'XDG_SESSION_ID',
        'WAYLAND_DISPLAY',
        'DISPLAY',
        'SESSIONNAME',
      ])
        env[key] ?? '',
    ].join('|');
    final suffix = sha256
        .convert(utf8.encode(session))
        .toString()
        .substring(0, 16);
    final prefix = '${directory.path}${Platform.pathSeparator}launcher-$suffix';
    final signal = await File('$prefix.toggle').create();
    final initialLength = await signal.length();
    final lock = await File('$prefix.lock').open(mode: FileMode.append);
    try {
      await lock.lock();
    } on FileSystemException catch (error) {
      await lock.close();
      final contention = Platform.isWindows
          ? const {32, 33}
          : const {11, 13, 35};
      if (!contention.contains(error.osError?.errorCode)) rethrow;
      await signal.writeAsBytes([1], mode: FileMode.append, flush: true);
      return null;
    }
    return LauncherToggle._(lock, signal, initialLength);
  }

  void watch(Future<void> Function() dismiss) {
    _timer = Timer.periodic(const Duration(milliseconds: 80), (timer) async {
      int length;
      try {
        length = await _signal.length();
      } on FileSystemException {
        return;
      }
      if (!timer.isActive || length == _initialLength) return;
      timer.cancel();
      await dismiss();
    });
  }

  Future<void> close() async {
    if (_closed) return;
    _closed = true;
    _timer?.cancel();
    await _lock.close();
  }
}
