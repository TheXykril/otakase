import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../services.dart';
import '../ui/components.dart';
import '../ui/device_status.dart';

// A key that opens the launcher from anywhere, replacing the CLI's
// Super+Shift+A rofi binding. On Hyprland the app writes the binding into
// the viewer's config (backed up first, safe to run twice, and it never
// takes a key someone else bound). Elsewhere it shows the command to bind
// in the system's own keyboard settings.

const launcherCombo = 'SUPER + SHIFT + A';
const _luaStart =
    '-- >>> otakase launcher keybinding (managed by the Otakase app) >>>';
const _luaEnd = '-- <<< otakase launcher keybinding <<<';
const _confStart =
    '# >>> otakase launcher keybinding (managed by the Otakase app) >>>';
const _confEnd = '# <<< otakase launcher keybinding <<<';
// What the CLI wrote; replaced by the app's block when found.
const _cliLuaStart =
    '-- >>> otakase keybinding (managed; remove with -remove-keybind) >>>';
const _cliLuaEnd = '-- <<< otakase keybinding <<<';
const _cliConfStart =
    '# >>> otakase keybinding (managed; remove with -remove-keybind) >>>';
const _cliConfEnd = '# <<< otakase keybinding <<<';

/// The command that opens the launcher: this app with --launcher.
String launcherCommand([Map<String, String>? env, String? executable]) {
  final e = env ?? Platform.environment;
  // An AppImage runs from a temporary mount; bind the AppImage itself.
  var exe = e['APPIMAGE'] ?? executable ?? Platform.resolvedExecutable;
  if (exe.contains(' ')) exe = '"$exe"';
  return '$exe --launcher';
}

class HyprlandConfig {
  const HyprlandConfig(this.path, {required this.lua});
  final String path;
  final bool lua;
  String get start => lua ? _luaStart : _confStart;
  String get end => lua ? _luaEnd : _confEnd;
  String block(String command) {
    final body = lua
        ? 'hl.unbind("$launcherCombo")\no.bind("$launcherCombo", "Otakase", "exec $command")'
        : 'bind = SUPER SHIFT, A, exec, $command';
    return '$start\n$body\n$end';
  }
}

/// Omarchy's bindings.lua when present, else hyprland.conf.
HyprlandConfig? findHyprlandConfig(String home) {
  final lua = '$home/.config/hypr/bindings.lua';
  if (File(lua).existsSync()) return HyprlandConfig(lua, lua: true);
  final conf = '$home/.config/hypr/hyprland.conf';
  if (File(conf).existsSync()) return HyprlandConfig(conf, lua: false);
  return null;
}

bool get onHyprland =>
    Platform.isLinux &&
    (Platform.environment['HYPRLAND_INSTANCE_SIGNATURE'] ?? '').isNotEmpty;

(int, int)? _range(String body, String start, String end) {
  final s = body.indexOf(start);
  if (s < 0) return null;
  final e = body.indexOf(end, s);
  if (e < 0) return null;
  return (s, e + end.length);
}

/// The managed block (the app's or the CLI's) in [body].
(int, int)? managedRange(String body, HyprlandConfig c) =>
    _range(body, c.start, c.end) ??
    (c.lua
        ? _range(body, _cliLuaStart, _cliLuaEnd)
        : _range(body, _cliConfStart, _cliConfEnd));

/// A line outside the managed block that already binds the key.
String conflictingBinding(String body, HyprlandConfig c) {
  for (final raw in body.split('\n')) {
    final t = raw.trim();
    if (t.isEmpty || t.startsWith('--') || t.startsWith('#')) continue;
    if (c.lua) {
      if (t.contains('o.bind') && t.contains(launcherCombo)) return t;
    } else if (t.startsWith('bind') &&
        t.toUpperCase().contains('SUPER SHIFT, A,')) {
      return t;
    }
  }
  return '';
}

/// The config with the binding added or updated; throws a [StateError]
/// naming the line when someone else owns the key.
String withLauncherBinding(String body, HyprlandConfig c, String command) {
  final r = managedRange(body, c);
  if (r != null) {
    return body.substring(0, r.$1) + c.block(command) + body.substring(r.$2);
  }
  final owner = conflictingBinding(body, c);
  if (owner.isNotEmpty) {
    throw StateError('$launcherCombo is already bound:\n$owner');
  }
  final sep = body.isEmpty || body.endsWith('\n') ? '' : '\n';
  return '$body$sep\n${c.block(command)}\n';
}

/// The config without the binding.
String withoutLauncherBinding(String body, HyprlandConfig c) {
  final r = managedRange(body, c);
  if (r == null) return body;
  final before = body.substring(0, r.$1).replaceFirst(RegExp(r'\n+$'), '');
  final after = body.substring(r.$2).replaceFirst(RegExp(r'^\n+'), '');
  return '$before\n$after';
}

