import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/data/catalog.dart';
import 'package:otakase/data/downloads.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/trakt.dart';
import 'package:otakase/data/watch_rooms.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/downloads_page.dart';
import 'package:otakase/ui/movies_pages.dart';

import 'dart:convert';

import 'package:otakase/data/movies.dart';
import 'package:otakase/data/store.dart';

import 'movies_test.dart' show MemoryTokens, MovieCore, freshStore;

const show = MediaTitle(
  kind: 'show',
  trakt: 1388,
  tmdb: 1396,
  title: 'Breaking Bad',
  status: 'ended',
);
const film = MediaTitle(kind: 'movie', trakt: 102156, title: 'Interstellar');
const episodes = [
  TitleEpisode(season: 1, number: 1),
  TitleEpisode(season: 1, number: 2),
  TitleEpisode(season: 2, number: 1),
];

Future<(AppServices, MovieCore)> services() async {
  final client = MockClient((r) async => http.Response('[]', 200));
  final core = MovieCore();
  final s = AppServices(
    catalog: Catalog(client: client),
    store: await freshStore(),
    core: core,
    auth: AuthService(client: client, storage: MemoryTokens()),
  );
  return (s, core);
}

DownloadItem saved(String id, Json meta) => DownloadItem(
  id: id,
  kind: meta['anime'] != null ? 'anime' : 'show',
  title: 'Saved',
  label: id,
  base: id,
  path: '/files/$id.mp4',
  state: 'done',
  meta: meta,
);

