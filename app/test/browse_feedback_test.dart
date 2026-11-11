import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/data/catalog.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/trakt.dart';
import 'package:otakase/main.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/catalog_pages.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/ui/home_page.dart';
import 'package:otakase/ui/movies_pages.dart';
import 'package:otakase/ui/show_page.dart';

import 'auth_test.dart' show MemoryTokens;
import 'widget_test.dart' as fixture;
import 'uniform_ui_test.dart' show desktop;

class FeedbackCatalog extends Catalog {
  FeedbackCatalog({super.client});
  final searches = <({String query, int page, String? format, String? sort})>[];
  Future<CatalogPage> Function(int page)? answer;
  Future<List<Airing>> Function(DateTime day, int page)? calendar;
  @override
  Future<HomeData> home({required bool adult}) async =>
      const HomeData(trending: [], season: []);
  @override
  Future<List<String>> genres({required bool adult}) async => ['Action'];
  @override
  Future<Anime> detail(Anime anime, {required bool adult}) async => anime;
  @override
  Future<CatalogPage> search(
    String query, {
    required bool adult,
    int page = 1,
    String? format,
    String? season,
    int? year,
    String? genre,
    String? status,
    String? sort,
  }) {
    searches.add((query: query, page: page, format: format, sort: sort));
    return answer?.call(page) ?? Future.value(const CatalogPage([]));
  }

  @override
  Future<List<Airing>> schedule(
    DateTime day, {
    required bool adult,
    int page = 1,
  }) => calendar?.call(day, page) ?? Future.value([]);
}

Future<AppServices> mount(
  WidgetTester tester,
  FeedbackCatalog catalog,
  Widget Function(AppServices app) page, {
  bool tv = false,
  double scale = 1,
  Future<void> Function(AppServices)? prepare,
}) async {
  await fixture.launch(tester, tv: tv);
  await tester.pumpWidget(const SizedBox());
  tester.platformDispatcher.textScaleFactorTestValue = scale;
  addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);
  final base = await fixture.services();
  final app = AppServices(
    catalog: catalog,
    store: base.store,
    core: base.core,
    auth: AuthService(storage: MemoryTokens(), client: catalog.client),
  );
  if (prepare != null) await prepare(app);
  await tester.pumpWidget(
    ImageSources(
      provider: (_) => const AssetImage('assets/otakase.png'),
      child: OtakaseApp(services: app, isTV: tv),
    ),
  );
  await tester.pumpAndSettle();
  catalog.searches.clear();
  Navigator.of(tester.element(find.byType(HomePage)))
      .push(MaterialPageRoute<void>(builder: (_) => page(app)));
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 300));
  addTearDown(() async {
    await tester.pumpWidget(const SizedBox());
    await app.close();
  });
  return app;
}

Widget search(AppServices app, {bool tv = false}) => Scaffold(
  body: SearchPage(
    services: app,
    isTV: tv,
    initialQuery: 'Frieren',
    initialFormat: 'TV',
    onOpen: (_) {},
    onVoice: () async => null,
  ),
);
http.Response reply(Object value, [int status = 200]) =>
    http.Response(jsonEncode(value), status);
const series = MediaTitle(
  kind: 'show',
  trakt: 7,
  title: 'Example series',
  overview: 'A story.',
  fanart: 'https://fixture.invalid/banner.jpg',
);

/// Poster cards whose key starts with [prefix], built or not.
/// Only the page under test counts, not the shell's own Search tab below.
Finder cardsKeyed(String prefix) => find.descendant(
  of: find.byKey(const Key('page-under-test'), skipOffstage: false),
  matching: find.byWidgetPredicate(
    (w) =>
        w is PosterCard &&
        w.key is ValueKey<String> &&
        (w.key! as ValueKey<String>).value.startsWith(prefix),
    skipOffstage: false,
  ),
  skipOffstage: false,
);

List<Map<String, Object>> traktRows(String kind, int count) => [
  for (var i = 1; i <= count; i++)
    {
      kind: {
        'title': 'Trending $kind $i',
        'year': 2020,
        'ids': {'trakt': (kind == 'movie' ? 1000 : 2000) + i},
      },
    },
];

