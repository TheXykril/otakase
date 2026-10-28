import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;

import 'models.dart';
import 'oauth_config.dart';

abstract class TokenStore {
  Future<String?> read(String key);
  Future<void> write(String key, String? value);
}

class SecureTokenStore implements TokenStore {
  final storage = const FlutterSecureStorage();
  @override
  Future<String?> read(String key) => storage.read(key: key);
  @override
  Future<void> write(String key, String? value) => value == null
      ? storage.delete(key: key)
      : storage.write(key: key, value: value);
}

class LoginFailure implements Exception {
  const LoginFailure(this.message, {this.statusCode});
  final String message;
  final int? statusCode;
  @override
  String toString() => message;
}

class Account {
  const Account({
    required this.service,
    required this.token,
    required this.expires,
    required this.username,
    this.userId = 0,
    this.refresh = '',
    this.avatar = '',
    this.displayAdultContent = false,
  });
  final String service, token, refresh, username, avatar;
  final int userId;
  final bool displayAdultContent;
  final DateTime expires;
  bool get expired => !expires.isAfter(DateTime.now());
  Json toJson() => {
    'service': service,
    'token': token,
    'refresh': refresh,
    'expires': expires.millisecondsSinceEpoch,
    'username': username,
    'userId': userId,
    'avatar': avatar,
    'displayAdultContent': displayAdultContent,
  };
  factory Account.fromJson(Json j) => Account(
    service: j['service'] as String,
    token: j['token'] as String,
    refresh: j['refresh'] as String? ?? '',
    expires: DateTime.fromMillisecondsSinceEpoch(number(j['expires'])),
    username: j['username'] as String,
    userId: number(j['userId']),
    avatar: j['avatar'] as String? ?? '',
    displayAdultContent: j['displayAdultContent'] == true,
  );
}

class AuthService extends ChangeNotifier {
  AuthService({http.Client? client, TokenStore? storage, this.findLanAddress})
    : client = client ?? http.Client(),
      storage = storage ?? SecureTokenStore();
  static const platform = MethodChannel('otakase/device');
  final Future<String?> Function()? findLanAddress;
  final http.Client client;
  final TokenStore storage;
  final Map<String, Account> accounts = {};
  final Map<String, Future<String>> _refreshing = {};
  final Map<String, Future<void>> _accountWrites = {};
  Json? _pending;
  HttpServer? _server;
  Timer? _expiry;
  bool _accepting = false;
  String? lastError;
  final _completed = StreamController<String>.broadcast();
  Stream<String> get completed => _completed.stream;
  bool signedIn(String service) => accounts.containsKey(service);
  bool get hasAccounts => accounts.isNotEmpty;
  Account? account(String service) => accounts[service];

  Future<void> initialize({bool platformCallbacks = true}) async {
    for (final service in ['AniList', 'MAL']) {
      final raw = await storage.read('account.$service');
      if (raw != null) {
        try {
          accounts[service] = Account.fromJson(
            (jsonDecode(raw) as Map).cast<String, dynamic>(),
          );
        } catch (_) {
          await _storeAccount(service, null);
        }
      }
    }
    final pending = await storage.read('login.pending');
    if (pending != null) {
      try {
        final p = (jsonDecode(pending) as Map).cast<String, dynamic>();
        if (number(p['expires']) > DateTime.now().millisecondsSinceEpoch) {
          _pending = p;
        }
      } catch (_) {
        /* Ignore expired or damaged login requests. */
      }
    }
    await refreshAniListOptions();
    if (platformCallbacks) {
      platform.setMethodCallHandler((call) async {
        if (call.method == 'login' && call.arguments is String) {
          try {
            await accept(Uri.parse(call.arguments as String));
          } catch (e) {
            lastError = e.toString();
            notifyListeners();
          }
        }
      });
      try {
        final initial = await platform.invokeMethod<String>('initialLogin');
        if (initial != null) await accept(Uri.parse(initial));
      } on MissingPluginException {
        /* Tests have no Android host. */
      } on PlatformException {
        lastError = 'Unable to open the login callback.';
      } on LoginFailure catch (e) {
        lastError = e.message;
      }
    }
    notifyListeners();
  }

  String _random() =>
      base64UrlEncode(List.generate(48, (_) => Random.secure().nextInt(256)))
          .replaceAll('=', '');

