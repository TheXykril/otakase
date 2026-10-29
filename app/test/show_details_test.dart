import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/main.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/ui/home_page.dart';
import 'package:otakase/ui/progress_thread.dart';
import 'package:otakase/ui/show_page.dart';

import 'auth_test.dart' show MemoryTokens, account, json;
import 'widget_test.dart' as fixture;

const story = Anime(
  id: 1,
  title: 'Story',
  episodes: 8,
  episodeArt: [
    {
      'episode': 3,
      'title': 'An actual episode title',
      'thumbnail': 'episode-3.jpg',
      'airdate': '1998-04-10',
    },
  ],
);

class DetailCatalog extends fixture.TestCatalog {
  @override
  Future<Anime> detail(Anime a, {required bool adult}) async => a;
}

Future<void> openShow(
  WidgetTester tester, {
  bool tv = false,
  bool resumed = false,
}) async {
  await fixture.launch(tester, tv: tv);
  await tester.pumpWidget(const SizedBox());
  final base = await fixture.services();
  final auth = AuthService(
    storage: MemoryTokens(),
    client: MockClient(
      (r) async => json({
        'data': {
          'Media': {
            'mediaListEntry': {
              'status': 'CURRENT',
              'progress': 3,
              'notes': '[otakase:4@90]',
            },
          },
        },
      }),
    ),
  );
  if (resumed) {
    auth.accounts['AniList'] = account('AniList');
    await base.store.progress(story, 2, 40, 1200);
  }
  final services = AppServices(
    catalog: DetailCatalog(),
    store: base.store,
    core: base.core,
    auth: auth,
  );
  await tester.pumpWidget(
    ImageSources(
      provider: (_) => const AssetImage('assets/otakase.png'),
      child: OtakaseApp(services: services, isTV: tv),
    ),
  );
  await tester.pumpAndSettle();
  Navigator.of(tester.element(find.byType(HomePage))).push(
    MaterialPageRoute<void>(
      builder: (_) => ShowPage(services: services, anime: story, isTV: tv),
    ),
  );
  await tester.pumpAndSettle();
  if (tv) {
    await tester.ensureVisible(find.text('All episodes'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('All episodes'));
    await tester.pumpAndSettle();
  }
  addTearDown(auth.dispose);
}

void main() {
  for (final tv in [false, true]) {
    testWidgets(
      'thread node follows the real remote resume episode on ${tv ? 'TV' : 'phone'}',
      (tester) async {
        await openShow(tester, tv: tv, resumed: true);
        await tester.scrollUntilVisible(
          find.byKey(const ValueKey('episode-4')),
          150,
          scrollable: find.byType(Scrollable).last,
        );
        await tester.pumpAndSettle();
        final row = find.byKey(const ValueKey('episode-4'));
        final node = find.byKey(const ValueKey('episode-thread-node-4'));
        expect(node, findsOneWidget);
        expect(
          find.byKey(const ValueKey('episode-thread-node-2')),
          findsNothing,
        );
        expect(find.descendant(of: row, matching: node), findsOneWidget);
        expect(tester.getSize(node), const Size(7, 7));
        expect(tester.getRect(row).contains(tester.getCenter(node)), isTrue);
        final painter = tester
            .widgetList<CustomPaint>(
              find.descendant(of: row, matching: find.byType(CustomPaint)),
            )
            .map((w) => w.painter)
            .whereType<EpisodeThreadPainter>()
            .single;
        expect(painter.current, 4);
        expect(painter.episode, 4);
        final next = find.byKey(const ValueKey('episode-5'));
        await tester.ensureVisible(next);
        await tester.pumpAndSettle();
        expect(
          tester
              .widgetList<CustomPaint>(
                find.descendant(of: next, matching: find.byType(CustomPaint)),
              )
              .any((w) => w.painter is EpisodeThreadPainter),
          isFalse,
        );
        Focus.of(
          tester.element(
            find.descendant(of: row, matching: find.text('Episode 4')),
          ),
        ).requestFocus();
        await tester.pumpAndSettle();
        await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
        await tester.pumpAndSettle();
        expect(
          Focus.of(
            tester.element(
              find.descendant(of: next, matching: find.text('Episode 5')),
            ),
          ).hasFocus,
          isTrue,
        );
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
      },
    );
  }

  testWidgets('unstarted show has no red episode line or node', (tester) async {
    await openShow(tester);
    await tester.scrollUntilVisible(
      find.byKey(const ValueKey('episode-3')),
      150,
      scrollable: find.byType(Scrollable).last,
    );
    await tester.pumpAndSettle();
    expect(
      find.byWidgetPredicate(
        (w) => w is CustomPaint && w.painter is EpisodeThreadPainter,
      ),
      findsNothing,
    );
    expect(find.byKey(const ValueKey('episode-thread-node-1')), findsNothing);
    final row = find.byKey(const ValueKey('episode-3'));
    expect(
      find.descendant(of: row, matching: find.text('An actual episode title')),
      findsOneWidget,
    );
    final image = tester.widget<CoverImage>(
      find.descendant(of: row, matching: find.byType(CoverImage)),
    );
    expect(image.url, 'episode-3.jpg');
    expect(
      find.byKey(const ValueKey('episode-cover-fallback-3')),
      findsNothing,
    );
    await tester.pumpWidget(const SizedBox());
  });

  for (final tv in [false, true]) {
    testWidgets(
      'hero uses banner or a sharp 2:3 poster and blurred backdrop on ${tv ? 'TV' : 'phone'}',
      (tester) async {
        tester.view.physicalSize = tv
            ? const Size(1280, 720)
            : const Size(412, 915);
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);
        final height = tv ? 310.0 : 346.0;
        Future<void> art(Anime anime) async {
          await tester.pumpWidget(
            MaterialApp(
              home: ImageSources(
                provider: (_) => const AssetImage('assets/otakase.png'),
                child: Scaffold(
                  body: SizedBox(
                    height: height,
                    child: HeroArt(anime, height: height),
                  ),
                ),
              ),
            ),
          );
          await tester.pumpAndSettle();
        }

        await art(const Anime(id: 1, title: 'Story', cover: 'portrait.jpg'));
        final poster = find.byKey(const Key('hero-fallback-poster'));
        expect(find.byKey(const Key('hero-fallback-backdrop')), findsOneWidget);
        expect(find.byType(ImageFiltered), findsOneWidget);
        expect(
          find.ancestor(of: poster, matching: find.byType(Opacity)),
          findsNothing,
        );
        expect(tester.getSize(poster).height, height);
        expect(tester.getSize(poster).width / height, closeTo(2 / 3, .001));
        expect(tester.widget<CoverImage>(poster).fit, BoxFit.contain);
        await art(
          const Anime(
            id: 1,
            title: 'Story',
            cover: 'portrait.jpg',
            banner: 'landscape.jpg',
          ),
        );
        expect(find.byKey(const Key('hero-fallback-poster')), findsNothing);
        expect(
          tester.widget<CoverImage>(find.byType(CoverImage)).url,
          'landscape.jpg',
        );
        await tester.pumpWidget(const SizedBox());
      },
    );
  }

  testWidgets(
    'player timeline uses Shu for real played progress and keeps keyboard seek',
    (tester) async {
      double position = 30;
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: StatefulBuilder(
              builder: (context, setState) => KageSeekSlider(
                value: position,
                max: 100,
                onChangeStart: (_) {},
                onChanged: (v) => setState(() => position = v),
                onChangeEnd: (_) {},
              ),
            ),
          ),
        ),
      );
      final slider = find.byType(Slider);
      final theme = SliderTheme.of(tester.element(slider));
      expect(theme.activeTrackColor, shu);
      expect(theme.trackHeight, 5);
      expect(
        theme.thumbShape!.getPreferredSize(true, false),
        const Size.square(13),
      );
      await tester.sendKeyEvent(LogicalKeyboardKey.tab);
      await tester.sendKeyEvent(LogicalKeyboardKey.arrowRight);
      await tester.pumpAndSettle();
      expect(position, greaterThan(30));
      await tester.pumpWidget(const SizedBox());
    },
  );
}
