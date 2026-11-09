import 'dart:convert';
import 'dart:io';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/desktop/player_bar.dart';
import 'package:otakase/main.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/ui/phone_player.dart';

import 'site_fixture.dart';

// Renders the "Look around" tour on the website: every screen of the real app
// that can be reached from Home by pressing tabs, More rows, covers and Play,
// once per device, plus where each of those buttons sits on the screen. The
// screens are found by walking the app itself, so a new More row, tab or
// page shows up on the site at the next Site deploy without touching this
// file. Off unless SITE_TOUR names an output folder; the Site workflow runs
// it and turns the result into site/tour/.
const output = String.fromEnvironment('SITE_TOUR');

// Upper bound per device, so a page that keeps linking to new pages cannot
// make the deploy run forever.
const maxScreens = 60;

class Device {
  const Device(this.name, this.size, {this.desktop = false, this.tv = false});
  final String name;
  final Size size;
  final bool desktop, tv;
  Size get playerSize => name == 'phone' ? Size(size.height, size.width) : size;
}

const devices = [
  Device('computer', Size(1440, 900), desktop: true, tv: true),
  Device('phone', Size(412, 915)),
  Device('tv', Size(1600, 900), tv: true),
];

/// Where a button goes. [path] is the list of keys pressed from Home to get
/// there; the player and Back are drawn by the site instead.
class Target {
  const Target(this.id, this.title, this.kind, [this.path]);
  final String id, title, kind;
  final List<String>? path;
}

class Link {
  Link(this.rect, this.target, this.key);
  final Rect rect;
  final Target target;
  final String key;
  Map<String, Object> toJson() => {
    'x': rect.left.round(),
    'y': rect.top.round(),
    'w': rect.width.round(),
    'h': rect.height.round(),
    'to': target.id,
    'label': target.title,
  };
}

final tabs = {'Home': 'home', 'Search': 'search', 'More': 'more'};
final showKey = RegExp(r'-al:(\d+)$');
final episodeKey = RegExp(r'^episode-\d+$');
const playKeys = {'show-play', 'home-resume', 'desktop-hero-play'};

/// Card rows that play instead of opening the show, such as Continue
/// watching. Learnt while walking: opening one starts the player.
final playRows = <String>{};
String rowOf(String key) => key.replaceFirst(showKey, '');

String slug(String s) => s
    .toLowerCase()
    .replaceAll('&', 'and')
    .replaceAll(RegExp(r'[^a-z0-9]+'), '-')
    .replaceAll(RegExp(r'^-|-$'), '');

Target player(Anime main) => Target('player', 'Play ${main.title}', 'player');
const back = Target('back', 'Back', 'back');

/// What pressing the widget with [key] does, seen from a screen reached by
/// [path]; null for anything the tour does not follow.
Target? follow(String key, List<String> path, Anime main) {
  final nav = key.startsWith('show-nav-')
      ? key.substring(9)
      : key.startsWith('nav-')
      ? key.substring(4)
      : null;
  if (nav != null) {
    final id = tabs[nav];
    if (id == null) return null;
    return Target(id, nav, 'tab', id == 'home' ? [] : ['nav-$nav']);
  }
  if (key.startsWith('more-')) {
    final name = key.substring(5);
    return Target('more-${slug(name)}', name, 'more', [...path, key]);
  }
  if (playKeys.contains(key) || episodeKey.hasMatch(key)) return player(main);
  if (showKey.hasMatch(key) && playRows.contains(rowOf(key))) {
    return player(main);
  }
  if (key == 'show-back') return back;
  if (key == 'search-filter-menu' || key == 'search-filters') {
    return Target('filters', 'Filters', 'page', [...path, key]);
  }
  final cover = showKey.firstMatch(key);
  final id = cover == null
      ? key == 'desktop-hero-details' || key == 'resume-card'
            ? main.id
            : null
      : int.parse(cover.group(1)!);
  if (id == null) return null;
  final show = shows.where((s) => s.id == id).firstOrNull;
  if (show == null) return null;
  return Target('show-$id', show.title, 'show', [...path, key]);
}

Rect? visibleRect(WidgetTester tester, Element e, Rect view) {
  final box = e.renderObject;
  if (box is! RenderBox || !box.attached || !box.hasSize) return null;
  final rect = MatrixUtils.transformRect(
    box.getTransformTo(null),
    Offset.zero & box.size,
  );
  final shown = rect.intersect(view);
  if (shown.width < 12 || shown.height < 12) return null;
  // Only what a press would really reach: not hidden behind a dialog, not
  // on a tab in the background, not scrolled out of its row.
  final hit = HitTestResult();
  WidgetsBinding.instance.hitTestInView(hit, shown.center, tester.view.viewId);
  if (!hit.path.any((entry) => entry.target == box)) return null;
  return shown;
}

