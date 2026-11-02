import 'dart:async';
import 'dart:io';

import 'package:flutter/widgets.dart';

// Tiling compositors (Hyprland, sway, i3) put every new window in the
// tiling layout, so the launcher opened as a big tile instead of the small
// popup the old rofi menu was. These ask the compositor to float this
// app's own window (found by its process id), size it, centre it and focus
// it; nothing has to be added to the viewer's config.

/// The compositor the app runs under, when it is one that tiles.
enum Tiler { hyprland, sway, i3 }

Tiler? tilerFrom(Map<String, String> env, {bool? linux}) {
  if (!(linux ?? Platform.isLinux)) return null;
  if ((env['HYPRLAND_INSTANCE_SIGNATURE'] ?? '').isNotEmpty) {
    return Tiler.hyprland;
  }
  if ((env['SWAYSOCK'] ?? '').isNotEmpty) return Tiler.sway;
  if ((env['I3SOCK'] ?? '').isNotEmpty) return Tiler.i3;
  return null;
}

/// Hyprland 0.55 and later (Lua config): each dispatcher is a Lua call,
/// sent one per `hyprctl dispatch`.
List<String> hyprlandLuaDispatches(int pid, {required Size? size}) {
  final window = 'window = "pid:$pid"';
  if (size == null) {
    return ['hl.dsp.window.float({ action = "unset", $window })'];
  }
  return [
    'hl.dsp.window.float({ action = "set", $window })',
    'hl.dsp.window.resize({ x = ${size.width.round()}, y = ${size.height.round()}, $window })',
    'hl.dsp.window.center({ $window })',
    'hl.dsp.focus({ $window })',
  ];
}

/// Hyprland before 0.55 (hyprlang config): one `hyprctl --batch` line.
/// centerwindow works on the focused window, so focus comes first.
String hyprlandLegacyBatch(int pid, {required Size? size}) {
  final w = 'pid:$pid';
  if (size == null) return 'dispatch settiled $w';
  return [
    'dispatch setfloating $w',
    'dispatch resizewindowpixel exact ${size.width.round()} ${size.height.round()},$w',
    'dispatch focuswindow $w',
    'dispatch centerwindow',
  ].join(' ; ');
}

/// sway and i3 take the same command.
String swayCommand(int pid, {required Size? size}) => size == null
    ? '[pid=$pid] floating disable'
    : '[pid=$pid] floating enable, resize set ${size.width.round()} ${size.height.round()}, move position center, focus';

typedef CommandRunner = Future<ProcessResult> Function(
  String executable,
  List<String> args,
);

/// Floats this window at [size] in the middle of the screen, or puts it
/// back in the tiling layout when [size] is null. Does nothing outside a
/// tiling compositor. Returns true when the compositor took it.
///
/// The window may not be mapped yet right after it is shown, so it tries a
/// few times.
Future<bool> placeOnTiler({
  Size? size,
  Map<String, String>? env,
  bool? linux,
  int? processId,
  CommandRunner? run,
  int attempts = 6,
  Duration wait = const Duration(milliseconds: 150),
}) async {
  final tiler = tilerFrom(env ?? Platform.environment, linux: linux);
  if (tiler == null) return false;
  final id = processId ?? pid;
  final runner = run ?? Process.run;
  for (var i = 0; i < attempts; i++) {
    try {
      if (await _place(tiler, id, size, runner)) return true;
    } catch (_) {
      return false; // hyprctl or swaymsg missing: nothing to do.
    }
    if (i + 1 < attempts) await Future<void>.delayed(wait);
  }
  return false;
}

bool _ok(ProcessResult r) {
  final out = '${r.stdout}'.trim().toLowerCase();
  return r.exitCode == 0 && (out.isEmpty || out.startsWith('ok'));
}

Future<bool> _place(Tiler tiler, int id, Size? size, CommandRunner run) async {
  switch (tiler) {
    case Tiler.hyprland:
      final lua = hyprlandLuaDispatches(id, size: size);
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
        hyprlandLegacyBatch(id, size: size),
      ]);
      final text = '${legacy.stdout}'.toLowerCase();
      return legacy.exitCode == 0 &&
          !text.contains('no such window') &&
          !text.contains('not found');
    case Tiler.sway || Tiler.i3:
      final r = await run(tiler == Tiler.sway ? 'swaymsg' : 'i3-msg', [
        swayCommand(id, size: size),
      ]);
      // An unmatched criteria reports success false.
      return r.exitCode == 0 && !'${r.stdout}'.contains('"success": false');
  }
}
