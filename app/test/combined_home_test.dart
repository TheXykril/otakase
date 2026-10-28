import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/main.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/ui/home_page.dart';

import 'auth_test.dart' show MemoryTokens, account, json;
import 'widget_test.dart' as fixture;

http.Response listResponse(http.Request request) {
  if (request.url.host == 'graphql.anilist.co') {
    expect(jsonDecode(request.body)['query'], contains('idMal'));
    return json({
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
              ],
            },
          ],
        },
      },
    });
  }
  return json({
    'data': [
      {
        'node': {'id': fixture.anime.malId, 'title': 'MAL title'},
        'list_status': {'status': 'on_hold', 'num_episodes_watched': 5},
      },
      {
        'node': {'id': 25, 'title': 'MAL-only story'},
        'list_status': {'status': 'plan_to_watch', 'num_episodes_watched': 0},
      },
    ],
  });
}

Future<AppServices> launchCombined(
  WidgetTester tester, {
  bool tv = false,
  bool Function()? malUnavailable,
}) async {
  await fixture.launch(tester, tv: tv);
  await tester.pumpWidget(const SizedBox());
  final base = await fixture.services();
  await base.store.set('SyncAniListMAL', false);
  final auth = AuthService(
    storage: MemoryTokens(),
    client: MockClient(
      (r) async =>
          r.url.host == 'api.myanimelist.net' && malUnavailable?.call() == true
          ? http.Response('', 503)
          : listResponse(r),
    ),
  );
  auth.accounts.addAll({'AniList': account('AniList'), 'MAL': account('MAL')});
  final services = AppServices(
    catalog: base.catalog,
    store: base.store,
    core: base.core,
    auth: auth,
  );
  await tester.pumpWidget(OtakaseApp(services: services, isTV: tv));
  await tester.pumpAndSettle();
  addTearDown(auth.dispose);
  return services;
}

void main() {
  for (final tv in [false, true]) {
    testWidgets(
      'combined Home has one list and shared categories on ${tv ? 'TV' : 'phone'}',
      (tester) async {
        final services = await launchCombined(tester, tv: tv);
        expect(find.byType(AccountBlock), findsOneWidget);
        expect(find.text('Your list'), findsOneWidget);
        expect(find.text('AniList + MAL · viewer'), findsOneWidget);
        final picker = find
            .byType(FocusBox)
            .evaluate()
            .map((e) => e.widget)
            .cast<FocusBox>()
            .singleWhere((w) => w.focusNode?.debugLabel == 'List category');
        picker.focusNode!.requestFocus();
        await tester.pump();
        await tester.sendKeyEvent(LogicalKeyboardKey.select);
        await tester.pumpAndSettle();
        expect(find.text('On hold'), findsNothing);
        for (final status in [
          'All titles',
          'Planning',
          'Completed',
          'Paused',
          'Dropped',
          'Repeating',
        ]) {
          expect(find.text(status), findsOneWidget);
        }
        if (tv) {
          await tester.sendKeyEvent(LogicalKeyboardKey.arrowUp);
          await tester.sendKeyEvent(LogicalKeyboardKey.select);
        } else {
          await tester.tap(find.text('All titles'));
        }
        await tester.pumpAndSettle();
        expect(
          find.byKey(ValueKey('Combined-ALL-${fixture.anime.key}')),
          findsOneWidget,
        );
        expect(
          find.byKey(const ValueKey('Combined-ALL-mal:25')),
          findsOneWidget,
        );
        expect(find.text('5 watched'), findsOneWidget);
        expect(
          find.descendant(
            of: find.byKey(ValueKey('Combined-ALL-${fixture.anime.key}')),
            matching: find.text('MAL title'),
          ),
          findsOneWidget,
        );
        await tester.tap(find.text('All titles'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Paused'));
        await tester.pumpAndSettle();
        expect(
          find.byKey(ValueKey('Combined-PAUSED-${fixture.anime.key}')),
          findsOneWidget,
        );
        expect(
          find.byKey(const ValueKey('Combined-PAUSED-mal:25')),
          findsNothing,
        );
        expect(services.store.text('ListCategory.Combined'), 'PAUSED');
        await tester.tap(find.byKey(const Key('nav-More')));
        await tester.pumpAndSettle();
        await tester.binding.handlePopRoute();
        await tester.pumpAndSettle();
        expect(find.text('Paused'), findsOneWidget);
        expect(find.byType(AccountBlock), findsOneWidget);
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
      },
    );
  }

  testWidgets('TV remote retries MAL while Home keeps AniList available', (
    tester,
  ) async {
    var unavailable = true;
    await launchCombined(tester, tv: true, malUnavailable: () => unavailable);
    expect(find.byType(AccountBlock), findsOneWidget);
    expect(find.text('AniList + MAL · viewer'), findsOneWidget);
    expect(find.text('MAL not loaded, retry'), findsOneWidget);
    expect(
      find.byKey(ValueKey('Combined-CURRENT-${fixture.anime.key}')),
      findsOneWidget,
    );
    expect(find.text('3 watched'), findsOneWidget);
    unavailable = false;
    Focus.of(tester.element(find.text('MAL not loaded, retry'))).requestFocus();
    await tester.pump();
    await tester.sendKeyEvent(LogicalKeyboardKey.select);
    await tester.pumpAndSettle();
    expect(find.text('MAL not loaded, retry'), findsNothing);
    await tester.tap(find.text('Watching'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('All titles'));
    await tester.pumpAndSettle();
    expect(
      find.byKey(ValueKey('Combined-ALL-${fixture.anime.key}')),
      findsOneWidget,
    );
    expect(find.byKey(const ValueKey('Combined-ALL-mal:25')), findsOneWidget);
    expect(find.text('5 watched'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
  for (final remaining in ['AniList', 'MAL']) {
    testWidgets('signing out the other account restores the $remaining list', (
      tester,
    ) async {
      final services = await launchCombined(tester);
      await services.auth.logout(remaining == 'AniList' ? 'MAL' : 'AniList');
      await tester.pumpAndSettle();
      expect(find.byType(AccountBlock), findsOneWidget);
      expect(find.text('$remaining · viewer'), findsOneWidget);
      expect(find.text('AniList + MAL · viewer'), findsNothing);
      if (remaining == 'AniList') {
        expect(
          find.byKey(ValueKey('AniList-CURRENT-${fixture.anime.key}')),
          findsOneWidget,
        );
        expect(find.text('3 watched'), findsOneWidget);
      } else {
        await tester.tap(find.text('Watching'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('On hold'));
        await tester.pumpAndSettle();
        expect(
          find.byKey(ValueKey('MAL-on_hold-mal:${fixture.anime.malId}')),
          findsOneWidget,
        );
        expect(find.text('5 watched'), findsOneWidget);
      }
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    });
  }
}
