import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/data/discord.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/store.dart';
import 'package:otakase/data/watch_party.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'auth_test.dart' show MemoryTokens, json;

class FakeSocket implements PartySocket {
  final incoming = StreamController<dynamic>();
  final sent = <Json>[];
  bool closed = false;
  @override
  Stream<dynamic> get messages => incoming.stream;
  @override
  void send(String text) =>
      sent.add((jsonDecode(text) as Map).cast<String, dynamic>());
  @override
  Future<void> close() async {
    closed = true;
    await incoming.close();
  }

  void push(Json m) => incoming.add(jsonEncode(m));
}

Future<(WatchParties, FakeSocket, List<http.Request>)> fixture({
  bool host = false,
  Future<http.Response> Function(http.Request)? bot,
  DateTime Function()? now,
}) async {
  final tokens = MemoryTokens();
  tokens.values['discord.link'] = jsonEncode({
    'token': 'app-secret',
    'discord_name': 'Reader',
  });
  final store = AppStore(await SharedPreferences.getInstance());
  final calls = <http.Request>[];
  final discord = DiscordService(
    client: MockClient((r) async {
      calls.add(r);
      return bot == null ? json({}) : await bot(r);
    }),
    storage: tokens,
    store: store,
  );
  await discord.initialize(background: false);
  final socket = FakeSocket();
  Uri? dialled;
  Map<String, String>? headers;
  final p = WatchParties(
    discord: discord,
    store: store,
    now: now,
    connect: (url, h) async {
      dialled = url;
      headers = h;
      scheduleMicrotask(
        () => socket.push({'type': 'welcome', 'host': host, 'show': 'Frieren'}),
      );
      return socket;
    },
  );
  addTearDown(() async {
    await p.close();
    await discord.close();
  });
  addTearDown(() {
    expect(dialled?.scheme, anyOf(isNull, 'wss'));
    if (headers != null) {
      expect(headers!['Authorization'], 'Bearer app-secret');
    }
  });
  return (p, socket, calls);
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  test('party ids come from ids, join links, app links and event links', () {
    expect(parsePartyId(' 1234567890123456789 '), '1234567890123456789');
    expect(
      parsePartyId('https://otakase.xyverion.com/open/?party=1234567890123'),
      '1234567890123',
    );
    expect(parsePartyId('otakase://party/1234567890123'), '1234567890123');
    expect(
      parsePartyId('https://discord.com/events/111/2222222222'),
      '2222222222',
    );
    expect(parsePartyId('frieren'), isNull);
    expect(parsePartyId('https://example.com/abc'), isNull);
    expect(
      partySocketUrl('https://bot.xyverion.com', '42').toString(),
      'wss://bot.xyverion.com/v1/party/42/ws',
    );
    expect(
      partySocketUrl('http://localhost:8080/', '42').toString(),
      'ws://localhost:8080/v1/party/42/ws',
    );
  });

  test('member sync follows the CLI rules', () {
    const h = PartyHostState(
      anilistId: 1,
      episode: 3,
      playing: true,
      position: 100,
      sentAt: 10000,
    );
    expect(partyHostPositionAt(h, 12000), 102);
    expect(partyHostPositionAt(h, 100000), 130, reason: 'age capped at 30 s');
    expect(decidePartySync(h, null, 10000).open, isTrue);
    PartyLocal local({
      int episode = 3,
      bool paused = false,
      double position = 100,
      Duration since = const Duration(seconds: 10),
    }) => PartyLocal(
      anilistId: 1,
      episode: episode,
      loaded: true,
      paused: paused,
      position: position,
      sinceSeek: since,
    );
    expect(decidePartySync(h, local(episode: 2), 10000).switchEpisode, isTrue);
    final close = decidePartySync(h, local(position: 101), 10000);
    expect(close.seek || close.pause || close.play, isFalse);
    final far = decidePartySync(h, local(position: 50), 10000);
    expect(far.seek, isTrue);
    expect(far.seekTo, closeTo(100.3, .001));
    expect(
      decidePartySync(
        h,
        local(position: 50, since: const Duration(seconds: 1)),
        10000,
      ).seek,
      isFalse,
      reason: 'a seek gets time to land',
    );
    expect(decidePartySync(h, local(paused: true), 10000).play, isTrue);
    const paused = PartyHostState(
      anilistId: 1,
      episode: 3,
      playing: false,
      position: 100,
      sentAt: 10000,
    );
    expect(decidePartySync(paused, local(), 50000).pause, isTrue);
  });

  test('host sends on changes, seeks and every five seconds', () {
    final t0 = DateTime(2026, 11, 1);
    const s = PartyHostState(
      anilistId: 1,
      episode: 1,
      playing: true,
      position: 10,
    );
    expect(partyShouldSend(null, t0, s, t0), isTrue);
    const none = PartyHostState(
      anilistId: 0,
      episode: 0,
      playing: false,
      position: 0,
    );
    expect(partyShouldSend(null, t0, none, t0), isFalse);
    const later = PartyHostState(
      anilistId: 1,
      episode: 1,
      playing: true,
      position: 12,
    );
    expect(
      partyShouldSend(s, t0, later, t0.add(const Duration(seconds: 2))),
      isFalse,
    );
    const jumped = PartyHostState(
      anilistId: 1,
      episode: 1,
      playing: true,
      position: 60,
    );
    expect(
      partyShouldSend(s, t0, jumped, t0.add(const Duration(seconds: 2))),
      isTrue,
    );
    expect(
      partyShouldSend(s, t0, later, t0.add(const Duration(seconds: 5))),
      isTrue,
    );
    expect(partyBackoff(0), const Duration(seconds: 1));
    expect(partyBackoff(3), const Duration(seconds: 8));
    expect(partyBackoff(9), const Duration(seconds: 30));
    final c = PartyClock();
    c.sample(1000, 1200, 5000);
    expect(c.serverNow(1100), 5000);
    c.sample(2000, 2600, 9999);
    expect(c.serverNow(1100), 5000, reason: 'the shortest trip wins');
  });

  test('planned list, RSVP and start labels', () async {
    final (p, _, calls) = await fixture(
      bot: (r) async => r.url.path == '/v1/parties'
          ? json({
              'parties': [
                {
                  'id': '123',
                  'title': 'Frieren',
                  'start': '2026-11-02T19:00:00Z',
                  'anilist_id': 154587,
                  'sync': true,
                  'going': 3,
                  'live': false,
                },
              ],
            })
          : json({'going': true, 'count': 4}),
    );
    final list = await p.planned();
    expect(list.single.title, 'Frieren');
    expect(list.single.sync, isTrue);
    final r = await p.rsvp('123', true);
    expect(r.count, 4);
    expect(calls.last.url.path, '/v1/party/123/rsvp');
    expect(jsonDecode(calls.last.body), {'going': true});
    final now = DateTime(2026, 11, 1, 12);
    expect(partyStartLabel(DateTime(2026, 11, 1, 20, 5), now), 'today 20:05');
    expect(partyStartLabel(DateTime(2026, 11, 2, 9), now), 'tomorrow 09:00');
    expect(partyStartLabel(DateTime(2026, 11, 1, 9), now), 'started 09:00');
    expect(partyStartLabel(DateTime(2026, 11, 5, 9), now), 'Thu 5 Nov 09:00');
  });

  test(
    'a member opens, switches, pauses and seeks to follow the host',
    () async {
      var clock = DateTime(2026, 11, 1, 20);
      final (p, socket, _) = await fixture(now: () => clock);
      final commands = <String>[];
      final opened = <(int, int)>[];
      Json? player;
      p.status = () => player;
      p.command = (action, args) async =>
          commands.add('$action ${args.values.join()}'.trim());
      p.open = (id, ep) async => opened.add((id, ep));
      await p.join('https://otakase.xyverion.com/open/?party=123456');
      expect(p.active, isTrue);
      expect(p.hosting, isFalse);
      expect(socket.sent.first['type'], 'hello');
      socket.push({
        'type': 'state',
        'anilist_id': 5,
        'episode': 2,
        'playing': true,
        'position': 40.0,
        'sent_at': 0,
      });
      await pumpEventQueue();
      expect(opened, [(5, 2)]);
      player = {
        'has_player': true,
        'anilist_id': 5,
        'episode': 1,
        'loaded': true,
        'playing': true,
        'position_ms': 0,
      };
      await p.memberTick();
      expect(opened, hasLength(1), reason: 'no second open while one loads');
      clock = clock.add(const Duration(seconds: 91));
      await p.memberTick();
      expect(commands, contains('episode 2'));
      player = {
        ...player,
        'episode': 2,
        'playing': false,
        'position_ms': 10000,
      };
      clock = clock.add(const Duration(seconds: 91));
      await p.memberTick();
      expect(commands, containsAll(['play', 'seek_to 40.3']));
      expect(
        socket.sent.where((m) => m['type'] == 'ready').single['episode'],
        2,
      );
      socket.push({'type': 'chat', 'name': 'Alex', 'text': 'hi'});
      socket.push({'type': 'votes', 'what': 'skip', 'votes': 1, 'needed': 2});
      await pumpEventQueue();
      expect(p.recentLines().single.text, 'hi');
      expect(p.note, '1 of 2 want to skip ahead');
      p.vote('next');
      expect(socket.sent.last, {'type': 'vote', 'what': 'next'});
      socket.push({'type': 'ended'});
      await pumpEventQueue();
      expect(p.active, isFalse);
    },
  );

  test('the host sends its state and does what the party voted', () async {
    final clock = DateTime(2026, 11, 1, 20);
    final (p, socket, _) = await fixture(host: true, now: () => clock);
    final commands = <String>[];
    p.status = () => {
      'has_player': true,
      'anilist_id': 9,
      'episode': 4,
      'loaded': true,
      'playing': true,
      'position_ms': 61500,
    };
    p.command = (action, _) async => commands.add(action);
    await p.join('987654');
    expect(p.hosting, isTrue);
    await p.hostTick();
    final state = socket.sent.lastWhere((m) => m['type'] == 'state');
    expect(state['anilist_id'], 9);
    expect(state['episode'], 4);
    expect(state['position'], 61.5);
    await p.hostTick();
    expect(socket.sent.where((m) => m['type'] == 'state'), hasLength(1));
    socket.push({'type': 'voted', 'what': 'skip'});
    socket.push({'type': 'voted', 'what': 'next'});
    socket.push({
      'type': 'members',
      'list': [
        {'name': 'Me', 'host': true},
        {'name': 'Alex', 'ready': true},
      ],
    });
    await pumpEventQueue();
    expect(commands, ['party_skip', 'next']);
    expect(p.watching, 1);
  });

  test('Watch with me hosts a party of its own and ends it', () async {
    var clock = DateTime(2026, 11, 1, 20);
    final (p, socket, calls) = await fixture(
      host: true,
      now: () => clock,
      bot: (r) async =>
          json({'id': '55555', 'join_url': 'https://x/open/?party=55555'}),
    );
    const anime = Anime(id: 7, title: 'A show');
    await p.playing(anime, 3);
    expect(p.active, isFalse, reason: 'off unless the setting is on');
    await p.store.set('DiscordWatchWithMe', true);
    await p.playing(const Anime(id: 8, title: 'Adult', isAdult: true), 1);
    expect(calls, isEmpty, reason: '18+ shows are never shared');
    await p.playing(anime, 3);
    expect(calls.single.url.path, '/v1/party/start');
    expect(jsonDecode(calls.single.body), {
      'anilist_id': 7,
      'episode': 3,
      'title': 'A show',
    });
    expect(p.active && p.watchWithMe && p.hosting, isTrue);
    expect(p.joinUrl, 'https://x/open/?party=55555');
    p.status = () => null;
    clock = clock.add(const Duration(seconds: 91));
    await p.hostTick();
    expect(socket.sent.any((m) => m['type'] == 'end'), isTrue);
    expect(p.active, isFalse);
  });

  test('a refused join says why', () async {
    final tokens = MemoryTokens();
    tokens.values['discord.link'] = jsonEncode({'token': 't'});
    final store = AppStore(await SharedPreferences.getInstance());
    final discord = DiscordService(
      client: MockClient((r) async => json({'error': 'no party'}, 404)),
      storage: tokens,
      store: store,
    );
    await discord.initialize(background: false);
    final p = WatchParties(
      discord: discord,
      store: store,
      connect: (_, _) async => throw StateError('not upgraded'),
    );
    addTearDown(() async {
      await p.close();
      await discord.close();
    });
    await expectLater(
      p.join('123'),
      throwsA(
        isA<PartyGone>().having(
          (e) => e.message,
          'message',
          contains('no synced party'),
        ),
      ),
    );
    await expectLater(p.join('nope'), throwsA(isA<PartyGone>()));
  });
}
