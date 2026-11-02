import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/trakt.dart';
import 'package:otakase/desktop/launcher.dart';
import 'package:otakase/services.dart';

import 'widget_test.dart' as fixture;

const film = MediaTitle(
  kind: 'movie',
  trakt: 1,
  title: 'Spirited Away',
  year: 2001,
);
const series = MediaTitle(kind: 'show', trakt: 2, title: 'Dark', year: 2017);

Future<void> pumpLauncher(
  WidgetTester tester, {
  VoidCallback? onOpen,
  String content = 'anime',
  Future<void> Function(AppServices services)? setUp,
}) async {
  await fixture.launch(tester, tv: true);
  final services = await fixture.services();
  await services.store.progress(fixture.anime, 3, 400, 1440);
  await services.store.set('Content', content);
  await setUp?.call(services);
  await tester.pumpWidget(
    MaterialApp(
      home: LauncherPage(services: services, onOpenApp: onOpen),
    ),
  );
  await tester.pumpAndSettle();
}

Future<void> press(WidgetTester tester, LogicalKeyboardKey key) async {
  await tester.sendKeyEvent(key);
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('starts on Continue and filters by typing', (tester) async {
    await pumpLauncher(tester);
    expect(find.text('Continue: Frieren · Ep 3'), findsOneWidget);
    expect(find.text('Open full app'), findsOneWidget);

    await tester.enterText(find.byKey(const Key('launcher-input')), 'full');
    await tester.pumpAndSettle();
    expect(find.text('Open full app'), findsOneWidget);
    expect(find.text('Continue: Frieren · Ep 3'), findsNothing);
    expect(find.text('Search for “full”'), findsOneWidget);
    // "Open full app" does not start with "full", so search is on top.
    await press(tester, LogicalKeyboardKey.enter);
    expect(find.text('Search'), findsOneWidget);
  });

  testWidgets('a row whose name starts with the text stays first', (
    tester,
  ) async {
    var opened = 0;
    await pumpLauncher(tester, onOpen: () => opened++);
    await tester.enterText(find.byKey(const Key('launcher-input')), 'open');
    await tester.pumpAndSettle();
    await press(tester, LogicalKeyboardKey.enter);
    expect(opened, 1);
  });

  testWidgets('search, show menu, episode menu and back', (tester) async {
    await pumpLauncher(tester);
    await tester.enterText(find.byKey(const Key('launcher-input')), 'frier');
    await tester.pumpAndSettle();
    // No row starts with the typed text, so Enter searches straight away.
    await press(tester, LogicalKeyboardKey.enter);
    expect(find.text('Search'), findsOneWidget);
    expect(find.text('Frieren'), findsOneWidget);

    await press(tester, LogicalKeyboardKey.enter);
    expect(find.text('Play next: resume episode 3'), findsOneWidget);
    expect(find.text('Choose episode…'), findsOneWidget);

    await press(tester, LogicalKeyboardKey.arrowDown);
    await press(tester, LogicalKeyboardKey.enter);
    expect(find.text('Episode 1'), findsOneWidget);

    await tester.enterText(find.byKey(const Key('launcher-input')), '27');
    await tester.pumpAndSettle();
    expect(find.text('Episode 27'), findsWidgets);

    // Esc clears the text first, then goes back one menu at a time.
    await press(tester, LogicalKeyboardKey.escape);
    expect(find.text('Episode 1'), findsOneWidget);
    await press(tester, LogicalKeyboardKey.escape);
    expect(find.text('Play next: resume episode 3'), findsOneWidget);
    await press(tester, LogicalKeyboardKey.escape);
    await press(tester, LogicalKeyboardKey.escape);
    expect(find.text('Continue: Frieren · Ep 3'), findsOneWidget);
  });

  testWidgets('Ctrl+W and the menu row open the full app', (tester) async {
    var opened = 0;
    await pumpLauncher(tester, onOpen: () => opened++);
    await tester.sendKeyDownEvent(LogicalKeyboardKey.controlLeft);
    await tester.sendKeyEvent(LogicalKeyboardKey.keyW);
    await tester.sendKeyUpEvent(LogicalKeyboardKey.controlLeft);
    await tester.pumpAndSettle();
    expect(opened, 1);
    await tester.tap(find.text('Open full app'));
    await tester.pumpAndSettle();
    expect(opened, 2);
  });

  Future<void> withTitles(AppServices services) async {
    await services.movies.progress(series, 600, 3000, season: 1, episode: 4);
    await services.movies.setWatchlist(film, true);
  }

  testWidgets('anime-only profiles see no film or series rows', (tester) async {
    await pumpLauncher(tester, setUp: withTitles);
    expect(find.text('Search anime'), findsOneWidget);
    expect(find.text('Trending movies & shows'), findsNothing);
    expect(find.text('Continue: Dark · S1E4'), findsNothing);
    expect(find.text('Spirited Away'), findsNothing);
  });

  testWidgets('Content both adds films and series next to anime', (
    tester,
  ) async {
    await pumpLauncher(tester, content: 'both', setUp: withTitles);
    // The series was watched last, so it leads.
    final rows = tester
        .widgetList<Text>(
          find.descendant(
            of: find.byKey(const Key('launcher-list')),
            matching: find.byType(Text),
          ),
        )
        .map((t) => t.data)
        .toList();
    expect(rows.first, 'Continue: Dark · S1E4');
    expect(find.text('Continue: Frieren · Ep 3'), findsOneWidget);
    expect(find.text('Spirited Away'), findsOneWidget);
    expect(find.text('Search anime, movies & shows'), findsOneWidget);
    expect(find.text('Trending movies & shows'), findsOneWidget);

    await tester.tap(find.text('Spirited Away'));
    await tester.pumpAndSettle();
    expect(find.text('Play'), findsOneWidget);
    expect(find.text('Remove from watchlist'), findsOneWidget);
    await tester.tap(find.text('Remove from watchlist'));
    await tester.pumpAndSettle();
    expect(find.text('Add to watchlist'), findsOneWidget);
    await press(tester, LogicalKeyboardKey.escape);
    expect(find.text('Spirited Away'), findsNothing);
  });

  testWidgets('movies-only profiles hide the anime rows', (tester) async {
    await pumpLauncher(tester, content: 'movies', setUp: withTitles);
    expect(find.text('Continue: Dark · S1E4'), findsOneWidget);
    expect(find.text('Continue: Frieren · Ep 3'), findsNothing);
    expect(find.text('Search movies & shows'), findsOneWidget);
    expect(find.text('Search anime'), findsNothing);
  });

  test('a series carries on from where it stopped', () async {
    final services = await fixture.services();
    const episodes = [
      TitleEpisode(season: 1, number: 1),
      TitleEpisode(season: 1, number: 2),
      TitleEpisode(season: 1, number: 3),
    ];
    final movies = services.movies;
    expect(nextTitleEpisode(movies, series, episodes), episodes[0]);
    await movies.progress(series, 600, 3000, season: 1, episode: 2);
    expect(nextTitleEpisode(movies, series, episodes), episodes[1]);
    await movies.progress(
      series,
      3000,
      3000,
      season: 1,
      episode: 2,
      completed: true,
    );
    expect(nextTitleEpisode(movies, series, episodes), episodes[2]);
    expect(nextTitleEpisode(movies, series, const []), isNull);
  });
}
