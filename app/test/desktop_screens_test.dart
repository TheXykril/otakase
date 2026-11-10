import 'dart:async';
import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';

import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/main.dart';
import 'package:otakase/ui/show_page.dart';
import 'package:otakase/desktop/desktop_core.dart';
import 'package:otakase/desktop/launcher.dart';
import 'package:otakase/desktop/player_bar.dart';
import 'package:otakase/desktop/window_frame.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/services.dart';
import 'package:otakase/data/catalog.dart';
import 'package:otakase/data/store.dart';
import 'package:shared_preferences/shared_preferences.dart';

// Opt-in renders of the desktop app. Image bytes and PNGs stay outside Git.
const output = String.fromEnvironment('DESKTOP_SCREENSHOTS');
const artwork = String.fromEnvironment('SCREENSHOT_ART');
const enabled = true;
const story = Anime(
  id: 154587,
  malId: 52991,
  title: 'Frieren: Beyond Journey’s End',
  cover: 'https://fixture.invalid/cover.jpg',
  coverLarge: 'https://fixture.invalid/cover.jpg',
  banner: 'https://fixture.invalid/banner.jpg',
  description: 'The adventure is over. A quieter journey begins, through old memories and the fleeting lives of new companions.',
  format: 'TV',
  year: 2023,
  episodes: 28,
  duration: 24,
  score: 9.2,
);

/// More shows for fuller renders, read from gallery.json next to the art
/// (index, title, genres, studio, score; c<i>.jpg covers, b<i>.jpg banners).
final gallery = <Anime>[];

void loadGallery() {
  final file = File('$artwork/gallery.json');
  if (artwork.isEmpty || !file.existsSync()) return;
  for (final item in jsonDecode(file.readAsStringSync()) as List) {
    final i = item['i'] as int;
    gallery.add(
      Anime(
        id: 900000 + i,
        title: item['t'] as String,
        cover: 'https://fixture.invalid/c$i.jpg',
        coverLarge: 'https://fixture.invalid/c$i.jpg',
        banner: item['banner'] == true ? 'https://fixture.invalid/b$i.jpg' : '',
        format: item['f'] as String? ?? 'TV',
        year: item['y'] as int? ?? 0,
        episodes: item['ep'] as int? ?? 12,
        duration: 24,
        score: ((item['score'] as int?) ?? 0) / 10,
        genres: (item['g'] as List).cast<String>(),
        studio: item['studio'] as String? ?? '',
        description: 'A new season picks up right where the last one ended, with old rivals and new friends.',
      ),
    );
  }
}

class ScreenCatalog extends Catalog {
  @override
  Future<HomeData> home({required bool adult}) async => HomeData(
    trending: [story, ...gallery.where((a) => a.banner.isNotEmpty)],
    season: [...gallery.reversed, story],
    recent: [
      Airing(story, 8, DateTime(2026, 10, 28, 19, 30)),
      for (final a in gallery.take(12)) Airing(a, 5, DateTime(2026, 10, 28)),
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
    String? sort,
  }) async => const CatalogPage([story]);
  @override
  Future<List<String>> genres({required bool adult}) async => [
    'Adventure',
    'Fantasy',
  ];
  @override
  Future<Anime> detail(Anime anime, {required bool adult}) async => Anime(
    id: anime.id == 21 ? 21 : story.id,
    malId: story.malId,
    title: story.title,
    nativeTitle: '葬送のフリーレン',
    titles: const ['Sousou no Frieren', '葬送のフリーレン'],
    cover: story.cover,
    coverLarge: story.coverLarge,
    banner: story.banner,
    description: story.description,
    format: 'TV',
    status: 'FINISHED',
    year: 2023,
    episodes: anime.id == 21 ? 1180 : 28,
    duration: 24,
    score: 9.2,
    genres: const ['Adventure', 'Drama', 'Fantasy'],
    studio: 'Madhouse',
    related: gallery.take(3).toList(),
    recommended: gallery.skip(3).toList(),
  );
  @override
  Future<List<Airing>> schedule(
    DateTime day, {
    required bool adult,
    int page = 1,
  }) async => [
    Airing(story, 8, DateTime(day.year, day.month, day.day, 17, 30)),
    for (var i = 0; i < gallery.length; i++)
      if ((i + day.weekday) % 6 == 0)
        Airing(
          gallery[i],
          2 + i % 11,
          DateTime(day.year, day.month, day.day, 19 + i % 4, i.isEven ? 0 : 30),
        ),
  ];
}

