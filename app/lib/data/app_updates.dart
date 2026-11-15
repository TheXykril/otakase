import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:http/http.dart' as http;

const androidPackage = 'com.xyverion.otakase';
const androidApk = 'otakase-android.apk';

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
  manual,
  failed,
}

class InstalledApp {
  const InstalledApp(this.name, this.code);
  final String name;
  final int code;
  String get label => '$name (build $code)';
}

/// Turns a release version into one number that only grows:
/// 26.7.0 -> 26007000. Android installs an update only with a higher
/// versionCode, and every platform compares updates the same way.
int versionCode(String version) {
  final parts = version.split('-').first.split('.');
  if (parts.length < 2 || parts.length > 3) return 0;
  final numbers = parts.map(int.tryParse).toList();
  if (numbers.any((n) => n == null || n < 0 || n > 999)) return 0;
  final [year, drop, ...rest] = numbers.cast<int>();
  return year * 1000000 + drop * 1000 + (rest.isEmpty ? 0 : rest.first);
}

/// A version the update check can order. Releases look like 26.7.0; dev
/// builds look like 26.8.0-dev.41: a test build of the version it is heading
/// to, numbered by the build run. Dev builds of a version come before its
/// release and after every earlier version.
class AppVersion implements Comparable<AppVersion> {
  const AppVersion._(this.code, this.dev);
  final int code;

  /// The dev build number, or null for a release.
  final int? dev;
  bool get isDev => dev != null;

  static final _pattern = RegExp(
    r'^(\d{1,3}\.\d{1,3}(?:\.\d{1,3})?)(?:-dev\.(\d{1,9}))?$',
  );

  /// Null when [name] is not a release or dev build version.
  static AppVersion? tryParse(String name) {
    final match = _pattern.firstMatch(name.trim());
    if (match == null) return null;
    final code = versionCode(match.group(1)!);
    if (code < 1) return null;
    final dev = match.group(2);
    return AppVersion._(code, dev == null ? null : int.parse(dev));
  }

  @override
  int compareTo(AppVersion other) {
    if (code != other.code) return code.compareTo(other.code);
    if (dev == other.dev) return 0;
    if (dev == null) return 1;
    if (other.dev == null) return -1;
    return dev!.compareTo(other.dev!);
  }
}

/// True when [update] is newer than what is installed. Builds that share a
/// version number (dev builds of one version, and its release) are told
/// apart by the version name; a name the app cannot read falls back to the
/// number alone.
bool isNewer(AppUpdate update, InstalledApp? installed) {
  if (installed == null) return update.code > 0;
  final next = AppVersion.tryParse(update.name);
  final current = AppVersion.tryParse(installed.name);
  if (next == null ||
      current == null ||
      next.code != update.code ||
      current.code != installed.code ||
      update.code != installed.code) {
    return update.code > installed.code;
  }
  return next.compareTo(current) > 0;
}

/// True when the installed app is a dev build.
bool isDevBuild(InstalledApp? installed) =>
    installed != null && AppVersion.tryParse(installed.name)?.isDev == true;

