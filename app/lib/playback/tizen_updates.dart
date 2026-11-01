import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/widgets.dart';

import '../data/discord.dart' show appVersion;
import 'platform.dart';
import 'tizen_core.dart';

/// Keeps a Samsung TV install current without a computer. Downloads run in
/// the background; a ready update starts the next time the app opens, so
/// leaving the app (Home button) after a download switches to it.
class TizenUpdater with WidgetsBindingObserver {
  TizenUpdater({required this.notify});

  /// Shows a short message to the viewer.
  final void Function(String) notify;
  Timer? _timer;
  bool _ready = false, _running = false;

  static int versionCode(String version) {
    final parts = version.split('-').first.split('.');
    int part(int i) => i < parts.length ? int.tryParse(parts[i]) ?? 0 : 0;
    return part(0) * 1000000 + part(1) * 1000 + part(2);
  }

  void start() {
    WidgetsBinding.instance.addObserver(this);
    // Tell the shell this update started cleanly; see app/tizen/Brain.cs.
    Timer(const Duration(seconds: 15), () {
      try {
        File('${TizenPaths.data}brain/booting').deleteSync();
      } catch (_) {
        /* Not started from an update. */
      }
      unawaited(check());
    });
    _timer = Timer.periodic(const Duration(hours: 6), (_) => check());
  }

  void stop() {
    _timer?.cancel();
    WidgetsBinding.instance.removeObserver(this);
  }

  Future<void> check() async {
    if (_running || _ready || tizenGroup.isEmpty) return;
    _running = true;
    try {
      final shell = File('${TizenPaths.resource}shell-id')
          .readAsStringSync()
          .trim();
      final raw = await TizenCore.tvUpdate(
        jsonEncode({
          'data_dir': TizenPaths.data,
          'group': tizenGroup,
          'shell': shell,
          'current': versionCode(appVersion),
          'running': tizenBrain?.split('/').where((s) => s.isNotEmpty).last,
        }),
      );
      final result = jsonDecode(raw) as Map;
      final data = result['data'];
      if (data is! Map) return;
      switch (data['status']) {
        case 'installed':
          _ready = true;
          notify(
            'Otakase ${data['version']} is ready. It starts next time you '
            'open Otakase.',
          );
        case 'reinstall':
          final marker = File('${TizenPaths.data}brain/reinstall-shown');
          final code = '${data['code']}';
          if (!marker.existsSync() || marker.readAsStringSync() != code) {
            marker
              ..createSync(recursive: true)
              ..writeAsStringSync(code);
            notify(
              'Otakase ${data['version']} needs to be installed again from a '
              'computer.',
            );
          }
      }
    } catch (_) {
      /* Offline or no release yet; try again later. */
    } finally {
      _running = false;
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    // Leaving the app with an update ready closes it, so opening it again
    // starts the new version.
    if (_ready && state == AppLifecycleState.paused) exit(0);
  }
}
