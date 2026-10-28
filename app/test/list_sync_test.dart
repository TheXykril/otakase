import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/data/list_sync.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/store.dart';
import 'package:otakase/data/tracking.dart';

import 'auth_test.dart' show account, MemoryTokens, json;
import 'data_test.dart' as fixture;

const show = Anime(id: 42, malId: 24, title: 'Story');
CombinedAccountPage page(List<ListEntry> a, List<ListEntry> m) =>
    CombinedAccountPage(mergeTrackerLists(a, m), anilist: a, mal: m);
Future<
  ({AuthService auth, ListSync sync, TrackingService tracking, AppStore store})
>
setup(
  http.Client client, {
  Future<void> Function(Duration)? delay,
  DateTime Function()? now,
}) async {
  final auth = AuthService(storage: MemoryTokens(), client: client);
  auth.accounts.addAll({'AniList': account('AniList'), 'MAL': account('MAL')});
  final store = await fixture.store();
  final tracking = TrackingService(auth, now: now);
  final sync = ListSync(
    tracking: tracking,
    store: store,
    now: now,
    delay: delay ?? (_) async {},
  );
  addTearDown(() {
    sync.dispose();
    auth.dispose();
  });
  return (auth: auth, sync: sync, tracking: tracking, store: store);
}

void main() {
  test('CLI timestamp ordering includes a time beating none and equal-time freshness', () {
    final at = DateTime.utc(2026, 10, 28);
    final old = ListEntry(show, 'COMPLETED', 12, updatedAt: at, score: 90);
    final newer = ListEntry(
      show,
      'PLANNING',
      0,
      updatedAt: at.add(const Duration(seconds: 1)),
      score: 0,
    );
    expect(mergeAnimeEntries(old, newer).progress, 0);
    expect(mergeAnimeEntries(old, newer).score, 0);
    expect(
      incomingIsNewer(const ListEntry(show, 'CURRENT', 99), newer),
      isTrue,
    );
    expect(
      incomingIsNewer(newer, const ListEntry(show, 'CURRENT', 99)),
      isFalse,
    );
    expect(incomingIsNewer(old, old), isFalse);
    expect(
      incomingIsNewer(
        const ListEntry(show, 'DROPPED', 4),
        const ListEntry(show, 'CURRENT', 4),
      ),
      isTrue,
    );
  });
  test('metadata preserves the winning state and fills schedule, IDs, cover and adult flag', () {
    const a = ListEntry(
      Anime(
        id: 4,
        malId: 400,
        title: 'Other',
        nativeTitle: '物語',
        cover: 'cover',
        isAdult: true,
        nextEpisode: 5,
        nextAiringAt: 1234,
      ),
      'CURRENT',
      8,
      repeat: 1,
    );
    const m = ListEntry(Anime(id: 0, malId: 400, title: 'Show'), 'PLANNING', 0);
    final merged = mergeEntryMetadata(m, a);
    expect(merged.progress, 0);
    expect(merged.status, 'PLANNING');
    expect(merged.anime.id, 4);
    expect(merged.anime.title, 'Show');
    expect(merged.anime.nativeTitle, '物語');
    expect(merged.anime.nextEpisode, 5);
    expect(merged.anime.nextAiringAt, 1234);
    expect(merged.anime.isAdult, isTrue);
    expect(merged.anime.cover, 'cover');
    expect(merged.repeat, 1);
  });
  test(
    'private AniList entries never queue a MAL write, including when matched',
    () {
      const a = ListEntry(show, 'CURRENT', 4, isPrivate: true);
      expect(planTrackerSync([a], [], adult: false), isEmpty);
      expect(
        planTrackerSync(
          [a],
          [const ListEntry(show, 'watching', 2)],
          adult: false,
        ).where((w) => w.service == 'MAL'),
        isEmpty,
      );
      final merged = mergeTrackerLists(
        [a],
        [const ListEntry(show, 'watching', 8)],
      ).single;
      expect(merged.isPrivate, isTrue);
    },
  );
  test('adult metadata from either tracker prevents sync while off', () {
    const hidden = ListEntry(
      Anime(id: 42, malId: 24, title: 'Story', isAdult: true),
      'CURRENT',
      2,
    );
    const other = ListEntry(show, 'watching', 8);
    expect(planTrackerSync([hidden], [other], adult: false), isEmpty);
    expect(planTrackerSync([hidden], [], adult: false), isEmpty);
    expect(planTrackerSync([hidden], [], adult: true).single.service, 'MAL');
  });
  test('one-sided shows copy to the other tracker and missing MAL repeat alone is ignored', () {
    const a = ListEntry(show, 'PLANNING', 0);
    const m = ListEntry(
      Anime(id: 43, malId: 25, title: 'Other'),
      'watching',
      2,
    );
    final writes = planTrackerSync([a], [m], adult: false);
    expect(writes.map((w) => w.service), ['AniList', 'MAL']);
    expect(writes.map((w) => w.entry.anime.id), [43, 42]);
    expect(
      planTrackerSync([a.copyWith(repeat: 2)], [a], adult: false),
      isEmpty,
    );
  });
  test('background sync does not block list display and is paced and capped at 100 writes', () async {
    final requests = <http.Request>[], waits = <Duration>[];
    final s = await setup(
      MockClient((r) async {
        requests.add(r);
        return json({});
      }),
      delay: (d) async {
        waits.add(d);
      },
    );
    final rows = List.generate(
      150,
      (i) => ListEntry(
        Anime(id: i + 1, malId: i + 1001, title: 'Show $i'),
        'PLANNING',
        0,
      ),
    );
    s.sync.afterLoad(page(rows, []));
    expect(s.sync.running, isTrue);
    expect(requests, isEmpty);
    await s.sync.pending;
    expect(requests, hasLength(100));
    expect(requests.every((r) => r.method == 'PATCH'), isTrue);
    expect(waits, hasLength(99));
    expect(waits.every((d) => d == const Duration(milliseconds: 500)), isTrue);
    expect(s.sync.status, contains('Synced 100 changes'));
    expect(s.sync.running, isFalse);
    s.sync.afterLoad(page(rows, []));
    expect(s.sync.running, isFalse);
  });
  test(
    'MAL-only copy resolves AniList ID and leaves tracker notes untouched',
    () async {
      final writes = <Map>[];
      final s = await setup(
        MockClient((r) async {
          final body = jsonDecode(r.body) as Map;
          if ((body['query'] as String).startsWith('mutation')) {
            writes.add(body['variables'] as Map);
            return json({
              'data': {
                'SaveMediaListEntry': {'id': 1},
              },
            });
          }
          expect(body['variables'], {'mal': 24, 'adult': false});
          return json({
            'data': {'Media': show.toJson()},
          });
        }),
      );
      s.sync.afterLoad(
        page([], [
          const ListEntry(
            Anime(id: 0, malId: 24, title: 'Story'),
            'watching',
            4,
            notes: 'do not copy [otakase:5@60]',
          ),
        ]),
      );
      await s.sync.pending;
      expect(writes, hasLength(1));
      expect(writes.single['id'], 42);
      expect(writes.single['progress'], 4);
      expect(writes.single.containsKey('notes'), isFalse);
    },
  );
  test(
    '429 stops the run and explicit refresh bypasses the cooldown',
    () async {
      var throttled = true, calls = 0;
      final s = await setup(
        MockClient((r) async {
          calls++;
          return throttled ? http.Response('', 429) : json({});
        }),
      );
      final rows = [
        const ListEntry(show, 'PLANNING', 0),
        const ListEntry(
          Anime(id: 43, malId: 25, title: 'Other'),
          'PLANNING',
          0,
        ),
      ];
      s.sync.afterLoad(page(rows, []));
      await s.sync.pending;
      expect(calls, 1);
      expect(s.sync.status, contains('rate limit'));
      s.sync.afterLoad(page(rows, []));
      expect(s.sync.running, isFalse);
      throttled = false;
      s.sync.afterLoad(page(rows, []), force: true);
      await s.sync.pending;
      expect(calls, 3);
      expect(s.sync.status, contains('Synced 2 changes'));
    },
  );
  test('completed MAL rewatch preserves CLI two-write order and retries a throttled count', () async {
    final bodies = <Map<String, String>>[];
    var throttle = true;
    final s = await setup(
      MockClient((r) async {
        final fields = Uri.splitQueryString(r.body);
        bodies.add(fields);
        return fields.containsKey('num_times_rewatched') && throttle
            ? http.Response('', 429)
            : json({});
      }),
    );
    const e = ListEntry(show, 'COMPLETED', 12, repeat: 2);
    s.sync.afterLoad(page([e], []));
    await s.sync.pending;
    expect(bodies, hasLength(2));
    expect(bodies.first.containsKey('is_rewatching'), isFalse);
    expect(bodies.first.containsKey('num_times_rewatched'), isFalse);
    expect(bodies.last, {'num_times_rewatched': '2'});
    throttle = false;
    s.sync.afterLoad(page([e], [e.copyWith(repeat: 0)]), force: true);
    await s.sync.pending;
    expect(bodies.last, {'num_times_rewatched': '2'});
    expect(s.store.value('ListSyncRetry'), isNull);
  });
  test('logout stops queued writes and disabled sync sends nothing', () async {
    final gate = Completer<void>(), waiting = Completer<void>();
    var calls = 0;
    final s = await setup(
      MockClient((r) async {
        calls++;
        return json({});
      }),
      delay: (_) async {
        waiting.complete();
        await gate.future;
      },
    );
    final rows = [
      const ListEntry(show, 'PLANNING', 0),
      const ListEntry(Anime(id: 43, malId: 25, title: 'Other'), 'PLANNING', 0),
    ];
    await s.store.set('SyncAniListMAL', false);
    s.sync.afterLoad(page(rows, []));
    expect(s.sync.pending, isNull);
    await s.store.set('SyncAniListMAL', true);
    s.sync.afterLoad(page(rows, []));
    await waiting.future;
    await s.auth.logout('MAL');
    gate.complete();
    await s.sync.pending;
    expect(calls, 1);
  });
}