/// Writes [contents] after backing [path] up; returns the backup's path.
Future<String> writeWithBackup(String path, String contents) async {
  final f = File(path);
  final backup = '$path.bak.${DateTime.now().millisecondsSinceEpoch ~/ 1000}';
  await f.copy(backup);
  await f.writeAsString(contents);
  return backup;
}

class LauncherShortcutPage extends StatefulWidget {
  const LauncherShortcutPage({super.key, required this.services});
  final AppServices services;
  @override
  State<LauncherShortcutPage> createState() => _LauncherShortcutPageState();
}

class _LauncherShortcutPageState extends State<LauncherShortcutPage> {
  String message = '';
  final command = launcherCommand();
  HyprlandConfig? get config =>
      findHyprlandConfig(Platform.environment['HOME'] ?? '');

  bool get bound {
    final c = config;
    if (c == null) return false;
    try {
      return managedRange(File(c.path).readAsStringSync(), c) != null;
    } catch (_) {
      return false;
    }
  }

  Future<void> change(bool add) async {
    final c = config;
    if (c == null) return;
    try {
      final body = await File(c.path).readAsString();
      final next = add
          ? withLauncherBinding(body, c, command)
          : withoutLauncherBinding(body, c);
      if (next == body) {
        setState(() => message = 'Nothing to change.');
        return;
      }
      final backup = await writeWithBackup(c.path, next);
      try {
        await Process.run('hyprctl', ['reload']);
      } catch (_) {}
      setState(
        () => message = add
            ? 'Super+Shift+A now opens the launcher. Backup: $backup'
            : 'Removed the binding. Backup: $backup',
      );
    } on StateError catch (e) {
      setState(
        () => message =
            '${e.message}\nChange that line yourself to use it for Otakase.',
      );
    } catch (e) {
      setState(() => message = 'Could not change ${c.path}: $e');
    }
  }

  String get help {
    if (Platform.isWindows) {
      return 'Right-click the Otakase shortcut in the Start menu, open its file location, then Properties: put the command below in Target and pick a Shortcut key.';
    }
    if (Platform.isMacOS) {
      return 'In Shortcuts, make a shortcut that runs the command below as a shell script, then give it a keyboard shortcut.';
    }
    final desktop = (Platform.environment['XDG_CURRENT_DESKTOP'] ?? '')
        .toLowerCase();
    if (desktop.contains('kde')) {
      return 'System Settings, Shortcuts, Add New, Command or Script: paste the command below and press the keys you want.';
    }
    if (desktop.contains('gnome')) {
      return 'Settings, Keyboard, View and Customise Shortcuts, Custom Shortcuts: add one with the command below.';
    }
    return 'Bind the command below to a key in your desktop’s keyboard settings.';
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: const Text('Launcher shortcut'),
      actions: const [DeviceStatus(isTV: true), SizedBox(width: 16)],
    ),
    body: ListView(
      padding: const EdgeInsets.all(32),
      children: [
        const PageHeading('Open from anywhere', '鍵'),
        const SizedBox(height: 12),
        Text(
          'The launcher is a small window like the old rofi menu: type, pick, watch.',
          style: uiStyle(context, color: muted),
        ),
        const SizedBox(height: 24),
        if (onHyprland && config != null) ...[
          Text(
            bound
                ? 'Super+Shift+A opens the launcher (in ${config!.path}).'
                : 'Bind Super+Shift+A in ${config!.path}. It is backed up first.',
            style: uiStyle(context),
          ),
          const SizedBox(height: 12),
          Wrap(
            spacing: 12,
            children: [
              AppButton(
                label: bound ? 'Update binding' : 'Add Super+Shift+A',
                filled: true,
                isTV: true,
                autofocus: true,
                onPressed: () => change(true),
              ),
              if (bound)
                AppButton(
                  label: 'Remove binding',
                  isTV: true,
                  onPressed: () => change(false),
                ),
            ],
          ),
        ] else
          Text(help, style: uiStyle(context)),
        const SizedBox(height: 20),
        SelectableText(command, style: mono.copyWith(color: fg)),
        const SizedBox(height: 12),
        Align(
          alignment: Alignment.centerLeft,
          child: AppButton(
            label: 'Copy command',
            isTV: true,
            autofocus: !onHyprland,
            onPressed: () async {
              await Clipboard.setData(ClipboardData(text: command));
              setState(() => message = 'Copied.');
            },
          ),
        ),
        if (message.isNotEmpty) ...[
          const SizedBox(height: 16),
          Text(message, style: uiStyle(context, color: accentText)),
        ],
      ],
    ),
  );
}
