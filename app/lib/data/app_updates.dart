import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:http/http.dart' as http;

const androidPackage = 'com.xyverion.otakase';
const androidApk = 'otakase-android.apk';
const androidManifest = 'otakase-android.json';

enum UpdatePhase {
  idle,
  checking,
  current,
  unpublished,
  available,
  downloading,
  ready,
  permission,
  installer,
  failed,
}

class InstalledApp {
  const InstalledApp(this.name, this.code);
  final String name;
  final int code;
  String get label => '$name (build $code)';
}

class AndroidUpdate {
  const AndroidUpdate({
    required this.name,
    required this.code,
    required this.url,
    required this.size,
    required this.checksum,
  });
  final String name, checksum;
  final int code, size;
  final Uri url;
  String get label => '$name (build $code)';

  factory AndroidUpdate.parse(Map<String, dynamic> data, Map asset) {
    final name = data['version_name'];
    final code = data['version_code'];
    final size = data['size'];
    final checksum = data['sha256'];
    if (data['schema'] != 1 ||
        data['package_name'] != androidPackage ||
        data['apk'] != androidApk ||
        name is! String ||
        name.isEmpty ||
        name.length > 80 ||
        code is! int ||
        code < 1 ||
        code > 2100000000 ||
        size is! int ||
        size < 1 ||
        size > 512 * 1024 * 1024 ||
        size != asset['size'] ||
        checksum is! String ||
        !RegExp(r'^[a-f0-9]{64}$').hasMatch(checksum) ||
        (asset['digest'] != null && asset['digest'] != 'sha256:$checksum')) {
      throw const FormatException('Invalid Android update metadata.');
    }
    return AndroidUpdate(
      name: name,
      code: code,
      url: releaseAssetUri(asset['browser_download_url'], androidApk),
      size: size,
      checksum: checksum,
    );
  }
}

Uri releaseAssetUri(dynamic value, String filename) {
  final uri = value is String ? Uri.tryParse(value) : null;
  final parts = uri?.pathSegments ?? const <String>[];
  if (uri == null ||
      uri.scheme != 'https' ||
      uri.host != 'github.com' ||
      uri.port != 443 ||
      uri.userInfo.isNotEmpty ||
      uri.hasQuery ||
      uri.hasFragment ||
      parts.length != 6 ||
      parts[0].toLowerCase() != 'thexykril' ||
      parts[1].toLowerCase() != 'otakase' ||
      parts[2] != 'releases' ||
      parts[3] != 'download' ||
      parts[4].isEmpty ||
      parts[5] != filename) {
    throw const FormatException('Invalid Android release address.');
  }
  return uri;
}

class AndroidUpdatePlatform {
  static const channel = MethodChannel('otakase/updates');

  Future<InstalledApp> installed() async {
    final data = await channel.invokeMapMethod<String, dynamic>('installed');
    return InstalledApp(data!['name'] as String, data['code'] as int);
  }

  Future<File> createDownload() async =>
      File((await channel.invokeMethod<String>('createDownload'))!);

  Future<bool> channelAllowed() async =>
      await channel.invokeMethod<bool>('installationAllowed') ?? false;

  Future<void> validate(File file, AndroidUpdate update) =>
      channel.invokeMethod<void>('validate', {
        'path': file.path,
        'code': update.code,
        'sha256': update.checksum,
      });

  Future<bool> install(File file, AndroidUpdate update) async =>
      await channel.invokeMethod<bool>('install', {
        'path': file.path,
        'code': update.code,
        'sha256': update.checksum,
      }) ??
      false;
}

class AppUpdates extends ChangeNotifier {
  AppUpdates({AndroidUpdatePlatform? platform, http.Client Function()? client})
    : platform = platform ?? AndroidUpdatePlatform(),
      _newClient = client ?? http.Client.new;

  final AndroidUpdatePlatform platform;
  final http.Client Function() _newClient;
  UpdatePhase phase = UpdatePhase.idle;
  InstalledApp? installed;
  AndroidUpdate? update;
  String? error;
  int received = 0;
  File? _download;
  http.Client? _client;
  int _generation = 0;
  bool _closed = false;
  bool _installPending = false;
  bool get busy =>
      phase == UpdatePhase.checking ||
      phase == UpdatePhase.downloading ||
      _installPending;
  bool get available => update != null && update!.code > (installed?.code ?? 0);
  bool get downloaded => _download != null;
  double? get progress => update == null ? null : received / update!.size;

  void _emit(UpdatePhase value) {
    if (_closed) return;
    phase = value;
    notifyListeners();
  }

  bool _active(int generation) => !_closed && generation == _generation;

  Future<void> loadInstalled() async {
    try {
      final value = await platform.installed();
      if (!_closed) {
        installed = value;
        notifyListeners();
      }
    } on MissingPluginException {
      // Version information comes from the Android host.
    } on PlatformException {
      // A manual check will offer a retry.
    }
  }

  Future<http.StreamedResponse> _get(http.Client client, Uri uri) async {
    final request = http.Request('GET', uri)
      ..headers['Accept'] = uri.host == 'api.github.com'
          ? 'application/vnd.github+json'
          : 'application/octet-stream'
      ..headers['User-Agent'] = 'Otakase-Android';
    final response = await client
        .send(request)
        .timeout(const Duration(seconds: 30));
    if (response.statusCode != 200 && response.statusCode != 404) {
      throw HttpException(
        response.statusCode == 403 || response.statusCode == 429
            ? 'Update checks are busy. Try again later.'
            : 'Update request failed. Try again later.',
      );
    }
    return response;
  }