  Future<Uri> begin(String service, {bool tv = false}) async {
    await cancelLogin();
    if (!['AniList', 'MAL'].contains(service)) {
      throw const LoginFailure('Unknown account.');
    }
    String? host;
    if (tv) {
      host = await (findLanAddress?.call() ?? _findLanAddress());
      if (host == null || !privateIPv4(host)) {
        throw const LoginFailure(
          'Connect your TV to the same home network as your phone.',
        );
      }
      _server = await HttpServer.bind(InternetAddress.anyIPv4, 0);
      _server!.listen(_receive, onError: (_) {});
    }
    final stateData = {
      'nonce': _random(),
      'service': service,
      'host': ?host,
      if (host != null) 'port': _server!.port,
    };
    final state = base64UrlEncode(utf8.encode(jsonEncode(stateData)))
        .replaceAll('=', '');
    final verifier = service == 'MAL' ? _random() : '';
    _pending = {
      'state': state,
      'service': service,
      'verifier': verifier,
      'expires': DateTime.now()
          .add(const Duration(minutes: 10))
          .millisecondsSinceEpoch,
    };
    await storage.write('login.pending', jsonEncode(_pending));
    _expiry = Timer(
      const Duration(minutes: 10),
      () => unawaited(cancelLogin()),
    );
    return Uri.https(
      service == 'MAL' ? 'myanimelist.net' : 'anilist.co',
      service == 'MAL' ? '/v1/oauth2/authorize' : '/api/v2/oauth/authorize',
      {
        'client_id': service == 'MAL' ? malId : anilistId,
        'response_type': service == 'MAL' ? 'code' : 'token',
        // AniList's token login only takes the redirect registered for the
        // client and turns the request down when one is sent.
        if (service == 'MAL') 'redirect_uri': loginRedirect,
        'state': state,
        if (service == 'MAL') 'code_challenge': verifier,
        if (service == 'MAL') 'code_challenge_method': 'plain',
      },
    );
  }

  Future<String?> _findLanAddress() async {
    final interfaces = await NetworkInterface.list(
      type: InternetAddressType.IPv4,
    );
    final addresses = interfaces
        .where((i) => !i.name.startsWith('tun'))
        .expand((i) => i.addresses)
        .where((a) => privateIPv4(a.address));
    return addresses.firstOrNull?.address;
  }

  static bool privateIPv4(String value) {
    final parts = value.split('.').map(int.tryParse).toList();
    if (parts.length != 4 || parts.any((p) => p == null || p < 0 || p > 255)) {
      return false;
    }
    return parts[0] == 10 ||
        (parts[0] == 192 && parts[1] == 168) ||
        (parts[0] == 172 && parts[1]! >= 16 && parts[1]! <= 31);
  }

  Future<void> _receive(HttpRequest request) async {
    request.response.headers.set('Cache-Control', 'no-store');
    request.response.headers.set('Referrer-Policy', 'no-referrer');
    request.response.headers.contentType = ContentType.html;
    var success = false;
    final service = _pending?['service'] as String?;
    try {
      if (request.method != 'GET' || request.uri.path != '/login') {
        request.response.statusCode = HttpStatus.notFound;
      } else {
        await accept(
          Uri(
            scheme: 'otakase',
            host: 'login',
            queryParameters: request.uri.queryParameters,
          ),
          closeServer: false,
        );
        success = true;
        request.response.write(
          '<!doctype html><meta name="viewport" content="width=device-width"><title>Otakase</title><p>Signed in. Return to your TV.</p>',
        );
      }
    } catch (_) {
      request.response.statusCode = HttpStatus.badRequest;
      request.response.write(
        '<!doctype html><title>Otakase</title><p>Login expired or unsuccessful. Start again on your TV.</p>',
      );
    } finally {
      await request.response.close();
      if (success) {
        await _closeServer();
        if (service != null) _completed.add(service);
      }
    }
  }

  Future<void> launch(Uri uri) async {
    try {
      await platform.invokeMethod<void>('launchUri', uri.toString());
    } on PlatformException {
      throw const LoginFailure('No browser is available.');
    }
  }

