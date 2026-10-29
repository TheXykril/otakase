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
const base = 'https://github.com/TheXykril/otakase/releases/download/v26.7.0';

Map<String, dynamic> manifest({int code = 1025}) => {
  'schema': 1,
  'package_name': androidPackage,
  'version_name': '0.1.0',
  'version_code': code,
  'apk': androidApk,
  'size': apkBytes.length,
  'sha256': checksum,
};
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
  bool allowed = true, reject = false;
  @override
  Future<InstalledApp> installed() async => InstalledApp('0.1.0', code);
  @override
  Future<File> createDownload() => File(
    '${directory.path}/download-${DateTime.now().microsecondsSinceEpoch}.apk',
  ).create();
  @override
  Future<void> validate(File file, AndroidUpdate update) async {
    if (reject) {
      throw PlatformException(
        code: 'invalid_update',
        message: 'Signing key does not match.',
      );
    }
    validated++;
  }

  @override
  Future<bool> install(File file, AndroidUpdate update) async {
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
  late Map<String, dynamic> release, metadata;
  late List<int> download;
  var status = 200;

  setUp(() async {
    directory = await Directory.systemTemp.createTemp('otakase-updates-');
    host = UpdateHost(directory);
    metadata = manifest();
    download = apkBytes;
    status = 200;
    release = {
      'tag_name': 'v26.7.0',
      'draft': false,
      'prerelease': false,
      'assets': [asset(androidManifest), asset(androidApk)],
    };
    updates = AppUpdates(
      platform: host,
      client: () => MockClient((request) async {
        if (request.url.host == 'api.github.com') {
          return http.Response(jsonEncode(release), status);
        }
        if (request.url.path.endsWith('.json')) {
          return http.Response(jsonEncode(metadata), 200);
        }
        return http.Response.bytes(download, 200);
      }),
    );
  });
  tearDown(() async {
    updates.dispose();
    await directory.delete(recursive: true);
  });

  test(
    'uses the Android code, ignoring the unrelated CLI tag and equal app names',
    () async {
      await updates.check();
      expect(updates.phase, UpdatePhase.available);
      expect(updates.installed!.code, 3);
      expect(updates.update!.code, 1025);
      for (final installed in [1025, 1026]) {
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
        [asset(androidApk)],
        [asset(androidManifest)],
      ]) {
        release['assets'] = assets;
        await updates.check();
        expect(updates.phase, UpdatePhase.unpublished);
        expect(updates.available, isFalse);
      }
      release['assets'] = [asset(androidManifest), asset(androidApk)];
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

  test('rejects invalid metadata, other repositories, mismatched assets and release tags', () async {
    for (final entry in <String, dynamic>{
      'schema': 2,
      'package_name': 'other.app',
      'version_code': -1,
      'size': apkBytes.length + 1,
      'sha256': '0' * 64,
    }.entries) {
      metadata = manifest()..[entry.key] = entry.value;
      await updates.check();
      expect(updates.phase, UpdatePhase.failed, reason: entry.key);
    }
    for (final url in [
      'http://github.com/TheXykril/otakase/releases/download/v26.7.0/$androidApk',
      'https://github.com/other/otakase/releases/download/v26.7.0/$androidApk',
      'https://evil.example/TheXykril/otakase/releases/download/v26.7.0/$androidApk',
      'https://github.com/TheXykril/otakase/releases/download/v26.8.0/$androidApk',
    ]) {
      metadata = manifest();
      release['assets'] = [
        asset(androidManifest),
        asset(androidApk)..['browser_download_url'] = url,
      ];
      await updates.check();
      expect(updates.phase, UpdatePhase.failed, reason: url);
    }
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
        (request) async => http.Response(
          jsonEncode(request.url.host == 'api.github.com' ? release : metadata),
          200,
        ),
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
    final candidate = AndroidUpdate.parse(manifest(), asset(androidApk));
    await platform.validate(file, candidate);
    expect(await platform.install(file, candidate), isFalse);
    expect(await platform.channelAllowed(), isTrue);
    expect(calls.where((call) => call.method == 'validate').single.arguments, {
      'path': file.path,
      'code': 1025,
      'sha256': checksum,
    });
  });
}
