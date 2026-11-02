import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/catalog.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/store.dart';
import 'package:otakase/desktop/desktop_core.dart';
import 'package:otakase/desktop/player_bar.dart';
import 'package:otakase/main.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/ui/show_page.dart';
import 'package:shared_preferences/shared_preferences.dart';

// Renders the app screens shown on the website, one set per show in
// test/fixtures/site/shows.json. Off unless SITE_SHOTS names an output folder;
// the Site workflow runs it and turns the PNGs into site/img/shots/*.webp.
const output = String.fromEnvironment('SITE_SHOTS');
const artwork = 'test/fixtures/site';
const only = String.fromEnvironment('SITE_ONLY');
const resumeAt = 3;

final images = <String, ui.Image>{};
final shows = <Anime>[];

String u(String f) => 'https://fixture.invalid/$f';

Anime fromJson(Map j) {
  final eps = (j['eps'] as List).cast<Map>();
  final total = j['episodes'] as int? ?? eps.length;
  return Anime(
    id: j['id'] as int,
    title: (j['title'] as String).replaceAll('`', '’').replaceAll("'", '’'),
    nativeTitle: j['native'] as String? ?? '',
    cover: u(j['cover'] as String),
    coverLarge: u(j['cover'] as String),
    banner: u(j['banner'] as String),
    description: j['desc'] as String? ?? '',
    format: j['format'] as String? ?? 'TV',
    status: total > eps.length ? 'RELEASING' : 'FINISHED',
    year: j['year'] as int? ?? 0,
    episodes: total,
    nextEpisode: total > eps.length && total < 500 ? eps.length + 1 : 0,
    duration: 24,
    score: ((j['score'] as int?) ?? 0) / 10,
    genres: (j['genres'] as List).cast<String>(),
    studio: j['studio'] as String? ?? '',
    episodeArt: [
      for (final e in eps)
        {
          'episode': e['n'],
          'title': e['t'],
          'thumbnail': u(e['img'] as String),
        },
    ],
  );
}

class SiteCatalog extends Catalog {
  SiteCatalog(this.main);
  final Anime main;
  List<Anime> get others => shows.where((s) => s.id != main.id).toList();
  @override
  Future<HomeData> home({required bool adult}) async => HomeData(
    trending: [main, ...others],
    season: [...others.reversed, main],
    recent: [
      Airing(main, resumeAt, DateTime(2026, 11, 2, 19, 30)),
      for (final a in others) Airing(a, 5, DateTime(2026, 11, 2)),
    ],
  );
  @override
  Future<CatalogPage> search(
    String query, {
    required bool adult,
    int page = 1,
    String? format,
    String? season,
    int? year,
    String? genre,
    String? status,
  }) async => CatalogPage([main, ...others]);
  @override
  Future<List<String>> genres({required bool adult}) async => ['Action'];
  @override
  Future<Anime> detail(Anime anime, {required bool adult}) async {
    final a = shows.firstWhere((s) => s.id == anime.id, orElse: () => main);
    return Anime(
      id: a.id,
      title: a.title,
      nativeTitle: a.nativeTitle,
      titles: [if (a.nativeTitle.isNotEmpty) a.nativeTitle],
      cover: a.cover,
      coverLarge: a.coverLarge,
      banner: a.banner,
      description: a.description,
      format: a.format,
      status: a.status,
      year: a.year,
      episodes: a.episodes,
      nextEpisode: a.nextEpisode,
      duration: 24,
      score: a.score,
      genres: a.genres,
      studio: a.studio,
      episodeArt: a.episodeArt,
      related: others.take(3).toList(),
      recommended: others.skip(3).toList(),
    );
  }

  @override
  Future<List<Airing>> schedule(
    DateTime day, {
    required bool adult,
    int page = 1,
  }) async => [Airing(main, resumeAt, day)];
}

class DecodedImage extends ImageProvider<DecodedImage> {
  const DecodedImage(this.image);
  final ui.Image image;
  @override
  Future<DecodedImage> obtainKey(ImageConfiguration configuration) =>
      SynchronousFuture(this);
  @override
  ImageStreamCompleter loadImage(
    DecodedImage key,
    ImageDecoderCallback decode,
  ) => OneFrameImageStreamCompleter(
    SynchronousFuture(ImageInfo(image: image.clone())),
  );
}

