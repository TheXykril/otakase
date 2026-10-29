import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/data/continuation.dart';
import 'package:otakase/data/discord.dart';
import 'package:otakase/data/heritage.dart';
import 'package:otakase/data/lan_page.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/store.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'auth_test.dart' show MemoryTokens, account, json;

final date = DateTime(2027, 4, 12, 10);
const show = Anime(id: 42, malId: 24, title: 'A story', episodes: 12);
Future<({DiscordService discord, AppStore store, MemoryTokens tokens})>
discordFixture(
  Future<http.Response> Function(http.Request) handler, {
  bool linked = true,
  String namespace = '',
}) async {
  final tokens = MemoryTokens();
  if (linked) {
    tokens.values['discord.link'] = jsonEncode({
      'token': 'app-secret',
      'discord_name': 'Reader',
    });
  }
  final store = AppStore(
    await SharedPreferences.getInstance(),
    namespace: namespace,
  );
  final d = DiscordService(
    client: MockClient(handler),
    storage: tokens,
    store: store,
    now: () => date,
  );
  await d.initialize(background: false);
  addTearDown(d.close);
  return (discord: d, store: store, tokens: tokens);
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));
  test(
    'watch meter follows CLI seek, speed, fractions, pause and restart rules',
    () {
      final m = WatchMeter();
      m.observe(0, date);
      m.observe(.6, date.add(const Duration(milliseconds: 600)));
      expect(m.take(), 0);
      m.observe(1.2, date.add(const Duration(milliseconds: 1200)));
      expect(m.take(), 1);
      m.observe(80, date.add(const Duration(seconds: 2)));
      expect(m.take(), 0);
      m.observe(80, date.add(const Duration(seconds: 4)));
      expect(m.take(), 0);
      m.observe(88, date.add(const Duration(seconds: 6)));
      expect(m.take(), 2);
      m.restart();
      m.observe(0, date.add(const Duration(seconds: 8)));
      m.observe(.8, date.add(const Duration(milliseconds: 8800)));
      expect(m.take(), 1);
    },
  );
  test(
    'link uses CLI code and poll API, secure key, cancellation and expiry',
    () async {
      final calls = <http.Request>[];
      var pending = true;
      final f = await discordFixture((r) async {
        calls.add(r);
        if (r.url.path == '/v1/link/start') {
          return json({'code': 'ABCD', 'poll': 'poll-secret', 'expires_in': 0});
        }
        if (r.url.path == '/v1/link/poll') {
          expect(jsonDecode(r.body), {'poll': 'poll-secret'});
          if (pending) return json({}, 202);
          return json({'token': 'new-token', 'discord_name': 'New name'});
        }
        if (r.url.path == '/v1/inbox') return Completer<http.Response>().future;
        return json({});
      }, linked: false);
      final code = await f.discord.beginLink();
      expect(code.expires, date.add(const Duration(minutes: 10)));
      expect(await f.discord.pollLink(code), false);
      pending = false;
      expect(await f.discord.pollLink(code), true);
      expect(
        jsonDecode(f.tokens.values['discord.link']!)['token'],
        'new-token',
      );
      expect(f.discord.name, 'New name');
      expect(calls.first.headers.containsKey('authorization'), false);
      await expectLater(
        f.discord.pollLink(DiscordLinkCode('old', 'x', date)),
        throwsA(isA<BotFailure>().having((e) => e.status, 'status', 410)),
      );
    },
  );
  test('heartbeat defaults hide watching, opt-in uses exact fields and serial closing', () async {
    final bodies = <Json>[];
    final f = await discordFixture((r) async {
      bodies.add((jsonDecode(r.body) as Map).cast<String, dynamic>());
      expect(r.headers['authorization'], 'Bearer app-secret');
      return json({'discord_name': 'Renamed'});
    });
    await f.discord.heartbeat(show, 3, -2);
    expect(bodies.single, {
      'version': appVersion,
      'dev_build': false,
      'os': 'android',
      'share_watching': false,
      'watching': null,
      'watched_seconds': 0,
    });
    await f.store.set('DiscordShareWatching', true);
    f.discord.observe(show, 3, 0);
    await f.discord.stopWatching();
    expect(bodies[1]['watching'], {
      'anilist_id': 42,
      'title': 'A story',
      'episode': 3,
      'episodes': 12,
    });
    expect(bodies.last['watching'], null);
    expect(f.discord.name, 'Renamed');
  });
  test(
    'inbox acks expired and unknown entries and only applies allowed settings',
    () async {
      final actions = <String>[];
      List? ack;
      var round = 0;
      final f = await discordFixture((r) async {
        if (r.method == 'GET') {
          expect(r.url.queryParameters['wait'], '25');
          return json({
            'items': [
              {
                'id': 'q$round',
                'kind': 'queue',
                'anilist_id': 42,
                'title': 'A story',
              },
              {'id': 'n', 'kind': 'notice', 'title': 'Reply'},
              {
                'id': 's',
                'kind': 'setting',
                'key': 'DevBuilds',
                'value': 'true',
              },
              {
                'id': 'a',
                'kind': 'setting',
                'key': 'AdultContent',
                'value': 'true',
              },
              {'id': 'r', 'kind': 'remote', 'key': 'skip'},
              {
                'id': 'x',
                'kind': 'remote',
                'key': 'stop',
                'expires': date
                    .subtract(const Duration(seconds: 1))
                    .toIso8601String(),
              },
              {'id': 'u', 'kind': 'unknown'},
            ],
          });
        }
        expect(r.url.path, '/v1/inbox/ack');
        ack = jsonDecode(r.body)['ids'] as List;
        return json({});
      });
      f.discord.remote = (action, args) async {
        actions.add(action);
      };
      await f.discord.pollInbox();
      round++;
      await f.discord.pollInbox();
      expect(ack, ['q1', 'n', 's', 'a', 'r', 'x', 'u']);
      expect(actions, ['skip', 'skip']);
      expect(f.store.flag('DevBuilds'), true);
      expect(f.store.adultAllowed, false);
      expect(f.discord.inbox.where((i) => i['kind'] == 'queue').length, 1);
      expect(f.discord.inbox.where((i) => i['kind'] == 'notice').length, 2);
    },
  );
  test('401 forgets the token with revoked identity, offline unlink forgets it too', () async {
    final f = await discordFixture(
      (r) async => json({'error': 'revoked'}, 401),
    );
    await expectLater(
      f.discord.request('GET', '/v1/inbox'),
      throwsA(isA<BotFailure>().having((e) => e.status, 'status', 401)),
    );
    expect(f.discord.linked, false);
    expect(f.discord.revoked, true);
    expect(f.discord.name, 'Reader');
    expect(jsonDecode(f.tokens.values['discord.link']!), {
      'discord_name': 'Reader',
      'revoked': true,
    });
    final other = await discordFixture(
      (r) async => throw const SocketException('offline'),
    );
    expect(await other.discord.unlink(), false);
    expect(other.tokens.values.containsKey('discord.link'), false);
  });
  test('provider reports redact private text, suppress repeated failures and cap at three', () async {
    final reports = <Json>[];
    final f = await discordFixture((r) async {
      reports.add((jsonDecode(r.body) as Map).cast<String, dynamic>());
      return json({});
    });
    for (var i = 0; i < 5; i++) {
      await f.discord.reportProvider(
        'source',
        false,
        'search',
        'Failed "A private title" at https://media.example/secret?a=1\n reason',
      );
      await f.discord.reportProvider('source', false, 'episode', 'again');
      await f.discord.reportProvider('source', true, 'playback', '');
      await f.discord.reportProvider('source', true, 'playback', '');
    }
    expect(reports.where((r) => r['ok'] == false).length, 3);
    expect(reports.first, {
      'provider': 'source',
      'ok': false,
      'kind': 'search',
      'error': 'Failed … at <url> reason',
    });
    expect(reports.every((r) => !r.containsKey('title')), true);
  });
  test('ratings ask, never share adult or invalid IDs, obey cached server switch and clip notes', () async {
    Json? body;
    final f = await discordFixture((r) async {
      body = (jsonDecode(r.body) as Map).cast<String, dynamic>();
      return json({'url': 'https://discord.com/channels/x'});
    });
    expect(f.discord.ratingWanted(show, 8.5), true);
    expect(
      f.discord.ratingWanted(
        const Anime(id: 42, title: 'Hidden', isAdult: true),
        8,
      ),
      false,
    );
    expect(
      f.discord.ratingWanted(const Anime(id: 0, title: 'MAL only'), 8),
      false,
    );
    expect(f.discord.ratingWanted(show, double.nan), false);
    await f.discord.shareRating(show, 8.5, text: '🦊' * 350);
    expect(body!['score'], 8.5);
    expect((body!['text'] as String).runes.length, 300);
    await f.store.set('DiscordNews', {
      'score_sharing': false,
      'fetched_at': date.toIso8601String(),
    });
    expect(f.discord.ratingWanted(show, 8), false);
    await f.store.set('DiscordNews', {
      'score_sharing': false,
      'fetched_at': date.subtract(const Duration(hours: 25)).toIso8601String(),
    });
    expect(f.discord.ratingWanted(show, 8), true);
    await f.store.set('DiscordShareRatings', 'never');
    expect(f.discord.ratingWanted(show, 8), false);
  });
  test('Touch Grass uses 1/100 then 1/10, pending survives offline and claims once', () async {
    var offline = true, calls = 0;
    final f = await discordFixture((r) async {
      calls++;
      if (offline) throw const SocketException('offline');
      return json({'new': true, 'finders': 2});
    });
    final odds = <int>[];
    final line = await f.discord.exitGreeting(
      roll: (n) {
        odds.add(n);
        return 0;
      },
    );
    expect(odds, [100, 3, 10]);
    expect(line, contains('Rare drop'));
    expect(f.store.text('TouchGrass'), 'pending');
    offline = false;
    await f.discord.claimPendingGrass();
    expect(f.store.text('TouchGrass'), 'claimed');
    await f.discord.claimPendingGrass();
    expect(calls, 2);
    odds.clear();
    await f.discord.exitGreeting(
      roll: (n) {
        odds.add(n);
        return 0;
      },
    );
    expect(odds, [100, 3]);
  });
  test('Continue hides caught-up airing and finished shows, preserves real resume and filters', () {
    final airing = Anime(
      id: 1,
      title: 'Airing',
      status: 'RELEASING',
      episodes: 12,
      nextEpisode: 9,
      nextAiringAt:
          date.add(const Duration(days: 2, hours: 4)).millisecondsSinceEpoch ~/
          1000,
    );
    final c = buildContinuation(
      [
        ListEntry(airing, 'CURRENT', 8),
        const ListEntry(show, 'CURRENT', 3, notes: 'Other note [otakase:4@60]'),
        const ListEntry(
          Anime(id: 2, title: 'Finished', episodes: 12),
          'CURRENT',
          12,
        ),
        const ListEntry(
          Anime(id: 3, title: 'Hidden', isAdult: true),
          'CURRENT',
          0,
        ),
        const ListEntry(
          Anime(id: 4, title: 'Music', format: 'MUSIC'),
          'CURRENT',
          0,
        ),
        const ListEntry(Anime(id: 5, title: 'Off list'), 'PAUSED', 2),
      ],
      [
        WatchEntry(
          anime: const Anime(id: 5, title: 'Off list'),
          episode: 3,
          position: 40,
        ),
      ],
      adult: false,
      now: date,
    );
    expect(c.continueWatching.map((e) => e.anime.id), [42]);
    expect(c.continueWatching.single.episode, 4);
    expect(c.continueWatching.single.position, 60);
    expect(c.upNext.single.caption(date), 'Ep 9 · in 2 d 4 h');
  });
  test(
    'Continue follows newest list state over old local completed progress',
    () {
      final c = buildContinuation(
        [ListEntry(show, 'CURRENT', 2, updatedAt: date)],
        [
          WatchEntry(
            anime: show,
            episode: 10,
            watched: List.generate(9, (i) => i + 1),
            updated: date
                .subtract(const Duration(days: 1))
                .millisecondsSinceEpoch,
          ),
        ],
        adult: false,
        now: date,
      );
      expect(c.continueWatching.single.episode, 3);
      final tiny = buildContinuation(
        [const ListEntry(show, 'CURRENT', 2, notes: '[otakase:3@9]')],
        [],
        adult: false,
        now: date,
      );
      expect(tiny.continueWatching.single.position, 0);
    },
  );
  test(
    'capsule date, replacement, due letters and storage match birthday rules',
    () async {
      final f = await discordFixture((r) async => json({}, 404), linked: false);
      final auth = AuthService(
        client: MockClient((r) async => json({})),
        storage: MemoryTokens(),
      );
      final h = Heritage(
        f.store,
        StatsRepository(auth),
        f.discord,
        now: () => date,
      );
      expect(nextBirthday(DateTime(2027, 4, 11)), 2027);
      expect(nextBirthday(date), 2028);
      expect(
        capsuleDate(current: date, override: '2030-04-12', debug: false),
        date,
      );
      expect(
        capsuleDate(current: date, override: '2030-04-12', debug: true),
        DateTime(2030, 4, 12, 10),
      );
      expect(
        capsuleDate(current: date, override: '2030-02-30', debug: true),
        date,
      );
      await h.writeLetter('first', snapshot: const WatchStats(episodes: 10));
      await h.writeLetter('replacement');
      expect(h.letters.length, 1);
      expect(h.pending!['text'], 'replacement');
      expect(h.dueLetters(2027), isEmpty);
      expect(h.dueLetters(2029), [0]);
      await h.markOpened(0);
      expect(h.dueLetters(2029), isEmpty);
      await h.markOffered(2027);
      await h.birthdayCommunity(2027);
      expect(h.capsule['offered'], 2027);
      expect(h.capsule['community_shown'], 2027);
      auth.dispose();
    },
  );
  test('capsule community uses existing API and retains server errors for revision', () async {
    final calls = <http.Request>[];
    final f = await discordFixture((r) async {
      calls.add(r);
      if (r.method == 'POST' && r.url.path == '/v1/capsule') {
        return json({'error': 'Please revise this message'}, 422);
      }
      return json({'text': 'old', 'opens': 2028});
    });
    final auth = AuthService(
      client: MockClient((r) async => json({})),
      storage: MemoryTokens(),
    );
    final h = Heritage(
      f.store,
      StatsRepository(auth),
      f.discord,
      now: () => date,
    );
    expect((await h.communityMine())!['text'], 'old');
    await expectLater(
      h.writeCommunity('🦊' * 350),
      throwsA(isA<BotFailure>().having((e) => e.status, 'status', 422)),
    );
    expect((jsonDecode(calls.last.body)['text'] as String).runes.length, 300);
    await h.reportCommunity('id');
    expect(jsonDecode(calls.last.body), {'id': 'id'});
    await h.removeCommunity();
    expect(calls.last.method, 'DELETE');
    auth.dispose();
  });
  test(
    'milestones baseline quietly, highest threshold once, remote progress only',
    () async {
      expect(milestoneToCelebrate(600, 0, false), (play: 0, record: 500));
      expect(milestoneToCelebrate(600, 250, true), (play: 500, record: 500));
      expect(milestoneToCelebrate(100, 500, true), (play: 0, record: 500));
      var total = 49, calls = 0;
      final f = await discordFixture((r) async => json({}));
      final auth = AuthService(
        storage: MemoryTokens(),
        client: MockClient((r) async {
          calls++;
          return json({
            'data': {
              'Viewer': {
                'statistics': {
                  'anime': {'episodesWatched': total},
                },
              },
            },
          });
        }),
      );
      auth.accounts['AniList'] = account('AniList');
      final h = Heritage(f.store, StatsRepository(auth), f.discord);
      expect(await h.milestoneOnExit(), 0);
      expect(calls, 0);
      h.progressWritten = true;
      expect(await h.milestoneOnExit(), 0);
      expect(f.store.integer('Milestone'), 0);
      total = 51;
      expect(await h.milestoneOnExit(), 50);
      expect(await h.milestoneOnExit(), 0);
      auth.dispose();
    },
  );
  test(
    'stats prefer AniList and fall back to MAL using fields query',
    () async {
      final auth = AuthService(
        storage: MemoryTokens(),
        client: MockClient((r) async {
          if (r.url.host == 'graphql.anilist.co') return json({}, 503);
          expect(r.url.path, '/v2/users/@me');
          expect(r.url.queryParameters['fields'], 'anime_statistics');
          return json({
            'anime_statistics': {
              'num_items': 5,
              'num_episodes': 100,
              'num_days_watched': 2.5,
              'num_items_completed': 3,
              'mean_score': 8.2,
            },
          });
        }),
      );
      auth.accounts.addAll({
        'AniList': account('AniList'),
        'MAL': account('MAL'),
      });
      final stats = StatsRepository(auth);
      expect(await stats.totalEpisodes(), 100);
      final snapshot = (await stats.snapshot())!;
      expect(snapshot.minutes, 3600);
      expect(snapshot.completed, 3);
      auth.dispose();
    },
  );
  test('phone text page keeps nonce local, accepts top-level form, escapes and expires', () async {
    final page = LanPage(
      findAddress: () async => '127.0.0.1',
      allowLoopback: true,
    );
    final client = http.Client();
    addTearDown(client.close);
    addTearDown(page.close);
    final url = await page.open(
      title: 'Capsule',
      message: '<script>never</script>',
      textInput: true,
      maxLength: 10,
    );
    final view = await client.get(url);
    expect(view.body, contains('&lt;script&gt;'));
    expect(view.body, contains('method="post"'));
    expect(view.body, isNot(contains('fetch(')));
    expect(
      (await client.get(url.replace(path: '/input/wrong'))).statusCode,
      403,
    );
    expect(
      (await client.post(
        url,
        body: {'text': 'too many characters'},
      )).statusCode,
      400,
    );
    final input = page.input.first;
    expect((await client.post(url, body: {'text': 'hello'})).statusCode, 200);
    expect(await input, 'hello');
    expect(
      (await client.post(
        url,
        headers: {'Origin': 'https://outside.example'},
        body: {'text': 'bad'},
      )).statusCode,
      403,
    );
    await page.closeServer();
    expect(privateAddress(InternetAddress('10.0.0.2')), true);
    expect(privateAddress(InternetAddress('8.8.8.8')), false);
  });
}
