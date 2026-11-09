import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/widgets.dart';

import '../host.dart';

// Tiling compositors (Hyprland, sway, i3) put every new window in the
// tiling layout, so the launcher opened as a big tile instead of the small
// popup the old rofi menu was. These ask the compositor to float this
// app's own window (found by its process id), size it, centre it and focus
// it; nothing has to be added to the viewer's config.

/// The compositor the app runs under, when it is one that tiles.
enum Tiler { hyprland, sway, i3 }

Tiler? tilerFrom(Map<String, String> env, {bool? linux}) {
  if (!(linux ?? Host.linux)) return null;
  if ((env['HYPRLAND_INSTANCE_SIGNATURE'] ?? '').isNotEmpty) {
    return Tiler.hyprland;
  }
  if ((env['SWAYSOCK'] ?? '').isNotEmpty) return Tiler.sway;
  if ((env['I3SOCK'] ?? '').isNotEmpty) return Tiler.i3;
  return null;
}

/// Hyprland 0.55 and later (Lua config): each dispatcher is a Lua call,
/// sent one per `hyprctl dispatch`.
List<String> hyprlandLuaDispatches(
  int pid, {
  required Size? size,
  bool raise = false,
  String? workspace,
}) {
  final window = 'window = "pid:$pid"';
  if (size == null) {
    return ['hl.dsp.window.float({ action = "unset", $window })'];
  }
  return [
    'hl.dsp.window.float({ action = "set", $window })',
    if (workspace != null)
      'hl.dsp.window.move({ workspace = ${jsonEncode(workspace)}, follow = false, $window })',
    'hl.dsp.window.resize({ x = ${size.width.round()}, y = ${size.height.round()}, $window })',
    'hl.dsp.window.center({ $window })',
    'hl.dsp.focus({ $window })',
    if (raise) 'hl.dsp.window.alter_zorder({ mode = "top", $window })',
  ];
}

/// Hyprland before 0.55 (hyprlang config): one `hyprctl --batch` line.
/// centerwindow works on the focused window, so focus comes first.
String hyprlandLegacyBatch(
  int pid, {
  required Size? size,
  bool raise = false,
  String? workspace,
}) {
  final w = 'pid:$pid';
  if (size == null) return 'dispatch settiled $w';
  return [
    'dispatch setfloating $w',
    if (workspace != null) 'dispatch movetoworkspacesilent $workspace,$w',
    'dispatch resizewindowpixel exact ${size.width.round()} ${size.height.round()},$w',
    'dispatch focuswindow $w',
    'dispatch centerwindow',
    if (raise) 'dispatch alterzorder top,$w',
  ].join(' ; ');
}

/// sway and i3 take the same command.
String swayCommand(int pid, {required Size? size, String? workspace}) {
  if (size == null) return '[pid=$pid] floating disable';
  return '[pid=$pid] floating enable, '
      '${workspace == null ? '' : 'move container to workspace ${jsonEncode(workspace)}, '}'
      'resize set ${size.width.round()} ${size.height.round()}, move position center, focus';
}

typedef CommandRunner = Future<ProcessResult> Function(
  String executable,
  List<String> args,
);

/// Captured before mapping, when the previous application still owns focus.
Future<String?> focusedTilerWorkspace({
  Map<String, String>? env,
  bool? linux,
  CommandRunner? run,
}) async {
  final tiler = tilerFrom(env ?? Host.environment, linux: linux);
  if (tiler == null) return null;
  final runner = run ?? Process.run;
  try {
    final result =
        await (tiler == Tiler.hyprland
                ? runner('hyprctl', ['-j', 'activeworkspace'])
                : runner(tiler == Tiler.sway ? 'swaymsg' : 'i3-msg', [
                    '-t',
                    'get_workspaces',
                  ]))
            .timeout(const Duration(milliseconds: 350));
    if (result.exitCode != 0) return null;
    final data = jsonDecode(result.stdout as String);
    if (tiler == Tiler.hyprland) {
      final id = data is Map ? data['id'] : null;
      return id is int && id > 0 ? '$id' : null;
    }
    if (data is! List) return null;
    for (final workspace in data.whereType<Map>()) {
      final name = workspace['name'];
      if (workspace['focused'] == true && name is String && name.isNotEmpty) {
        return name;
      }
    }
  } on Exception {
    return null;
  }
  return null;
}

