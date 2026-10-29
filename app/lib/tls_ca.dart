import 'dart:io';

// The full mpv libraries check HTTPS certificates, but their TLS library cannot
// read Android's certificate store, so every https stream failed. Collect the
// system and user-added CAs into one PEM file that mpv can load.
const _caDirs = [
  '/apex/com.android.conscrypt/cacerts',
  '/system/etc/security/cacerts',
  '/data/misc/user/0/cacerts-added',
];
final _pem = RegExp(
  r'-----BEGIN CERTIFICATE-----[\s\S]+?-----END CERTIFICATE-----',
);

Future<Map<String, String>>? _options;

/// mpv properties that point certificate checks at the device's CAs, built
/// once per run. Empty when no certificates could be read.
Future<Map<String, String>> tlsOptions({
  List<String> dirs = _caDirs,
  Directory? cache,
}) => _options ??= _build(dirs, cache ?? Directory.systemTemp);

Future<Map<String, String>> _build(List<String> dirs, Directory cache) async {
  final certs = <String>{};
  for (final path in dirs) {
    try {
      final dir = Directory(path);
      if (!await dir.exists()) continue;
      await for (final f in dir.list()) {
        if (f is! File) continue;
        try {
          certs.addAll(
            _pem.allMatches(await f.readAsString()).map((m) => m[0]!),
          );
        } catch (_) {
          /* An unreadable entry is skipped. */
        }
      }
    } catch (_) {
      /* A missing or private folder is skipped. */
    }
  }
  if (certs.isEmpty) return const {};
  try {
    final file = File('${cache.path}/otakase-cacert.pem');
    await file.writeAsString('${certs.join('\n')}\n', flush: true);
    return {'tls-ca-file': file.path};
  } catch (_) {
    return const {};
  }
}

/// Forget the cached options. Tests only.
void resetTlsOptions() => _options = null;
