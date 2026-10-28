import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/data/catalog.dart';
import 'package:otakase/data/store.dart';
import 'package:otakase/main.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/community_page.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/ui/home_page.dart';
import 'package:otakase/ui/movie_page.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'auth_test.dart' show MemoryTokens, account, json;
import 'widget_test.dart' as fixture;

Future<AppServices> communityApp(WidgetTester tester, {bool tv = false}) async {
  await fixture.launch(tester, tv: tv);
  await tester.pumpWidget(const SizedBox());
  final client = MockClient((r) async {
    if (r.url.host == 'bot.xyverion.com') {
      if (r.url.path == '/v1/link/start') {
        return json({'code': 'ABCD', 'poll': 'secret', 'expires_in': 600});
      }
      if (r.url.path == '/v1/link/poll') return json({}, 202);
      return json({});
    }
    final query = jsonDecode(r.body)['query'] as String;
    if (query.contains('MediaListCollection')) {
      return json({
        'data': {
          'MediaListCollection': {
            'lists': [
              {
                'entries': [
                  {
                    'status': 'CURRENT',
                    'progress': 8,
                    'media': {
                      'id': 1,
                      'idMal': 1,
                      'title': {'romaji': 'Scheduled story'},
                      'status': 'RELEASING',
                      'episodes': 12,
                      'nextAiringEpisode': {
                        'episode': 9,
                        'airingAt':
                            DateTime.now()
                                .add(const Duration(days: 2, hours: 4))
                                .millisecondsSinceEpoch ~/
                            1000,
                      },
                    },
                  },
                ],
              },
            ],
          },
        },
      });
    }
    return json({
      'data': {
        'Viewer': {
          'statistics': {
            'anime': {
              'count': 2,
              'episodesWatched': 50,
              'minutesWatched': 1200,
              'statuses': [
                {'status': 'COMPLETED', 'count': 1},
              ],
            },
          },
          'favourites': {
            'anime': {'nodes': []},
          },
        },
      },
    });
  });
  final auth = AuthService(client: client, storage: MemoryTokens());
  auth.accounts['AniList'] = account('AniList');
  final store = AppStore(await SharedPreferences.getInstance());
  final s = AppServices(
    catalog: Catalog(client: client),
    store: store,
    core: CoreClient(),
    auth: auth,
  );
  await tester.pumpWidget(OtakaseApp(services: s, isTV: tv));
  await tester.pumpAndSettle();
  return s;
}

void main() {
  for (final tv in [false, true]) {
    testWidgets(
      'caught-up airing story appears in Up next on ${tv ? 'TV' : 'phone'}',
      (tester) async {
        final s = await communityApp(tester, tv: tv);
        await tester.ensureVisible(find.text('Up next'));
        await tester.pumpAndSettle();
        expect(find.text('Up next'), findsOneWidget);
        expect(find.textContaining('Ep 9 · in 2 d'), findsOneWidget);
        expect(find.byType(ResumeCard), findsNothing);
        expect(s.store.history, isEmpty);
        await tester.pumpWidget(const SizedBox());
        await s.close();
      },
    );
  }
  testWidgets('More opens Discord link with remote, Back cancels polling', (
    tester,
  ) async {
    final s = await communityApp(tester, tv: true);
    await tester.tap(find.byKey(const Key('nav-More')));
    await tester.pumpAndSettle();
    final row = find.byKey(const Key('more-Discord'));
    await tester.ensureVisible(row);
    final node = Focus.of(tester.element(row), scopeOk: true);
    node.requestFocus();
    await tester.pump();
    await tester.tap(row);
    await tester.pumpAndSettle();
    expect(find.byType(DiscordPage), findsOneWidget);
    await tester.tap(find.text('Link Discord'));
    await tester.pumpAndSettle();
    expect(find.text('/link-app code:ABCD'), findsOneWidget);
    expect(find.textContaining('secret'), findsNothing);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(s.discord.linked, false);
    await tester.pumpWidget(const SizedBox());
    await s.close();
  });
  testWidgets(
    'capsule saves only its profile and movie responds to D-pad and Back',
    (tester) async {
      final s = await communityApp(tester, tv: true);
      await tester.pumpWidget(
        AppLayout(
          isTV: true,
          child: MaterialApp(
            theme: ThemeData.dark(),
            home: CapsulePage(services: s, isTV: true),
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('Write a letter'));
      await tester.pumpAndSettle();
      expect(find.text('Type on phone'), findsOneWidget);
      await tester.enterText(
        find.byKey(const Key('capsule-text')),
        'Remember this day',
      );
      await tester.pump();
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();
      expect(s.heritage.pending!['text'], 'Remember this day');
      expect(s.heritage.pending!['stats']['episodes'], 50);
      await tester.pumpWidget(
        AppLayout(
          isTV: true,
          child: MaterialApp(
            theme: ThemeData.dark(),
            home: Builder(
              builder: (context) => Scaffold(
                body: TextButton(
                  onPressed: () => Navigator.push(
                    context,
                    MaterialPageRoute<void>(
                      builder: (_) =>
                          OtakaseMovie(services: s, isTV: true, milestone: 50),
                    ),
                  ),
                  child: const Text('Film'),
                ),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('Film'));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));
      expect(find.text('Counting the Stars'), findsOneWidget);
      await tester.sendKeyEvent(LogicalKeyboardKey.arrowRight);
      await tester.pump(const Duration(milliseconds: 600));
      expect(find.text('The Voices'), findsOneWidget);
      await tester.sendKeyEvent(LogicalKeyboardKey.arrowRight);
      await tester.pump(const Duration(milliseconds: 600));
      expect(find.text('Memories That Never Fade'), findsOneWidget);
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(find.text('Film'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
      await s.close();
    },
  );
}