  Future<void> accept(Uri uri, {bool closeServer = true}) async {
    final p = _pending;
    final q = uri.queryParameters;
    if (uri.scheme != 'otakase' ||
        uri.host != 'login' ||
        p == null ||
        q['state'] != p['state'] ||
        _accepting ||
        number(p['expires']) <= DateTime.now().millisecondsSinceEpoch) {
      throw const LoginFailure('Login expired. Start again from Accounts.');
    }
    if (q.containsKey('error')) {
      throw const LoginFailure(
        'Login was declined. Start again from Accounts.',
      );
    }
    _accepting = true;
    try {
      final service = p['service'] as String;
      late Json tokens;
      if (service == 'MAL') {
        if ((q['code'] ?? '').isEmpty) {
          throw const LoginFailure('Login code is missing.');
        }
        tokens = await _token({
          'grant_type': 'authorization_code',
          'code': q['code']!,
          'code_verifier': p['verifier'] as String,
          'redirect_uri': loginRedirect,
        });
      } else {
        if ((q['access_token'] ?? '').isEmpty) {
          throw const LoginFailure('Login token is missing.');
        }
        tokens = {
          'access_token': q['access_token'],
          'expires_in': int.tryParse(q['expires_in'] ?? '') ?? 31536000,
        };
      }
      final token = tokens['access_token'] as String;
      final user = service == 'MAL'
          ? await _json(
              await client
                  .get(
                    Uri.https('api.myanimelist.net', '/v2/users/@me'),
                    headers: {'Authorization': 'Bearer $token'},
                  )
                  .timeout(const Duration(seconds: 20)),
            )
          : (await _graph(
                  token,
                  'query { Viewer { id name avatar { medium } options { displayAdultContent } } }',
                  {},
                ))['Viewer']
                as Map;
      final account = Account(
        service: service,
        token: token,
        refresh: tokens['refresh_token'] as String? ?? '',
        expires: DateTime.now().add(
          Duration(seconds: number(tokens['expires_in'])),
        ),
        displayAdultContent:
            service == 'AniList' &&
            (user['options'] as Map?)?['displayAdultContent'] == true,
        username: user['name'] as String,
        userId: number(user['id']),
        avatar: service == 'MAL'
            ? user['picture'] as String? ?? ''
            : (user['avatar'] as Map?)?['medium'] as String? ?? '',
      );
      if (_pending != p) {
        throw const LoginFailure('Login cancelled. Start again from Accounts.');
      }
      await _storeAccount(service, account);
      if (_pending != p) {
        throw const LoginFailure('Login cancelled. Start again from Accounts.');
      }
      accounts[service] = account;
      _pending = null;
      await storage.write('login.pending', null);
      _expiry?.cancel();
      if (closeServer) await _closeServer();
      lastError = null;
      if (closeServer) _completed.add(service);
      notifyListeners();
    } finally {
      _accepting = false;
    }
  }

