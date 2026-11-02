import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/data/oauth_config.dart';
import 'package:otakase/data/trakt.dart';

class MemoryTokens implements TokenStore {
  final values = <String, String>{};
  @override
  Future<String?> read(String key) async => values[key];
  @override
  Future<void> write(String key, String? value) async {
    value == null ? values.remove(key) : values[key] = value;
  }
}

/// Answers the Linux runner's "otakase/tokens" channel from memory.
class FakeKeyring {
  FakeKeyring(this.legacy);
  String? legacy;
  final items = <String, String>{};
  final calls = <String>[];
  Future<Object?> handle(MethodCall call) async {
    calls.add(call.method);
    final args = (call.arguments as Map?)?.cast<String, Object?>();
    switch (call.method) {
      case 'legacy':
        return legacy;
      case 'clearLegacy':
        legacy = null;
        return null;
      case 'read':
        return items[args!['key']];
      case 'write':
        items[args!['key'] as String] = args['value'] as String;
        return null;
      case 'delete':
        items.remove(args!['key']);
        return null;
    }
    throw MissingPluginException();
  }
}

Future<int> freePort() async {
  final socket = await ServerSocket.bind(InternetAddress.loopbackIPv4, 0);
  final port = socket.port;
  await socket.close();
  return port;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  // The test binding answers every real HTTP request with 400; these tests
  // talk to the sign-in server on 127.0.0.1.
  HttpOverrides.global = null;
  const channel = MethodChannel('otakase/tokens');
  FakeKeyring install(String? legacy) {
    final keyring = FakeKeyring(legacy);
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, keyring.handle);
    addTearDown(
      () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, null),
    );
    return keyring;
  }

  test('Linux keeps each token as its own keyring item', () async {
    final keyring = install(null);
    final store = LinuxTokenStore();
    await store.write('profile.1.account.AniList', '{"token":"a"}');
    await store.write('profile.1.trakt_token', '{"token":"t"}');
    expect(keyring.items.keys, [
      'profile.1.account.AniList',
      'profile.1.trakt_token',
    ]);
    // A second start reads the same items back.
    final again = LinuxTokenStore();
    expect(await again.read('profile.1.account.AniList'), '{"token":"a"}');
    await again.write('profile.1.account.AniList', null);
    expect(keyring.items.containsKey('profile.1.account.AniList'), isFalse);
    expect(keyring.items['profile.1.trakt_token'], '{"token":"t"}');
  });

  test('sign-ins from the old single keyring item move over once', () async {
    final keyring = install(
      jsonEncode({
        'profile.1.account.MAL': '{"token":"old-mal"}',
        'profile.1.account.AniList': '{"token":"old-anilist"}',
        'profile.1.empty': '',
      }),
    );
    // A newer sign-in already in its own item wins over the old copy.
    keyring.items['profile.1.account.AniList'] = '{"token":"new"}';
    final store = LinuxTokenStore();
    expect(await store.read('profile.1.account.MAL'), '{"token":"old-mal"}');
    expect(await store.read('profile.1.account.AniList'), '{"token":"new"}');
    expect(keyring.items.containsKey('profile.1.empty'), isFalse);
    expect(keyring.legacy, isNull);
    await store.read('profile.1.account.MAL');
    expect(keyring.calls.where((c) => c == 'legacy'), hasLength(1));
  });

  test('an empty or damaged old item is cleared without failing', () async {
    for (final legacy in ['', '{not json']) {
      final keyring = install(legacy);
      final store = LinuxTokenStore();
      expect(await store.read('profile.1.account.MAL'), isNull);
      await store.write('profile.1.account.MAL', 'x');
      expect(keyring.items['profile.1.account.MAL'], 'x');
    }
  });

  group('Trakt on a computer', () {
    test('signs in through the browser with PKCE and no secret', () async {
      Map<String, dynamic>? exchange;
      final client = MockClient((r) async {
        if (r.url.path == '/oauth/token') {
          exchange = jsonDecode(r.body) as Map<String, dynamic>;
          return http.Response(
            jsonEncode({
              'access_token': 'tok',
              'refresh_token': 'ref',
              'expires_in': 7776000,
              'created_at': DateTime.now().millisecondsSinceEpoch ~/ 1000,
            }),
            200,
          );
        }
        if (r.url.path == '/users/settings') {
          expect(r.headers['Authorization'], 'Bearer tok');
          return http.Response(
            jsonEncode({
              'user': {'username': 'xykril'},
            }),
            200,
          );
        }
        return http.Response('', 404);
      });
      final tokens = MemoryTokens();
      final trakt = Trakt(client: client, storage: tokens);
      final port = await freePort();
      final authorize = await trakt.beginBrowserSignIn(port: port);
      expect(authorize.host, 'trakt.tv');
      expect(authorize.path, '/oauth/authorize');
      final q = authorize.queryParameters;
      expect(q['redirect_uri'], loginRedirect);
      expect(q['code_challenge_method'], 'S256');
      expect(q.containsKey('client_secret'), isFalse);
      final state = jsonDecode(
        utf8.decode(base64Url.decode(base64Url.normalize(q['state']!))),
      ) as Map;
      expect(state['service'], 'Trakt');
      expect(state['desktop'], true);
      expect(state.containsKey('host'), isFalse);

      // A wrong state is turned away and the sign-in keeps waiting.
      final wrong = await http.get(
        Uri.http('127.0.0.1:$port', '/login', {'state': 'forged', 'code': 'c'}),
      );
      expect(wrong.statusCode, 400);
      expect(trakt.signedIn, isFalse);

      final done = trakt.browserSignIn;
      final response = await http.get(
        Uri.http('127.0.0.1:$port', '/login', {
          'state': q['state']!,
          'code': 'approved',
        }),
      );
      expect(response.statusCode, 200);
      expect(response.body, contains('Return to Otakase'));
      expect(await done, isTrue);
      expect(exchange!['grant_type'], 'authorization_code');
      expect(exchange!['code'], 'approved');
      expect(exchange!['redirect_uri'], loginRedirect);
      expect(exchange!.containsKey('client_secret'), isFalse);
      final verifier = exchange!['code_verifier'] as String;
      expect(
        base64UrlEncode(sha256.convert(ascii.encode(verifier)).bytes)
            .replaceAll('=', ''),
        q['code_challenge'],
      );
      expect(trakt.username, 'xykril');
      final saved = jsonDecode(tokens.values[traktTokenKey]!) as Map;
      expect(saved['access_token'], 'tok');
      expect(saved['redirect_uri'], loginRedirect);
      // The server is gone once the sign-in finished.
      await expectLater(
        http.get(Uri.http('127.0.0.1:$port', '/login')),
        throwsA(isA<Exception>()),
      );
      trakt.dispose();
    });

    test('a browser sign-in refreshes with its own redirect', () async {
      Map<String, dynamic>? refresh;
      final tokens = MemoryTokens()
        ..values[traktTokenKey] = jsonEncode({
          'access_token': 'old',
          'refresh_token': 'ref',
          'expires_in': 60,
          'created_at': 0,
          'redirect_uri': loginRedirect,
        });
      final client = MockClient((r) async {
        if (r.url.path == '/oauth/token') {
          refresh = jsonDecode(r.body) as Map<String, dynamic>;
          return http.Response(
            jsonEncode({
              'access_token': 'new',
              'refresh_token': 'ref2',
              'expires_in': 7776000,
              'created_at': DateTime.now().millisecondsSinceEpoch ~/ 1000,
            }),
            200,
          );
        }
        return http.Response('[]', 200);
      });
      final trakt = Trakt(client: client, storage: tokens);
      await trakt.library();
      expect(refresh!['grant_type'], 'refresh_token');
      expect(refresh!['redirect_uri'], loginRedirect);
      final saved = jsonDecode(tokens.values[traktTokenKey]!) as Map;
      expect(saved['access_token'], 'new');
      expect(saved['redirect_uri'], loginRedirect);
      trakt.dispose();
    });

    test('cancelling closes the sign-in', () async {
      final trakt = Trakt(
        client: MockClient((r) async => http.Response('', 500)),
        storage: MemoryTokens(),
      );
      final port = await freePort();
      await trakt.beginBrowserSignIn(port: port);
      final done = trakt.browserSignIn;
      await trakt.cancelBrowserSignIn();
      expect(await done, isFalse);
      await expectLater(
        http.get(Uri.http('127.0.0.1:$port', '/login')),
        throwsA(isA<Exception>()),
      );
      trakt.dispose();
    });
  });
}
