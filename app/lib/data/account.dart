import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:crypto/crypto.dart' as hash;
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';

import '../host.dart';
import 'auth.dart';
import 'models.dart';

/// The account server, then the address on the older domain for networks
/// whose DNS filter still blocks the new one.
const accountHosts = [
  'https://account.otakase.com',
  'https://otakase.xyverion.com/account',
];

class AccountFailure implements Exception {
  const AccountFailure(this.message, {this.status = 0});
  final String message;
  final int status;
  @override
  String toString() => message;
}

String _random(int bytes) =>
    base64UrlEncode(List.generate(bytes, (_) => Random.secure().nextInt(256)))
        .replaceAll('=', '');

/// The claims of a signed token, read without checking it: the server
/// checks it, the app only wants the role and whether Discord is attached.
Json tokenClaims(String token) {
  try {
    final part = token.split('.')[1];
    final padded = part.padRight((part.length + 3) ~/ 4 * 4, '=');
    return (jsonDecode(utf8.decode(base64Url.decode(padded))) as Map)
        .cast<String, dynamic>();
  } catch (_) {
    return {};
  }
}

/// Whether [e] means the address could not be reached at all, so the other
/// account host is worth a try.
bool unreachable(Object e) =>
    e is SocketException ||
    e is HandshakeException ||
    e is TimeoutException ||
    e is http.ClientException;

/// What the device tells the server about itself.
class DeviceInfo {
  const DeviceInfo({
    required this.name,
    required this.os,
    required this.isTV,
    required this.version,
  });
  final String name, os, version;
  final bool isTV;
  Json toJson({String publicKey = ''}) => {
    'name': name,
    'os': os,
    'is_tv': isTV,
    'app_version': version,
    if (publicKey.isNotEmpty) 'public_key': publicKey,
  };
}

/// Makes and keeps this device's key pair for the token vault. The private
/// half never leaves the device.
abstract class DeviceKeys {
  Future<String> publicKey(String account);
}

/// One Otakase account signed in on this device.
class AccountSession extends ChangeNotifier {
  AccountSession(this.manager, Json data)
    : id = data['id'] as String,
      _data = data;
  final AccountManager manager;
  final String id;
  Json _data;
  String _access = '';
  DateTime _accessUntil = DateTime.fromMillisecondsSinceEpoch(0);
  Future<String>? _refreshing;
  bool signedOut = false;

  /// Set when the account left this device for good (removed from another
  /// device or deleted), not by signing out here: the vault key goes too.
  bool removed = false;

  String get name => _data['name'] as String? ?? 'Otakase';
  String get avatar => _data['avatar'] as String? ?? '';
  String get role => _data['role'] as String? ?? 'user';
  String get deviceId => _data['device'] as String? ?? '';
  String get base => _data['base'] as String? ?? accountHosts.first;
  bool get hasDiscord => (_data['discord'] as String? ?? '').isNotEmpty;
  String get discordId => _data['discord'] as String? ?? '';
  Json get data => Map.unmodifiable(_data);
  String get _refreshKey => 'otakase.account.$id';

  @override
  void notifyListeners() {
    if (!signedOut) super.notifyListeners();
  }

  /// A current access token, refreshed when it has less than two minutes.
  Future<String> accessToken({bool force = false}) {
    if (!force &&
        _access.isNotEmpty &&
        _accessUntil.isAfter(manager.now().add(const Duration(minutes: 2)))) {
      return Future.value(_access);
    }
    // One refresh at a time: the server rotates the refresh token, so a
    // second call with the old one would sign the device out.
    return _refreshing ??= _refresh().whenComplete(() => _refreshing = null);
  }

  Future<String> _refresh() async {
    final refresh = await manager.tokens.read(_refreshKey);
    if (refresh == null || refresh.isEmpty) {
      throw const AccountFailure(
        'Sign in to your Otakase account again.',
        status: 401,
      );
    }
    final (Json, String) r;
    try {
      r = await manager.send(
        'POST',
        '/token',
        body: {
          'grant_type': 'refresh',
          'refresh_token': refresh,
          'app_version': manager.device.version,
        },
        base: base,
      );
    } on AccountFailure catch (e) {
      // A refused refresh token means this device was removed.
      if (e.status == 401) await manager._lost(this);
      rethrow;
    }
    await _adopt(r.$1, r.$2);
    return _access;
  }