final images = <String, ui.Image>{};

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

Future<AppServices> services() async {
  SharedPreferences.setMockInitialValues({});
  final store = AppStore(await SharedPreferences.getInstance());
  await store.progress(story, 8, 426, 1440);
  final watching = gallery.where((a) => a.banner.isNotEmpty).take(4);
  var n = 0;
  for (final a in watching) {
    n++;
    await store.progress(a, 3 + n * 7, 200 + n * 180, 1440);
  }
  return AppServices(
    catalog: ScreenCatalog(),
    store: store,
    core: CoreClient(),
  );
}

Future<void> capture(WidgetTester tester, String name) async {
  await tester.pumpAndSettle();
  await tester.pumpAndSettle();
  expect(tester.takeException(), isNull);
  if (output.isNotEmpty) {
    await expectLater(
      find.byKey(const Key('desktop-render')),
      matchesGoldenFile(Uri.file('$output/$name.png')),
    );
  }
}

void setUpScreen(WidgetTester tester, Size size) {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  tester.platformDispatcher.accessibilityFeaturesTestValue =
      FakeAccessibilityFeatures(disableAnimations: true);
  addTearDown(tester.platformDispatcher.clearAccessibilityFeaturesTestValue);
  debugDesktopOverride = true;
  addTearDown(() => debugDesktopOverride = null);
  final messenger =
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
  messenger.setMockMethodCallHandler(
    const MethodChannel('window_manager'),
    (call) async => call.method == 'isMaximized' ? false : null,
  );
  messenger.setMockMethodCallHandler(
    const MethodChannel('otakase/status'),
    (_) async => null,
  );
  messenger.setMockMethodCallHandler(
    const MethodChannel('otakase/device'),
    (call) async => call.method == 'status' ? {'use24Hour': true} : null,
  );
}

