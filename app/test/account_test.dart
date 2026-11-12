import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/data/account.dart';
import 'package:otakase/data/account_sync.dart';
import 'package:otakase/data/account_vault.dart';
import 'package:otakase/data/discord.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/profiles.dart';
import 'package:otakase/data/store.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'auth_test.dart' show MemoryTokens;
import 'profiles_test.dart' show manager;

const device = DeviceInfo(
  name: 'Pixel',
  os: 'android',
  isTV: false,
  version: '26.5.0',
);

String fakeAccess(String account, {String discord = '111'}) {
  String part(Object v) =>
      base64UrlEncode(utf8.encode(jsonEncode(v))).replaceAll('=', '');
  return '${part({'alg': 'HS256'})}.${part({'sub': account, 'dc': discord})}.sig';
}

/// Enough of the account server for the app's side: tokens, sync with the
/// per-field rule and the vault.
class FakeServer {
  int version = 0, keyVersion = 0, refreshes = 0, devicesMade = 0;
  final docs = <String, Map<String, dynamic>>{};
  final keys = <String, String>{};
  final publicKeys = <String, String>{};
  final blobs = <String, Map<String, dynamic>>{};
  final refreshTokens = <String, String>{}; // token -> device
  final requests = <String>[];
  bool stale = false;
  bool Function(http.Request r)? unreachable;

  String deviceOf(http.Request r) {
    final token = (r.headers['Authorization'] ?? '').replaceFirst(
      'Bearer ',
      '',
    );
    final parts = token.split('.');
    if (parts.length != 3) return '';
    return parts[2];
  }

  Map<String, dynamic> tokens(String dev) {
    final refresh = 'r-${++refreshes}';
    refreshTokens[refresh] = dev;
    final access = fakeAccess('acc-1');
    return {
      'access_token': '${access.substring(0, access.lastIndexOf('.'))}.$dev',
      'expires_in': 3600,
      'refresh_token': refresh,
      'device_id': dev,
      'account': {'id': 'acc-1', 'name': 'Alice', 'avatar': '', 'role': 'user'},
    };
  }

  http.Response ok(Object body, [int status = 200]) => http.Response(
    jsonEncode(body),
    status,
    headers: {'content-type': 'application/json'},
  );