  Future<Map<String, dynamic>> _json(
    http.StreamedResponse response,
    int limit,
  ) async {
    final bytes = <int>[];
    await for (final chunk in response.stream.timeout(
      const Duration(seconds: 30),
    )) {
      if (bytes.length + chunk.length > limit) {
        throw const FormatException('Update metadata is too large.');
      }
      bytes.addAll(chunk);
    }
    return jsonDecode(utf8.decode(bytes)) as Map<String, dynamic>;
  }

  Future<void> check() async {
    if (busy || _closed || downloaded) return;
    final generation = ++_generation;
    final client = _client = _newClient();
    error = null;
    update = null;
    _emit(UpdatePhase.checking);
    try {
      installed = await platform.installed();
      if (!_active(generation)) return;
      final response = await _get(
        client,
        Uri.https('api.github.com', '/repos/TheXykril/otakase/releases/latest'),
      );
      if (!_active(generation)) return;
      if (response.statusCode == 404) {
        _emit(UpdatePhase.unpublished);
        return;
      }
      final release = await _json(response, 2 * 1024 * 1024);
      if (!_active(generation)) return;
      if (release['draft'] == true || release['prerelease'] == true) {
        _emit(UpdatePhase.unpublished);
        return;
      }
      final assets = (release['assets'] as List? ?? []).whereType<Map>();
      final manifests = assets.where((a) => a['name'] == androidManifest);
      final apks = assets.where((a) => a['name'] == androidApk);
      if (manifests.length != 1 || apks.length != 1) {
        _emit(UpdatePhase.unpublished);
        return;
      }
      final uri = releaseAssetUri(
        manifests.single['browser_download_url'],
        androidManifest,
      );
      final manifest = await _get(client, uri);
      if (manifest.statusCode != 200) {
        throw const FormatException('Android update metadata is unavailable.');
      }
      final data = await _json(manifest, 16 * 1024);
      if (!_active(generation)) return;
      final candidate = AndroidUpdate.parse(data, apks.single);
      if (candidate.url.pathSegments[4] != uri.pathSegments[4]) {
        throw const FormatException('Android release files do not match.');
      }
      update = candidate;
      _emit(available ? UpdatePhase.available : UpdatePhase.current);
    } catch (e) {
      if (_active(generation)) {
        error = switch (e) {
          MissingPluginException() =>
            'Updates are available in the Android app.',
          HttpException() => e.message,
          FormatException() => e.message,
          _ => 'Unable to check for updates. Check your connection and retry.',
        };
        _emit(UpdatePhase.failed);
      }
    } finally {
      client.close();
      if (identical(_client, client)) _client = null;
    }
  }

  Future<void> download() async {
    if (busy || _closed || !available || downloaded) return;
    final candidate = update!;
    final generation = ++_generation;
    final client = _client = _newClient();
    File? file;
    RandomAccessFile? sink;
    error = null;
    received = 0;
    _emit(UpdatePhase.downloading);
    try {
      file = await platform.createDownload();
      if (!_active(generation)) return;
      final response = await _get(client, candidate.url);
      if (response.statusCode != 200 ||
          (response.contentLength != null &&
              response.contentLength != candidate.size)) {
        throw const FormatException('Android download size does not match.');
      }
      sink = await file.open(mode: FileMode.write);
      await for (final chunk in response.stream.timeout(
        const Duration(seconds: 30),
      )) {
        if (!_active(generation)) return;
        received += chunk.length;
        if (received > candidate.size) {
          throw const FormatException('Android download is too large.');
        }
        await sink.writeFrom(chunk);
        if (!_active(generation)) return;
        _emit(UpdatePhase.downloading);
      }
      await sink.flush();
      await sink.close();
      sink = null;
      if (!_active(generation)) return;
      final checksum = await sha256.bind(file.openRead()).first;
      if (received != candidate.size ||
          checksum.toString() != candidate.checksum) {
        throw const FormatException('Android download verification failed.');
      }
      await platform.validate(file, candidate);
      if (!_active(generation)) return;
      _download = file;
      file = null;
      _emit(UpdatePhase.ready);
    } catch (e) {
      if (_active(generation)) {
        error = e is PlatformException
            ? e.message
            : 'Download failed verification or was interrupted. Please retry.';
        _emit(UpdatePhase.failed);
      }
    } finally {
      await sink?.close();
      if (file != null && await file.exists()) await file.delete();
      client.close();
      if (identical(_client, client)) _client = null;
    }
  }

  Future<void> install() async {
    if (_closed || busy || _download == null || update == null) return;
    final generation = ++_generation;
    error = null;
    _installPending = true;
    _emit(UpdatePhase.installer);
    try {
      final opened = await platform.install(_download!, update!);
      if (_active(generation)) {
        _emit(opened ? UpdatePhase.installer : UpdatePhase.permission);
      }
    } on PlatformException catch (e) {
      if (_active(generation)) {
        error = e.message ?? 'Unable to open the Android installer.';
        if (e.code == 'invalid_update') {
          final file = _download;
          _download = null;
          if (file != null && await file.exists()) await file.delete();
        }
        _emit(UpdatePhase.failed);
      }
    } finally {
      _installPending = false;
      if (!_closed) notifyListeners();
    }
  }

  Future<void> resume() async {
    if (_closed || busy) return;
    if (phase == UpdatePhase.installer) _emit(UpdatePhase.ready);
    if (phase == UpdatePhase.permission &&
        await platform.channelAllowed() &&
        !_closed) {
      await install();
    }
  }

  void cancel() {
    if (!busy || _closed) return;
    _generation++;
    _client?.close();
    _client = null;
    received = 0;
    _emit(available ? UpdatePhase.available : UpdatePhase.idle);
  }

  @override
  void dispose() {
    _closed = true;
    _generation++;
    _client?.close();
    // The installer still needs its temporary URI after the page/session closes.
    // Android clears stale packages when the next download starts.
    super.dispose();
  }
}
