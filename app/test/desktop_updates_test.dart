import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/data/app_updates.dart';
import 'package:otakase/desktop/desktop_updates.dart';
import 'package:otakase/ui/app_updates.dart';

const base = 'https://otakase.com/download/v26.8.0';

// Starts of the real file kinds: an ELF AppImage and an MZ Windows program.
final elf = [0x7f, 0x45, 0x4c, 0x46, ...List.filled(60, 7)];
final mz = [0x4d, 0x5a, ...List.filled(62, 9)];

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late Directory directory;
  late List<List<String>> started;
  late int quits;

  setUp(() async {
    directory = await Directory.systemTemp.createTemp('otakase-desktop-');
    started = [];
    quits = 0;
  });
  tearDown(() => directory.delete(recursive: true));

  DesktopUpdatePlatform platform(
    DesktopTarget target, {
    String? executable,
    Map<String, String> environment = const {},
    String version = '26.7.0',
  }) => DesktopUpdatePlatform(
    target: target,
    executable: executable ?? '${directory.path}/otakase',
    environment: environment,
    version: version,
    temp: directory,
    processId: 4242,
    start: (program, arguments) async => started.add([program, ...arguments]),
    quit: () async => quits++,
  );

  AppUpdates updates(DesktopUpdatePlatform host, List<int> bytes) {
    final checksum = sha256.convert(bytes).toString();
    Map asset(String name) => {
      'name': name,
      'size': bytes.length,
      'digest': 'sha256:$checksum',
      'browser_download_url': '$base/$name',
    };
    final release = {
      'tag_name': 'v26.8.0',
      'draft': false,
      'prerelease': false,
      'assets': [
        asset(host.fileName),
        // Other systems' files are on the same release.
        asset('otakase-android.apk'),
        for (final other in DesktopTarget.values)
          if (other.fileName != host.fileName) asset(other.fileName),
      ],
    };
    return AppUpdates(
      platform: host,
      client: () => MockClient((request) async {
        if (request.url.path == '/download/latest.json') {
          return http.Response(jsonEncode(release), 200);
        }
        expect(request.url.pathSegments.last, host.fileName);
        return http.Response.bytes(bytes, 200);
      }),
    );
  }

  test('version codes match the Android build numbers', () {
    expect(versionCode('26.7.0'), 26007000);
    expect(versionCode('26.12.3'), 26012003);
    expect(versionCode('26.1.0-dev.4'), 26001000);
    expect(versionCode('26.7'), 26007000);
    expect(versionCode(''), 0);
    expect(versionCode('dev'), 0);
    expect(versionCode('1.2.3.4'), 0);
  });

  test('each system downloads only its own release file', () {
    expect(DesktopTarget.windows.fileName, 'otakase-app-windows-setup.exe');
    expect(DesktopTarget.macos.fileName, 'otakase-app-macos.zip');
    expect(DesktopTarget.linux.fileName, 'otakase-app-linux-x86_64.AppImage');
    final names = DesktopTarget.values.map((t) => t.fileName).toSet();
    expect(names, hasLength(3));
  });

  test('development builds count as older than any release', () async {
    final host = platform(DesktopTarget.linux, version: '');
    expect((await host.installed()).code, 0);
    expect((await platform(DesktopTarget.linux).installed()).code, 26007000);
  });

  test('an AppImage replaces itself and opens again after quitting', () async {
    final image = File('${directory.path}/Otakase.AppImage');
    await image.writeAsBytes(elf.reversed.toList());
    final host = platform(
      DesktopTarget.linux,
      environment: {'APPIMAGE': image.path},
    );
    final next = [...elf, ...List.filled(500, 1)];
    final app = updates(host, next);
    addTearDown(app.dispose);

    await app.check();
    expect(app.phase, UpdatePhase.available);
    expect(app.update!.label, '26.8.0 (build 26008000)');
    await app.download();
    expect(app.phase, UpdatePhase.ready, reason: app.error);
    await app.install();

    expect(await image.readAsBytes(), next);
    expect((await image.stat()).modeString(), 'rwxr-xr-x');
    expect(started, [
      [
        '/bin/sh',
        '-c',
        'while kill -0 "\$1" 2>/dev/null; do sleep 0.2; done; shift; exec "\$@"',
        'otakase-relaunch',
        '4242',
        image.path,
      ],
    ]);
    expect(quits, 1);
    expect(directory.listSync().map((e) => e.uri.pathSegments.last), [
      'Otakase.AppImage',
    ]);
  }, testOn: 'linux || mac-os');

  test('a download that is not an AppImage never replaces it', () async {
    final image = File('${directory.path}/Otakase.AppImage');
    await image.writeAsBytes(elf);
    final host = platform(
      DesktopTarget.linux,
      environment: {'APPIMAGE': image.path},
    );
    final app = updates(host, mz);
    addTearDown(app.dispose);
    await app.check();
    await app.download();
    expect(app.phase, UpdatePhase.failed);
    expect(app.error, 'The download is not an AppImage.');
    expect(app.downloaded, isFalse);
    expect(await image.readAsBytes(), elf);
    expect(started, isEmpty);
    expect(quits, 0);
    expect(directory.listSync(), hasLength(1));
  });

  test('a Linux package install links to the download page', () async {
    final app = updates(platform(DesktopTarget.linux), elf);
    addTearDown(app.dispose);
    await app.check();
    expect(app.phase, UpdatePhase.manual);
    expect(app.manualReason, contains('package'));
    await app.download();
    expect(app.phase, UpdatePhase.manual);
    expect(started, isEmpty);
  });

  test('Windows runs the installer silently once the app has quit', () async {
    await File('${directory.path}/unins000.exe').create();
    final host = platform(
      DesktopTarget.windows,
      executable: '${directory.path}${Platform.pathSeparator}otakase.exe',
    );
    final app = updates(host, mz);
    addTearDown(app.dispose);
    await app.check();
    expect(app.phase, UpdatePhase.available);
    await app.download();
    expect(app.phase, UpdatePhase.ready, reason: app.error);
    await app.install();
    final setup =
        '${directory.path}${Platform.pathSeparator}otakase-app-update-setup.exe';
    expect(started, [
      [
        setup,
        '/VERYSILENT',
        '/SUPPRESSMSGBOXES',
        '/NORESTART',
        '/WAITPID=4242',
      ],
    ]);
    expect(await File(setup).readAsBytes(), mz);
    expect(quits, 1);
  });

  test('a portable Windows copy is not overwritten by the installer', () async {
    final host = platform(
      DesktopTarget.windows,
      executable: '${directory.path}${Platform.pathSeparator}otakase.exe',
    );
    final app = updates(host, mz);
    addTearDown(app.dispose);
    await app.check();
    expect(app.phase, UpdatePhase.manual);
    expect(app.manualReason, contains('installer'));
  });

  test(
    'a macOS copy outside an app bundle links to the download page',
    () async {
      final host = platform(DesktopTarget.macos);
      expect(await host.manualReason(), isNotNull);
      final bundled = platform(
        DesktopTarget.macos,
        executable: '${directory.path}/Otakase.app/Contents/MacOS/Otakase',
      );
      expect(bundled.bundle.path, '${directory.path}/Otakase.app');
      expect(await bundled.manualReason(), isNull);
    },
    testOn: 'linux || mac-os',
  );

  testWidgets('copies that cannot update themselves show the download page', (
    tester,
  ) async {
    final app = updates(platform(DesktopTarget.linux), elf);
    addTearDown(app.dispose);
    await tester.runAsync(app.check);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AppUpdatePanel(updates: app, isTV: false, autoCheck: false),
        ),
      ),
    );
    expect(find.textContaining('installed by a package'), findsOneWidget);
    expect(find.byKey(const Key('open-update-page')), findsOneWidget);
    expect(find.byKey(const Key('install-update')), findsNothing);
    expect(find.textContaining('26.8.0 (build 26008000)'), findsOneWidget);
  });

  testWidgets('a desktop update restarts the app instead of an installer', (
    tester,
  ) async {
    final image = File('${directory.path}/Otakase.AppImage');
    await tester.runAsync(() => image.writeAsBytes(elf));
    final app = updates(
      platform(DesktopTarget.linux, environment: {'APPIMAGE': image.path}),
      elf,
    );
    addTearDown(app.dispose);
    await tester.runAsync(() async {
      await app.check();
      await app.download();
    });
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AppUpdatePanel(updates: app, isTV: false, autoCheck: false),
        ),
      ),
    );
    expect(find.text('Update verified. Otakase restarts to finish.'), findsOne);
    expect(find.text('Restart and update'), findsOneWidget);
    expect(find.textContaining('Android'), findsNothing);
  }, skip: Platform.isWindows);
}
