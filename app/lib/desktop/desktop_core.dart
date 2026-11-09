import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/services.dart';

import '../host.dart';

/// True when the app runs as a Windows, macOS or Linux desktop app.
/// `flutter test` also runs on a desktop host but tests the Android paths,
/// so it is off there unless a test sets [debugDesktopOverride].
bool get isDesktop => debugDesktopOverride ?? _desktopHost;

bool? debugDesktopOverride;

final bool _desktopHost =
    (Host.windows || Host.macOS || Host.linux) &&
    !Host.environment.containsKey('FLUTTER_TEST');

/// Starts the helper process. Tests replace it with a fake.
typedef CoreLauncher = Future<Process> Function(String executable);

/// The Go core on desktop: the otakase-core helper process (cmd/otakase-core)
/// speaking newline-delimited JSON over stdin and stdout. It answers the same
/// requests as the Android binding. A crashed helper is started again on the
/// next request; requests that were running get a `core_exited` error.
class DesktopCore {
  DesktopCore({String? executable, CoreLauncher? launcher})
    : executable = executable ?? helperPath(),
      _launcher = launcher ?? _launch;

  /// The shared instance used by [CoreClient] on desktop.
  static final DesktopCore instance = DesktopCore();

  final String executable;
  final CoreLauncher _launcher;
  Process? _process;
  Future<Process>? _starting;
  int _next = 0;
  final Map<int, Completer<String>> _pending = {};
  // Requests the helper has not answered yet, whether or not their result
  // has been collected.
  final Set<int> _running = {};
  final Set<int> _claimed = {};

  static Future<Process> _launch(String executable) =>
      Process.start(executable, const [], runInShell: false);

  /// Where the helper is installed: next to the app's executable (Windows,
  /// Linux) or in the bundle's Resources (macOS). OTAKASE_CORE overrides it
  /// for development builds.
  static String helperPath() {
    final override = Host.environment['OTAKASE_CORE'];
    if (override != null && override.isNotEmpty) return override;
    final name = Host.windows ? 'otakase-core.exe' : 'otakase-core';
    final dir = File(Host.executable).parent;
    if (Host.macOS) {
      return '${dir.parent.path}/Resources/$name';
    }
    return '${dir.path}${Host.pathSeparator}$name';
  }

  Future<Process> _ensure() {
    if (_process case final process?) return Future.value(process);
    return _starting ??= _spawn().whenComplete(() => _starting = null);
  }

  Future<Process> _spawn() async {
    final Process process;
    try {
      process = await _launcher(executable);
    } on ProcessException catch (e) {
      throw PlatformException(
        code: 'core_missing',
        message: 'Could not start otakase-core: ${e.message}',
      );
    }
    _process = process;
    process.stdout
        .transform(utf8.decoder)
        .transform(const LineSplitter())
        .listen(_onLine, onDone: () => _onExit(process));
    // A write after the helper died fails here; its requests already got
    // core_exited from _onExit.
    process.stdin.done.ignore();
    // Keep stderr drained so the helper never blocks on a full pipe.
    process.stderr.drain<void>().ignore();
    unawaited(process.exitCode.then((_) => _onExit(process)));
    return process;
  }

  void _onLine(String line) {
    if (line.isEmpty) return;
    final Object? message;
    try {
      message = jsonDecode(line);
    } on FormatException {
      return;
    }
    if (message is! Map) return;
    final id = message['id'];
    final result = message['result'];
    if (id is! int || result == null) return;
    _running.remove(id);
    final completer = _pending[id];
    if (completer != null && !completer.isCompleted) {
      completer.complete(jsonEncode(result));
    }
  }

  void _onExit(Process process) {
    if (!identical(_process, process)) return;
    _process = null;
    _running.clear();
    for (final completer in _pending.values) {
      if (completer.isCompleted) continue;
      completer.complete(
        jsonEncode({
          'version': 1,
          'error': {
            'code': 'core_exited',
            'message': 'The Otakase core stopped. Try again.',
            'retryable': true,
          },
        }),
      );
    }
  }

  void _send(Process process, Map<String, Object?> message) {
    process.stdin.writeln(jsonEncode(message));
  }

  /// Sends a request (the JSON CoreClient builds) and returns its handle.
  Future<int> start(String input) async {
    final process = await _ensure();
    final id = ++_next;
    _pending[id] = Completer<String>();
    _running.add(id);
    _send(process, {'id': id, 'op': 'request', 'input': jsonDecode(input)});
    return id;
  }

  /// The raw core response for [id], once it finishes. Call it once per
  /// handle, like the Android binding; it releases the handle.
  Future<String> result(int id) {
    final completer = _pending[id];
    if (completer == null || !_claimed.add(id)) {
      return Future.value(
        jsonEncode({
          'version': 1,
          'error': {
            'code': 'invalid_handle',
            'message': 'Unknown or already collected request.',
            'retryable': false,
          },
        }),
      );
    }
    return completer.future.whenComplete(() {
      _pending.remove(id);
      _claimed.remove(id);
    });
  }

  Future<void> cancel(int id) async {
    if (!_running.contains(id)) return;
    if (_process case final process?) {
      _send(process, {'id': id, 'op': 'cancel'});
    }
  }

  Future<void> stopPlayback() async {
    if (_process case final process?) _send(process, {'op': 'stop_playback'});
  }

  /// Closes the helper's input; it cancels its work and exits.
  Future<void> close() async {
    final process = _process;
    if (process == null) return;
    await process.stdin.close();
  }
}
