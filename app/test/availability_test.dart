import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/availability.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/movies.dart';
import 'package:otakase/data/store.dart';
import 'package:otakase/data/trakt.dart';
import 'package:otakase/ui/components.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'movies_test.dart' show offlineTrakt;

/// Answers the availability operations: [has] names the sources that have
/// every show, [down] the ones that cannot be reached; the catalog lists
/// [catalog].
class AvailabilityCore extends CoreClient {
  AvailabilityCore({
    this.has = const {},
    this.down = const {},
    this.catalog = const [],
  });
  final Set<String> has, down;
  final List<Map<String, String>> catalog;
  final calls = <Json>[];

  @override
  Future<Json> request(
    Json input, {
    void Function(int)? onHandle,
    RequestScope? scope,
    Duration timeout = const Duration(seconds: 40),
  }) async {
    calls.add(input);
    final provider = input['provider'];
    switch (input['operation']) {
      case 'providers':
        return {
          'providers': ['anikoto', 'megavid', 'animex'],
          'adult': <String>[],
        };
      case 'movie_providers':
        return {
          'providers': [
            {'name': 'vidsrc', 'label': 'v', 'language': 'en', 'series': true},
            {'name': '2embed', 'label': '2', 'language': 'en', 'series': true},
          ],
        };
      case 'available':
      case 'movie_available':
        if (provider == 'vidsrc') {
          throw const CoreFailure('unsupported_provider', 'Play to check.');
        }
        if (down.contains(provider)) {
          throw const CoreFailure('provider_error', 'Source request failed.');
        }
        return {'available': has.contains(provider)};
      case 'catalog':
        return {'shows': catalog};
    }
    return {};
  }
}

Future<AppStore> freshStore() async {
  SharedPreferences.setMockInitialValues({});
  return AppStore(await SharedPreferences.getInstance());
}

Availability availability(AppStore store, CoreClient core) => Availability(
  store: store,
  resolver: SourceResolver(core, store),
  movies: MovieService(trakt: offlineTrakt(), core: core, store: store),
);

const frieren = Anime(
  id: 154587,
  malId: 52991,
  title: "Frieren: Beyond Journey's End",
  titles: ['Sousou no Frieren', "Frieren: Beyond Journey's End"],
  status: 'FINISHED',
);
const obscure = Anime(
  id: 9001,
  malId: 9002,
  title: 'Nobody Uploaded This',
  status: 'FINISHED',
);
const upcoming = Anime(
  id: 9100,
  malId: 9101,
  title: 'Next Year',
  status: 'NOT_YET_RELEASED',
);