List<Link> links(
  WidgetTester tester,
  Size size,
  List<String> path,
  Anime main,
) {
  final view = Offset.zero & size;
  final found = <Link>[];
  void add(Element e, Target t, [String key = '']) {
    final r = visibleRect(tester, e, view);
    if (r == null) return;
    // A card is wrapped in more than one keyed widget; keep one press.
    final same = found.indexWhere(
      (l) =>
          l.target.id == t.id &&
          l.rect.intersect(r).width * l.rect.intersect(r).height >
              .6 * r.width * r.height,
    );
    if (same >= 0) {
      if (found[same].rect.width * found[same].rect.height <
          r.width * r.height) {
        found[same] = Link(r, t, key);
      }
      return;
    }
    found.add(Link(r, t, key));
  }

  for (final e
      in find
          .byWidgetPredicate(
            (w) => w.key is ValueKey<String> && w.key is! PageStorageKey,
          )
          .evaluate()) {
    final key = (e.widget.key! as ValueKey<String>).value;
    final t = follow(key, path, main);
    if (t != null) add(e, t, key);
  }
  for (final e in find.byType(BackButton).evaluate()) {
    add(e, back);
  }
  for (final e in find.byTooltip('Back').evaluate()) {
    add(e, back);
  }
  return found;
}

/// The page's vertical scrolls a press can reach: usually one, two on a
/// computer's settings (the section list and the section).
List<ScrollPosition> pageScrolls(WidgetTester tester, Size size) => [
  for (final e in find.byType(Scrollable).evaluate())
    if (((e as StatefulElement).state as ScrollableState).position case final p
        when p.axis == Axis.vertical &&
            p.hasContentDimensions &&
            p.maxScrollExtent > 0 &&
            visibleRect(tester, e, Offset.zero & size) != null)
      p,
];

/// Scrolls the page down to [y], each scroll as far as it goes.
void scrollTo(List<ScrollPosition> scrolls, double y) {
  for (final p in scrolls) {
    p.jumpTo(y.clamp(0, p.maxScrollExtent).toDouble());
  }
}

/// Lets the app finish loading without waiting forever on a page that
/// animates or polls.
Future<void> settle(WidgetTester tester) async {
  try {
    await tester.pumpAndSettle(
      const Duration(milliseconds: 100),
      EnginePhase.sendSemanticsUpdate,
      const Duration(seconds: 5),
    );
  } on FlutterError {
    await tester.pump(const Duration(seconds: 1));
  }
}

Future<void> capture(WidgetTester tester, String file) => expectLater(
  find.byKey(const Key('site-render')),
  matchesGoldenFile(Uri.file(file)),
);

Widget playerScreen(Device d, Anime main) {
  final ep = main.episodeArt.firstWhere(
    (e) => e['episode'] == resumeAt,
    orElse: () => main.episodeArt.first,
  );
  final frame = images[Uri.parse(ep['thumbnail'] as String).path]!;
  const spans = [SkipSpan('op', 40, 130), SkipSpan('ed', 1290, 1380)];
  final controls = d.desktop
      ? DesktopPlayerBar(
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
          stripes: spans,
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
        )
      : PhonePlayerControls(
          isTV: d.tv,
          title: main.title,
          subtitle: 'Episode $resumeAt · SUB',
          position: const Duration(minutes: 1, seconds: 12),
          duration: const Duration(minutes: 24),
          spans: spans,
          playing: true,
          buffering: false,
          onBack: () {},
          onPlayPause: () {},
          onSeekBy: (_) {},
          onSeekStart: (_) {},
          onSeekChanged: (_) {},
          onSeekEnd: (_) {},
          actions: [
            PlayerButton(
              icon: LucideIcons.skipForward,
              label: 'Next episode',
              onPressed: () {},
            ),
            PlayerButton(
              icon: LucideIcons.captions,
              label: 'Audio & subtitles',
              onPressed: () {},
            ),
            PlayerButton(
              icon: LucideIcons.settings2,
              label: 'Quality',
              onPressed: () {},
            ),
          ],
          above: [
            AppButton(label: 'Skip opening', onPressed: () {}, isTV: d.tv),
          ],
        );
  final stack = Stack(
    fit: StackFit.expand,
    children: [
      Image(image: DecodedImage(frame), fit: BoxFit.cover),
      controls,
      if (d.desktop)
        Positioned(
          right: 24,
          bottom: 170,
          child: AppButton(label: 'Skip opening', onPressed: () {}, isTV: true),
        ),
    ],
  );
  return MaterialApp(
    debugShowCheckedModeBanner: false,
    home: Scaffold(
      backgroundColor: Colors.black,
      body: d.tv ? TVViewport(child: stack) : stack,
    ),
  );
}

final tour = <String, Object>{};

