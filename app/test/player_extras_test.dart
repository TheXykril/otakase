import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/presence.dart';
import 'package:otakase/data/skip_times.dart';
import 'package:otakase/data/store.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'dart:math';

class RecordingCore extends CoreClient {
  final calls = <Json>[];
  Json answer = {};
  Object? fail;
  @override
  Future<Json> request(
    Json input, {
    void Function(int)? onHandle,
    RequestScope? scope,
    Duration timeout = const Duration(seconds: 40),
  }) async {
    calls.add(input);
    if (fail != null) throw fail!;
    return answer;
  }
}

Future<AppStore> store([Map<String, Object> values = const {}]) async {
  SharedPreferences.setMockInitialValues(values);
  return AppStore(await SharedPreferences.getInstance());
}

const frieren = Anime(
  id: 154587,
  malId: 52991,
  title: 'Frieren',
  cover: 'https://img.example/c.jpg',
  episodes: 28,
  status: 'FINISHED',
);

void main() {
  test('presence is sent on change, not every tick', () async {
    final core = RecordingCore();
    final p = DiscordPresence(core, await store(), desktop: true);
    await p.episode(frieren, 3, paused: false, position: 10, duration: 1440);
    await p.episode(frieren, 3, paused: false, position: 10.2, duration: 1440);
    expect(core.calls, hasLength(1));
    final sent = core.calls.single['presence'] as Map;
    expect(sent['title'], 'Frieren');
    expect(sent['state'], 'Episode 3');
    expect((sent['buttons'] as List).length, 2);

    await p.episode(frieren, 3, paused: true, position: 10, duration: 1440);
    expect(core.calls, hasLength(2), reason: 'pausing changes the status');
    await p.episode(frieren, 3, paused: true, position: 300, duration: 1440);
    expect(core.calls, hasLength(3), reason: 'a jump moves the timestamps');

    await p.clear();
    expect(core.calls.last['operation'], 'presence_clear');
  });

  test('presence never shows 18+ titles or runs off desktop', () async {
    final core = RecordingCore();
    final p = DiscordPresence(core, await store(), desktop: true);
    const adult = Anime(id: 1, title: 'Hidden', isAdult: true);
    await p.episode(adult, 1, paused: false, position: 0, duration: 1400);
    expect(core.calls, isEmpty);

    final phone = DiscordPresence(core, await store(), desktop: false);
    await phone.episode(frieren, 1, paused: false, position: 0, duration: 9);
    expect(core.calls, isEmpty);

    final off = DiscordPresence(
      core,
      await store({'settings.v1': '{"DiscordPresence":false}'}),
      desktop: true,
    );
    await off.episode(frieren, 1, paused: false, position: 0, duration: 9);
    expect(core.calls, isEmpty);
  });

  test('skip times come back with their source and vote id', () async {
    final core = RecordingCore()
      ..answer = {
        'spans': [
          {
            'type': 'op',
            'start': 80,
            'end': 170,
            'source': 'aniskip',
            'id': 'a1',
          },
          {'type': 'ed', 'start': 1300, 'end': 1390, 'source': 'theintrodb'},
          {'type': 'recap', 'start': 5, 'end': 2, 'source': 'anime-skip'},
        ],
      };
    final s = SkipTimes(core, await store());
    final spans = await s.lookup(frieren, 3, 1420);
    expect(spans.map((v) => v.source), ['aniskip', 'theintrodb']);
    expect(spans.first.id, 'a1');
    final asked = core.calls.single;
    expect(asked['introdb'], true);
    expect(asked['anime_skip'], 'auto');
    expect(asked['length'], 1420);
  });

  test('one vote per span, and a submission counts as one', () async {
    final core = RecordingCore()..answer = {'id': 'mine'};
    final s = SkipTimes(core, await store());
    await s.submit(frieren, 3, 'op', 80, 170, 1420);
    final submitted = core.calls.single;
    expect(submitted['submitter'], matches(RegExp(r'^[0-9a-f-]{36}$')));
    expect(await s.submitter(), submitted['submitter']);
    await s.vote(const SkipSpan('op', 80, 170, id: 'mine'), up: true);
    expect(core.calls, hasLength(1), reason: 'own times are not voted on');
    await s.vote(const SkipSpan('ed', 1, 90, id: 'other'), up: false);
    await s.vote(const SkipSpan('ed', 1, 90, id: 'other'), up: false);
    expect(core.calls, hasLength(2));
    expect(core.calls.last['up'], false);
  });

  test('uuids are version 4', () {
    final id = newUuid(Random(1));
    expect(
      id,
      matches(
        RegExp(
          r'^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$',
        ),
      ),
    );
  });
}
