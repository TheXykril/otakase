import 'dart:io';

import 'package:flutter/services.dart';

import '../data/app_updates.dart';
import 'window_frame.dart';

/// Which desktop build this is. Each has its own release file.
enum DesktopTarget {
  windows('otakase-app-windows-setup.exe'),
  macos('otakase-app-macos.zip'),
  linux('otakase-app-linux-x86_64.AppImage');

  const DesktopTarget(this.fileName);
  final String fileName;

  static DesktopTarget get current => Platform.isWindows
      ? windows
      : Platform.isMacOS
      ? macos
      : linux;
}

/// Starts a program that keeps running after the app quits. Tests replace it.
typedef DetachedStarter = Future<void> Function(
  String executable,
  List<String> arguments,
);

Future<void> _startDetached(String executable, List<String> arguments) =>
    Process.start(executable, arguments, mode: ProcessStartMode.detached);

/// Updates for the desktop app, installed the way each system expects:
///
/// * Windows: the release's installer runs silently over the installed copy
///   once the app has quit, then opens the new version.
/// * macOS: the new Otakase.app replaces the old one in place and opens.
/// * Linux: the new AppImage replaces the old file and opens.
///
/// A copy that cannot replace itself (a portable Windows folder, an app
/// outside a writable folder, a Linux package) gets a link to the download
/// page instead.
class DesktopUpdatePlatform extends UpdatePlatform {
  DesktopUpdatePlatform({
    DesktopTarget? target,
    String? executable,
    Map<String, String>? environment,
    String? version,
    this.temp,
    DetachedStarter? start,
    Future<void> Function()? quit,
    int? processId,
  }) : target = target ?? DesktopTarget.current,
       executable = executable ?? Platform.resolvedExecutable,
       environment = environment ?? Platform.environment,
       version = version ?? const String.fromEnvironment('OTAKASE_VERSION'),
       _start = start ?? _startDetached,
       _quit = quit ?? quitDesktopApp,
       processId = processId ?? pid;

  final DesktopTarget target;
  final String executable, version;
  final Map<String, String> environment;
  final Directory? temp;
  final DetachedStarter _start;
  final Future<void> Function() _quit;
  final int processId;

  @override
  String get fileName => target.fileName;
  @override
  String get idleText => 'Check for a new Otakase version.';
  @override
  String get readyText => 'Update verified. Otakase restarts to finish.';
  @override
  String get installLabel => 'Restart and update';
  @override
  String get footnote =>
      'Your profiles, accounts and watch progress are kept. Otakase closes for a moment and opens again on the new version.';

  /// The release page, for copies that update by hand.
  static final downloadPage = Uri.https(
    'github.com',
    '/TheXykril/otakase/releases/latest',
  );

  @override
  Future<InstalledApp> installed() async =>
      version.isEmpty || versionCode(version) == 0
      ? const InstalledApp('development build', 0)
      : InstalledApp(version, versionCode(version));

  /// The installed Otakase.app on macOS.
  Directory get bundle => File(executable).parent.parent.parent;

  /// The running AppImage on Linux. AppImages set APPIMAGE to their own path.
  String? get appImage {
    final path = environment['APPIMAGE'];
    return path == null || path.isEmpty ? null : path;
  }

  @override
  Future<String?> manualReason() async {
    switch (target) {
      case DesktopTarget.windows:
        // Copies from the installer have its uninstaller beside them; the
        // installer updates exactly that folder.
        final uninstaller = File(
          '${File(executable).parent.path}${Platform.pathSeparator}unins000.exe',
        );
        return await uninstaller.exists()
            ? null
            : 'This copy was not installed with the Otakase installer. Download the new version from the release page.';
      case DesktopTarget.macos:
        if (!bundle.path.endsWith('.app')) {
          return 'Download the new version from the release page.';
        }
        return await _writable(bundle.parent)
            ? null
            : 'Otakase cannot replace itself in ${bundle.parent.path}. Move it to Applications, or download the new version from the release page.';
      case DesktopTarget.linux:
        final image = appImage;
        if (image == null) {
          return 'Otakase was installed by a package or by hand. Update it the same way, or use the AppImage from the release page to get updates here.';
        }
        return await _writable(File(image).parent)
            ? null
            : 'Otakase cannot replace $image. Download the new version from the release page.';
    }
  }

  static Future<bool> _writable(Directory directory) async {
    try {
      final probe = File(
        '${directory.path}${Platform.pathSeparator}.otakase-update-check-$pid',
      );
      await probe.writeAsBytes(const [], flush: true);
      await probe.delete();
      return true;
    } on FileSystemException {
      return false;
    }
  }