  http.Client client() => MockClient((r) async {
    if (unreachable?.call(r) == true) throw const SocketException('no route');
    requests.add('${r.method} ${r.url.path}');
    final path = r.url.path.replaceFirst('/account', '');
    final body = r.body.isEmpty
        ? <String, dynamic>{}
        : (jsonDecode(r.body) as Map).cast<String, dynamic>();
    final dev = deviceOf(r);
    switch ('${r.method} $path') {
      case 'HEAD /login':
        return http.Response('', 200);
      case 'POST /token':
        if (body['grant_type'] == 'code') {
          final id = 'dev-${++devicesMade}';
          publicKeys[id] =
              (body['device'] as Map)['public_key'] as String? ?? '';
          return ok(tokens(id));
        }
        final owner = refreshTokens.remove(body['refresh_token']);
        if (owner == null) return ok({'error': 'sign in again'}, 401);
        return ok(tokens(owner));
      case 'GET /sync':
        final since = int.parse(r.url.queryParameters['since'] ?? '0');
        if (since == version) return ok({'version': version, 'docs': []});
        return ok({
          'version': version,
          'docs': [
            for (final MapEntry(:key, :value) in docs.entries)
              if ((value['version'] as int) > since)
                {
                  'profile': key.split('|')[0],
                  'kind': key.split('|')[1],
                  'fields': value['fields'],
                  'times': value['times'],
                  'version': value['version'],
                },
          ],
        });
      case 'POST /sync':
        var changed = 0;
        final next = version + 1;
        for (final d in body['docs'] as List) {
          final key = '${d['profile']}|${d['kind']}';
          final stored = docs.putIfAbsent(
            key,
            () => {
              'fields': <String, dynamic>{},
              'times': <String, dynamic>{},
              'version': 0,
            },
          );
          var touched = false;
          for (final MapEntry(:key, :value) in (d['fields'] as Map).entries) {
            final t = (d['times'] as Map)[key] as int;
            if ((stored['times'] as Map)[key] == null ||
                t > ((stored['times'] as Map)[key] as int)) {
              (stored['fields'] as Map)[key] = value;
              (stored['times'] as Map)[key] = t;
              touched = true;
            }
          }
          if (touched) {
            stored['version'] = next;
            changed++;
          }
        }
        if (changed > 0) version = next;
        return ok({'version': version, 'changed': changed});
      case 'GET /vault':
        final holders = keys.keys.toSet();
        return ok({
          'key_version': keyVersion,
          'stale': stale,
          'wrapped': keys[dev],
          'holders': holders.toList(),
          'waiting': [
            for (final MapEntry(:key, :value) in publicKeys.entries)
              if (!holders.contains(key) && value.isNotEmpty)
                {'id': key, 'name': 'Device $key', 'public_key': value},
          ],
          'devices': [
            for (final MapEntry(:key, :value) in publicKeys.entries)
              {'id': key, 'public_key': value},
          ],
          'blobs': [
            for (final MapEntry(:key, :value) in blobs.entries)
              {'profile': key, ...value},
          ],
        });
      case 'PUT /vault/keys':
        final v = body['key_version'] as int;
        if (body['rotate'] == true) {
          if (v != keyVersion + 1) return ok({'error': 'vault changed'}, 409);
          keys.clear();
          keyVersion = v;
          stale = false;
        } else if (v != keyVersion || !keys.containsKey(dev)) {
          return ok({'error': 'no key'}, 403);
        }
        keys.addAll((body['keys'] as Map).cast<String, String>());
        return ok({'ok': true});
      case 'PUT /vault/blob':
        final profile = body['profile'] as String;
        final current = blobs[profile];
        final base = body['base_version'] as int;
        if ((current?['version'] ?? 0) != base) {
          return ok({'error': 'vault changed', 'current': current}, 409);
        }
        blobs[profile] = {
          'version': base + 1,
          'key_version': body['key_version'],
          'ciphertext': body['ciphertext'],
        };
        return ok({'ok': true, 'version': base + 1});
    }
    return ok({'error': 'not found'}, 404);
  });
}

