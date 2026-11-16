import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/data/app_updates.dart';

final apkBytes = List<int>.generate(1024, (i) => i % 256);
final checksum = sha256.convert(apkBytes).toString();
const base = 'https://otakase.com/download/v26.7.0';

Map<String, dynamic> asset(String name) => {
  'name': name,
  'size': apkBytes.length,
  'digest': 'sha256:$checksum',
  'browser_download_url': '$base/$name',
};

class UpdateHost extends AndroidUpdatePlatform {
  UpdateHost(this.directory);
  final Directory directory;
  int validated = 0, installs = 0, code = 3;
  String name = '0.1.0';
  bool allowed = true, reject = false;
  @override
  Future<InstalledApp> installed() async => InstalledApp(name, code);
  @override
  Future<File> createDownload() => File(
    '${directory.path}/download-${DateTime.now().microsecondsSinceEpoch}.apk',
  ).create();
  @override
  Future<void> validate(File file, AppUpdate update) async {
    if (reject) {
      throw PlatformException(
        code: 'invalid_update',
        message: 'Signing key does not match.',
      );
    }
    validated++;
  }

  @override
  Future<bool> install(File file, AppUpdate update) async {
    installs++;
    return allowed;
  }

  @override
  Future<bool> channelAllowed() async => allowed;
}

class StreamClient extends http.BaseClient {
  StreamClient(this.stream);
  final Stream<List<int>> stream;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async =>
      http.StreamedResponse(stream, 200);
}