  /// Downloads go next to what they replace, so the swap is one rename on
  /// the same drive. The Windows installer goes to the temp folder.
  @override
  Future<File> createDownload() async {
    final Directory folder;
    final String name;
    switch (target) {
      case DesktopTarget.windows:
        folder = temp ?? Directory.systemTemp;
        name = 'otakase-app-update-setup.exe';
      case DesktopTarget.macos:
        folder = bundle.parent;
        name = '.Otakase-update.zip';
      case DesktopTarget.linux:
        final image = File(appImage!);
        folder = image.parent;
        name = '.${image.uri.pathSegments.last}.update';
    }
    final file = File('${folder.path}${Platform.pathSeparator}$name');
    if (await file.exists()) await file.delete();
    if (target == DesktopTarget.macos && await _staged.exists()) {
      await _staged.delete(recursive: true);
    }
    return file;
  }

  @override
  Future<bool> channelAllowed() async => true;

  /// Where the new Otakase.app is unpacked before the swap.
  Directory get _staged => Directory('${bundle.parent.path}/.Otakase-update');

  /// The checksum already matched the signed-off release metadata; this
  /// checks the file is what this system can run and prepares it.
  @override
  Future<void> validate(File file, AppUpdate update) async {
    final head = await file
        .openRead(0, 4)
        .fold<List<int>>([], (all, chunk) => all..addAll(chunk));
    switch (target) {
      case DesktopTarget.windows:
        if (head.length < 2 || head[0] != 0x4d || head[1] != 0x5a) {
          throw _invalid('The download is not a Windows installer.');
        }
      case DesktopTarget.linux:
        if (head.length < 4 ||
            head[0] != 0x7f ||
            head[1] != 0x45 ||
            head[2] != 0x4c ||
            head[3] != 0x46) {
          throw _invalid('The download is not an AppImage.');
        }
        final chmod = await Process.run('chmod', ['755', file.path]);
        if (chmod.exitCode != 0) {
          throw _invalid('Unable to make the update runnable.');
        }
      case DesktopTarget.macos:
        if (head.length < 4 || head[0] != 0x50 || head[1] != 0x4b) {
          throw _invalid('The download is not a macOS app.');
        }
        final staged = _staged;
        if (await staged.exists()) await staged.delete(recursive: true);
        final unpack = await Process.run('/usr/bin/ditto', [
          '-x',
          '-k',
          file.path,
          staged.path,
        ]);
        await file.delete();
        final app = File('${staged.path}/Otakase.app/Contents/MacOS/Otakase');
        if (unpack.exitCode != 0 || !await app.exists()) {
          if (await staged.exists()) await staged.delete(recursive: true);
          throw _invalid('The download could not be unpacked.');
        }
    }
  }

  static PlatformException _invalid(String message) =>
      PlatformException(code: 'invalid_update', message: message);

  /// Puts the update in place, starts the new version once this one has
  /// quit, and quits.
  @override
  Future<bool> install(File file, AppUpdate update) async {
    switch (target) {
      case DesktopTarget.windows:
        // The installer waits for this process to end before replacing
        // files, and opens Otakase again when it is done.
        await _start(file.path, [
          '/VERYSILENT',
          '/SUPPRESSMSGBOXES',
          '/NORESTART',
          '/WAITPID=$processId',
        ]);
      case DesktopTarget.macos:
        await _swapBundle();
        await _relaunch('/usr/bin/open', bundle.path);
      case DesktopTarget.linux:
        final image = appImage!;
        try {
          await file.rename(image);
        } on FileSystemException {
          throw _invalid('Unable to replace $image.');
        }
        await _relaunch(image, null);
    }
    await _quit();
    return true;
  }

  Future<void> _swapBundle() async {
    final current = bundle;
    final old = Directory('${current.parent.path}/.Otakase-old');
    if (await old.exists()) await old.delete(recursive: true);
    try {
      await current.rename(old.path);
    } on FileSystemException {
      throw _invalid('Unable to replace ${current.path}.');
    }
    try {
      await Directory('${_staged.path}/Otakase.app').rename(current.path);
    } on FileSystemException {
      // Put the running version back so nothing is lost.
      await old.rename(current.path);
      throw _invalid('Unable to replace ${current.path}.');
    }
    for (final leftover in [old, _staged]) {
      try {
        await leftover.delete(recursive: true);
      } on FileSystemException {
        // Removed on the next update.
      }
    }
  }

  /// Opens [program] (with [argument]) after this process has ended.
  Future<void> _relaunch(String program, String? argument) => _start(
    '/bin/sh',
    [
      '-c',
      'while kill -0 "\$1" 2>/dev/null; do sleep 0.2; done; shift; exec "\$@"',
      'otakase-relaunch',
      '$processId',
      program,
      ?argument,
    ],
  );
}