  Future<void> _adopt(Json tokens, String usedBase) async {
    final refresh = tokens['refresh_token'] as String? ?? '';
    final access = tokens['access_token'] as String? ?? '';
    if (refresh.isEmpty || access.isEmpty) {
      throw const AccountFailure('The account server answered oddly.');
    }
    // Save the new refresh token before anything else: the old one is dead.
    await manager.tokens.write(_refreshKey, refresh);
    _access = access;
    _accessUntil = manager.now().add(
      Duration(seconds: max(60, number(tokens['expires_in']))),
    );
    final account = (tokens['account'] as Map? ?? {}).cast<String, dynamic>();
    final claims = tokenClaims(access);
    final next = {
      ..._data,
      'name': account['name'] ?? name,
      'avatar': account['avatar'] ?? avatar,
      'role': account['role'] ?? role,
      'device': tokens['device_id'] ?? deviceId,
      'discord': claims['dc'] ?? '',
      'base': usedBase,
    };
    if (jsonEncode(next) != jsonEncode(_data)) {
      _data = next;
      await manager._save();
      notifyListeners();
    }
  }

  /// Calls the account server as this account. A 401 refreshes once.
  Future<Json> request(
    String method,
    String path, {
    Object? body,
    Duration timeout = const Duration(seconds: 15),
  }) async {
    var token = await accessToken();
    for (var attempt = 0; ; attempt++) {
      try {
        final r = await manager.send(
          method,
          path,
          body: body,
          token: token,
          base: base,
          timeout: timeout,
        );
        if (r.$2 != base) {
          _data = {..._data, 'base': r.$2};
          unawaited(manager._save());
        }
        return r.$1;
      } on AccountFailure catch (e) {
        if (e.status != 401 || attempt > 0) {
          if (e.status == 401) await manager._lost(this);
          rethrow;
        }
        token = await accessToken(force: true);
      }
    }
  }

  /// The live connection address with a fresh token in it.
  Future<Uri> liveUri(Map<String, String> query) async {
    final token = await accessToken();
    final u = Uri.parse('$base/live');
    return u.replace(
      scheme: u.scheme == 'http' ? 'ws' : 'wss',
      queryParameters: {...query, 'access': token},
    );
  }
}

/// A code a TV shows as a QR code and six digits until a phone approves it.
class PairCode {
  const PairCode({
    required this.code,
    required this.secret,
    required this.url,
    required this.expires,
    required this.base,
    this.keyName = '',
  });
  final String code, secret, url, base, keyName;
  final DateTime expires;
  String get spaced => '${code.substring(0, 3)} ${code.substring(3)}';
}

/// The Otakase accounts signed in on this device. Usually one; a family TV
/// can hold several, each with its own profiles in the profile picker.
class AccountManager extends ChangeNotifier {
  AccountManager({
    required this.tokens,
    required this.preferences,
    required this.device,
    http.Client? client,
    this.keys,
    DateTime Function()? now,
    this.hosts = accountHosts,
  }) : client = client ?? http.Client(),
       now = now ?? DateTime.now;
  final TokenStore tokens;
  final SharedPreferences preferences;
  DeviceInfo device;
  final http.Client client;
  final DeviceKeys? keys;
  final DateTime Function() now;
  final List<String> hosts;
  final _accounts = <AccountSession>[];
  bool initialized = false, closed = false;
  String? lastError;
  Json? _pending;
  HttpServer? _server;

  /// Runs after an account signs in on this device, before the UI hears
  /// of it: the app binds profiles and starts syncing here.
  Future<void> Function(AccountSession session, {required bool first})?
  onSignedIn;

  /// Runs when an account signs out or is removed from this device.
  Future<void> Function(AccountSession session)? onSignedOut;

  List<AccountSession> get accounts => List.unmodifiable(_accounts);
  bool get signedIn => _accounts.isNotEmpty;
  AccountSession? account(String id) =>
      _accounts.where((a) => a.id == id).firstOrNull;

  @override
  void notifyListeners() {
    if (!closed) super.notifyListeners();
  }

  Future<void> initialize() async {
    if (initialized) return;
    initialized = true;
    try {
      final raw = await tokens.read('otakase.accounts');
      for (final row in jsonDecode(raw ?? '[]') as List) {
        if (row is Map && row['id'] is String) {
          _accounts.add(AccountSession(this, row.cast<String, dynamic>()));
        }
      }
    } catch (_) {
      /* A damaged list signs this device out of its accounts. */
    }
    notifyListeners();
  }

  Future<void> _save() => tokens.write(
    'otakase.accounts',
    jsonEncode(_accounts.map((a) => a._data).toList()),
  );

