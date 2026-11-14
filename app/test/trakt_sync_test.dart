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

  test('watching an episode takes the show off the watchlist', () async {
    final client = MockClient((r) async {
      if (r.url.path == '/sync/watchlist') return http.Response('{}', 201);
      return http.Response('[]', 200);
    });
    final movies = await _service(client);
    await movies.sync(force: true);
    await movies.setWatchlist(mentalist, true);
    await movies.progress(
      mentalist,
      2600,
      2640,
      season: 1,
      episode: 1,
      completed: true,
    );
    // As Trakt does once an episode is watched.
    expect(movies.onWatchlist(mentalist), isFalse);
    await movies.sync(force: true);
    expect(movies.onWatchlist(mentalist), isFalse);
  });
}
