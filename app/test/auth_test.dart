import 'dart:convert';
import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/data/oauth_config.dart';

class MemoryTokens implements TokenStore {
  final values = <String, String>{};
  @override
  Future<String?> read(String key) async => values[key];
  @override
  Future<void> write(String key, String? value) async {
    if (value == null) {
      values.remove(key);
    } else {
      values[key] = value;
    }
  }
}

http.Response json(Object value, [int status = 200]) =>
    http.Response(jsonEncode(value), status);
Account account(String service, {bool expired = false}) => Account(
  service: service,
  token: 'access-$service',
  refresh: service == 'MAL' ? 'refresh' : '',
  expires: DateTime.now().add(Duration(days: expired ? -1 : 30)),
  username: 'viewer',
  userId: 7,
);

class DelayedTokens extends MemoryTokens {
  final started = Completer<void>(), release = Completer<void>();
  bool delayed = false;
  @override
  Future<void> write(String key, String? value) async {
    if (key == 'account.MAL' && value != null && !delayed) {
      delayed = true;
      started.complete();
      await release.future;
    }
    await super.write(key, value);
  }
}

void main() {
  test(
    'AniList implicit validates state and securely stores verified identity',
    () async {
      final storage = MemoryTokens();
      final auth = AuthService(
        storage: storage,
        client: MockClient((r) async {
          expect(r.headers['authorization'], 'Bearer secret');
          return json({
            'data': {
              'Viewer': {
                'id': 7,
                'name': 'viewer',
                'avatar': {'medium': 'avatar'},
              },
            },
          });
        }),
      );
      final url = await auth.begin('AniList');
      expect(url.queryParameters['client_id'], anilistId);
      expect(url.queryParameters['response_type'], 'token');
      expect(url.queryParameters.containsKey('code_challenge'), isFalse);
      expect(url.queryParameters.containsKey('redirect_uri'), isFalse);
      await expectLater(
        auth.accept(
          Uri(
            scheme: 'otakase',
            host: 'login',
            queryParameters: {'state': 'wrong', 'access_token': 'secret'},
          ),
        ),
        throwsA(isA<LoginFailure>()),
      );
      final callback = Uri(
        scheme: 'otakase',
        host: 'login',
        queryParameters: {
          'state': url.queryParameters['state']!,
          'access_token': 'secret',
        },
      );
      await auth.accept(callback);
      expect(auth.account('AniList')!.username, 'viewer');
      expect(
        auth.account('AniList')!.expires.difference(DateTime.now()).inDays,
        inInclusiveRange(364, 365),
      );
      expect(storage.values['account.AniList'], contains('secret'));
      expect(storage.values.containsKey('login.pending'), isFalse);
      await expectLater(auth.accept(callback), throwsA(isA<LoginFailure>()));
      final restored = AuthService(
        storage: storage,
        client: MockClient((r) async => json({}, 503)),
      );
      await restored.initialize(platformCallbacks: false);
      expect(restored.account('AniList')!.token, 'secret');
      await restored.logout('AniList');
      expect(storage.values.containsKey('account.AniList'), isFalse);
      auth.dispose();
      restored.dispose();
    },
  );
  test('MAL plain PKCE exchanges original verifier with no secret across recreation', () async {
    final storage = MemoryTokens();
    late Uri url;
    final auth = AuthService(
      storage: storage,
      client: MockClient((r) async {
        if (r.url.path.endsWith('/token')) {
          final form = Uri.splitQueryString(r.body);
          expect(form['code_verifier'], url.queryParameters['code_challenge']);
          expect(form['client_id'], malId);
          expect(form.containsKey('client_secret'), isFalse);
          expect(form['redirect_uri'], loginRedirect);
          expect(form['code'], 'approved');
          return json({
            'access_token': 'mal-token',
            'refresh_token': 'new-refresh',
            'expires_in': 3600,
          });
        }
        return json({'id': 7, 'name': 'viewer'});
      }),
    );
    url = await auth.begin('MAL');
    expect(url.queryParameters['code_challenge_method'], 'plain');
    expect(url.queryParameters['redirect_uri'], loginRedirect);
    expect(
      url.queryParameters['code_challenge']!.length,
      inInclusiveRange(43, 128),
    );
    final other = AuthService(storage: storage, client: auth.client);
    await other.initialize(platformCallbacks: false);
    await other.accept(
      Uri(
        scheme: 'otakase',
        host: 'login',
        queryParameters: {
          'code': 'approved',
          'state': url.queryParameters['state']!,
        },
      ),
    );
    expect(other.account('MAL')!.refresh, 'new-refresh');
    await other.logout('MAL');
    expect(other.accounts, isEmpty);
    await auth.cancelLogin();
    auth.dispose();
    other.dispose();
  });
  test(
    'concurrent MAL requests share refresh and persist rotated token',
    () async {
      var refreshes = 0;
      final storage = MemoryTokens();
      final auth = AuthService(
        storage: storage,
        client: MockClient((r) async {
          if (r.url.path.endsWith('/token')) {
            refreshes++;
            expect(Uri.splitQueryString(r.body), {
              'client_id': malId,
              'grant_type': 'refresh_token',
              'refresh_token': 'refresh',
            });
            return json({
              'access_token': 'fresh',
              'refresh_token': 'rotated',
              'expires_in': 3600,
            });
          }
          expect(r.headers['authorization'], 'Bearer fresh');
          return json({'name': 'viewer'});
        }),
      );
      auth.accounts['MAL'] = account('MAL', expired: true);
      await Future.wait([
        auth.mal('GET', '/users/@me'),
        auth.mal('GET', '/users/@me'),
      ]);
      expect(refreshes, 1);
      expect(auth.account('MAL')!.refresh, 'rotated');
      expect(storage.values['account.MAL'], contains('rotated'));
      auth.dispose();
    },
  );
  test('MAL retries once on 401, refresh failure preserves account', () async {
    var gets = 0;
    final auth = AuthService(
      storage: MemoryTokens(),
      client: MockClient((r) async {
        if (r.url.path.endsWith('/token')) {
          return json({'access_token': 'fresh', 'expires_in': 3600});
        }
        gets++;
        return gets == 1 ? json({}, 401) : json({'name': 'viewer'});
      }),
    );
    auth.accounts['MAL'] = account('MAL');
    await auth.mal('GET', '/users/@me');
    expect(gets, 2);
    expect(auth.account('MAL')!.refresh, 'refresh');
    auth.dispose();
    final failed = AuthService(
      storage: MemoryTokens(),
      client: MockClient((r) async => json({}, 400)),
    );
    failed.accounts['MAL'] = account('MAL', expired: true);
    await expectLater(failed.token('MAL'), throwsA(isA<LoginFailure>()));
    expect(failed.accounts.containsKey('MAL'), isTrue);
    failed.dispose();
  });
  test('expired AniList needs sign in, logout is per account', () async {
    final auth = AuthService(storage: MemoryTokens());
    auth.accounts['AniList'] = account('AniList', expired: true);
    auth.accounts['MAL'] = account('MAL');
    await expectLater(auth.token('AniList'), throwsA(isA<LoginFailure>()));
    await auth.logout('AniList');
    expect(auth.signedIn('MAL'), isTrue);
    auth.dispose();
  });
  test('TV callback accepts private IPv4 only', () {
    for (final s in [
      '192.168.1.7',
      '10.0.0.2',
      '172.16.1.1',
      '172.31.255.254',
    ]) {
      expect(AuthService.privateIPv4(s), isTrue);
    }
    for (final s in [
      '127.0.0.1',
      '169.254.169.254',
      '172.32.0.1',
      '192.168.1.999',
      'public.example',
      '192.168.1.1.evil',
    ]) {
      expect(AuthService.privateIPv4(s), isFalse);
    }
  });
  test(
    'TV pairing serves a top-level callback once and closes its listener',
    () async {
      final auth = AuthService(
        findLanAddress: () async => '192.168.1.7',
        storage: MemoryTokens(),
        client: MockClient(
          (r) async => json({
            'data': {
              'Viewer': {
                'id': 7,
                'name': 'viewer',
                'options': {'displayAdultContent': false},
              },
            },
          }),
        ),
      );
      final authorize = await auth.begin('AniList', tv: true);
      final encoded = authorize.queryParameters['state']!;
      final state = jsonDecode(
        utf8.decode(base64Url.decode(base64Url.normalize(encoded))),
      ) as Map;
      expect(AuthService.privateIPv4(state['host'] as String), isTrue);
      final response = await http.get(
        Uri(
          scheme: 'http',
          host: '127.0.0.1',
          port: state['port'] as int,
          path: '/login',
          queryParameters: {'state': encoded, 'access_token': 'token'},
        ),
      );
      expect(response.statusCode, 200);
      expect(response.headers['cache-control'], 'no-store');
      expect(auth.account('AniList')!.username, 'viewer');
      await auth.cancelLogin();
      await expectLater(
        http
            .get(
              Uri(
                scheme: 'http',
                host: '127.0.0.1',
                port: state['port'] as int,
                path: '/login',
              ),
            )
            .timeout(const Duration(seconds: 2)),
        throwsA(isA<Exception>()),
      );
      auth.dispose();
    },
  );
  test('logout wins over a refresh already writing secure storage', () async {
    final storage = DelayedTokens();
    final auth = AuthService(
      storage: storage,
      client: MockClient(
        (r) async => json({
          'access_token': 'fresh',
          'refresh_token': 'rotated',
          'expires_in': 3600,
        }),
      ),
    );
    auth.accounts['MAL'] = account('MAL', expired: true);
    final refresh = auth.token('MAL');
    final rejected = expectLater(refresh, throwsA(isA<LoginFailure>()));
    await storage.started.future;
    final logout = auth.logout('MAL');
    storage.release.complete();
    await rejected;
    await logout;
    expect(auth.signedIn('MAL'), isFalse);
    expect(storage.values.containsKey('account.MAL'), isFalse);
    auth.dispose();
  });
}