Future<AccountManager> signedInDevice(
  FakeServer server,
  MemoryTokens tokens,
  SharedPreferences prefs,
) async {
  final m = AccountManager(
    tokens: tokens,
    preferences: prefs,
    device: device,
    client: server.client(),
    keys: TokenStoreKeys(tokens),
  );
  await m.initialize();
  await m.begin();
  await m.accept(Uri.parse('otakase://account?code=abc'));
  return m;
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  group('signing in', () {
    test(
      'swaps the code with the PKCE verifier and saves the account',
      () async {
        final server = FakeServer();
        final tokens = MemoryTokens();
        final m = await signedInDevice(
          server,
          tokens,
          await SharedPreferences.getInstance(),
        );
        expect(m.accounts.single.name, 'Alice');
        expect(m.accounts.single.hasDiscord, isTrue);
        expect(m.accounts.single.deviceId, 'dev-1');
        expect(tokens.values['otakase.account.acc-1'], startsWith('r-'));
        expect(server.publicKeys['dev-1'], hasLength(43));
        // The key pair moved under the account id.
        expect(tokens.values.keys, contains('otakase.devicekey.acc-1'));
        // A second manager on the same device finds it again.
        final again = AccountManager(
          tokens: tokens,
          preferences: await SharedPreferences.getInstance(),
          device: device,
          client: server.client(),
        );
        await again.initialize();
        expect(again.accounts.single.id, 'acc-1');
      },
    );

    test('refuses a link without a sign-in in progress', () async {
      final m = AccountManager(
        tokens: MemoryTokens(),
        preferences: await SharedPreferences.getInstance(),
        device: device,
        client: FakeServer().client(),
      );
      await m.initialize();
      await expectLater(
        m.accept(Uri.parse('otakase://account?code=abc')),
        throwsA(isA<AccountFailure>()),
      );
    });

    test('refreshes once for parallel calls and survives rotation', () async {
      final server = FakeServer();
      final m = await signedInDevice(
        server,
        MemoryTokens(),
        await SharedPreferences.getInstance(),
      );
      final s = m.accounts.single;
      final tokens = await Future.wait([
        s.accessToken(force: true),
        s.accessToken(force: true),
        s.accessToken(force: true),
      ]);
      expect(tokens.toSet(), hasLength(1));
      expect(server.requests.where((r) => r == 'POST /token'), hasLength(2));
      await s.request('GET', '/sync?since=0');
    });

    test(
      'falls back to the second host when the first cannot be reached',
      () async {
        final server = FakeServer()
          ..unreachable = (r) => r.url.host == 'account.otakase.com';
        final m = await signedInDevice(
          server,
          MemoryTokens(),
          await SharedPreferences.getInstance(),
        );
        expect(m.accounts.single.base, 'https://otakase.xyverion.com/account');
        await m.accounts.single.request('GET', '/sync?since=0');
        expect(server.requests.last, 'GET /account/sync');
      },
    );

    test('a removed device signs out here', () async {
      final server = FakeServer();
      final m = await signedInDevice(
        server,
        MemoryTokens(),
        await SharedPreferences.getInstance(),
      );
      final s = m.accounts.single;
      server.refreshTokens.clear();
      await expectLater(
        s.accessToken(force: true),
        throwsA(isA<AccountFailure>()),
      );
      expect(s.signedOut, isTrue);
      expect(m.accounts, isEmpty);
      expect(m.lastError, contains('signed out'));
    });
  });

  group('sync', () {
    test(
      'settings, profiles, locks and resume spots travel between devices',
      () async {
        final server = FakeServer();
        // Device A.
        final prefsA = await SharedPreferences.getInstance();
        final profilesA = await manager(preferences: prefsA);
        final accountsA = await signedInDevice(server, MemoryTokens(), prefsA);
        final syncA = AccountSync(
          session: accountsA.accounts.single,
          profiles: profilesA,
          preferences: prefsA,
          pushDelay: Duration.zero,
        );
        addTearDown(syncA.dispose);
        await profilesA.session!.store.set('SubOrDub', 'dub');
        await profilesA.session!.store.set('KeepScreenOn', true);
        await profilesA.session!.store.set('WorkingSources', {
          'al:1': 'animex',
        });
        await profilesA.session!.store.progress(
          Anime(id: 1, title: 'Show'),
          3,
          600,
          1400,
        );
        await syncA.start(upload: true);
        expect(
          server.docs.keys,
          containsAll(['1|profile', '1|settings', '1|shows', '1|resume']),
        );
        expect(
          (server.docs['1|settings']!['fields'] as Map).containsKey(
            'KeepScreenOn',
          ),
          isFalse,
        );

        // Device B: a fresh phone signing in to the same account.
        SharedPreferences.setMockInitialValues({});
        final prefsB = await SharedPreferences.getInstance();
        final profilesB = await manager(preferences: prefsB);
        final accountsB = await signedInDevice(server, MemoryTokens(), prefsB);
        final syncB = AccountSync(
          session: accountsB.accounts.single,
          profiles: profilesB,
          preferences: prefsB,
          pushDelay: Duration.zero,
        );
        addTearDown(syncB.dispose);
        await syncB.start(upload: false);
        // Both devices started with profile id 1, so it is the same profile.
        expect(profilesB.profiles.single.account, 'acc-1');
        expect(profilesB.session!.store.text('SubOrDub'), 'dub');
        expect(profilesB.session!.store.flag('KeepScreenOn'), isFalse);
        expect(
          profilesB.session!.store.preferredSource(Anime(id: 1, title: 'Show')),
          'animex',
        );
        expect(
          profilesB.session!.store.entry(Anime(id: 1, title: 'Show'))!.position,
          600,
        );

        // B renames, locks the profile and changes quality; A takes it.
        await profilesB.edit('1', name: 'Alice', color: profileColors[1]);
        await profilesB.setLock('1', 'UDLR');
        await profilesB.session!.store.set('Quality', '720');
        await syncB.sync();
        await syncA.sync();
        expect(profilesA.profiles.single.name, 'Alice');
        expect(profilesA.profiles.single.locked, isTrue);
        expect(
          profilesA.profiles.single.lockHash,
          profilesB.profiles.single.lockHash,
        );
        expect(profilesA.session!.store.text('Quality'), '720');
        // A's older, unsent change to the same field loses to B's newer one,
        // but its own new field still goes up.
        expect(profilesA.session!.store.text('SubOrDub'), 'dub');
      },
    );

    test('a switched-off part stays on the device', () async {
      final server = FakeServer();
      final prefs = await SharedPreferences.getInstance();
      final profiles = await manager(preferences: prefs);
      final accounts = await signedInDevice(server, MemoryTokens(), prefs);
      final sync = AccountSync(
        session: accounts.accounts.single,
        profiles: profiles,
        preferences: prefs,
      );
      addTearDown(sync.dispose);
      await sync.switches.set(SyncPart.settings, false);
      await profiles.session!.store.set('SubOrDub', 'dub');
      await sync.start(upload: true);
      expect(server.docs.containsKey('1|settings'), isFalse);
      expect(server.docs.containsKey('1|profile'), isTrue);
    });

    test('a profile deleted on one device goes on the other', () async {
      final server = FakeServer();
      final prefs = await SharedPreferences.getInstance();
      final profiles = await manager(preferences: prefs);
      final kids = await profiles.add('Kids');
      final accounts = await signedInDevice(server, MemoryTokens(), prefs);
      final sync = AccountSync(
        session: accounts.accounts.single,
        profiles: profiles,
        preferences: prefs,
      );
      addTearDown(sync.dispose);
      await sync.start(upload: true);
      expect(server.docs.containsKey('${kids.id}|profile'), isTrue);
      await profiles.delete(kids.id);
      await sync.sync();
      expect(
        (server.docs['${kids.id}|profile']!['fields'] as Map)['_deleted'],
        isTrue,
      );
      // Another device that still had it removes it.
      await profiles.upsertSynced('acc-1', 'zz', {'name': 'Guest'});
      expect(profiles.profiles.map((p) => p.name), contains('Guest'));
      await profiles.removeSynced('zz');
      expect(profiles.profiles.map((p) => p.name), isNot(contains('Guest')));
    });
  });

  group('vault', () {
    test('wraps, unwraps, seals and opens', () async {
      final keys = TokenStoreKeys(MemoryTokens());
      final pair = await keys.pair('a');
      final vaultKey = List<int>.generate(32, (i) => i);
      final wrapped = await wrapKey(vaultKey, await keys.publicKey('a'));
      expect(await unwrapKey(wrapped, pair), vaultKey);
      final other = await keys.pair('b');
      await expectLater(unwrapKey(wrapped, other), throwsA(anything));
      final sealed = await seal(vaultKey, {'account.AniList': 'secret'});
      expect(sealed, isNot(contains('secret')));
      expect(await open(vaultKey, sealed), {'account.AniList': 'secret'});
    });

    test('logins reach a second device once the first approves it', () async {
      final server = FakeServer();
      final tokensA = MemoryTokens(), tokensB = MemoryTokens();
      final prefsA = await SharedPreferences.getInstance();
      final a = await signedInDevice(server, tokensA, prefsA);
      // Device B has preferences of its own.
      SharedPreferences.setMockInitialValues({});
      final prefsB = await SharedPreferences.getInstance();
      final b = await signedInDevice(server, tokensB, prefsB);
      final storeA = MemoryTokens()
        ..values['account.AniList'] = '{"token":"t1"}';
      final storeB = MemoryTokens();
      var arrived = 0;
      final vaultA = AccountVault(
        session: a.accounts.single,
        keys: TokenStoreKeys(tokensA),
        preferences: prefsA,
        profileTokens: (_) => storeA,
        boundProfiles: () => ['1'],
      );
      final vaultB = AccountVault(
        session: b.accounts.single,
        keys: TokenStoreKeys(tokensB),
        preferences: prefsB,
        profileTokens: (_) => storeB,
        boundProfiles: () => ['1'],
        onLoginsChanged: (_) async => arrived++,
      );
      addTearDown(vaultA.dispose);
      addTearDown(vaultB.dispose);
      await vaultA.sync();
      expect(vaultA.hasKey, isTrue);
      expect(server.blobs['1']!['ciphertext'], isNot(contains('t1')));
      await vaultB.sync();
      expect(vaultB.hasKey, isFalse);
      expect(storeB.values, isEmpty);
      await vaultA.sync();
      expect(vaultA.waiting.single.id, 'dev-2');
      expect(vaultA.waiting.single.check, await vaultB.ownCheck());
      await vaultA.approve(vaultA.waiting.single);
      await vaultB.sync();
      expect(storeB.values['account.AniList'], '{"token":"t1"}');
      expect(arrived, 1);
      // MAL hands out a new token on refresh: the device that refreshed
      // sends it, the other takes it.
      storeB.values['account.AniList'] = '{"token":"t2"}';
      vaultB.changed('1', 'account.AniList', '{"token":"t2"}');
      await vaultB.sync();
      await vaultA.sync();
      expect(storeA.values['account.AniList'], '{"token":"t2"}');
      // A removed device: the next device with the key rotates it.
      server.stale = true;
      server.publicKeys.remove('dev-2');
      await vaultA.sync();
      expect(server.keyVersion, 2);
      expect(server.keys.keys, ['dev-1']);
      await vaultA.sync();
      expect(storeA.values['account.AniList'], '{"token":"t2"}');
    });
  });

  group('Discord through the account', () {
    test(
      'bot calls go to the account server, heartbeats over the live connection',
      () async {
        final seen = <String>[];
        final client = MockClient((r) async {
          seen.add('${r.url} ${r.headers['Authorization']}');
          return http.Response('{"url":"https://discord.com/x"}', 200);
        });
        final d = DiscordService(
          client: client,
          storage: MemoryTokens(),
          store: AppStore(await SharedPreferences.getInstance()),
        );
        addTearDown(d.close);
        final beats = <Json>[];
        d.bridge = _Bridge(beats);
        expect(d.linked, isTrue);
        expect(d.name, 'Alice');
        final r = await d.request('POST', '/v1/rating', body: {'score': 9});
        expect(r['url'], 'https://discord.com/x');
        expect(
          seen.where((s) => s.contains('/v1/rating')).single,
          'https://account.otakase.com/v1/rating Bearer access',
        );
        await d.heartbeat(null, 0, 30);
        expect(beats.last['watched_seconds'], 30);
        expect(await d.endpoint('/v1/party/abc/ws'), (
          'https://account.otakase.com',
          'access',
        ));
        expect(bridgedPath('/v1/inbox'), isFalse);
        expect(bridgedPath('/v1/party/a1/rsvp'), isTrue);
      },
    );
  });
}

class _Bridge implements DiscordBridge {
  _Bridge(this.beats);
  final List<Json> beats;
  @override
  bool get ready => true;
  @override
  String get name => 'Alice';
  @override
  String get base => 'https://account.otakase.com';
  @override
  Future<String> token() async => 'access';
  @override
  bool heartbeat(Json body) {
    beats.add(body);
    return true;
  }
}
