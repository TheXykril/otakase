import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/host.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/ui/settings.dart' show SettingsPage;
import 'package:otakase/data/catalog.dart';
import 'package:otakase/data/trakt.dart' show MediaTitle;
import 'package:otakase/main.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/movies_pages.dart';

import 'auth_test.dart' show MemoryTokens;
import 'profiles_widget_test.dart' show prepare;
import 'movies_test.dart' show MovieCore;
import 'tv_focus_test.dart' show key, navigation;
import 'tv_reachability_test.dart' show describe, focusedOn, onTopPage;
import 'widget_test.dart' as fixture;

Future<AppServices> movieServices({bool longOverview = true}) async {
  final base = await fixture.services();
  Map<String, Object> movie(int id, String name) => {
    'title': name,
    'year': 2020,
    'overview': longOverview
        ? List.filled(40, 'A very long story about a film.').join(' ')
        : 'Short.',
    'tagline': 'One night only.',
    'ids': {'trakt': id},
  };
  final client = MockClient((r) async {
    final path = r.url.path;
    if (path.endsWith('/related')) {
      return http.Response(
        jsonEncode([for (var i = 2; i < 14; i++) movie(i, 'Related $i')]),
        200,
      );
    }
    if (path.startsWith('/movies/1')) {
      return http.Response(jsonEncode(movie(1, 'Long Film')), 200);
    }
    return http.Response('[]', 200);
  });
  return AppServices(
    catalog: Catalog(client: client),
    store: base.store,
    core: MovieCore(),
    auth: AuthService(storage: MemoryTokens(), client: client),
  );
}