  /// One request to the account server. Tries the other host when [base]
  /// can't be reached. Returns the answer and the host that answered.
  Future<(Json, String)> send(
    String method,
    String path, {
    Object? body,
    String? token,
    String? base,
    Duration timeout = const Duration(seconds: 15),
  }) async {
    final order = [?base, ...hosts.where((h) => h != base)];
    Object? last;
    for (final host in order) {
      final r = http.Request(method, Uri.parse('$host$path'));
      r.headers.addAll({
        'Accept': 'application/json',
        'User-Agent': 'Otakase/${device.version}',
        if (token != null) 'Authorization': 'Bearer $token',
      });
      if (body != null) {
        r.headers['Content-Type'] = 'application/json';
        r.body = body is String ? body : jsonEncode(body);
      }
      final http.Response response;
      try {
        response = await http.Response.fromStream(
          await client.send(r).timeout(timeout),
        ).timeout(timeout);
      } catch (e) {
        if (!unreachable(e)) rethrow;
        last = e;
        continue;
      }
      Json value = {};
      try {
        if (response.body.trim().isNotEmpty) {
          value = (jsonDecode(response.body) as Map).cast<String, dynamic>();
        }
      } catch (_) {
        if (response.statusCode ~/ 100 == 2) {
          throw const AccountFailure('The account server answered oddly.');
        }
      }
      if (response.statusCode ~/ 100 != 2) {
        throw AccountFailure(
          value['error']?.toString() ??
              'The account server answered ${response.statusCode}.',
          status: response.statusCode,
        );
      }
      return ({'_status': response.statusCode, ...value}, host);
    }
    throw AccountFailure(
      last is TimeoutException
          ? 'The account server did not answer. Check your connection.'
          : "Couldn't reach the account server. Check your connection.",
    );
  }

  // ---- Signing in on a phone or computer ---------------------------------

  /// Starts a sign-in in the browser. The desktop app takes the answer on
  /// its local sign-in port; phones get it back as an otakase:// link.
  Future<Uri> begin({bool desktop = false, String? base}) async {
    await cancel();
    final verifier = _random(48);
    final challenge = base64UrlEncode(
      hash.sha256.convert(utf8.encode(verifier)).bytes,
    ).replaceAll('=', '');
    if (desktop) {
      try {
        _server = await HttpServer.bind(
          InternetAddress.loopbackIPv4,
          AuthService.desktopLoginPort,
        );
      } on SocketException {
        throw const AccountFailure(
          'Another sign-in is still open. Close it and try again.',
        );
      }
      _server!.listen(_receive, onError: (_) {});
    }
    final host = base ?? await _reachableHost();
    _pending = {
      'verifier': verifier,
      'base': host,
      'expires': now().add(const Duration(minutes: 10)).millisecondsSinceEpoch,
    };
    await tokens.write('otakase.account.pending', jsonEncode(_pending));
    return Uri.parse('$host/login').replace(
      queryParameters: {
        'challenge': challenge,
        'return': desktop ? 'desktop' : 'app',
      },
    );
  }

  Future<String> _reachableHost() async {
    for (final host in hosts) {
      try {
        await client
            .head(Uri.parse('$host/login'))
            .timeout(const Duration(seconds: 5));
        return host;
      } catch (e) {
        if (!unreachable(e)) return host;
      }
    }
    return hosts.first;
  }

  Future<void> _receive(HttpRequest request) async {
    request.response.headers.set('Cache-Control', 'no-store');
    request.response.headers.contentType = ContentType.html;
    try {
      if (request.method != 'GET' || request.uri.path != '/account') {
        request.response.statusCode = HttpStatus.notFound;
        return;
      }
      await accept(
        Uri(
          scheme: 'otakase',
          host: 'account',
          queryParameters: request.uri.queryParameters,
        ),
      );
      request.response.write(
        '<!doctype html><meta name="viewport" content="width=device-width"><title>Otakase</title><p>Signed in. Return to Otakase.</p>',
      );
    } catch (e) {
      request.response.statusCode = HttpStatus.badRequest;
      request.response.write(
        '<!doctype html><title>Otakase</title><p>Sign-in expired or unsuccessful. Start again in Otakase.</p>',
      );
    } finally {
      await request.response.close();
    }
  }

  /// Counts finished sign-ins, so a waiting screen can tell one happened
  /// even when it was the same account signing in again.
  int signIns = 0;

  /// Finishes a sign-in from an otakase://account?code=… link.
  Future<AccountSession> accept(Uri uri) async {
    var p = _pending;
    if (p == null) {
      try {
        p = (jsonDecode(
          await tokens.read('otakase.account.pending') ?? '',
        ) as Map).cast<String, dynamic>();
      } catch (_) {
        p = null;
      }
    }
    final code = uri.queryParameters['code'] ?? '';
    if (uri.scheme != 'otakase' ||
        uri.host != 'account' ||
        p == null ||
        code.isEmpty ||
        number(p['expires']) <= now().millisecondsSinceEpoch) {
      throw const AccountFailure('Sign-in expired. Start again from Accounts.');
    }
    await cancel();
    final keyName = 'new-${_random(6)}';
    final (answer, host) = await send(
      'POST',
      '/token',
      body: {
        'grant_type': 'code',
        'code': code,
        'verifier': p['verifier'],
        'device': device.toJson(
          publicKey: await keys?.publicKey(keyName) ?? '',
        ),
      },
      base: p['base'] as String?,
    );
    return _signIn(answer, host, keyName);
  }

