import 'dart:async';
import 'dart:convert';

import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/profiles.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'auth_test.dart' show MemoryTokens, account, json;

Future<String> fastLock(String salt, String code) async =>
    sha256.convert(utf8.encode('$salt:$code')).toString();

class RejectMigration extends MemoryTokens {
  bool reject = true;
  @override
  Future<void> write(String key, String? value) async {
    if (reject && key.startsWith('profile.') && value != null) {
      throw StateError('Storage unavailable');
    }
    await super.write(key, value);
  }
}

http.Client profileClient() => MockClient((r) async {
  if (r.url.host == 'api.myanimelist.net') return json({'data': []});
  final query = (jsonDecode(r.body) as Map)['query'] as String;
  if (query.contains('Viewer')) {
    return json({
      'data': {
        'Viewer': {
          'options': {'displayAdultContent': true},
        },
      },
    });
  }
  return json({
    'data': {
      'MediaListCollection': {
        'lists': [
          {
            'entries': [
              {
                'status': 'CURRENT',
                'progress': 3,
                'media': {
                  'id': 42,
                  'idMal': 24,
                  'title': {'romaji': 'Saved story'},
                },
              },
            ],
          },
        ],
      },
    },
  });
});
Future<ProfileManager> manager({
  bool tv = false,
  MemoryTokens? tokens,
  SharedPreferences? preferences,
  DateTime Function()? now,
  http.Client Function()? clientFactory,
}) async {
  final m = ProfileManager(
    preferences: preferences ?? await SharedPreferences.getInstance(),
    tokens: tokens ?? MemoryTokens(),
    core: CoreClient(),
    isTV: tv,
    clientFactory: clientFactory ?? profileClient,
    hashLock: fastLock,
    now: now,
    platformCallbacks: false,
  );
  await m.initialize();
  addTearDown(m.close);
  return m;
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));
  test('failed secure migration retains legacy data and retries without overwriting it', () async {
    SharedPreferences.setMockInitialValues({
      'settings.v1': jsonEncode({'SubsLanguage': 'lithuanian'}),
    });
    final tokens = RejectMigration();
    tokens.values['account.AniList'] = jsonEncode(account('AniList').toJson());
    final prefs = await SharedPreferences.getInstance();
    final m = ProfileManager(
      preferences: prefs,
      tokens: tokens,
      core: CoreClient(),
      isTV: false,
      clientFactory: profileClient,
      hashLock: fastLock,
      platformCallbacks: false,
    );
    addTearDown(m.close);
    await expectLater(m.initialize(), throwsStateError);
    expect(prefs.getString('profiles.v1'), isNull);
    expect(prefs.getString('settings.v1'), isNotNull);
    expect(tokens.values['account.AniList'], isNotNull);
    tokens.reject = false;
    await m.initialize();
    expect(m.session!.store.text('SubsLanguage'), 'lithuanian');
    expect(m.session!.auth.signedIn('AniList'), isTrue);
    expect(prefs.getString('settings.v1'), isNull);
  });
  test(
    'linked avatars and account visibility belong to each profile',
    () async {
      final tokens = MemoryTokens();
      tokens.values['account.AniList'] = jsonEncode({
        ...account('AniList').toJson(),
        'avatar': 'anilist-avatar',
      });
      final m = await manager(tokens: tokens);
      expect(m.profile('1').avatar, 'anilist-avatar');
      final second = await m.add('Family');
      await ProfileTokens(tokens, m.preferences, second.id).write(
        'account.MAL',
        jsonEncode({...account('MAL').toJson(), 'avatar': 'mal-avatar'}),
      );
      await m.switchTo(second.id);
      expect(m.active.avatar, 'mal-avatar');
      expect(m.session!.auth.signedIn('AniList'), isFalse);
      expect(m.session!.store.adultAllowed, isFalse);
      expect(m.profile('1').avatar, 'anilist-avatar');
      await m.session!.auth.logout('MAL');
      expect(m.active.avatar, '');
    },
  );
  test('existing install migrates settings, history, accounts and Discord into profile 1 once', () async {
    final tokens = MemoryTokens();
    tokens.values['account.AniList'] = jsonEncode(account('AniList').toJson());
    tokens.values['discord.link'] = jsonEncode({'token': 'discord-p1'});
    SharedPreferences.setMockInitialValues({
      'settings.v1': jsonEncode({'SubsLanguage': 'lithuanian'}),
      'history.v1': jsonEncode([
        WatchEntry(
          anime: const Anime(id: 42, title: 'Saved story'),
          episode: 4,
          position: 90,
        ).toJson(),
      ]),
    });
    final m = await manager(tokens: tokens);
    expect(m.profiles.single.id, '1');
    expect(m.session!.store.text('SubsLanguage'), 'lithuanian');
    expect(m.session!.store.history.single.position, 90);
    expect(m.session!.auth.account('AniList')!.username, 'viewer');
    expect(m.session!.store.adultAllowed, isTrue);
    expect(tokens.values.containsKey('account.AniList'), isFalse);
    expect(
      tokens.values['profile.1.account.AniList'],
      contains('access-AniList'),
    );
    expect(tokens.values['profile.1.discord.link'], contains('discord-p1'));
    expect(m.preferences.getString('history.v1'), isNull);
    await m.session!.store.set('SubsLanguage', 'japanese');
    await m.close();
    final reopened = await manager(tokens: tokens, preferences: m.preferences);
    expect(reopened.session!.store.text('SubsLanguage'), 'japanese');
    expect(reopened.session!.store.history.single.episode, 4);
  });
  test('switch awaits player stop and isolates accounts, settings, history, 18+ and list cache', () async {
    final tokens = MemoryTokens();
    tokens.values['account.AniList'] = jsonEncode(account('AniList').toJson());
    final m = await manager(tokens: tokens);
    final first = m.session!;
    await first.store.set('SubsLanguage', 'lithuanian');
    await first.store.progress(
      const Anime(id: 42, title: 'Saved story'),
      4,
      90,
      1200,
    );
    await first.tracking.list('AniList', adult: true);
    final discord = ProfileTokens(tokens, m.preferences, '1');
    await discord.write('discord.link', 'first-link');
    final next = await m.add('Family');
    final stopping = Completer<void>(), release = Completer<void>();
    first.stopPlayback = () async {
      stopping.complete();
      await release.future;
    };
    final changing = m.switchTo(next.id);
    await stopping.future;
    expect(m.activeId, '1');
    expect(m.busy, isTrue);
    release.complete();
    await changing;
    expect(first.closed, isTrue);
    expect(first.auth.accounts, isEmpty);
    expect(m.activeId, next.id);
    expect(m.session!.auth.hasAccounts, isFalse);
    expect(m.session!.store.adultAllowed, isFalse);
    expect(m.session!.store.history, isEmpty);
    expect(m.session!.store.text('SubsLanguage'), 'english');
    expect(m.session!.tracking.cachedList('AniList', adult: false), isNull);
    expect(
      await ProfileTokens(tokens, m.preferences, next.id).read('discord.link'),
      isNull,
    );
    await m.session!.store.set('SkipOp', false);
    await m.switchTo('1');
    expect(m.session!.store.history.single.episode, 4);
    expect(m.session!.store.flag('SkipOp'), isTrue);
    expect(
      m.session!.tracking
          .cachedList('AniList', adult: true)!
          .entries
          .single
          .progress,
      3,
    );
    expect(
      await ProfileTokens(tokens, m.preferences, '1').read('discord.link'),
      'first-link',
    );
  });
  test(
    'login state and verifier from one profile cannot authorize another',
    () async {
      final tokens = MemoryTokens();
      final m = await manager(tokens: tokens);
      final a = AuthService(
        storage: ProfileTokens(tokens, m.preferences, 'a'),
        client: profileClient(),
        findLanAddress: () async => '192.168.1.2',
      );
      final b = AuthService(
        storage: ProfileTokens(tokens, m.preferences, 'b'),
        client: profileClient(),
        findLanAddress: () async => '192.168.1.2',
      );
      final one = await a.begin('MAL', tv: true),
          two = await b.begin('MAL', tv: true);
      expect(one.queryParameters['state'], isNot(two.queryParameters['state']));
      await expectLater(
        b.accept(
          Uri(
            scheme: 'otakase',
            host: 'login',
            queryParameters: {
              'state': one.queryParameters['state']!,
              'code': 'wrong-profile',
            },
          ),
        ),
        throwsA(isA<LoginFailure>()),
      );
      expect(b.accounts, isEmpty);
      expect(
        tokens.values['profile.a.login.pending'],
        isNot(tokens.values['profile.b.login.pending']),
      );
      await a.cancelLogin();
      await b.cancelLogin();
      a.dispose();
      b.dispose();
    },
  );
  test('TV asks on start for multiple profiles, phone defaults off, one profile skips', () async {
    final tv = await manager(tv: true);
    expect(tv.session, isNotNull);
    expect(tv.askOnStart, isTrue);
    await tv.add('Second');
    await tv.close();
    final startup = await manager(tv: true, preferences: tv.preferences);
    expect(startup.session, isNull);
    await startup.setAskOnStart(false);
    await startup.initialize();
    expect(startup.session, isNotNull);
    SharedPreferences.setMockInitialValues({});
    final phone = await manager();
    expect(phone.askOnStart, isFalse);
  });
  test('profile direction locks are salted, five wrong attempts wait 30 seconds, permits are single use', () async {
    var clock = DateTime.utc(2026, 10, 28);
    final m = await manager(now: () => clock);
    final other = await m.add('Other');
    await m.setLock('1', 'UDLR');
    final p = m.profile('1');
    expect(p.lockHash, isNot('UDLR'));
    expect(p.lockSalt, isNotEmpty);
    expect(m.preferences.getString('profiles.v1'), isNot(contains('UDLR')));
    for (var i = 0; i < 5; i++) {
      expect(await m.unlock('1', 'UUUU'), isNull);
    }
    expect(m.waitSeconds('1'), 30);
    expect(await m.unlock('1', 'UDLR'), isNull);
    clock = clock.add(const Duration(seconds: 31));
    final permit = await m.unlock('1', 'UDLR');
    expect(permit, isNotNull);
    await m.switchTo(other.id);
    await expectLater(m.switchTo('1'), throwsA(isA<ProfileLocked>()));
    await m.switchTo('1', unlock: permit);
    await m.switchTo(other.id);
    await expectLater(
      m.switchTo('1', unlock: permit),
      throwsA(isA<ProfileLocked>()),
    );
    final remove = await m.unlock('1', 'UDLR');
    await m.removeLock('1', unlock: remove);
    expect(m.profile('1').locked, isFalse);
  });
  test('PBKDF2 lock hash matches the SHA-256 reference vector', () async {
    expect(
      await deriveProfileLock('salt', 'UDLR'),
      '4tTWnC-7UmJEzM2N72zERX9yt58A2RoT7_GolDZ7L3E=',
    );
  });
  test(
    'six profile limit and deletion clear only the selected secure namespace',
    () async {
      final tokens = MemoryTokens();
      final m = await manager(tokens: tokens);
      final target = await m.add('Delete me');
      final scoped = ProfileTokens(tokens, m.preferences, target.id);
      await scoped.write('account.AniList', 'account-token');
      await scoped.write('remote.pair.device', 'pair-token');
      await ProfileTokens(
        tokens,
        m.preferences,
        '1',
      ).write('account.MAL', 'keep');
      for (var i = 0; i < 4; i++) {
        await m.add('Profile $i');
      }
      expect(m.profiles, hasLength(6));
      await expectLater(m.add('Seventh'), throwsStateError);
      await m.delete(target.id);
      expect(
        await m.tokens.read('profile.${target.id}.account.AniList'),
        isNull,
      );
      expect(
        await m.tokens.read('profile.${target.id}.remote.pair.device'),
        isNull,
      );
      expect(
        m.preferences.getKeys().where(
          (key) => key.startsWith('profile.${target.id}.'),
        ),
        isEmpty,
      );
      expect(await tokens.read('profile.1.account.MAL'), 'keep');
      expect(m.profiles, hasLength(5));
    },
  );
  test(
    'late reads from a closed profile cannot populate a new profile cache',
    () async {
      final response = Completer<http.Response>(), started = Completer<void>();
      var clients = 0;
      final tokens = MemoryTokens();
      tokens.values['account.AniList'] = jsonEncode(
        account('AniList').toJson(),
      );
      final m = await manager(
        tokens: tokens,
        clientFactory: () {
          clients++;
          return clients == 1
              ? MockClient((r) async {
                  if ((jsonDecode(r.body)['query'] as String).contains(
                    'Viewer',
                  )) {
                    return json({
                      'data': {
                        'Viewer': {
                          'options': {'displayAdultContent': false},
                        },
                      },
                    });
                  }
                  started.complete();
                  return response.future;
                })
              : profileClient();
        },
      );
      final first = m.session!, next = await m.add('Next');
      final loading = first.tracking.list('AniList', adult: false);
      final checked = expectLater(loading, throwsA(isA<LoginFailure>()));
      await started.future;
      await m.switchTo(next.id);
      response.complete(
        json({
          'data': {
            'MediaListCollection': {
              'lists': [
                {
                  'entries': [
                    {
                      'status': 'CURRENT',
                      'progress': 9,
                      'media': {
                        'id': 99,
                        'title': {'romaji': 'Old profile'},
                      },
                    },
                  ],
                },
              ],
            },
          },
        }),
      );
      await checked;
      expect(m.session!.store.value('ListCache.AniList'), isNull);
      expect(m.session!.store.history, isEmpty);
    },
  );
}