Future<void> openMovie(WidgetTester tester, AppServices services) async {
  await tester.pumpWidget(OtakaseApp(services: services, isTV: true));
  await tester.pumpAndSettle();
  final navigator = tester.state<NavigatorState>(find.byType(Navigator).first);
  unawaited(
    navigator.push(
      MaterialPageRoute<void>(
        builder: (_) => TitlePage(
          services: services,
          title: const MediaTitle(kind: 'movie', trakt: 1, title: 'Long Film'),
          isTV: true,
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('TV shows the focus ring before any key or after a touch', (
    tester,
  ) async {
    Host.tv = true;
    addTearDown(() => Host.tv = false);
    FocusManager.instance.highlightStrategy = FocusHighlightStrategy.automatic;
    final node = FocusNode();
    addTearDown(node.dispose);
    await tester.pumpWidget(
      MaterialApp(
        home: Center(
          child: FocusBox(
            isTV: true,
            focusNode: node,
            onPressed: () {},
            child: const SizedBox(width: 40, height: 40),
          ),
        ),
      ),
    );
    // A touch (or the touch default at start) must not hide the TV ring.
    await tester.tapAt(const Offset(5, 5));
    await tester.pump();
    expect(FocusManager.instance.highlightMode, FocusHighlightMode.touch);
    node.requestFocus();
    await tester.pumpAndSettle();
    expect(FocusRings.show, isTrue);
    final frame = tester.widget<AnimatedContainer>(
      find
          .ancestor(
            of: find.byType(InkWell),
            matching: find.byType(AnimatedContainer),
          )
          .first,
    );
    final border = (frame.decoration! as BoxDecoration).border! as Border;
    expect(border.top.color.a, greaterThan(0));
  });

  testWidgets('TV movies Home starts below the top bar, no Downloads', (
    tester,
  ) async {
    await fixture.launch(tester, tv: true);
    await tester.pumpWidget(const SizedBox());
    final services = await movieServices();
    addTearDown(services.close);
    services.store.set('Content', 'movies');
    await tester.pumpWidget(OtakaseApp(services: services, isTV: true));
    await tester.pumpAndSettle();
    final heading = find.text('Movies & shows');
    expect(heading, findsOneWidget);
    final bar = tester.getRect(find.byKey(const Key('tv-header-scrim')));
    expect(tester.getRect(heading).top, greaterThanOrEqualTo(bar.bottom));
    expect(find.byKey(const Key('movies-downloads')), findsNothing);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('TV movie page: Right stops at the last button, Down reaches '
      'Related with a labelled focus', (tester) async {
    await fixture.launch(tester, tv: true);
    await tester.pumpWidget(const SizedBox());
    tester.view.physicalSize = const Size(1920, 1080);
    final semantics = tester.ensureSemantics();
    final services = await movieServices();
    addTearDown(services.close);
    await openMovie(tester, services);
    expect(onTopPage(), isTrue);
    for (var i = 0; i < 6; i++) {
      await key(tester, LogicalKeyboardKey.arrowRight);
    }
    expect(focusedOn('Mark watched'), isTrue);
    for (var i = 0; i < 6; i++) {
      await key(tester, LogicalKeyboardKey.arrowLeft);
    }
    expect(focusedOn('Play'), isTrue);
    final trail = <String>[];
    for (var i = 0; i < 4; i++) {
      await key(tester, LogicalKeyboardKey.arrowDown);
      trail.add(describe(FocusManager.instance.primaryFocus!));
    }
    final card = FocusManager.instance.primaryFocus!;
    expect(
      find.ancestor(
        of: find.byWidget(card.context!.widget),
        matching: find.byType(PosterCard),
      ),
      findsOneWidget,
      reason: '$trail',
    );
    // Screen readers see which card the remote is on.
    expect(
      tester.getSemantics(find.bySemanticsLabel('Related 2')),
      isSemantics(
        label: 'Related 2',
        isButton: true,
        isFocused: true,
        hasTapAction: true,
      ),
    );
    semantics.dispose();
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('TV settings: a switch row is one labelled node and Down from '
      'the last row stays in the pane', (tester) async {
    final semantics = tester.ensureSemantics();
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AppRow(
            isTV: true,
            title: 'Keep screen on',
            subtitle: 'Stop the screen turning off.',
            toggle: true,
            onTap: () {},
          ),
        ),
      ),
    );
    Focus.of(tester.element(find.text('Keep screen on'))).requestFocus();
    await tester.pumpAndSettle();
    expect(
      tester.getSemantics(find.text('Keep screen on')),
      isSemantics(
        label: 'Keep screen on\nStop the screen turning off.',
        hasToggledState: true,
        isToggled: true,
        isFocused: true,
        hasTapAction: true,
      ),
    );
    semantics.dispose();

    await fixture.launch(tester, tv: true);
    await tester.pump();
    navigation(tester, 'More').requestFocus();
    await tester.pump();
    await key(tester, LogicalKeyboardKey.select);
    for (var i = 0; i < 30 && !focusedOn('Display'); i++) {
      await key(tester, LogicalKeyboardKey.arrowDown);
    }
    await key(tester, LogicalKeyboardKey.select);
    expect(find.byType(SettingsPage), findsOneWidget);
    final pane = find.byType(SettingsPage);
    FocusNode? last;
    for (var i = 0; i < 60; i++) {
      final now = FocusManager.instance.primaryFocus!;
      expect(
        find.descendant(of: pane, matching: find.byWidget(now.context!.widget)),
        findsOneWidget,
        reason: 'Down left the settings pane at ${describe(now)}',
      );
      if (now == last) break;
      last = now;
      await key(tester, LogicalKeyboardKey.arrowDown);
    }
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('TV profile switch with the remote throws nothing', (
    tester,
  ) async {
    final m = await prepare(tester, tv: true);
    final second = await m.add('Other');
    await tester.pumpWidget(ProfileHost(manager: m));
    await tester.pumpAndSettle();
    navigation(tester, 'More').requestFocus();
    await tester.pump();
    await key(tester, LogicalKeyboardKey.select);
    for (var i = 0; i < 30 && !focusedOn('Profiles'); i++) {
      await key(tester, LogicalKeyboardKey.arrowDown);
    }
    expect(focusedOn('Profiles'), isTrue);
    await key(tester, LogicalKeyboardKey.select);
    expect(focusedOn('Switch profile'), isTrue);
    await key(tester, LogicalKeyboardKey.select);
    final target = find.byKey(ValueKey('profile-${second.id}'));
    expect(target, findsOneWidget);
    for (var i = 0; i < 6 && !focusedOn('Other'); i++) {
      await key(tester, LogicalKeyboardKey.arrowRight);
    }
    expect(focusedOn('Other'), isTrue);
    await tester.sendKeyEvent(LogicalKeyboardKey.select);
    for (var i = 0; i < 20; i++) {
      await tester.pump(const Duration(milliseconds: 16));
      expect(tester.takeException(), isNull);
    }
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    expect(m.activeId, second.id);
    await tester.pumpWidget(const SizedBox());
    await m.close();
  });
}
