import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/data/catalog.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/main.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/ui/home_page.dart';
import 'package:otakase/ui/show_page.dart';

import 'auth_test.dart' show MemoryTokens, account, json;
import 'tv_focus_test.dart' show focusPoster, key, navigation;
import 'tv_navigation_audit_test.dart' show focusedWithin;
import 'widget_test.dart' as fixture;

const seasonal = Anime(id: 2, title: 'Seasonal story', episodes: 12);
const restricted = Anime(id: 99, title: 'Restricted story', isAdult: true);

class HomeCatalog extends fixture.TestCatalog {
  int calls = 0;
  final adultFlags = <bool>[];
  HomeData data = const HomeData(
    trending: [fixture.anime, restricted],
    season: [seasonal],
  );
  Object? error;
  Completer<HomeData>? pending;
  @override
  Future<HomeData> home({required bool adult}) async {
    calls++;
    adultFlags.add(adult);
    if (pending != null) return pending!.future;
    if (error != null) throw error!;
    return data;
  }

  @override
  Future<Anime> detail(Anime anime, {required bool adult}) async => anime;
}

Future<AppServices> launchHome(
  WidgetTester tester, {
  bool tv = false,
  required HomeCatalog catalog,
  AuthService? auth,
}) async {
  await fixture.launch(tester, tv: tv);
  await tester.pumpWidget(const SizedBox());
  final base = await fixture.services();
  await base.store.set('SyncAniListMAL', false);
  final services = AppServices(
    catalog: catalog,
    store: base.store,
    core: base.core,
    auth: auth ?? AuthService(storage: MemoryTokens()),
  );
  await tester.pumpWidget(OtakaseApp(services: services, isTV: tv));
  await tester.pumpAndSettle();
  addTearDown(() async {
    await tester.pumpWidget(const SizedBox());
    await services.close();
  });
  return services;
}