// Longest page drawn, in screens: Home and More scroll further than that, but
// the site only needs enough to reach every row.
const maxPages = 4;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  if (output.isEmpty) {
    test('site tour', () {}, skip: 'set SITE_TOUR to render');
    return;
  }
  readShows();
  final main = shows.first;
  setUpAll(() async {
    await Directory(output).create(recursive: true);
    await loadArt();
  });
  tearDownAll(() {
    File('$output/tour.json')
        .writeAsStringSync(jsonEncode({'show': main.title, 'devices': tour}));
  });

  for (final d in devices) {
    testWidgets(
      '${d.name} tour',
      timeout: const Timeout(Duration(minutes: 30)),
      (tester) async {
        final dir = '$output/${d.name}';
        final screens = <String, Map<String, Object>>{};
        final queue = <Target>[const Target('home', 'Home', 'tab', [])];
        final seen = {'home'};
        final broken = <String>{};
        setUpView(tester, d.size, desktop: d.desktop);
        if (d.name == 'phone') {
          tester.view.padding = const FakeViewPadding(top: 24, bottom: 24);
          addTearDown(tester.view.resetPadding);
        }

        // Starts the app afresh and presses [path]; a `scroll:N` step scrolls
        // the page to N. Null when that worked, else what went wrong.
        Future<Object?> open(List<String> path) async {
          await tester.pumpWidget(const SizedBox());
          final app = await services(main);
          await tester.pumpWidget(art(OtakaseApp(services: app, isTV: d.tv)));
          await settle(tester);
          for (final step in path) {
            if (step.startsWith('scroll:')) {
              final scrolls = pageScrolls(tester, d.size);
              if (scrolls.isEmpty) return 'nothing to scroll';
              scrollTo(scrolls, double.parse(step.substring(7)));
              await settle(tester);
              continue;
            }
            // Press the part that shows: a row cut by the screen edge has
            // its middle off screen.
            final at = find
                .byKey(ValueKey(step))
                .evaluate()
                .map((e) => visibleRect(tester, e, Offset.zero & d.size))
                .nonNulls
                .firstOrNull;
            if (at == null) return '$step not found';
            await tester.tapAt(at.center);
            await settle(tester);
          }
          return tester.takeException();
        }

        void found(List<Link> links) {
          for (final l in links) {
            if (l.target.path != null && seen.add(l.target.id)) {
              queue.add(l.target);
            }
          }
        }

        while (queue.isNotEmpty && screens.length < maxScreens) {
          final t = queue.removeAt(0);
          final path = t.path!;
          final error = await open(path);
          if (error != null) {
            if ('$error'.contains('MediaKit') && path.isNotEmpty) {
              // That card plays the episode: the row is a Continue row.
              playRows.add(rowOf(path.last));
              seen.remove(t.id);
            } else {
              broken.add(t.id);
              debugPrint('tour: ${d.name}/${t.id} skipped: $error');
            }
            continue;
          }
          final pages = <Map<String, Object>>[];
          final first = links(tester, d.size, path, main);
          await capture(tester, '$dir/${t.id}.png');
          pages.add({'img': t.id, 'y': 0, 'links': first});
          found(first);
          // Further down the page, most of a screen at a time.
          final scrolls = pageScrolls(tester, d.size);
          if (scrolls.isNotEmpty) {
            final end = scrolls.map((p) => p.maxScrollExtent).reduce(math.max);
            final step = (d.size.height * .75).roundToDouble();
            for (var i = 1; i < maxPages; i++) {
              final y = math.min(step * i, end);
              if (y <= (pages.last['y']! as num)) break;
              scrollTo(scrolls, y);
              await settle(tester);
              final more = links(tester, d.size, [...path, 'scroll:$y'], main);
              await capture(tester, '$dir/${t.id}~$i.png');
              pages.add({'img': '${t.id}~$i', 'y': y.round(), 'links': more});
              found(more);
            }
          }
          screens[t.id] = {
            'title': t.title,
            'kind': t.kind,
            'w': d.size.width.round(),
            'h': d.size.height.round(),
            'pages': pages,
          };
        }
        await tester.pumpWidget(const SizedBox());

        // The player is drawn from its own widgets: playing needs a real video.
        tester.view.physicalSize = d.playerSize;
        tester.view.resetPadding();
        await tester.pumpWidget(art(playerScreen(d, main)));
        await settle(tester);
        final controls = links(tester, d.playerSize, const [], main);
        await capture(tester, '$dir/player.png');
        screens['player'] = {
          'title': main.title,
          'kind': 'player',
          'w': d.playerSize.width.round(),
          'h': d.playerSize.height.round(),
          'pages': [
            {'img': 'player', 'y': 0, 'links': controls},
          ],
        };
        await tester.pumpWidget(const SizedBox());

        // Cards found before their row turned out to play, and presses that
        // lead to a screen that could not be drawn.
        for (final s in screens.values) {
          for (final page in s['pages']! as List<Map<String, Object>>) {
            final list = page['links']! as List<Link>;
            for (var i = 0; i < list.length; i++) {
              final l = list[i];
              if (showKey.hasMatch(l.key) && playRows.contains(rowOf(l.key))) {
                list[i] = Link(l.rect, player(main), l.key);
              }
            }
            list.removeWhere(
              (l) => !screens.containsKey(l.target.id) && l.target.id != 'back',
            );
          }
        }
        tour[d.name] = {'start': 'home', 'screens': screens};
        expect(screens.length, greaterThan(3), reason: 'broken: $broken');
      },
    );
  }
}
