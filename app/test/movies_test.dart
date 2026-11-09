import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/data/catalog.dart';
import 'package:otakase/data/downloads.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/movies.dart';
import 'package:otakase/data/store.dart';
import 'package:otakase/data/trakt.dart';
import 'package:otakase/main.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/app_shell.dart';
import 'package:otakase/ui/home_page.dart';
import 'package:otakase/ui/movies_pages.dart';
import 'package:shared_preferences/shared_preferences.dart';

class MemoryTokens implements TokenStore {
  final values = <String, String>{};
  @override
  Future<String?> read(String key) async => values[key];
  @override
  Future<void> write(String key, String? value) async {
    value == null ? values.remove(key) : values[key] = value;
  }
}

/// Answers the movie operations the way the Go core does.
class MovieCore extends CoreClient {
  final calls = <Json>[];
  final missing = <String>{};
  @override
  Future<Json> request(
    Json input, {
    void Function(int)? onHandle,
    RequestScope? scope,
    Duration timeout = const Duration(seconds: 40),
  }) async {
    calls.add(input);
    switch (input['operation']) {
      case 'movie_providers':
        return {
          'providers': [
            {
              'name': 'vidsrc',
              'label': 'vidsrc',
              'language': 'en',
              'series': true,
            },
            {
              'name': '8filmai',
              'label': '8Filmai',
              'language': 'lt',
              'series': true,
            },
            {
              'name': 'filmukas',
              'label': 'Filmukas',
              'language': 'lt',
              'series': false,
            },
          ],
        };
      case 'movie_match':
        if (missing.contains(input['provider'])) {
          throw const CoreFailure('not_found', 'No match');
        }
        return {
          'movie': {
            'provider': input['provider'],
            'path': '/p/${input['provider']}',
            'title': 'x',
          },
        };
      case 'movie_resolve':
        return {
          'stream': {
            'url':
                'https://cdn.example/${(input['movie'] as Map)['provider']}.m3u8',
            'hls': true,
            'headers': {'Referer': 'https://site.example/'},
            'server': 'server 3',
            'audio_language': 'lt',
            'subtitles': [],
          },
        };
      case 'download_start':
        return {'id': 7};
      case 'download_status':
        return {
          'downloads': [
            {
              'id': 7,
              'state': 'done',
              'bytes': 2048,
              'total': 2048,
              'path': '${input['path'] ?? '/tmp/x'}.mp4',
            },
          ],
        };
    }
    return {};
  }
}

const film = MediaTitle(
  kind: 'movie',
  trakt: 102156,
  title: 'Interstellar',
  imdb: 'tt0816692',
  year: 2014,
);
const series = MediaTitle(
  kind: 'show',
  trakt: 1388,
  title: 'Breaking Bad',
  imdb: 'tt0903747',
  year: 2008,
);

Future<AppStore> freshStore([Map<String, Object> values = const {}]) async {
  SharedPreferences.setMockInitialValues(values);
  return AppStore(await SharedPreferences.getInstance());
}

Trakt offlineTrakt({http.Client? client}) => Trakt(
  client: client ?? MockClient((r) async => http.Response('[]', 200)),
  storage: MemoryTokens(),
);