void main() {
  test('a series episode knows the ones before and after it', () async {
    final (s, _) = await services();
    final middle = titlePlay(s, show, episode: episodes[1], episodes: episodes);
    expect(middle.number, 2);
    expect(middle.count, 2);
    expect(middle.previous?.call()?.subtitle, episodes[0].label);
    expect(middle.next?.call()?.subtitle, episodes[2].label);
    expect(middle.pick?.call(1)?.subtitle, episodes[0].label);
    expect(middle.pick?.call(9), isNull);
    final first = titlePlay(s, show, episode: episodes[0], episodes: episodes);
    expect(first.previous?.call(), isNull);
    // A film has no episodes either way.
    final movie = titlePlay(s, film);
    expect(movie.previous, isNull);
    expect(movie.pick, isNull);
    expect(movie.skips, isNull);
  });

  test('a series episode asks TheIntroDB by its TMDB id', () async {
    final (s, core) = await services();
    final request = titlePlay(
      s,
      show,
      episode: episodes[1],
      episodes: episodes,
    );
    await request.skips!(2900, RequestScope());
    final asked = core.calls.where((c) => c['operation'] == 'skiptimes_show');
    expect(asked.single, {
      'operation': 'skiptimes_show',
      'tmdb_id': 1396,
      'season': 1,
      'episode': 2,
      'length': 2900.0,
    });
    // Off in the settings, nothing is asked.
    await s.store.set('IntroDBSkipTimes', false);
    await request.skips!(2900, RequestScope());
    expect(
      core.calls.where((c) => c['operation'] == 'skiptimes_show'),
      hasLength(1),
    );
  });

  test(
    'a film or the last episode of an ended show asks for a score',
    () async {
      final (s, _) = await services();
      final list = [
        for (final e in episodes) (season: e.season, number: e.number),
      ];
      expect(s.movies.finishes(film, const []), isTrue);
      expect(s.movies.finishes(show, list, season: 2, episode: 1), isTrue);
      expect(s.movies.finishes(show, list, season: 1, episode: 2), isFalse);
      const airing = MediaTitle(
        kind: 'show',
        trakt: 2,
        title: 'Still airing',
        status: 'returning series',
      );
      expect(airing.isShow, isTrue);
      expect(s.movies.finishes(airing, list, season: 2, episode: 1), isFalse);
      await s.store.set('ScoreOnCompletion', false);
      expect(s.movies.finishes(film, const []), isFalse);
    },
  );

  test(
    'saved episodes play on one after another and can move to a TV',
    () async {
      final (s, _) = await services();
      const anime = Anime(id: 21, title: 'Saved show');
      s.downloads.items.addAll([
        saved('anime:al:21:3:sub', {'anime': anime.toJson(), 'episode': 3}),
        saved('anime:al:21:1:sub', {'anime': anime.toJson(), 'episode': 1}),
        saved('anime:al:21:5:dub', {
          'anime': anime.toJson(),
          'episode': 5,
          'mode': 'dub',
        }),
        saved('anime:al:99:2:sub', {
          'anime': const Anime(id: 99, title: 'Other').toJson(),
          'episode': 2,
        }),
      ]);
      final third = downloadPlay(s, s.downloads.items.first);
      expect(third.next?.call()?.subtitle, 'anime:al:21:5:dub');
      expect(third.previous?.call()?.subtitle, 'anime:al:21:1:sub');
      expect(third.remote, {'anilist_id': 21, 'episode': 3, 'mode': 'sub'});
      final last = downloadPlay(s, s.downloads.items[2]);
      expect(last.next?.call(), isNull);
      expect(last.remote?['mode'], 'dub');

      s.downloads.items.addAll([
        saved('title:show:1388:S2E1', {
          'title': show.toJson(),
          'season': 2,
          'episode': 1,
        }),
        saved('title:show:1388:S1E2', {
          'title': show.toJson(),
          'season': 1,
          'episode': 2,
        }),
      ]);
      final s1e2 = downloadPlay(s, s.downloads.items.last);
      expect(s1e2.next?.call()?.subtitle, 'title:show:1388:S2E1');
      expect(s1e2.previous?.call(), isNull);
      expect(s1e2.remote?['episode'], 2);
    },
  );

  test(
    'a card taken off Continue watching comes back once it moves on',
    () async {
      final store = await freshStore();
      final movies = MovieService(
        trakt: Trakt(
          client: MockClient((r) async => http.Response('[]', 200)),
          storage: MemoryTokens(),
        ),
        core: MovieCore(),
        store: store,
      );
      await movies.progress(show, 300, 2900, season: 1, episode: 2);
      final e = movies.continuing.single;
      await movies.hideContinue(e);
      expect(movies.continuing, isEmpty);
      // Playing on brings it back.
      await movies.progress(show, 320, 2900, season: 1, episode: 2);
      expect(movies.continuing, hasLength(1));

      expect(store.continueHidden('al:1', 3, 100), isFalse);
      await store.hideContinue('al:1', 3, 100);
      expect(store.continueHidden('al:1', 3, 100), isTrue);
      expect(store.continueHidden('al:1', 4, 100), isFalse);
      expect(store.continueHidden('al:1', 3, 101), isFalse);
    },
  );

  test('the Schedule reads series episodes from the Trakt calendar', () async {
    Uri? asked;
    final tokens = MemoryTokens()
      ..values[traktTokenKey] = jsonEncode({
        'access_token': 'tok',
        'expires_in': 0,
      });
    final trakt = Trakt(
      client: MockClient((r) async {
        asked = r.url;
        return http.Response(
          jsonEncode([
            {
              'first_aired': '2026-11-12T01:00:00.000Z',
              'episode': {'season': 2, 'number': 5, 'title': 'Five'},
              'show': {
                'title': 'Breaking Bad',
                'ids': {'trakt': 1388, 'tmdb': 1396},
              },
            },
            {'first_aired': 'not a date', 'episode': {}, 'show': {}},
          ]),
          200,
        );
      }),
      storage: tokens,
    );
    final found = await trakt.calendar(DateTime(2026, 11, 9), 7);
    expect(asked!.path, '/calendars/my/shows/2026-11-09/7');
    expect(found.single.show.title, 'Breaking Bad');
    expect(found.single.episode.code, 'S2E5');
    expect(found.single.at.isUtc, isFalse);
  });

  test('stored own-mpv and fallback switches are dropped', () async {
    final store = await freshStore({
      'settings.v1': jsonEncode({
        'ExternalMpv': true,
        'MpvPath': '/usr/bin/mpv',
        'AutoFallback': false,
      }),
    });
    expect(store.value('ExternalMpv'), isNull);
    expect(store.value('MpvPath'), isNull);
    expect(store.value('AutoFallback'), isNull);
    expect(store, isA<AppStore>());
  });

  test('a shared title is found on Trakt by its IMDb id', () async {
    Uri? asked;
    final trakt = Trakt(
      client: MockClient((r) async {
        asked = r.url;
        return http.Response(
          jsonEncode([
            {
              'type': 'movie',
              'movie': {
                'title': 'The Matrix',
                'year': 1999,
                'ids': {'trakt': 481, 'imdb': 'tt0133093'},
              },
            },
          ]),
          200,
        );
      }),
      storage: MemoryTokens(),
    );
    final found = await trakt.byImdb('tt0133093', kind: 'movie');
    expect(asked!.path, '/search/imdb/tt0133093');
    expect(asked!.queryParameters['type'], 'movie');
    expect(found!.title, 'The Matrix');
    expect(await trakt.byImdb('0133093; drop'), isNull);
  });

  test('a watch room playing a film shows no episode number', () {
    final film = RoomState({
      'room': 'r',
      'phase': 'play',
      'ep': 1,
      'show': {'id': -134093, 'title': 'The Matrix', 'format': 'MOVIE'},
    }, DateTime(2026));
    expect(film.film, isTrue);
    expect(film.anime!.id, lessThan(0));
    final series = RoomState({
      'show': {'id': 1, 'title': 'Frieren', 'format': 'TV'},
    }, DateTime(2026));
    expect(series.film, isFalse);
  });
}