void main() {
  for (final tv in [false, true]) {
    testWidgets('unlinked ${tv ? 'TV' : 'phone'} Home offers safe discovery', (
      tester,
    ) async {
      final catalog = HomeCatalog();
      await launchHome(tester, tv: tv, catalog: catalog);
      expect(find.text('Find your next story'), findsOneWidget);
      expect(find.byKey(const Key('discovery-featured')), findsOneWidget);
      expect(find.byKey(const Key('home-refresh')), findsNothing);
      expect(find.byTooltip('Refresh lists'), findsNothing);
      expect(find.byType(AccountBlock), findsNothing);
      expect(find.text('Back to your story'), findsNothing);
      expect(find.text('Restricted story'), findsNothing);
      expect(catalog.adultFlags, [false]);
      await tester.ensureVisible(find.text('Trending'));
      expect(find.text('Trending'), findsOneWidget);
      await tester.ensureVisible(find.text('Popular this season'));
      expect(find.text('Popular this season'), findsOneWidget);
      final poster = find.byKey(const ValueKey('Popular this season-al:2'));
      await tester.ensureVisible(poster);
      await tester.pumpAndSettle();
      await tester.tap(poster);
      await tester.pumpAndSettle();
      expect(find.byType(ShowPage), findsOneWidget);
      expect(find.text('Seasonal story'), findsWidgets);
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(catalog.calls, 2);
      expect(find.byKey(const Key('discovery-featured')), findsOneWidget);
      expect(tester.takeException(), isNull);
    });
  }

  testWidgets('unlinked Home retains local resume above discovery', (
    tester,
  ) async {
    final services = await launchHome(tester, catalog: HomeCatalog());
    await services.store.progress(fixture.anime, 5, 720, 1440);
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('resume-card')), findsOneWidget);
    expect(find.text('Back to your story'), findsOneWidget);
    expect(find.byKey(const Key('discovery-featured')), findsNothing);
    expect(find.byKey(const Key('home-refresh')), findsNothing);
    await tester.ensureVisible(find.text('Trending'));
    expect(find.text('Trending'), findsOneWidget);
  });

  testWidgets(
    'discovery refreshes on Home return without stealing header focus',
    (tester) async {
      final catalog = HomeCatalog();
      final services = await launchHome(tester, tv: true, catalog: catalog);
      navigation(tester, 'Home').requestFocus();
      await tester.pumpAndSettle();
      expect(catalog.calls, 1);
      await key(tester, LogicalKeyboardKey.arrowRight);
      catalog.pending = Completer<HomeData>();
      await key(tester, LogicalKeyboardKey.arrowLeft);
      expect(catalog.calls, 2);
      expect(navigation(tester, 'Home').hasPrimaryFocus, isTrue);
      catalog.pending!.complete(catalog.data);
      await tester.pumpAndSettle();
      expect(navigation(tester, 'Home').hasPrimaryFocus, isTrue);
      await services.store.set('Volume', 80);
      await tester.pumpAndSettle();
      expect(catalog.calls, 2);
    },
  );

  testWidgets(
    'discovery failure keeps Search and Accounts and supports Retry',
    (tester) async {
      final catalog = HomeCatalog()
        ..error = const CatalogFailure('Catalog unavailable');
      await launchHome(tester, catalog: catalog);
      expect(find.text('Catalog unavailable'), findsOneWidget);
      expect(find.text('Search anime'), findsOneWidget);
      expect(find.text('Accounts'), findsOneWidget);
      catalog.error = null;
      await tester.ensureVisible(find.text('Retry'));
      await tester.tap(find.text('Retry'));
      await tester.pumpAndSettle();
      expect(catalog.calls, 2);
      expect(find.text('Catalog unavailable'), findsNothing);
      expect(find.byKey(const Key('discovery-featured')), findsOneWidget);
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets('empty discovery keeps useful guest actions', (tester) async {
    await launchHome(tester, catalog: HomeCatalog()..data = const HomeData());
    expect(find.text('Search anime'), findsOneWidget);
    expect(find.text('Accounts'), findsOneWidget);
    expect(
      find.text(
        'No featured anime right now. Search for a title to start watching.',
      ),
      findsOneWidget,
    );
    expect(find.byKey(const Key('discovery-featured')), findsNothing);
    expect(tester.takeException(), isNull);
  });

  for (final service in ['AniList', 'MAL']) {
    testWidgets('$service refreshes on Home return and changed category', (
      tester,
    ) async {
      var requests = 0;
      final auth = AuthService(
        storage: MemoryTokens(),
        client: MockClient((request) async {
          final list = service == 'AniList'
              ? request.body.contains('MediaListCollection')
              : request.url.path.endsWith('/animelist');
          if (!list) return json({'data': <String, dynamic>{}});
          requests++;
          return service == 'AniList'
              ? json({
                  'data': {
                    'MediaListCollection': {
                      'lists': [
                        {
                          'entries': [
                            {
                              'status': 'CURRENT',
                              'progress': 3,
                              'media': fixture.anime.toJson(),
                            },
                            {
                              'status': 'PLANNING',
                              'progress': 0,
                              'media': seasonal.toJson(),
                            },
                          ],
                        },
                      ],
                    },
                  },
                })
              : json({
                  'data': [
                    {
                      'node': {
                        'id': fixture.anime.malId,
                        'title': fixture.anime.title,
                        'num_episodes': 28,
                      },
                      'list_status': {
                        'status': 'watching',
                        'num_episodes_watched': 3,
                      },
                    },
                    {
                      'node': {
                        'id': 2,
                        'title': seasonal.title,
                        'num_episodes': 12,
                      },
                      'list_status': {
                        'status': 'plan_to_watch',
                        'num_episodes_watched': 0,
                      },
                    },
                  ],
                });
        }),
      );
      auth.accounts[service] = account(service);
      final catalog = HomeCatalog();
      final services = await launchHome(
        tester,
        tv: true,
        catalog: catalog,
        auth: auth,
      );
      expect(catalog.calls, 0);
      expect(requests, 1);
      expect(find.byKey(const Key('home-refresh')), findsNothing);
      expect(find.byTooltip('Refresh lists'), findsNothing);
      navigation(tester, 'Search').requestFocus();
      await tester.pumpAndSettle();
      await key(tester, LogicalKeyboardKey.arrowLeft);
      expect(requests, 2);
      expect(navigation(tester, 'Home').hasPrimaryFocus, isTrue);
      final trigger = tester
          .widgetList<FocusBox>(find.byType(FocusBox))
          .singleWhere((w) => w.focusNode?.debugLabel == 'List category')
          .focusNode!;
      trigger.requestFocus();
      await tester.pumpAndSettle();
      await key(tester, LogicalKeyboardKey.select);
      await key(tester, LogicalKeyboardKey.arrowDown);
      await key(tester, LogicalKeyboardKey.select);
      expect(requests, 3);
      expect(trigger.hasPrimaryFocus, isTrue);
      final chosen = service == 'MAL' ? 'plan_to_watch' : 'PLANNING';
      expect(services.store.text('ListCategory.$service'), chosen);
      final poster = find.byKey(
        ValueKey('$service-$chosen-${service == 'MAL' ? 'mal:2' : 'al:2'}'),
      );
      await focusPoster(tester, poster);
      await key(tester, LogicalKeyboardKey.select);
      expect(find.byType(ShowPage), findsOneWidget);
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(requests, 4);
      expect(focusedWithin(poster), isTrue);
      expect(services.store.text('ListCategory.$service'), chosen);
      expect(tester.takeException(), isNull);
    });
  }
}