Widget art(Widget child) => RepaintBoundary(
  key: const Key('desktop-render'),
  child: ImageSources(
    provider: (url) => DecodedImage(images[Uri.parse(url).path]!),
    child: child,
  ),
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUpAll(() async {
    if (output.isNotEmpty) await Directory(output).create(recursive: true);
    await fonts();
    loadGallery();
    final names = ['cover.jpg', 'banner.jpg'];
    if (gallery.isNotEmpty) {
      for (final f in Directory(artwork).listSync()) {
        final name = f.uri.pathSegments.last;
        if (name.endsWith('.jpg') && !names.contains(name)) names.add(name);
      }
    }
    for (final name in names) {
      final codec = await ui.instantiateImageCodec(
        artwork.isNotEmpty
            ? File('$artwork/$name').readAsBytesSync()
            : (await rootBundle.load('assets/otakase.png')).buffer
                  .asUint8List(),
      );
      images['/$name'] = (await codec.getNextFrame()).image;
      codec.dispose();
    }
  });

  for (final screen in [
    'home',
    'search',
    'schedule',
    'show',
    'show-long',
    'settings',
  ]) {
    testWidgets('desktop render: $screen', (tester) async {
      setUpScreen(tester, const Size(1440, 900));
      final app = await services();
      await tester.pumpWidget(art(OtakaseApp(services: app, isTV: true)));
      await tester.pumpAndSettle();
      if (screen == 'search') {
        await tester.tap(find.byKey(const ValueKey('nav-Search')));
        await tester.pumpAndSettle();
        await tester.enterText(find.byType(TextField).first, 'frieren');
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const Key('search-filters')));
        await tester.pumpAndSettle();
        await tester.tap(find.text('TV').last);
        await tester.pumpAndSettle();
        await tester.tap(find.text('Fantasy').last);
        await tester.pumpAndSettle();
        await capture(tester, 'search-filters');
        await tester.tapAt(const Offset(200, 600));
      } else if (screen == 'settings') {
        await tester.tap(find.byKey(const ValueKey('nav-More')));
      } else if (screen == 'schedule') {
        await tester.tap(find.byKey(const ValueKey('nav-More')));
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const ValueKey('more-Schedule')));
      } else if (screen == 'show-long') {
        unawaited(
          tester
              .state<NavigatorState>(find.byType(Navigator).first)
              .push(
                OtakaseRoute<void>(
                  builder: (_) => ShowPage(
                    services: app,
                    anime: const Anime(id: 21, title: 'A very long show'),
                    isTV: true,
                  ),
                ),
              ),
        );
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const Key('episode-range-next')));
        await tester.pumpAndSettle();
        expect(find.text('51–100'), findsOneWidget);
        expect(find.byKey(const Key('episode-range-3')), findsNothing);
      } else if (screen == 'show') {
        unawaited(
          tester
              .state<NavigatorState>(find.byType(Navigator).first)
              .push(
                OtakaseRoute<void>(
                  builder: (_) =>
                      ShowPage(services: app, anime: story, isTV: true),
                ),
              ),
        );
      }
      await capture(tester, screen);
      await tester.pumpWidget(const SizedBox());
    });
  }

  for (final style in WindowButtonStyle.values) {
    testWidgets('desktop render: top bar ${style.name}', (tester) async {
      setUpScreen(tester, const Size(1440, 900));
      debugWindowButtonStyle = style;
      addTearDown(() => debugWindowButtonStyle = null);
      await tester.pumpWidget(
        art(OtakaseApp(services: await services(), isTV: true)),
      );
      await capture(tester, 'home-${style.name}');
      await tester.pumpWidget(const SizedBox());
    });
  }

  testWidgets('desktop render: top bar macos', (tester) async {
    setUpScreen(tester, const Size(1440, 900));
    debugMacLayout = true;
    addTearDown(() => debugMacLayout = null);
    // macOS draws its own traffic lights over the window; stand-ins here.
    Widget light(int color) => Container(
      width: 12,
      height: 12,
      margin: const EdgeInsets.only(right: 8),
      decoration: BoxDecoration(color: Color(color), shape: BoxShape.circle),
    );
    await tester.pumpWidget(
      art(
        Directionality(
          textDirection: TextDirection.ltr,
          child: Stack(
            children: [
              OtakaseApp(services: await services(), isTV: true),
              Positioned(
                left: 20,
                top: 20,
                child: Row(
                  children: [
                    light(0xffff5f57),
                    light(0xfffebc2e),
                    light(0xff28c840),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
    await capture(tester, 'home-macos');
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('desktop render: launcher', (tester) async {
    setUpScreen(tester, launcherSize);
    await tester.pumpWidget(
      art(
        MaterialApp(
          debugShowCheckedModeBanner: false,
          theme: ThemeData(brightness: Brightness.dark),
          home: LauncherPage(services: await services()),
        ),
      ),
    );
    await capture(tester, 'launcher');
    await tester.enterText(find.byKey(const Key('launcher-input')), 'frier');
    await capture(tester, 'launcher-typed');
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('desktop render: player', (tester) async {
    setUpScreen(tester, const Size(1440, 810));
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
                  Image(
                    image: DecodedImage(images['/banner.jpg']!),
                    fit: BoxFit.cover,
                  ),
                  DesktopPlayerBar(
                    title: story.title,
                    subtitle: 'Ep 8 · SUB',
                    centerLabel: 'Episode 8',
                    position: const Duration(minutes: 7, seconds: 12),
                    duration: const Duration(minutes: 24),
                    buffered: const Duration(minutes: 10),
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
                ],
              ),
            ),
          ),
        ),
      ),
    );
    await capture(tester, 'player');
    await tester.pumpWidget(const SizedBox());
  });
}
