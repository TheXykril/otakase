import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/data/discord.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/store.dart';
import 'package:otakase/data/watch_rooms.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'auth_test.dart' show MemoryTokens, json;

Json roomFixture() => (jsonDecode(
  File('test/fixtures/watch_room.json').readAsStringSync(),
) as Map).cast<String, dynamic>();
final now = DateTime.utc(2027, 1, 1);
Future<({WatchRooms rooms, DiscordService discord, AppStore store})> fixture(
  Future<http.Response> Function(http.Request) handler, {
  bool polling = false,
}) async {
  final store = AppStore(await SharedPreferences.getInstance());
  final tokens = MemoryTokens();
  tokens.values['discord.link'] = jsonEncode({
    'token': 'app-token',
    'discord_name': 'Reader',
  });
  final discord = DiscordService(
    client: MockClient(handler),
    storage: tokens,
    store: store,
    now: () => now,
  );
  await discord.initialize(background: false);
  final rooms = WatchRooms(
    discord: discord,
    store: store,
    now: () => now,
    polling: polling,
  );
  addTearDown(() async {
    await rooms.close();
    await discord.close();
  });
  return (rooms: rooms, discord: discord, store: store);
}

void main() {
  test('room subtitles follow profile language order and off preference', () {
    final tracks = <Json>[
      {'lang': 'ja', 'label': 'Japanese'},
      {'lang': 'en', 'label': 'English'},
    ];
    expect(roomSubtitleTrack(tracks, 'french,english')!['lang'], 'en');
    expect(roomSubtitleTrack(tracks, 'french,off'), null);
    expect(roomSubtitleTrack(tracks, 'none,english'), null);
    expect(roomSubtitleTrack(tracks, 'Japanese')!['lang'], 'ja');
  });
  setUp(() => SharedPreferences.setMockInitialValues({}));
  test('room clock, host jumps, ready hold and Activity drift rules', () {
    final s = RoomState(roomFixture(), now);
    expect(s.anime!.malId, 24);
    expect(s.episode, 4);
    expect(s.target(now.add(const Duration(seconds: 2))), 32);
    expect(
      RoomState({
        ...s.json,
        'pos': 1199,
      }, now).target(now.add(const Duration(seconds: 10))),
      1200,
    );
    expect(
      RoomState({
        ...s.json,
        'playing': false,
      }, now).target(now.add(const Duration(seconds: 10))),
      30,
    );
    expect(followRoom(position: 32, target: 30, playing: true).seek, null);
    expect(followRoom(position: 33, target: 30, playing: true).seek, 30);
    expect(followRoom(position: 27, target: 30, playing: true).rate, 1.15);
    expect(followRoom(position: 20, target: 30, playing: true).behind, 10);
    expect(
      followRoom(position: 20, target: 30, playing: true, force: true).seek,
      30,
    );
    expect(
      followRoom(position: 28.8, target: 30, playing: false, hold: true).seek,
      30,
    );
    expect(
      RoomState({
        ...s.json,
        'pos': 80,
      }, now.add(const Duration(seconds: 1))).jumpedFrom(s),
      true,
    );
  });
  test(
    'lobby shows joinable rooms and follows adult and link changes',
    () async {
      final f = await fixture(
        (r) async => json({
          'rooms': [
            {'id': 'room-1', 'show': 'Shared story', 'people': 1, 'max': 8},
            {'id': 'private', 'adult': true},
            {'adult': true, 'channel': 'private'},
          ],
        }),
      );
      await f.rooms.load();
      expect(f.rooms.rooms.map((r) => r['id']), ['room-1']);
      f.store.setAdultAllowed(true);
      await f.rooms.load();
      expect(f.rooms.rooms.map((r) => r['id']), ['room-1', 'private']);
      f.store.setAdultAllowed(false);
      expect(f.rooms.rooms.map((r) => r['id']), ['room-1']);
      await f.discord.unlink();
      expect(f.rooms.rooms, isEmpty);
    },
  );
  test(
    'guest join, requests, ready, sync, count and leave use bot payloads',
    () async {
      final calls = <http.Request>[];
      final f = await fixture((r) async {
        calls.add(r);
        expect(r.headers['authorization'], 'Bearer app-token');
        if (r.url.path.endsWith('/sync')) return json({'sync': false});
        if (r.url.path.endsWith('/leave')) {
          return json({'ep': 4, 'secs': 100, 'done': false});
        }
        return json(roomFixture());
      });
      await f.rooms.join({'id': 'room-1', 'channel': 'vc'});
      expect(calls.last.url.path, '/activity/api/watch/open');
      expect(jsonDecode(calls.last.body), {'room': 'room-1', 'channel': 'vc'});
      await f.rooms.control('pause');
      expect(calls.last.url.path, '/activity/api/watch/ask');
      expect(jsonDecode(calls.last.body), {'room': 'room-1', 'kind': 'pause'});
      await f.rooms.jumpBy(10);
      expect(jsonDecode(calls.last.body), {
        'room': 'room-1',
        'kind': 'forward',
        'by': 10.0,
      });
      await f.rooms.ready(9);
      expect(jsonDecode(calls.last.body), {'room': 'room-1', 'id': 9});
      await f.rooms.beat(
        position: 31,
        duration: 1200,
        playing: true,
        buffering: false,
      );
      expect(jsonDecode(calls.last.body), {
        'room': 'room-1',
        'pos': 31.0,
        'dur': 1200.0,
        'playing': true,
        'buffering': false,
      });
      await f.rooms.sync(false);
      expect(f.rooms.state!.json['sync'], false);
      await f.rooms.count(4, true);
      expect(jsonDecode(calls.last.body), {
        'room': 'room-1',
        'ep': 4,
        'ok': true,
      });
      final result = await f.rooms.leave();
      expect(result['secs'], 100);
      expect(f.rooms.state, null);
      expect(
        f.rooms.mediaUri('api/watch/media/test/index.m3u8').toString(),
        '$discordBotUrl/activity/api/watch/media/test/index.m3u8',
      );
      expect(
        () => f.rooms.mediaUri('https://outside.example/file'),
        throwsFormatException,
      );
    },
  );
  test('host controls and accepts requests, guest jumps accumulate up to 60 seconds', () async {
    final calls = <http.Request>[];
    var host = true;
    final f = await fixture((r) async {
      calls.add(r);
      return json({
        ...roomFixture(),
        'is_host': host,
        'asks': [
          {'id': 1, 'user': 'viewer', 'kind': 'forward', 'by': 50},
        ],
      });
    });
    await f.rooms.join({'id': 'room-1'});
    await f.rooms.control('pause', position: 80);
    expect(calls.last.url.path, '/activity/api/watch/ctl');
    expect(jsonDecode(calls.last.body), {
      'room': 'room-1',
      'act': 'pause',
      'pos': 80.0,
    });
    await f.rooms.answer(1, true);
    expect(jsonDecode(calls.last.body), {
      'room': 'room-1',
      'id': 1,
      'ok': true,
    });
    host = false;
    await f.rooms.leave();
    await f.rooms.join({'id': 'room-1'});
    await f.rooms.jumpBy(20);
    expect(jsonDecode(calls.last.body)['by'], 60);
  });
  test(
    'adult preference changing during join leaves without exposing the room',
    () async {
      final calls = <String>[];
      final f = await fixture((r) async {
        calls.add(r.url.path);
        return json({...roomFixture(), 'adult': true});
      });
      f.store.setAdultAllowed(true);
      final pending = f.rooms.join({'id': 'room-1', 'adult': true});
      f.store.setAdultAllowed(false);
      await pending;
      await Future<void>.delayed(Duration.zero);
      expect(f.rooms.state, null);
      expect(calls, contains('/activity/api/watch/leave'));
    },
  );
  test('late long poll cannot restore a closed profile session', () async {
    final response = Completer<http.Response>(), started = Completer<void>();
    final f = await fixture((r) async {
      if (r.method == 'GET') {
        started.complete();
        expect(r.url.queryParameters, {'room': 'room-1', 'v': '7'});
        return response.future;
      }
      if (r.url.path.endsWith('/leave')) return json({});
      return json(roomFixture());
    }, polling: true);
    await f.rooms.join({'id': 'room-1'});
    await started.future;
    await f.rooms.close();
    response.complete(json({...roomFixture(), 'ver': 8}));
    await Future<void>.delayed(Duration.zero);
    expect(f.rooms.state, null);
    expect(f.rooms.closed, true);
  });
  test('an older bot Activity 401 preserves a valid app link', () async {
    final f = await fixture(
      (r) async => r.url.path.startsWith('/activity/')
          ? json({'error': 'sign in again'}, 401)
          : json({'ok': true}),
    );
    await f.rooms.load();
    expect(f.rooms.error, contains('Watch rooms are unavailable'));
    expect(f.discord.linked, true);
  });
}