class AppUpdate {
  const AppUpdate({
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
  bool get isDev => AppVersion.tryParse(name)?.isDev == true;

  /// Reads an update from the release data the Otakase site serves (GitHub's
  /// release shape): the version from the tag (v26.1.0), and the size and
  /// SHA-256 checksum recorded for the uploaded file. The dev build has the
  /// tag "dev" and its version (26.8.0-dev.41) in "version".
  factory AppUpdate.parse(
    Map release,
    Map asset,
    UpdatePlatform platform, {
    String host = updateSite,
  }) {
    final tag = release['tag_name'];
    final String? name;
    if (tag == 'dev') {
      final version = release['version'];
      name = version is String && AppVersion.tryParse(version)?.isDev == true
          ? version
          : null;
    } else {
      name = tag is String
          ? RegExp(r'^v(\d{1,3}\.\d{1,3}(?:\.\d{1,3})?)$')
                .firstMatch(tag)
                ?.group(1)
          : null;
    }
    final code = name == null ? 0 : versionCode(name);
    final size = asset['size'];
    final digest = asset['digest'];
    final checksum = digest is String && digest.startsWith('sha256:')
        ? digest.substring(7)
        : null;
    if (name == null ||
        code < 1 ||
        asset['name'] != platform.fileName ||
        size is! int ||
        size < 1 ||
        size > 512 * 1024 * 1024 ||
        checksum == null ||
        !RegExp(r'^[a-f0-9]{64}$').hasMatch(checksum)) {
      throw const FormatException('Invalid update metadata.');
    }
    final url = releaseAssetUri(
      asset['browser_download_url'],
      platform.fileName,
      host: host,
    );
    if (url.pathSegments[1] != tag) {
      throw const FormatException('Release files do not match.');
    }
    return AppUpdate(
      name: name,
      code: code,
      url: url,
      size: size,
      checksum: checksum,
    );
  }
}

/// The site the apps get updates from. It serves the release files from
/// GitHub, or from the private copy of the releases when GitHub cannot, so
/// updates keep working without GitHub.
const updateSite = 'otakase.com';

/// The older address of the same site. It answers `/download/` the same way,
/// with links on its own host. The app falls back to it when [updateSite]
/// cannot be reached: some DNS filters and networks block a newly registered
/// domain for weeks.
const fallbackUpdateSite = 'otakase.xyverion.com';

/// Hosts asked for updates, in order.
const updateSites = [updateSite, fallbackUpdateSite];

/// The newest release, in GitHub's release shape, with links on the host
/// that served it.
Uri latestReleaseOn(String host) => Uri.https(host, '/download/latest.json');

/// The newest release on [updateSite].
final latestRelease = latestReleaseOn(updateSite);

/// The current dev build (the "dev" pre-release), in the same shape, with
/// its version in "version". Asked only with Dev builds turned on.
Uri devBuildOn(String host) => Uri.https(host, '/download/dev.json');

/// Checks a download link: `/download/<tag>/<filename>` on [host], the
/// update site the release data came from.
Uri releaseAssetUri(
  dynamic value,
  String filename, {
  String host = updateSite,
}) {
  final uri = value is String ? Uri.tryParse(value) : null;
  final parts = uri?.pathSegments ?? const <String>[];
  if (uri == null ||
      uri.scheme != 'https' ||
      uri.host != host ||
      uri.port != 443 ||
      uri.userInfo.isNotEmpty ||
      uri.hasQuery ||
      uri.hasFragment ||
      parts.length != 3 ||
      parts[0] != 'download' ||
      parts[1].isEmpty ||
      parts[2] != filename) {
    throw const FormatException('Invalid release address.');
  }
  return uri;
}

/// True when [error] means the update site could not be reached at all
/// (no DNS answer, refused or dropped connection, TLS failure, no answer in
/// time), as opposed to an answer the app did not like.
bool unreachable(Object error) =>
    error is SocketException ||
    error is TlsException ||
    error is TimeoutException ||
    error is http.ClientException;

/// What to tell the user when no update site could be reached.
String unreachableText(Object error) {
  final text = error.toString().toLowerCase();
  if (text.contains('host lookup') ||
      text.contains('no address associated') ||
      text.contains('nodename nor servname')) {
    return 'Could not find the update site. Check your connection, or a DNS '
        'or ad blocker that blocks $updateSite, and retry.';
  }
  if (error is TlsException ||
      text.contains('handshake') ||
      text.contains('certificate')) {
    return 'Could not open a secure connection to the update site. Check the '
        'date and time on this device and retry.';
  }
  if (error is TimeoutException || text.contains('timed out')) {
    return 'The update site did not answer in time. Retry later.';
  }
  return 'Unable to check for updates. Check your connection and retry.';
}

/// Where an update comes from and how it is installed on this kind of
/// device. The release check, download and checksum are shared.
abstract class UpdatePlatform {
  /// The release file this device installs.
  String get fileName;

  /// Shown above the update buttons.
  String get idleText;
  String get readyText;
  String get installLabel;
  String get footnote;

  Future<InstalledApp> installed();

  /// Why this copy cannot update itself, or null when it can. Updates for
  /// such a copy are offered as a link to the download page.
  Future<String?> manualReason() async => null;

  Future<File> createDownload();
  Future<bool> channelAllowed();
  Future<void> validate(File file, AppUpdate update);
  Future<bool> install(File file, AppUpdate update);
}

class AndroidUpdatePlatform extends UpdatePlatform {
  static const channel = MethodChannel('otakase/updates');

  @override
  String get fileName => androidApk;
  @override
  String get idleText => 'Check for a new Android app version.';
  @override
  String get readyText => 'Update verified. Ready to install.';
  @override
  String get installLabel => 'Install update';
  @override
  String get footnote =>
      'Your profiles, accounts and watch progress stay on this device. Android asks you to approve installation.';

  @override
  Future<InstalledApp> installed() async {
    final data = await channel.invokeMapMethod<String, dynamic>('installed');
    return InstalledApp(data!['name'] as String, data['code'] as int);
  }

  @override
  Future<File> createDownload() async =>
      File((await channel.invokeMethod<String>('createDownload'))!);

  @override
  Future<bool> channelAllowed() async =>
      await channel.invokeMethod<bool>('installationAllowed') ?? false;

  @override
  Future<void> validate(File file, AppUpdate update) =>
      channel.invokeMethod<void>('validate', {
        'path': file.path,
        'code': update.code,
        'name': update.name,
        'sha256': update.checksum,
      });

  @override
  Future<bool> install(File file, AppUpdate update) async =>
      await channel.invokeMethod<bool>('install', {
        'path': file.path,
        'code': update.code,
        'name': update.name,
        'sha256': update.checksum,
      }) ??
      false;
}

/// No update site could be reached; [error] is what the first one failed with.
class _Unreachable implements Exception {
  const _Unreachable(this.error);
  final Object error;
}

class AppUpdates extends ChangeNotifier {
  AppUpdates({
    UpdatePlatform? platform,
    http.Client Function()? client,
    bool Function()? devBuilds,
  }) : platform = platform ?? AndroidUpdatePlatform(),
       _newClient = client ?? http.Client.new,
       _devBuilds = devBuilds ?? _never;

  static bool _never() => false;

  final UpdatePlatform platform;
  final http.Client Function() _newClient;

  /// Whether the Dev builds setting is on: checks then also look at the
  /// current dev build and offer it when it is newer than the release.
  final bool Function() _devBuilds;
  bool get devBuilds => _devBuilds();
  UpdatePhase phase = UpdatePhase.idle;
  InstalledApp? installed;
  AppUpdate? update;
  String? error;

  /// Set when this copy cannot install updates itself (see
  /// [UpdatePlatform.manualReason]).
  String? manualReason;
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
  bool get available => update != null && isNewer(update!, installed);
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
      ..headers['Accept'] = uri.path.endsWith('.json')
          ? 'application/json'
          : 'application/octet-stream'
      ..headers['User-Agent'] = 'Otakase-App';
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

  /// Asks the update sites in order for [feed]; the first that answers is
  /// used, even with an error, and the next is tried only when one cannot be
  /// reached at all.
  Future<(http.StreamedResponse, String)?> _ask(
    http.Client client,
    Uri Function(String host) feed,
    int generation,
  ) async {
    Object? firstError;
    for (final site in updateSites) {
      try {
        return (await _get(client, feed(site)), site);
      } catch (e) {
        if (!unreachable(e)) rethrow;
        firstError ??= e;
        if (!_active(generation)) return null;
      }
    }
    throw _Unreachable(firstError!);
  }

  /// The release (or dev build) one feed offers for this device, or null
  /// when it has none.
  Future<AppUpdate?> _offer(
    http.Client client,
    Uri Function(String host) feed,
    int generation, {
    required bool dev,
  }) async {
    final answer = await _ask(client, feed, generation);
    if (answer == null || !_active(generation)) return null;
    final (response, host) = answer;
    if (response.statusCode == 404) return null;
    final release = await _json(response, 2 * 1024 * 1024);
    if (!_active(generation)) return null;
    if (release['draft'] == true || (release['prerelease'] == true) != dev) {
      return null;
    }
    final assets = (release['assets'] as List? ?? []).whereType<Map>();
    final files = assets.where((a) => a['name'] == platform.fileName);
    if (files.length != 1) return null;
    final offer = AppUpdate.parse(release, files.single, platform, host: host);
    if (offer.isDev != dev) {
      throw const FormatException('Invalid update metadata.');
    }
    return offer;
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
      var candidate = await _offer(
        client,
        latestReleaseOn,
        generation,
        dev: false,
      );
      if (!_active(generation)) return;
      if (devBuilds) {
        // A broken or missing dev build never hides the release.
        AppUpdate? dev;
        try {
          dev = await _offer(client, devBuildOn, generation, dev: true);
        } on FormatException {
          dev = null;
        } on HttpException {
          dev = null;
        } on _Unreachable {
          dev = null;
        }
        if (!_active(generation)) return;
        if (dev != null &&
            (candidate == null ||
                AppVersion.tryParse(dev.name)!
                        .compareTo(AppVersion.tryParse(candidate.name)!) >
                    0)) {
          candidate = dev;
        }
      }
      if (candidate == null) {
        _emit(UpdatePhase.unpublished);
        return;
      }
      update = candidate;
      manualReason = available ? await platform.manualReason() : null;
      if (!_active(generation)) return;
      _emit(
        !available
            ? UpdatePhase.current
            : manualReason != null
            ? UpdatePhase.manual
            : UpdatePhase.available,
      );
    } catch (e) {
      if (_active(generation)) {
        error = switch (e) {
          MissingPluginException() =>
            'Updates are available in the Android app.',
          HttpException() => e.message,
          FormatException() => e.message,
          _Unreachable(:final error) => unreachableText(error),
          _ => 'Unable to check for updates. Check your connection and retry.',
        };
        _emit(UpdatePhase.failed);
      }
    } finally {
      client.close();
      if (identical(_client, client)) _client = null;
    }
  }

  /// Checks again from scratch, dropping a downloaded update that was not
  /// installed. Used when the Dev builds setting changes, so a dev build
  /// downloaded before is not installed after turning it off.
  Future<void> recheck() async {
    if (_closed || busy) return;
    final file = _download;
    _download = null;
    update = null;
    received = 0;
    if (file != null && await file.exists()) await file.delete();
    await check();
  }

  Future<void> download() async {
    if (busy || _closed || !available || downloaded || manualReason != null) {
      return;
    }
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
        throw const FormatException('Download size does not match.');
      }
      sink = await file.open(mode: FileMode.write);
      await for (final chunk in response.stream.timeout(
        const Duration(seconds: 30),
      )) {
        if (!_active(generation)) return;
        received += chunk.length;
        if (received > candidate.size) {
          throw const FormatException('Download is too large.');
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
        throw const FormatException('Download verification failed.');
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
        error = e.message ?? 'Unable to start the update.';
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
