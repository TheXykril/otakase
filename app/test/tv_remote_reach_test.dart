import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/main.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/ui/phone_player.dart';
import 'package:otakase/ui/movies_pages.dart';
import 'package:otakase/data/catalog.dart';
import 'package:otakase/data/trakt.dart' show MediaTitle;

import 'dart:convert';

import 'auth_test.dart' show MemoryTokens;
import 'tv_focus_test.dart' show key, navigation;
import 'tv_reachability_test.dart' show describe, focusedOn, unreachable;
import 'widget_test.dart' as fixture;
import 'movies_test.dart' show MovieCore;
import 'tv_reachability_test.dart' show onTopPage;

/// A core with as many sources as a real device lists, so Diagnostics is
/// longer than the TV screen.
class SourcesCore extends CoreClient {
  @override
  Future<Json> request(
    Json input, {
    void Function(int)? onHandle,
    RequestScope? scope,
    Duration timeout = const Duration(seconds: 40),
  }) async => switch (input['operation']) {
    'providers' => {
      'providers': [for (var i = 1; i <= 8; i++) 'source$i'],
      'adult': <String>[],
    },
    'movie_providers' => {
      'providers': [
        for (var i = 1; i <= 5; i++)
          {
            'name': 'movie$i',
            'label': 'Movie $i',
            'language': i.isEven ? 'lt' : 'en',
            'series': true,
          },
      ],
    },
    'movie_match' when input['provider'] == 'movie2' => throw const CoreFailure(
      'provider_error',
      'Source request failed (certificate not trusted by this device).',
    ),
    _ => throw const CoreFailure('not_found', 'Nothing here.'),
  };
}

void main() {
  longPageTests();
  titlePageTests();
  testWidgets('TV Diagnostics reaches Check all below a long source list', (
    tester,
  ) async {
    await fixture.launch(tester, tv: true);
    await tester.pumpWidget(const SizedBox());
    final base = await fixture.services();
    final services = AppServices(
      catalog: base.catalog,
      store: base.store,
      core: SourcesCore(),
      auth: AuthService(
        storage: MemoryTokens(),
        client: MockClient((_) async => http.Response('{}', 200)),
      ),
    );
    addTearDown(services.close);
    await tester.pumpWidget(OtakaseApp(services: services, isTV: true));
    await tester.pumpAndSettle();
    navigation(tester, 'More').requestFocus();
    await tester.pump();
    await key(tester, LogicalKeyboardKey.select);
    final row = find.byKey(const ValueKey('more-Diagnostics'));
    for (var i = 0; i < 30 && !focusedOn('Diagnostics'); i++) {
      await key(tester, LogicalKeyboardKey.arrowDown);
    }
    expect(row, findsOneWidget);
    await key(tester, LogicalKeyboardKey.select);
    expect(find.text('Source 8'), findsOneWidget);
    for (var i = 0; i < 4 && !focusedOn('Check all'); i++) {
      await key(tester, LogicalKeyboardKey.arrowDown);
    }
    expect(focusedOn('Check all'), isTrue);
    await key(tester, LogicalKeyboardKey.select);
    await tester.pumpAndSettle();
    // Why a source failed is kept beside it.
    expect(services.store.providerHealth['movie:movie1'], 'No match');
    expect(
      services.store.providerHealth['movie:movie2'],
      'Unavailable (certificate not trusted by this device)',
    );
    await tester.pumpWidget(const SizedBox());
  });
}

Widget tvApp(Widget home) => MaterialApp(
  builder: (context, child) =>
      AppLayout(isTV: true, child: TVViewport(child: child!)),
  home: home,
);

void tvScreen(WidgetTester tester) {
  tester.view.physicalSize = const Size(1280, 720);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  tester.platformDispatcher.accessibilityFeaturesTestValue =
      FakeAccessibilityFeatures(disableAnimations: true);
  addTearDown(tester.platformDispatcher.clearAccessibilityFeaturesTestValue);
}

