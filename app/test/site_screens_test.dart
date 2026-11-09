import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/desktop/player_bar.dart';
import 'package:otakase/main.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/ui/show_page.dart';

import 'site_fixture.dart';

// Renders the app screens shown on the website, one set per show in
// test/fixtures/site/shows.json. Off unless SITE_SHOTS names an output folder;
// the Site workflow runs it and turns the PNGs into site/img/shots/*.webp.
const output = String.fromEnvironment('SITE_SHOTS');
const only = String.fromEnvironment('SITE_ONLY');

Future<void> capture(WidgetTester tester, String name) async {
  await tester.pumpAndSettle();
  await tester.pumpAndSettle();
  expect(tester.takeException(), isNull);
  await expectLater(
    find.byKey(const Key('site-render')),
    matchesGoldenFile(Uri.file('$output/$name.png')),
  );
}

Future<void> openShow(
  WidgetTester tester,
  AppServices app,
  Anime a,
  bool tv,
) async {
  await tester.pumpAndSettle();
  unawaited(
    tester
        .state<NavigatorState>(find.byType(Navigator).first)
        .push(
          OtakaseRoute<void>(
            builder: (_) => ShowPage(services: app, anime: a, isTV: tv),
          ),
        ),
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  if (output.isEmpty) {
    test('site renders', () {}, skip: 'set SITE_SHOTS to render');
    return;
  }
  readShows();
  setUpAll(() async {
    await Directory(output).create(recursive: true);
    await loadArt();
  });

  for (final main in shows) {
    if (only.isNotEmpty && '${main.id}' != only) continue;
    final id = main.id;

    testWidgets('$id phone', (tester) async {
      setUpView(tester, const Size(412, 915));
      tester.view.padding = const FakeViewPadding(top: 24, bottom: 24);
      addTearDown(tester.view.resetPadding);
      final app = await services(main);
      await tester.pumpWidget(art(OtakaseApp(services: app, isTV: false)));
      await capture(tester, '$id-phone');
      await tester.pumpWidget(const SizedBox());
    });

    testWidgets('$id computer', (tester) async {
      setUpView(tester, const Size(1440, 900), desktop: true);
      final app = await services(main);
      await tester.pumpWidget(art(OtakaseApp(services: app, isTV: true)));
      await openShow(tester, app, main, true);
      await capture(tester, '$id-computer');
      await tester.pumpWidget(const SizedBox());
    });

    testWidgets('$id tv', (tester) async {
      setUpView(tester, const Size(1600, 900));
      final app = await services(main);
      await tester.pumpWidget(art(OtakaseApp(services: app, isTV: true)));
      await openShow(tester, app, main, true);
      await capture(tester, '$id-tv');
      await tester.pumpWidget(const SizedBox());
    });

    testWidgets('$id player', (tester) async {
      setUpView(tester, const Size(1440, 900), desktop: true);
      final ep = main.episodeArt.firstWhere(
        (e) => e['episode'] == resumeAt,
        orElse: () => main.episodeArt.first,
      );
      final frame = images[Uri.parse(ep['thumbnail'] as String).path]!;
      await tester.pumpWidget(
        art(
          MaterialApp(
            debugShowCheckedModeBanner: false,
            home: Scaffold(
              backgroundColor: Colors.black,
              body: TVViewport(
                child: Stack(
                  fit: StackFit.expand,
                  children: [
                    Image(image: DecodedImage(frame), fit: BoxFit.cover),
                    DesktopPlayerBar(
                      title: main.title,
                      subtitle: 'Ep $resumeAt · SUB',
                      centerLabel: 'Episode $resumeAt',
                      position: const Duration(minutes: 1, seconds: 12),
                      duration: const Duration(minutes: 24),
                      buffered: const Duration(minutes: 4),
                      playing: true,
                      buffering: false,
                      silent: false,
                      fullscreen: false,
                      trackLabel: 'SUB',
                      stripes: const [
                        SkipSpan('op', 40, 130),
                        SkipSpan('ed', 1290, 1380),
                      ],
                      onBack: () {},
                      onPlayPause: () {},
                      onSeekBy: (_) {},
                      onSeekStart: (_) {},
                      onSeekChanged: (_) {},
                      onSeekEnd: (_) {},
                      onNext: () {},
                      onMute: () {},
                      onTracks: () {},
                      onQuality: () {},
                      onFullscreen: () {},
                    ),
                    Positioned(
                      right: 24,
                      bottom: 170,
                      child: AppButton(
                        label: 'Skip opening',
                        onPressed: () {},
                        isTV: true,
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      );
      await capture(tester, '$id-player');
      await tester.pumpWidget(const SizedBox());
    });
  }
}
