import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/movies.dart';
import 'package:otakase/data/store.dart';
import 'package:otakase/data/trakt.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _Tokens implements TokenStore {
  final values = <String, String>{
    traktTokenKey: jsonEncode({'access_token': 'tok', 'expires_in': 0}),
  };
  @override
  Future<String?> read(String key) async => values[key];
  @override
  Future<void> write(String key, String? value) async {
    value == null ? values.remove(key) : values[key] = value;
  }
}

class _Core extends CoreClient {
  @override
  Future<Json> request(
    Json input, {
    void Function(int)? onHandle,
    RequestScope? scope,
    Duration timeout = const Duration(seconds: 40),
  }) async => {};
}

const mentalist = MediaTitle(kind: 'show', trakt: 4553, title: 'The Mentalist');
const other = MediaTitle(kind: 'show', trakt: 1388, title: 'Breaking Bad');
const ended = MediaTitle(kind: 'show', trakt: 1390, title: 'Ended Show');
const film = MediaTitle(kind: 'movie', trakt: 102156, title: 'Interstellar');

Json _show(MediaTitle t) => {
  'title': t.title,
  'ids': {'trakt': t.trakt},
};

Json _watchedShow(MediaTitle t, int episodes) => {
  'show': _show(t),
  'seasons': [
    {
      'number': 1,
      'episodes': [
        for (var n = 1; n <= episodes; n++)
          {
            'number': n,
            'last_watched_at': DateTime.now()
                .subtract(Duration(hours: 10 - n))
                .toUtc()
                .toIso8601String(),
          },
      ],
    },
  ],
};

Future<MovieService> _service(MockClient client) async {
  SharedPreferences.setMockInitialValues({});
  final store = AppStore(await SharedPreferences.getInstance());
  return MovieService(
    trakt: Trakt(client: client, storage: _Tokens()),
    core: _Core(),
    store: store,
    pushGap: Duration.zero,
  );
}