  Future<Json> _json(http.Response response) async {
    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw LoginFailure(
        response.statusCode == 401
            ? 'Login expired. Sign in again from Accounts.'
            : 'Account request failed (${response.statusCode}). Retry shortly.',
        statusCode: response.statusCode,
      );
    }
    final j = (jsonDecode(response.body) as Map).cast<String, dynamic>();
    if ((j['errors'] as List? ?? []).isNotEmpty) {
      throw LoginFailure(
        'Account request failed. Check your login and retry.',
        statusCode: number((j['errors'] as List).first['status']),
      );
    }
    return j;
  }

  Future<Json> _token(Map<String, String> form) async => _json(
    await client
        .post(
          Uri.https('myanimelist.net', '/v1/oauth2/token'),
          body: {'client_id': malId, ...form},
        )
        .timeout(const Duration(seconds: 20)),
  );

  Future<String> token(String service, {bool forceRefresh = false}) async {
    final a = accounts[service];
    if (a == null) throw const LoginFailure('Sign in from Accounts first.');
    if (!forceRefresh &&
        a.expires.isAfter(DateTime.now().add(const Duration(seconds: 30)))) {
      return a.token;
    }
    if (service != 'MAL' || a.refresh.isEmpty) {
      notifyListeners();
      throw const LoginFailure('Login expired. Sign in again from Accounts.');
    }
    return _refreshing[service] ??= _refresh(a).whenComplete(() {
      _refreshing.remove(service);
    });
  }

  Future<String> _refresh(Account a) async {
    final t = await _token({
      'grant_type': 'refresh_token',
      'refresh_token': a.refresh,
    });
    if (accounts[a.service] != a) {
      throw const LoginFailure('Account signed out.');
    }
    final fresh = Account(
      service: a.service,
      token: t['access_token'] as String,
      refresh: t['refresh_token'] as String? ?? a.refresh,
      expires: DateTime.now().add(Duration(seconds: number(t['expires_in']))),
      username: a.username,
      userId: a.userId,
      avatar: a.avatar,
    );
    await _storeAccount(a.service, fresh);
    if (accounts[a.service] != a) {
      throw const LoginFailure('Account signed out.');
    }
    accounts[a.service] = fresh;
    notifyListeners();
    return fresh.token;
  }

  Future<Json> _graph(String token, String query, Json variables) async {
    final j = await _json(
      await client
          .post(
            Uri.https('graphql.anilist.co'),
            headers: {
              'Authorization': 'Bearer $token',
              'Content-Type': 'application/json',
            },
            body: jsonEncode({
              'query': query,
              'variables': graphVariables(variables),
            }),
          )
          .timeout(const Duration(seconds: 20)),
    );
    return (j['data'] as Map).cast<String, dynamic>();
  }

  Future<Json> graph(String query, Json variables) async =>
      _graph(await token('AniList'), query, variables);

  Future<Json> mal(
    String method,
    String path, [
    Map<String, String> fields = const {},
  ]) async {
    Future<http.Response> send(String access) {
      final uri = Uri.https(
        'api.myanimelist.net',
        '/v2$path',
        method == 'GET' ? fields : null,
      );
      final headers = {'Authorization': 'Bearer $access'};
      return (method == 'GET'
              ? client.get(uri, headers: headers)
              : method == 'PATCH'
              ? client.patch(uri, headers: headers, body: fields)
              : client.put(uri, headers: headers, body: fields))
          .timeout(const Duration(seconds: 20));
    }

    var response = await send(await token('MAL'));
    if (response.statusCode == 401) {
      response = await send(await token('MAL', forceRefresh: true));
    }
    return _json(response);
  }

  Future<void> refreshAniListOptions() async {
    final old = accounts['AniList'];
    if (old == null) return;
    try {
      final data = await graph(
        'query { Viewer { options { displayAdultContent } } }',
        {},
      );
      final viewer = data['Viewer'] as Map?;
      if (viewer == null || viewer['options'] is! Map) return;
      if (accounts['AniList'] != old) return;
      final updated = Account(
        service: old.service,
        token: old.token,
        refresh: old.refresh,
        expires: old.expires,
        username: old.username,
        userId: old.userId,
        avatar: old.avatar,
        displayAdultContent:
            (viewer['options'] as Map)['displayAdultContent'] == true,
      );
      await _storeAccount('AniList', updated);
      if (accounts['AniList'] != old) return;
      accounts['AniList'] = updated;
      notifyListeners();
    } catch (_) {
      // Keep this account's cached preference when its viewer lookup fails.
    }
  }

  Future<void> _storeAccount(String service, Account? account) {
    final previous = _accountWrites[service] ?? Future.value();
    final next = previous
        .catchError((Object _) {})
        .then(
          (_) => storage.write(
            'account.$service',
            account == null ? null : jsonEncode(account.toJson()),
          ),
        );
    _accountWrites[service] = next;
    return next;
  }

  Future<void> logout(String service) async {
    accounts.remove(service);
    if (_pending?['service'] == service) await cancelLogin();
    await _storeAccount(service, null);
    notifyListeners();
  }

  Future<void> _closeServer() async {
    final server = _server;
    _server = null;
    await server?.close(force: true);
  }

  Future<void> cancelLogin() async {
    _expiry?.cancel();
    final service = _pending?['service'] as String?;
    _pending = null;
    if (_accepting && service != null) {
      await _storeAccount(service, accounts[service]);
    }
    await storage.write('login.pending', null);
    await _closeServer();
  }

  @override
  void dispose() {
    _expiry?.cancel();
    unawaited(_closeServer());
    unawaited(_completed.close());
    super.dispose();
  }
}