void longPageTests() {
  testWidgets('TV Down scrolls a long page to controls not built yet', (
    tester,
  ) async {
    tvScreen(tester);
    await tester.pumpWidget(
      tvApp(
        Scaffold(
          body: ListView(
            children: [
              AppButton(
                label: 'Top',
                autofocus: true,
                onPressed: () {},
                isTV: true,
              ),
              for (var i = 0; i < 60; i++)
                SizedBox(height: 40, child: Text('Line $i')),
              AppButton(label: 'Bottom', onPressed: () {}, isTV: true),
            ],
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(focusedOn('Top'), isTrue);
    expect(find.text('Bottom'), findsNothing);
    for (var i = 0; i < 20 && !focusedOn('Bottom'); i++) {
      await key(tester, LogicalKeyboardKey.arrowDown);
    }
    expect(focusedOn('Bottom'), isTrue);
    for (var i = 0; i < 20 && !focusedOn('Top'); i++) {
      await key(tester, LogicalKeyboardKey.arrowUp);
    }
    expect(focusedOn('Top'), isTrue);
  });

  testWidgets('TV player controls are all reachable with the remote', (
    tester,
  ) async {
    tvScreen(tester);
    final play = FocusNode();
    addTearDown(play.dispose);
    await tester.pumpWidget(
      tvApp(
        Scaffold(
          body: PhonePlayerControls(
            isTV: true,
            title: 'Frieren',
            subtitle: 'Episode 8 · sub',
            position: const Duration(minutes: 8),
            duration: const Duration(minutes: 24),
            spans: const [],
            playing: true,
            buffering: false,
            playFocus: play,
            onBack: () {},
            onPlayPause: () {},
            onSeekBy: (_) {},
            onSeekStart: (_) {},
            onSeekChanged: (_) {},
            onSeekEnd: (_) {},
            topActions: const [],
            actions: [
              for (final label in ['Next', 'Audio & subs', 'Quality'])
                PlayerButton(
                  icon: LucideIcons.captions,
                  label: label,
                  onPressed: () {},
                ),
            ],
          ),
        ),
      ),
    );
    play.requestFocus();
    await tester.pumpAndSettle();
    expect(await unreachable(tester), isEmpty);
  });
}

void titlePageTests() {
  testWidgets('TV series page reaches every episode and related title', (
    tester,
  ) async {
    await fixture.launch(tester, tv: true);
    await tester.pumpWidget(const SizedBox());
    final base = await fixture.services();
    Map<String, Object> show(int id, String name) => {
      'title': name,
      'year': 2011,
      'overview': 'A long story.',
      'ids': {'trakt': id},
    };
    final client = MockClient((r) async {
      final path = r.url.path;
      if (path.endsWith('/seasons')) {
        return http.Response(
          jsonEncode([
            for (var s = 1; s <= 2; s++)
              {
                'number': s,
                'episodes': [
                  for (var e = 1; e <= 10; e++)
                    {
                      'number': e,
                      'title': 'Season $s episode $e',
                      'overview': 'Things happen.',
                      'first_aired':
                          '2011-0$s-${e.toString().padLeft(2, '0')}T00:00:00Z',
                      'runtime': 50,
                    },
                ],
              },
          ]),
          200,
        );
      }
      if (path.endsWith('/related')) {
        return http.Response(
          jsonEncode([for (var i = 2; i < 8; i++) show(i, 'Related $i')]),
          200,
        );
      }
      if (path.startsWith('/shows/1')) {
        return http.Response(jsonEncode(show(1, 'Long Show')), 200);
      }
      return http.Response('[]', 200);
    });
    final services = AppServices(
      catalog: Catalog(client: client),
      store: base.store,
      core: MovieCore(),
      auth: AuthService(storage: MemoryTokens(), client: client),
    );
    addTearDown(services.close);
    await tester.pumpWidget(OtakaseApp(services: services, isTV: true));
    await tester.pumpAndSettle();
    final navigator = tester.state<NavigatorState>(
      find.byType(Navigator).first,
    );
    unawaited(
      navigator.push(
        MaterialPageRoute<void>(
          builder: (_) => TitlePage(
            services: services,
            title: const MediaTitle(kind: 'show', trakt: 1, title: 'Long Show'),
            isTV: true,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(onTopPage(), isTrue, reason: 'Focus starts behind the page');
    // Down walks the play button, the season, every episode and then the
    // related titles at the bottom, where it stops.
    final seen = <String>[];
    FocusNode? last;
    for (var i = 0; i < 30; i++) {
      final now = FocusManager.instance.primaryFocus!;
      if (now == last) break;
      last = now;
      seen.add(describe(now));
      await key(tester, LogicalKeyboardKey.arrowDown);
    }
    expect(seen.where((s) => s.startsWith('S1E')), hasLength(10));
    expect(seen.last, isNot(startsWith('S1E')));
    for (var i = 0; i < 30 && !focusedOn('Season 1'); i++) {
      await key(tester, LogicalKeyboardKey.arrowUp);
    }
    expect(focusedOn('Season 1'), isTrue);
    await tester.pumpWidget(const SizedBox());
  });
}
