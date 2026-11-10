import 'dart:io';

import 'package:flutter/material.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../host.dart';

/// Runs a program and returns how it went. Tests replace it.
typedef InstallRunner = Future<ProcessResult> Function(
  String executable,
  List<String> arguments,
);

Future<ProcessResult> _defaultRun(String executable, List<String> arguments) =>
    Process.run(executable, arguments);

/// Hooks the desktop app into the system, so it opens from the app menu and
/// as `otakase` in a terminal.
///
/// * Linux: the AppImage installs itself with `--install`
///   (packaging/linux/install.sh): it moves to ~/.local/share/otakase-app,
///   gets a menu entry, an icon and ~/.local/bin/otakase.
/// * macOS: Otakase.app in Applications is already in Launchpad and
///   Spotlight; this adds /usr/local/bin/otakase, asking for the password
///   when that folder is not writable.
/// * Windows: the installer does all of it, so there is nothing to do here.
class SystemInstall {
  SystemInstall({
    Map<String, String>? environment,
    String? executable,
    InstallRunner? run,
    bool? linux,
    bool? macos,
    this.binDir = '/usr/local/bin',
    this.temp,
  }) : environment = environment ?? Host.environment,
       executable = executable ?? Host.executable,
       _run = run ?? _defaultRun,
       linux = linux ?? Host.linux,
       macos = macos ?? Host.macOS;

  final Map<String, String> environment;
  final String executable;
  final InstallRunner _run;
  final bool linux, macos;

  /// Where the macOS command goes. It is on PATH on every Mac.
  final String binDir;
  final Directory? temp;

  String get _home => environment['HOME'] ?? '';

  /// The running AppImage, when this is one.
  String? get appImage {
    final path = environment['APPIMAGE'];
    return path == null || path.isEmpty ? null : path;
  }

  String get _data {
    final xdg = environment['XDG_DATA_HOME'] ?? '';
    return xdg.startsWith('/') ? xdg : '$_home/.local/share';
  }

  /// Where install.sh puts the AppImage.
  String get installedImage => '$_data/otakase-app/otakase.AppImage';
  String get linuxCommand => '$_home/.local/bin/otakase';
  String get menuEntry => '$_data/applications/otakase.desktop';

  /// The Otakase.app this runs from.
  Directory get bundle => File(executable).parent.parent.parent;
  String get macCommand => '$binDir/otakase';

  /// Whether this copy can be installed from inside the app.
  bool get available =>
      (linux && appImage != null) || (macos && bundle.path.endsWith('.app'));

  /// Whether the menu entry and the command are in place for this copy.
  Future<bool> installed() async {
    if (linux) {
      final link = Link(linuxCommand);
      if (!await link.exists()) return false;
      final target = await link.target();
      return target == installedImage &&
          await File(installedImage).exists() &&
          await File(menuEntry).exists();
    }
    if (macos) {
      final command = File(macCommand);
      if (!await command.exists()) return false;
      return (await command.readAsString()).contains(executable);
    }
    return false;
  }

  /// The macOS command: runs this app with whatever it was given.
  String get macScript =>
      '#!/bin/sh\n# The otakase command, added by the Otakase app.\nexec "$executable" "\$@"\n';

  /// Installs, and returns what to tell the viewer. Throws an
  /// [InstallFailure] with a message for them when it cannot.
  Future<String> install() async {
    if (linux) {
      final image = appImage;
      if (image == null) {
        throw const InstallFailure(
          'Only the AppImage from the Otakase website can install itself.',
        );
      }
      final result = await _run(image, ['--install']);
      final out = '${result.stdout}'.trim();
      if (result.exitCode != 0) {
        final err = '${result.stderr}'.trim();
        throw InstallFailure(err.isNotEmpty ? err : 'Install failed.');
      }
      return out;
    }
    if (macos) {
      final app = bundle.path;
      if (!app.endsWith('.app')) {
        throw const InstallFailure('Open Otakase from its app to add this.');
      }
      final parent = bundle.parent.path;
      if (parent != '/Applications' && parent != '$_home/Applications') {
        throw const InstallFailure(
          'Move Otakase to Applications first, then open it from there and try again.',
        );
      }
      final command = File(macCommand);
      try {
        await command.parent.create(recursive: true);
        await command.writeAsString(macScript);
        final chmod = await _run('chmod', ['755', command.path]);
        if (chmod.exitCode != 0) throw const FileSystemException();
      } on FileSystemException {
        // Not writable without an admin: ask for the password the way
        // macOS does, through osascript.
        final dir = temp ?? Directory.systemTemp;
        final script = File('${dir.path}/otakase-command-$pid');
        await script.writeAsString(macScript);
        final shell =
            'mkdir -p ${_shellQuote(binDir)} && install -m 755 ${_shellQuote(script.path)} ${_shellQuote(command.path)}';
        final result = await _run('/usr/bin/osascript', [
          '-e',
          'do shell script "${_appleQuote(shell)}" with administrator privileges',
        ]);
        try {
          await script.delete();
        } on FileSystemException {
          // Left in the temp folder; macOS clears it.
        }
        if (result.exitCode != 0) {
          throw const InstallFailure('The otakase command was not added.');
        }
      }
      return 'Type otakase in a new terminal to open Otakase, or otakase --launcher for the quick launcher.';
    }
    throw const InstallFailure('The installer already did this.');
  }

  static String _shellQuote(String s) => "'${s.replaceAll("'", r"'\''")}'";
  static String _appleQuote(String s) =>
      s.replaceAll(r'\', r'\\').replaceAll('"', r'\"');
}

class InstallFailure implements Exception {
  const InstallFailure(this.message);
  final String message;
  @override
  String toString() => message;
}

const _askedKey = 'desktopInstallAsked';

/// Once, on the first start of an AppImage that is not installed yet, offers
/// to install it. Never again after an answer.
Future<void> offerSystemInstall(
  BuildContext context,
  SharedPreferences preferences, {
  SystemInstall? system,
}) async {
  final s = system ?? SystemInstall();
  if (!s.linux || !s.available) return;
  if (preferences.getBool(_askedKey) ?? false) return;
  if (await s.installed()) return;
  if (!context.mounted) return;
  final yes = await showDialog<bool>(
    context: context,
    builder: (c) => AlertDialog(
      title: const Text('Install Otakase?'),
      content: const Text(
        'Adds Otakase to your app menu and the otakase command to your '
        'terminal. It keeps itself up to date there, and you can delete '
        'the file you downloaded.',
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(c, false),
          child: const Text('Not now'),
        ),
        TextButton(
          autofocus: true,
          onPressed: () => Navigator.pop(c, true),
          child: const Text('Install'),
        ),
      ],
    ),
  );
  await preferences.setBool(_askedKey, true);
  if (yes == true && context.mounted) {
    await runSystemInstall(context, s);
  }
}

/// Installs and shows how it went.
Future<void> runSystemInstall(BuildContext context, SystemInstall s) async {
  String title, body;
  try {
    body = await s.install();
    title = 'Installed';
  } on InstallFailure catch (e) {
    title = 'Not installed';
    body = e.message;
  } on ProcessException catch (e) {
    title = 'Not installed';
    body = e.message;
  }
  if (!context.mounted) return;
  await showDialog<void>(
    context: context,
    builder: (c) => AlertDialog(
      title: Text(title),
      content: SelectableText(body),
      actions: [
        TextButton(
          autofocus: true,
          onPressed: () => Navigator.pop(c),
          child: const Text('Close'),
        ),
      ],
    ),
  );
}