void main() {
  test('upcoming titles are known at once, without a request', () async {
    final store = await freshStore();
    final core = AvailabilityCore();
    final known = availability(store, core);
    expect(known.of(upcoming), Playable.no);
    expect(known.of(upcoming, upcomingOk: true), Playable.unknown);
    expect(known.of(frieren), Playable.unknown);
    expect(core.calls, isEmpty);
  });

  test('a catalog page marks every show on it playable', () async {
    final store = await freshStore();
    final core = AvailabilityCore(
      catalog: [
        {
          'title': "Frieren: Beyond Journey's End",
          'alt_title': 'Sousou no Frieren',
        },
      ],
    );
    final known = availability(store, core);
    await known.refreshCatalog();
    expect(known.of(frieren), Playable.yes);
    expect(known.of(obscure), Playable.unknown);
    final pages = core.calls.where((c) => c['operation'] == 'catalog');
    expect(pages.length, Availability.catalogPages.length);

    // Read once a day: a second start asks nothing.
    core.calls.clear();
    await known.refreshCatalog();
    expect(core.calls.where((c) => c['operation'] == 'catalog'), isEmpty);

    // Kept on the device for the next start.
    await pumpEventQueue();
    expect(
      availability(AppStore(store.preferences), core).of(frieren),
      Playable.yes,
    );
  });

  test('a search also asks the catalog source once', () async {
    final store = await freshStore();
    final core = AvailabilityCore(
      catalog: [
        {'title': 'Nobody Uploaded This'},
      ],
    );
    final known = availability(store, core);
    await known.searched('nobody');
    expect(
      core.calls.where((c) => c['operation'] == 'catalog').single['query'],
      'nobody',
    );
    expect(known.of(obscure), Playable.yes);
  });

  test(
    'a title checked stops at the first source that has it, megavid first',
    () async {
      final store = await freshStore();
      final core = AvailabilityCore(has: {'megavid'});
      final known = availability(store, core);
      await known.check(frieren);
      expect(known.of(frieren), Playable.yes);
      final asked = core.calls.where((c) => c['operation'] == 'available');
      expect(asked.map((c) => c['provider']), ['megavid']);
    },
  );

  test('only every source saying no makes a title unplayable', () async {
    final store = await freshStore();
    final none = availability(store, AvailabilityCore());
    await none.check(obscure);
    expect(none.of(obscure), Playable.no);

    final store2 = await freshStore();
    final oneDown = availability(store2, AvailabilityCore(down: {'animex'}));
    await oneDown.check(obscure);
    expect(oneDown.of(obscure), Playable.unknown);
  });

  test('a no is asked again after a few days, a yes after a month', () async {
    final store = await freshStore();
    var now = DateTime(2026, 11, 16);
    final core = AvailabilityCore();
    final known = Availability(
      store: store,
      resolver: SourceResolver(core, store),
      movies: MovieService(trakt: offlineTrakt(), core: core, store: store),
      now: () => now,
    );
    known.remember(obscure.key, false);
    known.remember(frieren.key, true);
    now = now.add(const Duration(days: 4));
    expect(known.of(obscure), Playable.unknown);
    expect(known.of(frieren), Playable.yes);
    now = now.add(const Duration(days: 30));
    expect(known.of(frieren), Playable.unknown);
  });

  test('playback teaches it: a play is a yes', () async {
    final store = await freshStore();
    final core = AvailabilityCore();
    final known = availability(store, core);
    known.resolver.learned!(obscure, true);
    expect(known.of(obscure), Playable.yes);
  });

  test('hiding leaves out only known unplayable titles, when on', () async {
    final store = await freshStore();
    final known = availability(store, AvailabilityCore());
    known.remember(obscure.key, false);
    final list = [frieren, obscure, upcoming];
    expect(known.visible(list), list);
    await store.set('HideUnplayable', true);
    expect(known.visible(list), [frieren]);
    expect(known.visible(list, upcomingOk: true), [frieren, upcoming]);
  });

  test(
    'films: 2Embed answers for vidsrc too, upcoming films are known',
    () async {
      final store = await freshStore();
      final core = AvailabilityCore(has: {'2embed'});
      final known = availability(store, core);
      const film = MediaTitle(kind: 'movie', trakt: 1, title: 'A', imdb: 'tt1');
      await known.checkTitle(film);
      expect(
        core.calls
            .where((c) => c['operation'] == 'movie_available')
            .map((c) => c['provider']),
        ['2embed'],
      );
      await store.set('HideUnplayable', true);
      expect(known.visibleTitles([film]), [film]);

      const soon = MediaTitle(
        kind: 'movie',
        trakt: 2,
        title: 'B',
        status: 'post production',
      );
      expect(known.visibleTitles([soon]), isEmpty);
      expect(known.visibleTitles([soon], upcomingOk: true), [soon]);

      final none = availability(await freshStore(), AvailabilityCore());
      const missing = MediaTitle(
        kind: 'movie',
        trakt: 3,
        title: 'C',
        imdb: 'tt3',
      );
      await none.checkTitle(missing);
      await none.store.set('HideUnplayable', true);
      expect(none.visibleTitles([missing]), isEmpty);
    },
  );

  testWidgets('a card no source has is greyed and says so', (tester) async {
    final store = await freshStore();
    final known = availability(store, AvailabilityCore());
    Availability.active = known;
    addTearDown(() => Availability.active = null);
    known.remember(obscure.key, false);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Row(
            children: [
              PosterCard(anime: obscure, onOpen: () {}, isTV: false),
              PosterCard(anime: frieren, onOpen: () {}, isTV: false),
            ],
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('No source has this'), findsOneWidget);
    final faded = tester
        .widgetList<AnimatedOpacity>(find.byType(AnimatedOpacity))
        .map((o) => o.opacity)
        .toList();
    expect(faded, containsAll([.38, 1.0]));
  });
}