  /// Adds the account from a token answer. [keyName] is the temporary
  /// name of the vault key pair made for this device before the account id
  /// was known; it moves under the account id.
  Future<AccountSession> _signIn(
    Json answer,
    String host,
    String keyName,
  ) async {
    final info = (answer['account'] as Map? ?? {}).cast<String, dynamic>();
    final id = info['id'] as String? ?? '';
    if (id.isEmpty) {
      throw const AccountFailure('The account server answered oddly.');
    }
    if (keys case final MovableKeys k) await k.move(keyName, id);
    final existing = account(id);
    if (existing != null) {
      // Signed in again on the same device: the new device entry takes over.
      _accounts.remove(existing);
      existing.signedOut = true;
    }
    final session = AccountSession(this, {'id': id});
    await session._adopt(answer, host);
    _accounts.add(session);
    await _save();
    lastError = null;
    signIns++;
    await onSignedIn?.call(session, first: existing == null);
    notifyListeners();
    return session;
  }

  Future<void> cancel() async {
    _pending = null;
    await tokens.write('otakase.account.pending', null);
    final server = _server;
    _server = null;
    await server?.close(force: true);
  }

  // ---- TVs: QR code, approved from a phone -------------------------------

  Future<PairCode> startPair() async {
    final keyName = 'pair-${_random(6)}';
    final key = await keys?.publicKey(keyName) ?? '';
    final (r, host) = await send(
      'POST',
      '/pair/start',
      body: {'device': device.toJson(publicKey: key)},
    );
    return PairCode(
      code: r['code'] as String,
      secret: r['secret'] as String,
      url: r['url'] as String,
      base: host,
      keyName: keyName,
      expires: now().add(Duration(seconds: number(r['expires_in']))),
    );
  }

  /// Asks once whether the code was approved. Returns the new account
  /// when it was, null while it waits.
  Future<AccountSession?> pollPair(PairCode pair) async {
    if (!now().isBefore(pair.expires)) {
      throw const AccountFailure(
        'The code ran out. Show a new one.',
        status: 410,
      );
    }
    final (r, host) = await send(
      'POST',
      '/pair/poll',
      body: {'code': pair.code, 'secret': pair.secret},
      base: pair.base,
    );
    if (r['_status'] == 202) return null;
    return _signIn(r, host, pair.keyName);
  }

  /// A signed-in phone or computer approves a TV showing [code].
  Future<String> approve(AccountSession session, String code) async {
    final digits = code.replaceAll(RegExp(r'\D'), '');
    if (digits.length != 6) {
      throw const AccountFailure('Type the 6 digits shown on the TV.');
    }
    final r = await session.request(
      'POST',
      '/pair/approve',
      body: {'code': digits},
    );
    return r['name'] as String? ?? 'TV';
  }

  // ---- Signing out --------------------------------------------------------

  /// Signs this device out of [session] and forgets it here.
  Future<void> signOut(AccountSession session, {bool tellServer = true}) async {
    if (tellServer) {
      try {
        await session.request('POST', '/logout', body: {});
      } catch (_) {
        /* Offline: the device entry stays until removed elsewhere. */
      }
    }
    await _forget(session);
  }

  /// Deletes the whole account and everything the server holds for it.
  Future<void> deleteAccount(AccountSession session) async {
    await session.request('DELETE', '/me');
    session.removed = true;
    await _forget(session);
  }

  Future<void> _forget(AccountSession session) async {
    _accounts.remove(session);
    await tokens.write(session._refreshKey, null);
    await _save();
    await onSignedOut?.call(session);
    session.signedOut = true;
    notifyListeners();
  }

  /// The server no longer knows this device: it was removed or signed out
  /// elsewhere.
  Future<void> _lost(AccountSession session) async {
    if (!_accounts.contains(session)) return;
    lastError = '${session.name}: this device was signed out of the account.';
    session.removed = true;
    await _forget(session);
  }

  Future<void> close() async {
    closed = true;
    await cancel();
  }

  /// The base system name the server shows in Devices.
  static String osName() => Host.android
      ? 'android'
      : Host.linux
      ? 'linux'
      : Host.macOS
      ? 'macos'
      : Host.windows
      ? 'windows'
      : 'other';
}

/// Device keys that can be renamed once the account id is known.
abstract class MovableKeys implements DeviceKeys {
  Future<void> move(String from, String to);
}