class PendingFile implements File {
  PendingFile(this.delegate);
  final File delegate;
  final writing = Completer<void>(), finish = Completer<void>();
  @override
  String get path => delegate.path;
  @override
  Future<bool> exists() => delegate.exists();
  @override
  Future<FileSystemEntity> delete({bool recursive = false}) =>
      delegate.delete(recursive: recursive);
  @override
  Future<RandomAccessFile> open({FileMode mode = FileMode.read}) async =>
      PendingWriter(await delegate.open(mode: mode), writing, finish);
  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class PendingWriter implements RandomAccessFile {
  PendingWriter(this.delegate, this.writing, this.finish);
  final RandomAccessFile delegate;
  final Completer<void> writing, finish;
  @override
  Future<RandomAccessFile> writeFrom(
    List<int> buffer, [
    int start = 0,
    int? end,
  ]) async {
    writing.complete();
    await finish.future;
    return delegate.writeFrom(buffer, start, end);
  }

  @override
  Future<void> close() => delegate.close();
  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class SlowUpdateHost extends UpdateHost {
  SlowUpdateHost(super.directory, this.file);
  final PendingFile file;
  @override
  Future<File> createDownload() async => file;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late Directory directory;
  late UpdateHost host;
  late AppUpdates updates;
  late Map<String, dynamic> release;
  late List<int> download;
  var status = 200;

  setUp(() async {
    directory = await Directory.systemTemp.createTemp('otakase-updates-');
    host = UpdateHost(directory);
    download = apkBytes;
    status = 200;
    release = {
      'tag_name': 'v26.7.0',
      'draft': false,
      'prerelease': false,
      'assets': [asset(androidApk)],
    };
    updates = AppUpdates(
      platform: host,
      client: () => MockClient((request) async {
        if (request.url.path == '/download/latest.json') {
          return http.Response(jsonEncode(release), status);
        }
        expect(request.url.pathSegments.last, androidApk);
        return http.Response.bytes(download, 200);
      }),
    );
  });
  tearDown(() async {
    updates.dispose();
    await directory.delete(recursive: true);
  });

  test('checks and downloads only through the Otakase site', () async {
    final hosts = <String>{};
    final app = AppUpdates(
      platform: host,
      client: () => MockClient((request) async {
        hosts.add(request.url.host);
        return request.url.path == '/download/latest.json'
            ? http.Response(jsonEncode(release), 200)
            : http.Response.bytes(download, 200);
      }),
    );
    addTearDown(app.dispose);
    await app.check();
    expect(app.update!.url.toString(), '$base/$androidApk');
    await app.download();
    expect(app.phase, UpdatePhase.ready);
    expect(hosts, {'otakase.com'});
  });

  test(
    'reads the version from the release tag and offers only newer builds',
    () async {
      await updates.check();
      expect(updates.phase, UpdatePhase.available);
      expect(updates.installed!.code, 3);
      expect(updates.update!.code, 26007000);
      expect(updates.update!.name, '26.7.0');
      expect(updates.update!.checksum, checksum);
      for (final installed in [26007000, 26007001]) {
        host.code = installed;
        await updates.check();
        expect(updates.phase, UpdatePhase.current);
        expect(updates.available, isFalse);
      }
    },
  );

  test(
    'old releases, missing APKs, drafts and prereleases are not install offers',
    () async {
      for (final assets in [
        <Map>[],
        [asset('otakase-app-macos.zip')],
        [asset(androidApk), asset(androidApk)],
      ]) {
        release['assets'] = assets;
        await updates.check();
        expect(updates.phase, UpdatePhase.unpublished);
        expect(updates.available, isFalse);
      }
      release['assets'] = [asset(androidApk)];
      for (final field in ['draft', 'prerelease']) {
        release[field] = true;
        await updates.check();
        expect(updates.phase, UpdatePhase.unpublished);
        release[field] = false;
      }
      status = 404;
      await updates.check();
      expect(updates.phase, UpdatePhase.unpublished);
      status = 403;
      await updates.check();
      expect(updates.phase, UpdatePhase.failed);
    },
  );

  test('rejects invalid tags and file data, other hosts and mismatched assets', () async {
    for (final tag in [
      '26.7.0',
      'v26',
      'v26.7.0.1',
      'v1000.1.0',
      'dev',
      null,
    ]) {
      release['tag_name'] = tag;
      await updates.check();
      expect(updates.phase, UpdatePhase.failed, reason: '$tag');
    }
    release['tag_name'] = 'v26.7.0';
    for (final size in [0, -1, '1024', 600 * 1024 * 1024]) {
      release['assets'] = [asset(androidApk)..['size'] = size];
      await updates.check();
      expect(updates.phase, UpdatePhase.failed, reason: '$size');
    }
    for (final digest in [null, 'sha1:$checksum', 'sha256:${'g' * 64}']) {
      release['assets'] = [asset(androidApk)..['digest'] = digest];
      await updates.check();
      expect(updates.phase, UpdatePhase.failed, reason: '$digest');
    }
    // A checksum that does not match the download never gets installed.
    release['assets'] = [asset(androidApk)..['digest'] = 'sha256:${'0' * 64}'];
    await updates.check();
    await updates.download();
    expect(updates.phase, UpdatePhase.failed);
    expect(updates.downloaded, isFalse);
    expect(host.validated, 0);
    for (final url in [
      'http://otakase.com/download/v26.7.0/$androidApk',
      'https://evil.example/download/v26.7.0/$androidApk',
      'https://otakase.com.evil.example/download/v26.7.0/$androidApk',
      'https://otakase.com/download/v26.8.0/$androidApk',
      'https://otakase.com/download/v26.7.0/otakase-app-macos.zip',
      'https://otakase.com/download/v26.7.0/$androidApk?x=1',
      'https://github.com/TheXykril/otakase/releases/download/v26.7.0/$androidApk',
    ]) {
      release['assets'] = [asset(androidApk)..['browser_download_url'] = url];
      await updates.check();
      expect(updates.phase, UpdatePhase.failed, reason: url);
    }
  });

  group('when otakase.com cannot be reached', () {
    const fallbackBase = 'https://otakase.xyverion.com/download/v26.7.0';
    Map<String, dynamic> fallbackRelease() => {
      ...release,
      'assets': [
        asset(androidApk)
          ..['browser_download_url'] = '$fallbackBase/$androidApk',
      ],
    };

    AppUpdates withSites({
      required Object? Function() primary,
      required Object? Function() fallback,
      List<Uri>? requests,
    }) {
      final app = AppUpdates(
        platform: host,
        client: () => MockClient((request) async {
          requests?.add(request.url);
          if (request.url.path != '/download/latest.json') {
            return http.Response.bytes(download, 200);
          }
          final answer = request.url.host == updateSite
              ? primary()
              : fallback();
          if (answer is Exception) throw answer;
          return answer as http.Response;
        }),
      );
      addTearDown(app.dispose);
      return app;
    }

    for (final error in [
      const SocketException("Failed host lookup: 'otakase.com'"),
      http.ClientException("Failed host lookup: 'otakase.com'"),
      const HandshakeException('CERTIFICATE_VERIFY_FAILED'),
      TimeoutException('no answer'),
    ]) {
      test('falls back to the old site after ${error.runtimeType}', () async {
        final requests = <Uri>[];
        final app = withSites(
          primary: () => error,
          fallback: () => http.Response(jsonEncode(fallbackRelease()), 200),
          requests: requests,
        );
        await app.check();
        expect(app.phase, UpdatePhase.available);
        expect(app.update!.url.toString(), '$fallbackBase/$androidApk');
        await app.download();
        expect(app.phase, UpdatePhase.ready);
        expect(requests.map((u) => u.toString()), [
          'https://otakase.com/download/latest.json',
          'https://otakase.xyverion.com/download/latest.json',
          '$fallbackBase/$androidApk',
        ]);
      });
    }

    test('an answer from otakase.com is used, even an error', () async {
      final requests = <Uri>[];
      final app = withSites(
        primary: () => http.Response('busy', 403),
        fallback: () => fail('fallback asked'),
        requests: requests,
      );
      await app.check();
      expect(app.phase, UpdatePhase.failed);
      expect(app.error, contains('busy'));
      expect(requests.single.host, updateSite);
    });

    test('links must stay on the site that answered', () async {
      final fromFallback = withSites(
        primary: () => const SocketException('Failed host lookup'),
        fallback: () => http.Response(jsonEncode(release), 200),
      );
      await fromFallback.check();
      expect(fromFallback.phase, UpdatePhase.failed);
      expect(fromFallback.error, 'Invalid release address.');
      final fromPrimary = withSites(
        primary: () => http.Response(jsonEncode(fallbackRelease()), 200),
        fallback: () => fail('fallback asked'),
      );
      await fromPrimary.check();
      expect(fromPrimary.phase, UpdatePhase.failed);
      expect(fromPrimary.error, 'Invalid release address.');
    });

    test('says why when no site answers', () async {
      for (final (error, text) in [
        (
          const SocketException("Failed host lookup: 'otakase.com'"),
          'Could not find the update site',
        ),
        (
          const HandshakeException('CERTIFICATE_VERIFY_FAILED'),
          'secure connection',
        ),
        (TimeoutException('no answer'), 'did not answer in time'),
        (
          const SocketException('Connection refused'),
          'Unable to check for updates',
        ),
      ]) {
        final app = withSites(primary: () => error, fallback: () => error);
        await app.check();
        expect(app.phase, UpdatePhase.failed);
        expect(app.error, contains(text), reason: '$error');
      }
    });
  });

  test('downloads inside the app, verifies and opens the native installer only on request', () async {
    await updates.check();
    await updates.download();
    expect(updates.phase, UpdatePhase.ready);
    expect(updates.progress, 1);
    expect(host.validated, 1);
    expect(host.installs, 0);
    expect(await directory.list().length, 1);
    await updates.install();
    expect(host.installs, 1);
    expect(updates.phase, UpdatePhase.installer);
    await updates.resume();
    expect(updates.phase, UpdatePhase.ready);
    expect(host.installs, 1);
  });

  test(
    'permission denial keeps the verified package and resumes after approval',
    () async {
      await updates.check();
      await updates.download();
      host.allowed = false;
      await updates.install();
      expect(updates.phase, UpdatePhase.permission);
      await updates.resume();
      expect(host.installs, 1);
      host.allowed = true;
      await updates.resume();
      expect(host.installs, 2);
      expect(updates.phase, UpdatePhase.installer);
    },
  );

  test('corrupt, truncated, oversized and differently signed packages never install', () async {
    await updates.check();
    for (final bytes in [
      List<int>.filled(apkBytes.length, 0),
      apkBytes.sublist(1),
      [...apkBytes, 0],
    ]) {
      download = bytes;
      await updates.download();
      expect(updates.phase, UpdatePhase.failed);
      expect(updates.downloaded, isFalse);
      expect(await directory.list().length, 0);
    }
    download = apkBytes;
    host.reject = true;
    await updates.download();
    expect(updates.phase, UpdatePhase.failed);
    expect(updates.error, contains('Signing key'));
    expect(await directory.list().length, 0);
    expect(host.installs, 0);
    expect(host.validated, 0);
  });

  test(
    'cancelled downloads delete partial files and ignore late bytes',
    () async {
      final stream = StreamController<List<int>>();
      final file = PendingFile(
        await File('${directory.path}/slow.apk').create(),
      );
      final slowHost = SlowUpdateHost(directory, file);
      final first = MockClient(
        (request) async => http.Response(jsonEncode(release), 200),
      );
      var calls = 0;
      final cancellable = AppUpdates(
        platform: slowHost,
        client: () => calls++ == 0 ? first : StreamClient(stream.stream),
      );
      await cancellable.check();
      final running = cancellable.download();
      await Future.doWhile(() async {
        await Future<void>.delayed(Duration.zero);
        return !stream.hasListener;
      }).timeout(const Duration(seconds: 5));
      stream.add(apkBytes.sublist(0, 16));
      await file.writing.future.timeout(const Duration(seconds: 5));
      cancellable.cancel();
      file.finish.complete();
      stream.add(apkBytes.sublist(16));
      await stream.close();
      await running;
      expect(cancellable.phase, UpdatePhase.available);
      expect(cancellable.downloaded, isFalse);
      expect(slowHost.validated, 0);
      expect(await directory.list().length, 0);
      cancellable.dispose();
    },
  );

  test('Android channel receives only the staged path, checksum and expected version', () async {
    final calls = <MethodCall>[];
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(AndroidUpdatePlatform.channel, (call) async {
          calls.add(call);
          return switch (call.method) {
            'installed' => {'name': '0.1.0', 'code': 3},
            'createDownload' => '${directory.path}/update.apk',
            'installationAllowed' => true,
            'install' => false,
            _ => null,
          };
        });
    addTearDown(
      () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(AndroidUpdatePlatform.channel, null),
    );
    final platform = AndroidUpdatePlatform();
    expect((await platform.installed()).code, 3);
    final file = await platform.createDownload();
    final candidate = AppUpdate.parse(release, asset(androidApk), platform);
    await platform.validate(file, candidate);
    expect(await platform.install(file, candidate), isFalse);
    expect(await platform.channelAllowed(), isTrue);
    expect(calls.where((call) => call.method == 'validate').single.arguments, {
      'path': file.path,
      'code': 26007000,
      'name': '26.7.0',
      'sha256': checksum,
    });
  });

  test('orders dev builds before their release and by build number', () {
    AppVersion v(String name) => AppVersion.tryParse(name)!;
    expect(v('26.8.0-dev.41').compareTo(v('26.8.0-dev.42')), lessThan(0));
    expect(v('26.8.0-dev.100').compareTo(v('26.8.0-dev.99')), greaterThan(0));
    expect(v('26.8.0-dev.99').compareTo(v('26.8.0')), lessThan(0));
    expect(v('26.7.3').compareTo(v('26.8.0-dev.1')), lessThan(0));
    expect(v('26.8.0').compareTo(v('26.8.0')), 0);
    for (final bad in ['dev', '26.8.0-dev', '26.8.0-beta.1', '26.8.0-dev.x']) {
      expect(AppVersion.tryParse(bad), isNull, reason: bad);
    }
    AppUpdate offer(String name) => AppUpdate(
      name: name,
      code: versionCode(name),
      url: Uri.https('otakase.com'),
      size: 1,
      checksum: checksum,
    );
    InstalledApp app(String name) => InstalledApp(name, versionCode(name));
    expect(isNewer(offer('26.8.0-dev.42'), app('26.8.0-dev.41')), isTrue);
    expect(isNewer(offer('26.8.0-dev.41'), app('26.8.0-dev.41')), isFalse);
    expect(isNewer(offer('26.8.0'), app('26.8.0-dev.41')), isTrue);
    expect(isNewer(offer('26.8.0-dev.50'), app('26.8.0')), isFalse);
    expect(isNewer(offer('26.7.2'), app('26.8.0-dev.41')), isFalse);
    expect(isDevBuild(app('26.8.0-dev.41')), isTrue);
    expect(isDevBuild(app('26.8.0')), isFalse);
  });

  group('with Dev builds on', () {
    const devBase = 'https://otakase.com/download/dev';
    late Map<String, dynamic> dev;
    late bool devOn;
    var devStatus = 200;
    late List<String> asked;
    late AppUpdates app;
    Map<String, dynamic> freshDev() => {
      'tag_name': 'dev',
      'version': '26.8.0-dev.41',
      'draft': false,
      'prerelease': true,
      'assets': [
        asset(androidApk)..['browser_download_url'] = '$devBase/$androidApk',
      ],
    };

    setUp(() {
      devOn = true;
      devStatus = 200;
      asked = [];
      dev = freshDev();
      app = AppUpdates(
        platform: host,
        devBuilds: () => devOn,
        client: () => MockClient((request) async {
          asked.add(request.url.path);
          return switch (request.url.path) {
            '/download/latest.json' => http.Response(jsonEncode(release), 200),
            '/download/dev.json' => http.Response(jsonEncode(dev), devStatus),
            _ => http.Response.bytes(download, 200),
          };
        }),
      );
    });
    tearDown(() => app.dispose());

    test('offers the dev build when it is newer than the release', () async {
      await app.check();
      expect(app.phase, UpdatePhase.available);
      expect(app.update!.name, '26.8.0-dev.41');
      expect(app.update!.isDev, isTrue);
      expect(app.update!.url.toString(), '$devBase/$androidApk');
      await app.download();
      expect(app.phase, UpdatePhase.ready);
      expect(asked.last, '/download/dev/$androidApk');
    });

    test('moves on to later dev builds and then the release', () async {
      host
        ..name = '26.8.0-dev.41'
        ..code = 26008000;
      await app.check();
      expect(app.phase, UpdatePhase.current);
      dev['version'] = '26.8.0-dev.42';
      await app.check();
      expect(app.phase, UpdatePhase.available);
      expect(app.update!.name, '26.8.0-dev.42');
      // The release of the version a dev build was heading to replaces it.
      release['tag_name'] = 'v26.8.0';
      release['assets'] = [
        asset(androidApk)
          ..['browser_download_url'] =
              'https://otakase.com/download/v26.8.0/$androidApk',
      ];
      await app.check();
      expect(app.update!.name, '26.8.0');
      expect(app.phase, UpdatePhase.available);
    });

    test('a release newer than the dev build wins', () async {
      dev['version'] = '26.6.0-dev.30';
      await app.check();
      expect(app.update!.name, '26.7.0');
    });

    test('turned off, only releases are asked for', () async {
      devOn = false;
      host
        ..name = '26.8.0-dev.41'
        ..code = 26008000;
      await app.check();
      expect(asked, ['/download/latest.json']);
      expect(app.phase, UpdatePhase.current);
      expect(isDevBuild(app.installed), isTrue);
    });

    test('a missing or broken dev build never hides the release', () async {
      for (final change in <void Function()>[
        () => devStatus = 404,
        () => devStatus = 502,
        () => dev['version'] = '26.8.0',
        () => dev['prerelease'] = false,
        () => dev['tag_name'] = 'v26.8.0',
        () => dev['assets'] = [asset(androidApk)],
      ]) {
        dev = freshDev();
        devStatus = 200;
        change();
        await app.check();
        expect(app.phase, UpdatePhase.available);
        expect(app.update!.name, '26.7.0');
      }
    });

    test('a dev build in the release feed is refused', () async {
      release = {...dev, 'prerelease': false};
      await app.check();
      expect(app.phase, UpdatePhase.failed);
    });

    test('recheck drops a downloaded dev build', () async {
      await app.check();
      await app.download();
      expect(app.downloaded, isTrue);
      devOn = false;
      await app.recheck();
      expect(app.downloaded, isFalse);
      expect(app.update!.name, '26.7.0');
    });
  });
}