/// Floats this window at [size] in the middle of the screen, or puts it
/// back in the tiling layout when [size] is null. Does nothing outside a
/// tiling compositor. Returns true when the compositor took it.
///
/// The window may not be mapped yet right after it is shown, so it tries a
/// few times.
Future<bool> placeOnTiler({
  Size? size,
  bool raise = false,
  String? workspace,
  Map<String, String>? env,
  bool? linux,
  int? processId,
  CommandRunner? run,
  int attempts = 6,
  Duration wait = const Duration(milliseconds: 150),
}) async {
  final tiler = tilerFrom(env ?? Host.environment, linux: linux);
  if (tiler == null) return false;
  final id = processId ?? pid;
  final runner = run ?? Process.run;
  for (var i = 0; i < attempts; i++) {
    try {
      if (await _place(tiler, id, size, raise, workspace, runner)) return true;
    } catch (_) {
      return false; // hyprctl or swaymsg missing: nothing to do.
    }
    if (i + 1 < attempts) await Future<void>.delayed(wait);
  }
  return false;
}

/// Hyprland handles its modifier drag before the window receives pointer
/// input. Keep the popup anchored by putting it back when its position settles.
class HyprlandLauncherAnchor {
  HyprlandLauncherAnchor({
    required this.processId,
    required this.restore,
    CommandRunner? run,
  }) : _run = run ?? Process.run;

  final int processId;
  final Future<void> Function(String? workspace) restore;
  final CommandRunner _run;
  Timer? _timer;
  Future<void>? _restoring;
  ({int x, int y, String? workspace})? _home, _displaced;
  bool _checking = false, _stopped = false;

  Future<void> start() async {
    await check();
    if (!_stopped && _home != null) {
      _timer = Timer.periodic(
        const Duration(milliseconds: 180),
        (_) => unawaited(check()),
      );
    }
  }

  Future<void> stop() async {
    _stopped = true;
    _timer?.cancel();
    await _restoring;
  }

  Future<void> check() async {
    if (_stopped || _checking) return;
    _checking = true;
    try {
      final result = await _run('hyprctl', [
        '-j',
        'clients',
      ]).timeout(const Duration(milliseconds: 500));
      if (_stopped || result.exitCode != 0) return;
      final clients = jsonDecode(result.stdout.toString());
      if (clients is! List) return;
      for (final client in clients.whereType<Map>()) {
        if (client['pid'] != processId || client['mapped'] != true) continue;
        final at = client['at'];
        final workspace = client['workspace'];
        final id = workspace is Map ? workspace['id'] : null;
        if (at is! List || at.length != 2 || at[0] is! int || at[1] is! int) {
          return;
        }
        final position = (
          x: at[0] as int,
          y: at[1] as int,
          workspace: id is int && id > 0 ? '$id' : null,
        );
        _home ??= position;
        if (position == _home) {
          _displaced = null;
        } else if (position == _displaced) {
          _displaced = null;
          await (_restoring = restore(_home!.workspace));
        } else {
          _displaced = position;
        }
        return;
      }
    } on Exception {
      // A compositor reload can interrupt the request. Retry on the next tick.
    } finally {
      _restoring = null;
      _checking = false;
    }
  }
}

bool _ok(ProcessResult r) {
  final out = '${r.stdout}'.trim().toLowerCase();
  return r.exitCode == 0 && (out.isEmpty || out.startsWith('ok'));
}

Future<bool> _place(
  Tiler tiler,
  int id,
  Size? size,
  bool raise,
  String? workspace,
  CommandRunner run,
) async {
  switch (tiler) {
    case Tiler.hyprland:
      final lua = hyprlandLuaDispatches(
        id,
        size: size,
        raise: raise,
        workspace: workspace,
      );
      final first = await run('hyprctl', ['dispatch', lua.first]);
      if (_ok(first)) {
        for (final d in lua.skip(1)) {
          await run('hyprctl', ['dispatch', d]);
        }
        return true;
      }
      // An older Hyprland reads the classic dispatchers instead.
      final out = '${first.stdout}'.toLowerCase();
      if (out.contains('not found') || out.contains('no window')) {
        return false; // Lua config, window not mapped yet: try again.
      }
      final legacy = await run('hyprctl', [
        '--batch',
        hyprlandLegacyBatch(id, size: size, raise: raise, workspace: workspace),
      ]);
      final text = '${legacy.stdout}'.toLowerCase();
      return legacy.exitCode == 0 &&
          !text.contains('no such window') &&
          !text.contains('not found');
    case Tiler.sway || Tiler.i3:
      final r = await run(tiler == Tiler.sway ? 'swaymsg' : 'i3-msg', [
        swayCommand(id, size: size, workspace: workspace),
      ]);
      // An unmatched criteria reports success false.
      return r.exitCode == 0 && !'${r.stdout}'.contains('"success": false');
  }
}