Future<void> fonts() async {
  final manifest =
      jsonDecode(await rootBundle.loadString('FontManifest.json')) as List;
  for (final item in manifest) {
    final loader = FontLoader(item['family'] as String);
    for (final font in item['fonts'] as List) {
      loader.addFont(rootBundle.load(font['asset'] as String));
    }
    await loader.load();
  }
}

Future<AppServices> services(Anime main) async {
  SharedPreferences.setMockInitialValues({});
  final store = AppStore(await SharedPreferences.getInstance());
  var n = 0;
  for (final a in shows.where((s) => s.id != main.id).take(4)) {
    n++;
    await store.progress(a, 1 + n, 200 + n * 180, 1440);
    sleep(const Duration(milliseconds: 5));
  }
  // the main show last, so it leads Back to your story
  sleep(const Duration(milliseconds: 20));
  await store.progress(main, resumeAt, 426, 1440);
  return AppServices(
    catalog: SiteCatalog(main),
    store: store,
    core: CoreClient(),
  );
}

void setUp(WidgetTester tester, Size size, {bool desktop = false}) {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  tester.platformDispatcher.accessibilityFeaturesTestValue =
      FakeAccessibilityFeatures(disableAnimations: true);
  addTearDown(tester.platformDispatcher.clearAccessibilityFeaturesTestValue);
  debugDesktopOverride = desktop;
  addTearDown(() => debugDesktopOverride = null);
  final m = TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
  m.setMockMethodCallHandler(
    const MethodChannel('window_manager'),
    (call) async => call.method == 'isMaximized' ? false : null,
  );
  m.setMockMethodCallHandler(
    const MethodChannel('otakase/status'),
    (_) async => null,
  );
  m.setMockMethodCallHandler(
    const MethodChannel('otakase/device'),
    (call) async => {'use24Hour': true},
  );
}

Widget art(Widget child) => RepaintBoundary(
  key: const Key('site-render'),
  child: ImageSources(
    provider: (url) =>
        DecodedImage(images[Uri.parse(url).path] ?? images.values.first),
    child: child,
  ),
);

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
  final list =
      jsonDecode(File('$artwork/shows.json').readAsStringSync()) as List;
  for (final j in list) {
    shows.add(fromJson(j as Map));
  }
  setUpAll(() async {
    await Directory(output).create(recursive: true);
    await fonts();
    for (final f in Directory(artwork).listSync()) {
      final name = f.uri.pathSegments.last;
      if (!name.endsWith('.jpg')) continue;
      final codec = await ui.instantiateImageCodec(
        File(f.path).readAsBytesSync(),
      );
      images['/$name'] = (await codec.getNextFrame()).image;
      codec.dispose();
    }
  });

  for (final main in shows) {
    if (only.isNotEmpty && '${main.id}' != only) continue;
    final id = main.id;

    testWidgets('$id phone', (tester) async {
      setUp(tester, const Size(412, 915));
      tester.view.padding = const FakeViewPadding(top: 24, bottom: 24);
      addTearDown(tester.view.resetPadding);
      final app = await services(main);
      await tester.pumpWidget(art(OtakaseApp(services: app, isTV: false)));
      await capture(tester, '$id-phone');
      await tester.pumpWidget(const SizedBox());
    });

    testWidgets('$id computer', (tester) async {
      setUp(tester, const Size(1440, 900), desktop: true);
      final app = await services(main);
      await tester.pumpWidget(art(OtakaseApp(services: app, isTV: true)));
      await openShow(tester, app, main, true);
      await capture(tester, '$id-computer');
      await tester.pumpWidget(const SizedBox());
    });

    testWidgets('$id tv', (tester) async {
      setUp(tester, const Size(1600, 900));
      final app = await services(main);
      await tester.pumpWidget(art(OtakaseApp(services: app, isTV: true)));
      await openShow(tester, app, main, true);
      await capture(tester, '$id-tv');
      await tester.pumpWidget(const SizedBox());
    });

    testWidgets('$id player', (tester) async {
      setUp(tester, const Size(1440, 900), desktop: true);
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