void main() {
  test('every page of watched shows is read, episodes included', () async {
    final asked = <Uri>[];
    final client = MockClient((r) async {
      if (r.url.path == '/sync/watched/shows') {
        asked.add(r.url);
        // Trakt's watched lists since July 2026: seasons only with
        // extended=progress, 100 rows a page.
        final progress = r.url.queryParameters['extended'] == 'progress';
        final page = int.parse(r.url.queryParameters['page'] ?? '1');
        final rows = page == 1
            ? [
                progress ? _watchedShow(other, 3) : {'show': _show(other)},
              ]
            : [
                progress
                    ? _watchedShow(mentalist, 7)
                    : {'show': _show(mentalist)},
              ];
        return http.Response(
          jsonEncode(rows),
          200,
          headers: {'x-pagination-page-count': '2'},
        );
      }
      if (r.url.path.startsWith('/shows/')) {
        return http.Response(
          jsonEncode({
            'next_episode': {'season': 1, 'number': 8},
          }),
          200,
        );
      }
      return http.Response('[]', 200);
    });
    final movies = await _service(client);
    await movies.sync(force: true);
    for (var n = 1; n <= 7; n++) {
      expect(movies.watched(mentalist, season: 1, episode: n), isTrue);
    }
    expect(movies.watched(other, season: 1, episode: 3), isTrue);
    expect(asked, hasLength(2));
    expect(asked.first.queryParameters['extended'], 'progress');
    // Up next follows the episodes watched on another device.
    expect(movies.entry(mentalist)!.code, 'S1E8');
  });

  test('a page past the end repeating the last one stops paging', () async {
    var pages = 0;
    final client = MockClient((r) async {
      if (r.url.path == '/sync/watched/movies') {
        pages++;
        return http.Response(
          jsonEncode([
            {
              'last_watched_at': '2026-11-01T10:00:00.000Z',
              'movie': {
                'title': 'Interstellar',
                'ids': {'trakt': 102156},
              },
            },
          ]),
          200,
          headers: {'x-pagination-page-count': '50'},
        );
      }
      return http.Response('[]', 200);
    });
    final movies = await _service(client);
    await movies.sync(force: true);
    expect(pages, 2);
  });

  test('a watchlist change survives a pull already under way', () async {
    final gate = Completer<void>();
    var listed = <Json>[];
    final client = MockClient((r) async {
      switch (r.url.path) {
        case '/sync/watchlist/shows':
          final answer = jsonEncode(listed);
          await gate.future;
          return http.Response(answer, 200);
        case '/sync/watchlist':
          listed = [
            {'show': _show(mentalist)},
          ];
          return http.Response('{}', 201);
        case '/sync/watchlist/remove':
          listed = [];
          return http.Response('{}', 200);
      }
      return http.Response('[]', 200);
    });
    final movies = await _service(client);
    final pulling = movies.sync(force: true);
    await Future<void>.delayed(Duration.zero);
    await movies.setWatchlist(mentalist, true);
    gate.complete();
    await pulling;
    // Trakt's answer was read before the change reached it.
    expect(movies.onWatchlist(mentalist), isTrue);

    await movies.sync(force: true);
    expect(movies.onWatchlist(mentalist), isTrue);

    // Removed on trakt.tv: the next pull follows.
    listed = [];
    await movies.sync(force: true);
    expect(movies.onWatchlist(mentalist), isFalse);
  });

  test('a watchlist change Trakt missed is sent on the next sync', () async {
    var down = true;
    var listed = <Json>[];
    final client = MockClient((r) async {
      switch (r.url.path) {
        case '/sync/watchlist':
          if (down) return http.Response('', 503);
          listed = [
            {'show': _show(mentalist)},
          ];
          return http.Response('{}', 201);
        case '/sync/watchlist/shows':
          return http.Response(jsonEncode(listed), 200);
      }
      return http.Response('[]', 200);
    });
    final movies = await _service(client);
    await movies.sync(force: true);
    await movies.setWatchlist(mentalist, true);
    expect(movies.onWatchlist(mentalist), isTrue);

    // Trakt still away: the change stays here.
    await movies.sync(force: true);
    expect(movies.onWatchlist(mentalist), isTrue);

    down = false;
    await movies.sync(force: true);
    expect(listed, hasLength(1));
    expect(movies.onWatchlist(mentalist), isTrue);
  });

  group('a started show stays on the watchlist until completed', () {
    // Trakt as it behaves: a watch takes the title off the watchlist.
    late Map<String, Json> listed;
    late List<Json> watched;
    late List<String> adds;
    late MockClient client;
    String keyOf(Json body) {
      final shows = body['shows'] as List?;
      final row = (shows ?? body['movies'] as List).single as Map;
      return '${shows != null ? 'show' : 'movie'}:${(row['ids'] as Map)['trakt']}';
    }

    setUp(() {
      listed = {};
      watched = [];
      adds = [];
      client = MockClient((r) async {
        switch (r.url.path) {
          case '/sync/watchlist':
            final body = jsonDecode(r.body) as Json;
            final key = keyOf(body);
            adds.add(key);
            final row = ((body['shows'] ?? body['movies']) as List).single;
            listed[key] = {
              key.startsWith('show') ? 'show' : 'movie': {
                'title': 'x',
                'ids': (row as Map)['ids'],
                'status': key == 'show:${ended.trakt}'
                    ? 'ended'
                    : 'returning series',
                'aired_episodes': 2,
              },
            };
            return http.Response('{}', 201);
          case '/sync/watchlist/remove':
            listed.remove(keyOf(jsonDecode(r.body) as Json));
            return http.Response('{}', 200);
          case '/scrobble/stop':
            final body = jsonDecode(r.body) as Json;
            final ids = ((body['show'] ?? body['movie']) as Map)['ids'] as Map;
            listed.remove(
              '${body['show'] != null ? 'show' : 'movie'}:${ids['trakt']}',
            );
            return http.Response('{}', 201);
          case '/sync/watchlist/shows':
            return http.Response(
              jsonEncode([
                for (final MapEntry(:key, :value) in listed.entries)
                  if (key.startsWith('show')) value,
              ]),
              200,
            );
          case '/sync/watchlist/movies':
            return http.Response(
              jsonEncode([
                for (final MapEntry(:key, :value) in listed.entries)
                  if (key.startsWith('movie')) value,
              ]),
              200,
            );
          case '/sync/watched/shows':
            return http.Response(jsonEncode(watched), 200);
        }
        return http.Response('[]', 200);
      });
    });

    test('watched here', () async {
      final movies = await _service(client);
      await movies.sync(force: true);
      await movies.setWatchlist(mentalist, true);
      await movies.sync(force: true);
      await movies.progress(
        mentalist,
        2600,
        2640,
        season: 1,
        episode: 1,
        completed: true,
      );
      expect(movies.onWatchlist(mentalist), isTrue);
      await movies.scrobble('stop', mentalist, 100, season: 1, episode: 1);
      // Trakt dropped it on the watch; it was put back.
      expect(listed.keys, contains(mentalist.key));
      await movies.sync(force: true);
      expect(movies.onWatchlist(mentalist), isTrue);
    });

    test('watched on another device', () async {
      final movies = await _service(client);
      await movies.sync(force: true);
      await movies.setWatchlist(mentalist, true);
      await movies.sync(force: true);
      await Future<void>.delayed(const Duration(milliseconds: 5));
      // Another app scrobbles episode 1 and Trakt drops the show.
      listed.remove(mentalist.key);
      watched = [
        {
          'show': _show(mentalist),
          'seasons': [
            {
              'number': 1,
              'episodes': [
                {
                  'number': 1,
                  'last_watched_at': DateTime.now().toUtc().toIso8601String(),
                },
              ],
            },
          ],
        },
      ];
      await movies.sync(force: true);
      expect(movies.onWatchlist(mentalist), isTrue);
      expect(listed.keys, contains(mentalist.key));
    });

    test('taken off on trakt.tv with no watch stays off', () async {
      final movies = await _service(client);
      await movies.sync(force: true);
      await movies.setWatchlist(mentalist, true);
      await movies.sync(force: true);
      listed.remove(mentalist.key);
      await movies.sync(force: true);
      expect(movies.onWatchlist(mentalist), isFalse);
      expect(adds, hasLength(1));
    });

    test('an ended show with every episode watched leaves', () async {
      final movies = await _service(client);
      await movies.sync(force: true);
      await movies.setWatchlist(ended, true);
      await movies.sync(force: true);
      await movies.markWatched(ended, true, season: 1, episode: 1);
      expect(movies.onWatchlist(ended), isTrue);
      await movies.markWatched(ended, true, season: 1, episode: 2);
      expect(movies.onWatchlist(ended), isFalse);
    });

    test('a watched film leaves', () async {
      final movies = await _service(client);
      await movies.sync(force: true);
      await movies.setWatchlist(film, true);
      await movies.sync(force: true);
      await movies.progress(film, 7000, 7200, completed: true);
      expect(movies.onWatchlist(film), isFalse);
    });
  });
}