void main() {
  test('Trakt titles carry posters as full addresses', () {
    final t = MediaTitle.fromTrakt({
      'title': 'Verity',
      'year': 2026,
      'ids': {'trakt': 1043653, 'imdb': 'tt32261958', 'slug': 'verity-2026'},
      'images': {
        'poster': [
          'media.trakt.tv/images/movies/001/043/653/posters/medium/3440b41fbc.jpg.webp',
        ],
        'fanart': <String>[],
      },
      'genres': ['thriller'],
    }, 'movie');
    expect(t.poster, startsWith('https://media.trakt.tv/'));
    expect(t.fanart, isEmpty);
    expect(t.key, 'movie:1043653');
    expect(MediaTitle.fromJson(t.toJson()).poster, t.poster);
  });

  test('sources follow the language order and the remembered source', () async {
    final store = await freshStore();
    final movies = MovieService(
      trakt: offlineTrakt(),
      core: MovieCore(),
      store: store,
    );
    expect((await movies.sourcesFor(film)).map((s) => s.name), [
      'vidsrc',
      '8filmai',
      'filmukas',
    ]);
    await store.set('MovieLanguages', ['lithuanian', 'english']);
    expect((await movies.sourcesFor(film)).map((s) => s.name), [
      '8filmai',
      'filmukas',
      'vidsrc',
    ]);
    // Filmukas has no series.
    expect((await movies.sourcesFor(series)).map((s) => s.name), [
      '8filmai',
      'vidsrc',
    ]);
    await store.set('DisabledMovieSources', ['8filmai']);
    expect((await movies.sourcesFor(film)).map((s) => s.name), [
      'filmukas',
      'vidsrc',
    ]);
    await movies.rememberSource(film, 'vidsrc');
    expect((await movies.sourcesFor(film)).first.name, 'vidsrc');
  });

  test(
    'resolve falls through to the next language and skips failed servers',
    () async {
      final store = await freshStore();
      await store.set('MovieLanguages', ['lithuanian', 'english']);
      final core = MovieCore()..missing.addAll({'8filmai', 'filmukas'});
      final trakt = offlineTrakt(
        client: MockClient(
          (r) async => http.Response.bytes(
            utf8.encode(
              r.url.path.endsWith('/translations/lt')
                  ? jsonEncode([
                      {'title': 'Tarp žvaigždžių'},
                    ])
                  : '[]',
            ),
            200,
          ),
        ),
      );
      final movies = MovieService(trakt: trakt, core: core, store: store);
      final p = await movies.resolve(
        film,
        scope: RequestScope(),
        status: (_) {},
        skip: {'vidsrc:server 1', '8filmai:server 2'},
      );
      expect(p.source.name, 'vidsrc');
      expect(p.stream.url, 'https://cdn.example/vidsrc.m3u8');
      expect(p.server, 'server 3');
      final matches = core.calls
          .where((c) => c['operation'] == 'movie_match')
          .toList();
      expect(matches.first['provider'], '8filmai');
      expect(matches.first['titles'], ['Tarp žvaigždžių', 'Interstellar']);
      final resolve = core.calls.lastWhere(
        (c) => c['operation'] == 'movie_resolve',
      );
      expect(resolve['skip'], ['server 1']);
      expect(store.providerHealth['movie:8filmai'], 'No match');
    },
  );

  test('progress, watched episodes and the continue row persist', () async {
    final store = await freshStore();
    final movies = MovieService(
      trakt: offlineTrakt(),
      core: MovieCore(),
      store: store,
    );
    await movies.progress(series, 600, 2700, season: 1, episode: 2);
    await movies.progress(film, 120, 9000);
    expect(movies.continuing.map((e) => e.title.key), [film.key, series.key]);
    await movies.progress(film, 8800, 9000, completed: true);
    expect(movies.watched(film), isTrue);
    await movies.markWatched(series, true, season: 1, episode: 2);
    final again = MovieService(
      trakt: offlineTrakt(),
      core: MovieCore(),
      store: store,
    );
    expect(again.watched(series, season: 1, episode: 2), isTrue);
    expect(again.continuing.map((e) => e.title.key), [series.key]);
    expect(again.entry(series)!.code, 'S1E2');
  });

  test(
    'a watched film being watched again resumes and stays in Continue',
    () async {
      final movies = MovieService(
        trakt: offlineTrakt(),
        core: MovieCore(),
        store: await freshStore(),
      );
      await movies.progress(film, 8800, 9000, completed: true);
      expect(movies.continuing, isEmpty);
      await movies.progress(film, 300, 9000);
      expect(movies.watched(film), isTrue);
      expect(movies.continuing.map((e) => e.title.key), [film.key]);
      expect(movies.entry(film)!.resumeAt(9000), 300);
    },
  );

  test(
    'the newer spot wins between Trakt and this device, and local spots go up',
    () async {
      final now = DateTime.now();
      var rows = <Json>[];
      final scrobbles = <Json>[];
      final client = MockClient((r) async {
        if (r.url.path == '/sync/playback') {
          return http.Response(jsonEncode(rows), 200);
        }
        if (r.url.path == '/scrobble/pause') {
          scrobbles.add(jsonDecode(r.body) as Json);
          return http.Response('{}', 201);
        }
        return http.Response('[]', 200);
      });
      final tokens = MemoryTokens()
        ..values[traktTokenKey] = jsonEncode({
          'access_token': 'tok',
          'expires_in': 0,
        });
      final movies = MovieService(
        trakt: Trakt(client: client, storage: tokens),
        core: MovieCore(),
        store: await freshStore(),
        pushGap: Duration.zero,
      );
      Json episodeRow(double progress, DateTime at) => {
        'type': 'episode',
        'progress': progress,
        'paused_at': at.toUtc().toIso8601String(),
        'episode': {'season': 1, 'number': 2},
        'show': {
          'title': 'Breaking Bad',
          'ids': {'trakt': 1388},
        },
      };

      // Watched further here than Trakt knows: keep it and send it up.
      await movies.progress(series, 900, 2700, season: 1, episode: 2);
      rows = [
        episodeRow(10, now.subtract(const Duration(days: 1))),
        {
          'type': 'movie',
          'progress': 50,
          'paused_at': now.subtract(const Duration(hours: 2)).toIso8601String(),
          'movie': {
            'title': 'Interstellar',
            'ids': {'trakt': 102156},
          },
        },
      ];
      await movies.sync();
      expect(movies.entry(series)!.position, 900);
      expect(scrobbles, hasLength(1));
      expect(scrobbles.single['progress'], 33.33);
      expect(movies.lastProgressSentAt, isNotNull);
      expect(movies.hasPendingResume, isFalse);
      expect(scrobbles.single['episode'], {'season': 1, 'number': 2});
      // A film paused on another device shows up to resume here.
      expect(movies.entry(film)!.resumeAt(6000), 3000);
      expect(movies.continuing.map((e) => e.title.key), contains(film.key));

      // Nothing new here: nothing sent again.
      await movies.sync();
      expect(scrobbles, hasLength(1));

      // Paused later on another device: that spot wins before playing.
      rows = [episodeRow(60, now.add(const Duration(minutes: 5)))];
      await movies.refreshResume();
      expect(movies.entry(series)!.position, 1620);
      expect(movies.entry(series)!.progress, closeTo(.6, .001));
      expect(scrobbles, hasLength(1));
    },
  );

  test(
    'Trakt device sign-in waits for the code, then syncs the library',
    () async {
      var polls = 0;
      final client = MockClient((r) async {
        switch (r.url.path) {
          case '/oauth/device/code':
            return http.Response(
              jsonEncode({
                'device_code': 'd',
                'user_code': 'ABCD1234',
                'verification_url': 'https://trakt.tv/activate',
                'expires_in': 600,
                'interval': 5,
              }),
              200,
            );
          case '/oauth/device/token':
            return ++polls < 2
                ? http.Response('', 400)
                : http.Response(
                    jsonEncode({
                      'access_token': 'tok',
                      'refresh_token': 'ref',
                      'expires_in': 7776000,
                      'created_at':
                          DateTime.now().millisecondsSinceEpoch ~/ 1000,
                    }),
                    200,
                  );
          case '/users/settings':
            expect(r.headers['Authorization'], 'Bearer tok');
            return http.Response(
              jsonEncode({
                'user': {'username': 'xykril'},
              }),
              200,
            );
          case '/sync/watchlist/movies':
            return http.Response(
              jsonEncode([
                {
                  'movie': {
                    'title': 'Interstellar',
                    'year': 2014,
                    'ids': {'trakt': 102156, 'imdb': 'tt0816692'},
                  },
                },
              ]),
              200,
            );
          case '/sync/watched/shows':
            return http.Response(
              jsonEncode([
                {
                  'show': {
                    'title': 'Breaking Bad',
                    'ids': {'trakt': 1388},
                  },
                  'seasons': [
                    {
                      'number': 1,
                      'episodes': [
                        {'number': 1},
                        {'number': 2},
                      ],
                    },
                  ],
                },
              ]),
              200,
            );
          case '/sync/playback':
            return http.Response(
              jsonEncode([
                {
                  'type': 'episode',
                  'progress': 40.5,
                  'episode': {'season': 1, 'number': 3},
                  'show': {
                    'title': 'Breaking Bad',
                    'ids': {'trakt': 1388},
                  },
                },
              ]),
              200,
            );
        }
        return http.Response('[]', 200);
      });
      final tokens = MemoryTokens();
      final trakt = Trakt(client: client, storage: tokens);
      final code = await trakt.startSignIn();
      expect(code.userCode, 'ABCD1234');
      expect(
        await trakt.finishSignIn(
          code,
          cancelled: () => false,
          interval: Duration.zero,
        ),
        isTrue,
      );
      expect(trakt.username, 'xykril');
      expect(tokens.values[traktTokenKey], contains('tok'));
      final movies = MovieService(
        trakt: trakt,
        core: MovieCore(),
        store: await freshStore(),
      );
      await movies.sync();
      expect(movies.onWatchlist(film), isTrue);
      final e = movies.entry(series)!;
      expect(e.code, 'S1E3');
      expect(e.progress, closeTo(.405, .001));
      expect(e.watched, containsAll(['S1E1', 'S1E2']));
      // A revoked session signs out instead of failing every call.
      final revoked = Trakt(
        client: MockClient((r) async => http.Response('', 401)),
        storage: tokens,
      );
      await expectLater(revoked.library(), throwsA(isA<TraktFailure>()));
      expect(revoked.signedIn, isFalse);
    },
  );

  test('a scrobble names the episode and rounds the progress', () async {
    Json? body;
    final tokens = MemoryTokens()
      ..values[traktTokenKey] = jsonEncode({
        'access_token': 'tok',
        'expires_in': 0,
      });
    final trakt = Trakt(
      client: MockClient((r) async {
        body = jsonDecode(r.body) as Json;
        return http.Response('{}', 201);
      }),
      storage: tokens,
    );
    await trakt.scrobble('pause', series, 41.23456, season: 2, episode: 5);
    expect(body!['episode'], {'season': 2, 'number': 5});
    expect((body!['show'] as Map)['ids'], containsPair('trakt', 1388));
    expect(body!['progress'], 41.23);
  });

  test(
    'downloads go into a folder per title and finish from the core status',
    () async {
      SharedPreferences.setMockInitialValues({});
      final prefs = await SharedPreferences.getInstance();
      final core = MovieCore();
      final downloads = DownloadManager(
        core: core,
        preferences: prefs,
        client: MockClient((r) async => http.Response('', 404)),
        folder: () async => '/videos',
      );
      final item = await downloads.start(
        id: 'title:movie:1:',
        kind: 'movie',
        title: 'Kill Bill: Vol. 1',
        label: '2003',
        stream: ResolvedStream.fromJson({
          'url': 'https://cdn.example/a.mp4',
          'referrer': 'https://site.example/',
        }),
      );
      final start = core.calls.firstWhere(
        (c) => c['operation'] == 'download_start',
      );
      expect(start['path'], '/videos/Kill Bill Vol. 1/Kill Bill Vol. 1 - 2003');
      expect((start['headers'] as Map)['Referer'], 'https://site.example/');
      await downloads.refresh();
      expect(item.done, isTrue);
      expect(item.path, endsWith('.mp4'));
      final reopened = DownloadManager(
        core: core,
        preferences: prefs,
        client: MockClient((r) async => http.Response('', 404)),
      );
      expect(reopened.items.single.done, isTrue);
      downloads.dispose();
      reopened.dispose();
      expect(safeFileName('a/b:c?'), 'a b c');
      await expectLater(
        DownloadManager(
          core: core,
          preferences: prefs,
          client: MockClient((r) async => http.Response('', 404)),
        ).start(
          id: 't',
          kind: 'anime',
          title: 't',
          label: '',
          stream: ResolvedStream.fromJson({
            'url': 'http://127.0.0.1:1/x',
            'torrent': true,
          }),
        ),
        throwsA(isA<CoreFailure>()),
      );
    },
  );

  group('content setting', () {
    Future<AppServices> launchWith(WidgetTester tester, String content) async {
      tester.view.physicalSize = const Size(412, 915);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      tester.platformDispatcher.accessibilityFeaturesTestValue =
          FakeAccessibilityFeatures(disableAnimations: true);
      addTearDown(
        tester.platformDispatcher.clearAccessibilityFeaturesTestValue,
      );
      final messenger =
          TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
      messenger.setMockMethodCallHandler(
        const MethodChannel('otakase/status'),
        (_) async => null,
      );
      messenger.setMockMethodCallHandler(
        const MethodChannel('otakase/device'),
        (call) async => call.method == 'status'
            ? {'use24Hour': true, 'battery': 77, 'charging': false}
            : null,
      );
      final store = await freshStore({
        'settings.v1': jsonEncode({'Content': content}),
      });
      final client = MockClient((r) async {
        if (r.url.host == 'api.trakt.tv' && r.url.path.startsWith('/search/')) {
          return http.Response(
            jsonEncode([
              {
                'type': 'movie',
                'movie': {
                  'title': 'Interstellar',
                  'year': 2014,
                  'ids': {'trakt': 102156},
                },
              },
            ]),
            200,
          );
        }
        if (r.url.host == 'api.trakt.tv' &&
            r.url.path.startsWith('/movies/trending')) {
          return http.Response(
            jsonEncode([
              {
                'movie': {
                  'title': 'Interstellar',
                  'year': 2014,
                  'ids': {'trakt': 102156},
                },
              },
            ]),
            200,
          );
        }
        return http.Response('[]', 200);
      });
      final services = AppServices(
        catalog: Catalog(client: client),
        store: store,
        core: MovieCore(),
      );
      await tester.pumpWidget(OtakaseApp(services: services, isTV: false));
      await tester.pumpAndSettle();
      return services;
    }

    testWidgets('both shares Home and Search instead of a Movies tab', (
      tester,
    ) async {
      final services = await launchWith(tester, 'both');
      expect(rootTabs, ['Home', 'Search', 'More']);
      expect(find.byKey(const ValueKey('nav-Movies')), findsNothing);
      expect(find.byType(MoviesHome), findsNothing);

      // A film being watched joins Continue watching on Home.
      const film = MediaTitle(
        kind: 'movie',
        trakt: 102156,
        title: 'Interstellar',
        year: 2014,
      );
      await services.movies.progress(film, 600, 6000);
      await tester.pumpAndSettle();
      final row = find.byKey(
        const ValueKey('section-Continue watching'),
        skipOffstage: false,
      );
      expect(row, findsOneWidget);
      expect(
        find.descendant(
          of: row,
          matching: find.text('Interstellar', skipOffstage: false),
          skipOffstage: false,
        ),
        findsWidgets,
      );
      expect(find.text('90 min left', skipOffstage: false), findsOne);

      // Search finds films and series beside anime and opens their page.
      await tester.tap(find.byKey(const ValueKey('nav-Search')));
      await tester.pumpAndSettle();
      expect(find.text('Search anime, movies and shows'), findsOneWidget);
      await tester.enterText(find.byKey(const Key('catalog-search')), 'inter');
      await tester.pump(const Duration(milliseconds: 500));
      await tester.pumpAndSettle();
      final titles = find.byKey(const Key('search-titles'));
      expect(titles, findsOneWidget);
      await tester.tap(
        find.descendant(of: titles, matching: find.text('Interstellar')),
      );
      await tester.pumpAndSettle();
      expect(find.byType(TitlePage), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
      shellContent = ContentMode.anime;
    });

    testWidgets('a film watchlist gets its own row on Home', (tester) async {
      final services = await launchWith(tester, 'both');
      services.movies.watchlist = [
        const MediaTitle(kind: 'show', trakt: 1390, title: 'Game of Thrones'),
      ];
      services.movies.notifyListeners();
      await tester.pumpAndSettle();
      expect(
        find.byKey(const Key('home-title-watchlist'), skipOffstage: false),
        findsOneWidget,
      );
      expect(find.text('Game of Thrones', skipOffstage: false), findsWidgets);
      await tester.pumpWidget(const SizedBox());
      shellContent = ContentMode.anime;
    });

    testWidgets('movies only puts films on Home and Search', (tester) async {
      await launchWith(tester, 'movies');
      expect(rootTabs, ['Home', 'Search', 'More']);
      expect(find.text('Trending movies'), findsOneWidget);
      expect(find.byType(HomePage), findsNothing);
      await tester.tap(find.byKey(const ValueKey('nav-Search')));
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('movie-search-field')), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
    });

    testWidgets('anime only keeps the anime tabs', (tester) async {
      await launchWith(tester, 'anime');
      expect(rootTabs, ['Home', 'Search', 'More']);
      expect(find.byType(MoviesHome), findsNothing);
      expect(
        find.byKey(const Key('home-title-watchlist'), skipOffstage: false),
        findsNothing,
      );
      await tester.tap(find.byKey(const ValueKey('nav-Search')));
      await tester.pumpAndSettle();
      expect(find.text('Search anime'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
      shellContent = ContentMode.anime;
    });
  });
}
