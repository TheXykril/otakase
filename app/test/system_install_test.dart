import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/desktop/system_install.dart';

void main() {
  late Directory dir;
  late List<List<String>> ran;

  setUp(() async {
    dir = await Directory.systemTemp.createTemp('otakase-install-');
    ran = [];
  });
  tearDown(() => dir.delete(recursive: true));

  Future<ProcessResult> ok(String exe, List<String> args) async {
    ran.add([exe, ...args]);
    return ProcessResult(0, 0, 'Otakase is installed.\n', '');
  }

  group('Linux', () {
    SystemInstall linux({String? appImage, InstallRunner? run}) =>
        SystemInstall(
          environment: {'HOME': dir.path, 'APPIMAGE': ?appImage},
          executable: '/tmp/.mount_x/usr/lib/otakase/otakase',
          run: run ?? ok,
          linux: true,
          macos: false,
        );

    test('only an AppImage can install itself', () {
      expect(linux().available, isFalse);
      expect(
        linux(appImage: '/home/me/Downloads/o.AppImage').available,
        isTrue,
      );
    });

    test('install runs the AppImage with --install', () async {
      final s = linux(appImage: '/home/me/Downloads/o.AppImage');
      expect(await s.install(), 'Otakase is installed.');
      expect(ran, [
        ['/home/me/Downloads/o.AppImage', '--install'],
      ]);
    });

    test('a failed install says why', () async {
      final s = linux(
        appImage: '/x.AppImage',
        run: (_, _) async => ProcessResult(0, 1, '', 'disk full'),
      );
      expect(
        s.install,
        throwsA(
          isA<InstallFailure>().having(
            (e) => e.message,
            'message',
            'disk full',
          ),
        ),
      );
    });

    test('installed needs the command, the copy and the menu entry', () async {
      final s = linux(appImage: '/x.AppImage');
      expect(await s.installed(), isFalse);
      await File(s.installedImage).create(recursive: true);
      await File(s.menuEntry).create(recursive: true);
      await Directory('${dir.path}/.local/bin').create(recursive: true);
      await Link(s.linuxCommand).create('/somewhere/else.AppImage');
      expect(await s.installed(), isFalse);
      await Link(s.linuxCommand).update(s.installedImage);
      expect(await s.installed(), isTrue);
    });
  });

  group('macOS', () {
    SystemInstall mac(String app, {InstallRunner? run, String? bin}) =>
        SystemInstall(
          environment: {'HOME': '/Users/me'},
          executable: '$app/Contents/MacOS/Otakase',
          run: run ?? ok,
          linux: false,
          macos: true,
          binDir: bin ?? '${dir.path}/bin',
          temp: dir,
        );

    test('writes the otakase command for the app in Applications', () async {
      final s = mac('/Applications/Otakase.app');
      expect(s.available, isTrue);
      expect(await s.installed(), isFalse);
      await s.install();
      final command = File('${dir.path}/bin/otakase');
      expect(
        await command.readAsString(),
        contains(
          'exec "/Applications/Otakase.app/Contents/MacOS/Otakase" "\$@"',
        ),
      );
      expect(ran.single, ['chmod', '755', command.path]);
      expect(await s.installed(), isTrue);
    });

    test('asks for the password when the folder is not writable', () async {
      await File('${dir.path}/blocked').writeAsString('');
      // A file where the folder should be: creating it fails.
      final s = mac('/Applications/Otakase.app', bin: '${dir.path}/blocked');
      await s.install();
      expect(ran.single.first, '/usr/bin/osascript');
      expect(ran.single.last, contains('with administrator privileges'));
      expect(ran.single.last, contains("install -m 755"));
      expect(
        dir.listSync().where((f) => f.path.contains('otakase-command')),
        isEmpty,
      );
    });

    test('a cancelled password prompt is a failure', () async {
      await File('${dir.path}/blocked').writeAsString('');
      final s = mac(
        '/Applications/Otakase.app',
        bin: '${dir.path}/blocked',
        run: (_, _) async => ProcessResult(0, 1, '', 'User canceled.'),
      );
      expect(s.install, throwsA(isA<InstallFailure>()));
    });

    test('wants the app in Applications first', () async {
      final s = mac('/Users/me/Downloads/Otakase.app');
      expect(s.install, throwsA(isA<InstallFailure>()));
      expect(await File('${dir.path}/bin/otakase').exists(), isFalse);
    });
  });

  group('install.sh', () {
    // Runs the real script against a stand-in AppImage in a scratch home.
    late String home, appDir, image;

    Future<ProcessResult> sh(String action, {String? appImage}) => Process.run(
      'sh',
      ['packaging/linux/install.sh', action],
      environment: {
        'HOME': home,
        'PATH': '$home/.local/bin:/usr/bin:/bin',
        'APPDIR': appDir,
        'APPIMAGE': ?appImage,
      },
      includeParentEnvironment: false,
    );

    setUp(() async {
      home = '${dir.path}/home';
      appDir = '${dir.path}/AppDir';
      image = '$home/Downloads/otakase-app-linux-x86_64.AppImage';
      await File(image).create(recursive: true);
      await File(image).writeAsString('appimage v1');
      await File('$appDir/usr/share/icons/hicolor/256x256/apps/otakase.png')
          .create(recursive: true);
    });

    test('installs, refreshes and uninstalls', () async {
      final installed = '$home/.local/share/otakase-app/otakase.AppImage';
      final r = await sh('--install', appImage: image);
      expect(r.exitCode, 0, reason: '${r.stderr}');
      expect(r.stdout, contains('Otakase is installed.'));
      expect(await File(installed).readAsString(), 'appimage v1');
      expect(await Link('$home/.local/bin/otakase').target(), installed);
      final entry = await File(
        '$home/.local/share/applications/otakase.desktop',
      ).readAsString();
      expect(entry, contains('Exec="$installed"'));
      expect(entry, contains('Exec="$installed" --launcher'));
      final icon = File(
        '$home/.local/share/icons/hicolor/256x256/apps/otakase.png',
      );
      expect(await icon.exists(), isTrue);

      // The installed copy puts a lost icon back on start; the download
      // does not touch anything.
      await icon.delete();
      expect((await sh('--refresh', appImage: image)).exitCode, 0);
      expect(await icon.exists(), isFalse);
      expect((await sh('--refresh', appImage: installed)).exitCode, 0);
      expect(await icon.exists(), isTrue);

      final u = await sh('--uninstall', appImage: installed);
      expect(u.exitCode, 0, reason: '${u.stderr}');
      expect(await File(installed).exists(), isFalse);
      expect(await Link('$home/.local/bin/otakase').exists(), isFalse);
      expect(await icon.exists(), isFalse);
      expect(await File(image).exists(), isTrue);
    });

    test('keeps a file already at ~/.local/bin/otakase', () async {
      await File('$home/.local/bin/otakase').create(recursive: true);
      await File('$home/.local/bin/otakase').writeAsString('old build');
      final r = await sh('--install', appImage: image);
      expect(r.exitCode, 0, reason: '${r.stderr}');
      expect(
        await File('$home/.local/bin/otakase.old').readAsString(),
        'old build',
      );
    });

    test('needs the AppImage', () async {
      final r = await sh('--install');
      expect(r.exitCode, 1);
      expect(r.stderr, contains('--install'));
    });
  });
}