void main() {
  testWidgets(
    'empty search shows trending anime and films, each capped so both fit',
    (tester) async {
      final paths = <String>[];
      final catalog =
          FeedbackCatalog(
              client: MockClient((request) async {
                paths.add(request.url.path);
                return switch (request.url.path) {
                  '/movies/trending' => reply(traktRows('movie', 20)),
                  '/shows/trending' => reply(traktRows('show', 20)),
                  '/search/movie,show' => reply([
                    for (final r in traktRows('movie', 3))
                      {'type': 'movie', ...r},
                  ]),
                  _ => reply([]),
                };
              }),
            )
            ..answer = (page) async => CatalogPage([
              for (var i = 1; i <= 30; i++)
                Anime(id: page * 100 + i, title: 'Anime $page-$i'),
            ], hasMore: true);
      await mount(
        tester,
        catalog,
        (app) => Scaffold(
          key: const Key('page-under-test'),
          body: SearchPage(
            services: app,
            isTV: false,
            titles: true,
            onOpen: (_) {},
            onVoice: () async => null,
          ),
        ),
      );
      // A tall phone, so every section of the page is built.
      tester.view.physicalSize = const Size(412, 4000);
      await tester.pumpAndSettle();
      expect(catalog.searches.single.query, '');
      expect(catalog.searches.single.sort, 'TRENDING');
      expect(paths, containsAll(['/movies/trending', '/shows/trending']));
      expect(find.text('Trending anime'), findsOneWidget);
      expect(find.text('Trending movies & shows'), findsOneWidget);
      final anime = cardsKeyed('search-').evaluate().length;
      final titles = cardsKeyed('title-').evaluate().length;
      expect(anime, inInclusiveRange(1, 12));
      expect(titles, inInclusiveRange(1, 12));
      // Films and series take turns.
      expect(find.text('Trending movie 1'), findsOneWidget);
      expect(find.text('Trending show 1'), findsOneWidget);
      // The capped anime grid does not page on above the films; More opens
      // each section.
      expect(find.text('Load more'), findsNothing);
      expect(find.text('More anime'), findsOneWidget);
      expect(find.text('More movies & shows'), findsOneWidget);
      tester
          .widget<AppButton>(
            find.widgetWithText(AppButton, 'More movies & shows'),
          )
          .onPressed!();
      await tester.pumpAndSettle();
      expect(cardsKeyed('title-').evaluate().length, 40);
      expect(find.text('More movies & shows'), findsNothing);

      // Typing swaps trending for results; clearing brings trending back.
      await tester.enterText(find.byKey(const Key('catalog-search')), 'Fri');
      await tester.pump(const Duration(milliseconds: 500));
      await tester.pumpAndSettle();
      expect(catalog.searches.last.query, 'Fri');
      expect(catalog.searches.last.sort, isNull);
      expect(find.text('Trending anime'), findsNothing);
      expect(find.text('Trending movies & shows'), findsNothing);
      // Results stay split too: some anime, then movies and shows.
      expect(find.text('Anime'), findsOneWidget);
      expect(find.text('Movies & shows'), findsOneWidget);
      expect(cardsKeyed('search-').evaluate().length, inInclusiveRange(1, 15));
      expect(find.text('Load more'), findsNothing);
      tester
          .widget<AppButton>(find.widgetWithText(AppButton, 'More anime'))
          .onPressed!();
      await tester.pumpAndSettle();
      expect(cardsKeyed('search-').evaluate().length, 30);
      expect(find.text('Load more'), findsOneWidget);
      await tester.enterText(find.byKey(const Key('catalog-search')), '');
      await tester.pump(const Duration(milliseconds: 500));
      await tester.pumpAndSettle();
      expect(catalog.searches.last.sort, 'TRENDING');
      expect(find.text('Trending anime'), findsOneWidget);
      expect(find.text('Trending movies & shows'), findsOneWidget);
    },
  );
  testWidgets(
    'a films-only genre keeps anime and films together where it can',
    (tester) async {
      final paths = <Uri>[];
      final catalog =
          FeedbackCatalog(
              client: MockClient((request) async {
                paths.add(request.url);
                return switch (request.url.path) {
                  '/movies/popular' => reply(traktRows('movie', 3)),
                  '/shows/popular' => reply(traktRows('show', 3)),
                  _ => reply([]),
                };
              }),
            )
            ..answer = (page) async => CatalogPage([
              for (var i = 1; i <= 5; i++)
                Anime(id: page * 100 + i, title: 'Anime $page-$i'),
            ]);
      await mount(
        tester,
        catalog,
        (app) => Scaffold(
          key: const Key('page-under-test'),
          body: SearchPage(
            services: app,
            isTV: false,
            titles: true,
            onOpen: (_) {},
            onVoice: () async => null,
          ),
        ),
      );
      tester.view.physicalSize = const Size(412, 4000);
      await tester.pumpAndSettle();
      final filters = find.descendant(
        of: find.byKey(const Key('page-under-test')),
        matching: find.byKey(const Key('search-filters')),
      );
      // Action is both an anime and a film genre: both sections show.
      await tester.tap(filters);
      await tester.pumpAndSettle();
      // Film genres join the anime ones.
      expect(find.text('Crime'), findsOneWidget);
      await tester.ensureVisible(find.text('Action'));
      await tester.tap(find.text('Action'));
      await tester.ensureVisible(find.byKey(const Key('filters-apply')));
      await tester.tap(find.byKey(const Key('filters-apply')));
      await tester.pumpAndSettle();
      expect(find.text('Anime'), findsOneWidget);
      expect(find.text('Movies & shows'), findsOneWidget);
      expect(cardsKeyed('search-').evaluate().length, 5);
      expect(cardsKeyed('title-').evaluate().length, 6);
      expect(paths.last.queryParameters['genres'], 'action');

      // Crime has no anime: no anime lookup, films still show.
      final searches = catalog.searches.length;
      await tester.tap(filters);
      await tester.pumpAndSettle();
      await tester.ensureVisible(find.text('Crime'));
      await tester.tap(find.text('Crime'));
      await tester.ensureVisible(find.byKey(const Key('filters-apply')));
      await tester.tap(find.byKey(const Key('filters-apply')));
      await tester.pumpAndSettle();
      expect(catalog.searches.length, searches);
      expect(cardsKeyed('search-').evaluate(), isEmpty);
      expect(cardsKeyed('title-').evaluate().length, 6);
      expect(paths.last.queryParameters['genres'], 'crime');
    },
  );
  testWidgets('anime-only empty search pages through trending anime', (
    tester,
  ) async {
    final catalog = FeedbackCatalog()
      ..answer = (page) async => CatalogPage([
        for (var i = 1; i <= 30; i++)
          Anime(id: page * 100 + i, title: 'Anime $page-$i'),
      ], hasMore: true);
    await mount(
      tester,
      catalog,
      (app) => Scaffold(
        key: const Key('page-under-test'),
        body: SearchPage(
          services: app,
          isTV: false,
          onOpen: (_) {},
          onVoice: () async => null,
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(catalog.searches.single.sort, 'TRENDING');
    expect(find.text('Trending anime'), findsOneWidget);
    expect(find.text('Trending movies & shows'), findsNothing);
    expect(cardsKeyed('search-').evaluate().length, 30);
    await tester.scrollUntilVisible(
      find.text('Load more'),
      400,
      scrollable: find
          .descendant(
            of: find.byKey(const Key('page-under-test')),
            matching: find.byType(Scrollable),
          )
          .first,
    );
    // The bottom bar may cover it, so press it directly.
    tester
        .widget<AppButton>(
          find.widgetWithText(AppButton, 'Load more', skipOffstage: false),
        )
        .onPressed!();
    await tester.pumpAndSettle();
    expect(catalog.searches.last.page, 2);
    expect(catalog.searches.last.sort, 'TRENDING');
  });
  testWidgets('movie search shows trending films and series until asked', (
    tester,
  ) async {
    final paths = <String>[];
    final catalog = FeedbackCatalog(
      client: MockClient((request) async {
        paths.add(request.url.path);
        return switch (request.url.path) {
          '/movies/trending' => reply(traktRows('movie', 3)),
          '/shows/trending' => reply(traktRows('show', 3)),
          _ => reply([]),
        };
      }),
    );
    await mount(
      tester,
      catalog,
      (app) => Scaffold(body: MovieSearchPage(services: app, isTV: false)),
    );
    await tester.pumpAndSettle();
    expect(find.text('Trending movies & shows'), findsOneWidget);
    expect(find.text('Trending movie 1'), findsOneWidget);
    expect(find.text('Trending show 1'), findsOneWidget);
    expect(find.text('Search for a movie or show.'), findsNothing);
    final field = find.byKey(const Key('movie-search-field'));
    await tester.enterText(field, 'Arrival');
    await tester.pump(const Duration(milliseconds: 500));
    await tester.pumpAndSettle();
    expect(find.text('Trending movies & shows'), findsNothing);
    expect(find.text('No matching movies or shows.'), findsOneWidget);
    final asked = paths.where((p) => p.endsWith('/trending')).length;
    await tester.enterText(field, '');
    await tester.pump(const Duration(milliseconds: 500));
    await tester.pumpAndSettle();
    expect(find.text('Trending movie 1'), findsOneWidget);
    // Clearing the field reuses the trending lists already fetched.
    expect(paths.where((p) => p.endsWith('/trending')).length, asked);
  });
  for (final tv in [false, true]) {
    testWidgets(
      'anime failure is distinct from empty and retry retains query/filters on ${tv ? 'TV' : 'phone'}',
      (tester) async {
        final gate = Completer<CatalogPage>();
        final catalog = FeedbackCatalog()..answer = (_) => gate.future;
        await mount(tester, catalog, (app) => search(app, tv: tv), tv: tv);
        expect(find.byType(SkeletonBlock), findsWidgets);
        expect(find.byType(FailureView), findsNothing);
        expect(find.textContaining('No matching anime'), findsNothing);
        gate.completeError(
          const CatalogFailure(
            'ExampleProvider https://source.example.invalid/api?token=hidden failed.',
          ),
        );
        await tester.pumpAndSettle();
        expect(find.byType(FailureView), findsOneWidget);
        expect(find.textContaining('ExampleProvider'), findsNothing);
        expect(find.textContaining('source.example.invalid'), findsNothing);
        expect(find.textContaining('No matching anime'), findsNothing);
        catalog.answer = (_) async => const CatalogPage([]);
        await tester.ensureVisible(find.text('Retry'));
        await tester.tap(find.text('Retry'));
        await tester.pumpAndSettle();
        expect(catalog.searches.map((r) => (r.query, r.page, r.format)), [
          ('Frieren', 1, 'TV'),
          ('Frieren', 1, 'TV'),
        ]);
        expect(find.byType(FailureView), findsNothing);
        expect(find.textContaining('No matching anime'), findsOneWidget);
        expect(find.text('Clear filters'), findsOneWidget);
      },
    );
  }
  testWidgets(
    'failed Load more retains cards and retries the same page before advancing',
    (tester) async {
      var fail = true;
      final catalog = FeedbackCatalog()
        ..answer = (page) async {
          if (page == 2 && fail) {
            throw const CatalogFailure(
              'ExampleProvider https://source.example.invalid/page failed.',
            );
          }
          return CatalogPage([
            Anime(id: page, title: 'Result $page'),
          ], hasMore: page < 3);
        };
      await mount(tester, catalog, search);
      await tester.pumpAndSettle();
      final original = tester.element(find.byKey(const Key('search-al:1')));
      await tester.ensureVisible(find.text('Load more'));
      await tester.tap(find.text('Load more'));
      await tester.pumpAndSettle();
      expect(find.byType(FailureView), findsOneWidget);
      expect(find.textContaining('ExampleProvider'), findsNothing);
      expect(find.textContaining('source.example.invalid'), findsNothing);
      expect(
        identical(
          tester.element(find.byKey(const Key('search-al:1'))),
          original,
        ),
        isTrue,
      );
      await tester.ensureVisible(find.text('Retry'));
      await tester.tap(find.text('Retry'));
      await tester.pumpAndSettle();
      expect(catalog.searches.map((r) => r.page), [1, 2, 2]);
      fail = false;
      await tester.tap(find.text('Retry'));
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('search-al:1')), findsOneWidget);
      expect(find.byKey(const Key('search-al:2')), findsOneWidget);
      await tester.ensureVisible(find.text('Load more'));
      await tester.tap(find.text('Load more'));
      await tester.pumpAndSettle();
      expect(catalog.searches.map((r) => r.page), [1, 2, 2, 2, 3]);
      expect(
        catalog.searches.every((r) => r.query == 'Frieren' && r.format == 'TV'),
        isTrue,
      );
      expect(find.byKey(const Key('search-al:3')), findsOneWidget);
    },
  );
  testWidgets(
    'movie search retries the same query, distinguishes empty and clears failed/loading state',
    (tester) async {
      final gates = <Completer<http.Response>>[];
      final queries = <String?>[];
      final catalog = FeedbackCatalog(
        client: MockClient((request) async {
          if (request.url.path.startsWith('/search/')) {
            queries.add(request.url.queryParameters['query']);
            final gate = Completer<http.Response>();
            gates.add(gate);
            return gate.future;
          }
          return reply([]);
        }),
      );
      await mount(
        tester,
        catalog,
        (app) => Scaffold(body: MovieSearchPage(services: app, isTV: false)),
      );
      await tester.pumpAndSettle();
      expect(find.text('Search for a movie or show.'), findsOneWidget);
      final field = find.byKey(const Key('movie-search-field'));
      await tester.enterText(field, 'Arrival');
      await tester.pump(const Duration(milliseconds: 500));
      expect(find.text('Searching movies and shows…'), findsOneWidget);
      gates[0].complete(reply({}, 503));
      await tester.pumpAndSettle();
      expect(find.byType(FailureView), findsOneWidget);
      expect(find.textContaining('ExampleProvider'), findsNothing);
      expect(find.textContaining('source.example.invalid'), findsNothing);
      expect(find.text('No matching movies or shows.'), findsNothing);
      await tester.ensureVisible(find.text('Retry'));
      await tester.tap(find.text('Retry'));
      await tester.pump();
      expect(queries, ['Arrival', 'Arrival']);
      gates[1].complete(reply([]));
      await tester.pumpAndSettle();
      expect(find.text('No matching movies or shows.'), findsOneWidget);
      await tester.enterText(field, 'Pending');
      await tester.pump(const Duration(milliseconds: 500));
      await tester.enterText(field, '');
      await tester.pump(const Duration(milliseconds: 500));
      expect(find.byType(LinearProgressIndicator), findsNothing);
      expect(find.text('Search for a movie or show.'), findsOneWidget);
      gates[2].complete(
        reply([
          {
            'type': 'movie',
            'movie': {
              'title': 'Stale result',
              'ids': {'trakt': 42},
            },
          },
        ]),
      );
      await tester.pumpAndSettle();
      expect(find.text('Stale result'), findsNothing);
    },
  );
  testWidgets(
    'successful empty movie shelves are not reported as a network failure',
    (tester) async {
      final catalog = FeedbackCatalog(
        client: MockClient((_) async => reply([])),
      );
      await mount(
        tester,
        catalog,
        (app) => MoviesHome(services: app, isTV: false),
      );
      await tester.pumpAndSettle();
      expect(
        find.text('No movies or shows to display right now.'),
        findsOneWidget,
      );
      expect(find.byType(FailureView), findsNothing);
    },
  );
  testWidgets(
    'failed movie shelves offer retry and preserve success/error distinction',
    (tester) async {
      var failed = true;
      final catalog = FeedbackCatalog(
        client: MockClient((_) async => failed ? reply({}, 503) : reply([])),
      );
      await mount(
        tester,
        catalog,
        (app) => MoviesHome(services: app, isTV: false),
      );
      await tester.pumpAndSettle();
      expect(find.byType(FailureView), findsOneWidget);
      expect(find.textContaining('ExampleProvider'), findsNothing);
      expect(find.textContaining('source.example.invalid'), findsNothing);
      expect(
        find.text('No movies or shows to display right now.'),
        findsNothing,
      );
      failed = false;
      await tester.ensureVisible(find.text('Retry'));
      await tester.tap(find.text('Retry'));
      await tester.pumpAndSettle();
      expect(
        find.text('No movies or shows to display right now.'),
        findsOneWidget,
      );
      expect(find.byType(FailureView), findsNothing);
    },
  );
  for (final tv in [false, true]) {
    testWidgets(
      'unreleased anime explains disabled playback/downloads on ${tv ? 'TV' : 'phone'}',
      (tester) async {
        await mount(
          tester,
          FeedbackCatalog(),
          (app) => ShowPage(
            services: app,
            anime: const Anime(
              id: 8,
              title: 'Upcoming',
              status: 'NOT_YET_RELEASED',
            ),
            isTV: tv,
          ),
          tv: tv,
          scale: tv ? 1.8 : 1,
        );
        await tester.pumpAndSettle();
        final play = tester.widget<AppButton>(
          find.byKey(const Key('show-play')),
        );
        expect(play.onPressed, isNull);
        expect(play.disabledReason, 'No episodes have aired yet.');
        expect(
          find.byKey(const Key('show-action-explanation')),
          findsOneWidget,
        );
        if (!tv) {
          expect(
            tester
                .widget<AppButton>(find.byKey(const Key('show-download')))
                .disabledReason,
            play.disabledReason,
          );
        }
        expect(tester.takeException(), isNull);
      },
    );
  }
  testWidgets(
    'series distinguishes loading, failure and no aired episodes with a working retry',
    (tester) async {
      final gates = <Completer<http.Response>>[];
      final catalog = FeedbackCatalog(
        client: MockClient((request) async {
          if (request.url.path.endsWith('/seasons')) {
            final gate = Completer<http.Response>();
            gates.add(gate);
            return gate.future;
          }
          return reply([]);
        }),
      );
      await mount(
        tester,
        catalog,
        (app) => TitlePage(services: app, title: series, isTV: true),
        tv: true,
      );
      expect(
        tester
            .widget<AppButton>(find.byKey(const Key('title-play')))
            .disabledReason,
        'Episode information is loading.',
      );
      gates[0].complete(reply({}, 503));
      await tester.pumpAndSettle();
      expect(find.byType(FailureView), findsOneWidget);
      expect(find.textContaining('ExampleProvider'), findsNothing);
      expect(find.textContaining('source.example.invalid'), findsNothing);
      expect(
        tester.widget<AppButton>(find.byKey(const Key('title-play'))).label,
        'Episodes unavailable',
      );
      expect(find.text('No episodes to display yet.'), findsNothing);
      await tester.ensureVisible(find.text('Retry'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Retry'));
      await tester.pump();
      expect(find.text('Episodes could not be loaded.'), findsNothing);
      gates[1].complete(reply([]));
      await tester.pumpAndSettle();
      expect(find.byType(FailureView), findsNothing);
      expect(find.text('No episodes to display yet.'), findsOneWidget);
      expect(
        tester
            .widget<AppButton>(find.byKey(const Key('title-play')))
            .disabledReason,
        'No aired episodes are available.',
      );
      expect(tester.takeException(), isNull);
    },
  );
  testWidgets(
    'episode retry restores the saved season when the first load failed',
    (tester) async {
      var failed = true;
      final catalog = FeedbackCatalog(
        client: MockClient((request) async {
          if (request.url.path.endsWith('/seasons')) {
            if (failed) return reply({}, 503);
            return reply([
              for (final season in [1, 2])
                {
                  'number': season,
                  'episodes': [
                    {
                      'number': 1,
                      'title': 'First',
                      'first_aired': '2020-01-01T00:00:00Z',
                    },
                  ],
                },
            ]);
          }
          return reply([]);
        }),
      );
      await mount(
        tester,
        catalog,
        (app) => TitlePage(services: app, title: series, isTV: false),
        prepare: (app) =>
            app.movies.progress(series, 90, 1400, season: 2, episode: 1),
      );
      await tester.pumpAndSettle();
      failed = false;
      await tester.ensureVisible(find.text('Retry'));
      await tester.tap(find.text('Retry'));
      await tester.pumpAndSettle();
      expect(
        tester.widget<AppTab>(find.byKey(const Key('season-2'))).selected,
        isTrue,
      );
      expect(
        tester.widget<AppButton>(find.byKey(const Key('title-play'))).onPressed,
        isNotNull,
      );
    },
  );
  testWidgets(
    'Schedule keeps the selected day and loaded rows when an extra page fails',
    (tester) async {
      final requested = <(DateTime, int)>[];
      var failed = true;
      final catalog = FeedbackCatalog()
        ..calendar = (day, page) async {
          requested.add((day, page));
          if (page == 2 && failed) {
            throw const CatalogFailure(
              'ExampleProvider https://source.example.invalid/calendar failed.',
            );
          }
          return List.generate(
            page == 1 ? 50 : 1,
            (i) => Airing(
              Anime(id: page * 100 + i, title: 'Airing $page-$i'),
              1,
              DateTime(day.year, day.month, day.day, 18),
            ),
          );
        };
      await mount(
        tester,
        catalog,
        (app) => Scaffold(
          body: SchedulePage(services: app, isTV: false, onOpen: (_) {}),
        ),
      );
      await tester.pumpAndSettle();
      await tester.scrollUntilVisible(
        find.text('Load more'),
        600,
        scrollable: find
            .descendant(
              of: find.byType(SchedulePage),
              matching: find.byType(Scrollable),
            )
            .first,
      );
      await tester.pumpAndSettle();
      await Scrollable.ensureVisible(
        tester.element(find.text('Load more')),
        alignment: .5,
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('Load more'));
      await tester.pumpAndSettle();
      expect(find.text('Airing 1-49'), findsOneWidget);
      expect(find.text('A quiet day on the calendar.'), findsNothing);
      failed = false;
      await tester.scrollUntilVisible(
        find.text('Retry'),
        -600,
        scrollable: find
            .descendant(
              of: find.byType(SchedulePage),
              matching: find.byType(Scrollable),
            )
            .first,
      );
      await tester.pumpAndSettle();
      await Scrollable.ensureVisible(
        tester.element(find.text('Retry')),
        alignment: .5,
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('Retry'));
      await tester.pumpAndSettle();
      expect(requested.map((r) => r.$2), [1, 2, 2]);
      expect(requested.map((r) => r.$1).toSet(), hasLength(1));
      await tester.scrollUntilVisible(
        find.text('Airing 2-0'),
        600,
        scrollable: find
            .descendant(
              of: find.byType(SchedulePage),
              matching: find.byType(Scrollable),
            )
            .first,
      );
      await tester.pumpAndSettle();
      expect(find.text('Airing 1-49'), findsOneWidget);
      expect(find.text('Airing 2-0'), findsOneWidget);
      expect(find.byType(FailureView), findsNothing);
    },
  );
  testWidgets(
    'partial movie-list failure keeps available titles and offers retry',
    (tester) async {
      var failed = true;
      final catalog = FeedbackCatalog(
        client: MockClient((request) async {
          if (request.url.path == '/movies/trending') {
            return reply([
              {
                'movie': {
                  'title': 'Available movie',
                  'ids': {'trakt': 99},
                },
              },
            ]);
          }
          return failed ? reply({}, 503) : reply([]);
        }),
      );
      await mount(
        tester,
        catalog,
        (app) => MoviesHome(services: app, isTV: false),
      );
      await tester.pumpAndSettle();
      expect(find.text('Available movie'), findsWidgets);
      expect(
        find.textContaining('Some movie and show lists could not be loaded.'),
        findsOneWidget,
      );
      failed = false;
      await tester.ensureVisible(find.text('Retry'));
      await tester.pumpAndSettle();
      await Scrollable.ensureVisible(
        tester.element(find.text('Retry')),
        alignment: .5,
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('Retry'));
      await tester.pumpAndSettle();
      expect(find.text('Available movie'), findsWidgets);
      expect(find.byType(FailureView), findsNothing);
    },
  );
  testWidgets('weekly Schedule keeps upstream errors out of visible copy', (
    tester,
  ) async {
    final catalog = FeedbackCatalog()
      ..calendar = (_, page) async {
        throw const CatalogFailure(
          'ExampleProvider https://source.example.invalid/calendar failed.',
        );
      };
    await mount(
      tester,
      catalog,
      (app) => Scaffold(
        body: SchedulePage(services: app, isTV: true, onOpen: (_) {}),
      ),
      tv: true,
      prepare: (_) async => desktop(),
    );
    await tester.pumpAndSettle();
    expect(find.byType(FailureView), findsOneWidget);
    expect(
      find.text(
        'Airing times could not be loaded. Check your connection and retry.',
      ),
      findsOneWidget,
    );
    expect(find.textContaining('ExampleProvider'), findsNothing);
    expect(find.textContaining('source.example.invalid'), findsNothing);
  });

  testWidgets('rate-limit guidance does not name the catalog service', (
    tester,
  ) async {
    final catalog = FeedbackCatalog()
      ..answer = (_) async {
        throw const CatalogFailure(
          'ExampleProvider limit at https://source.example.invalid',
          statusCode: 429,
        );
      };
    await mount(tester, catalog, search);
    await tester.pumpAndSettle();
    expect(
      find.text('Too many requests. Wait a moment and retry.'),
      findsOneWidget,
    );
    expect(find.textContaining('ExampleProvider'), findsNothing);
    expect(find.textContaining('source.example.invalid'), findsNothing);
  });
}
