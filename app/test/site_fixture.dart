// Shared fixture for the website renders: the shows in
// test/fixtures/site/shows.json with their committed art, an app with a
// little watch history, and a test view sized like a real screen.
// Used by site_screens_test.dart and site_tour_test.dart.
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
import 'package:otakase/services.dart';
import 'package:otakase/ui/components.dart';
import 'package:shared_preferences/shared_preferences.dart';

const artwork = 'test/fixtures/site';
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

void setUpView(WidgetTester tester, Size size, {bool desktop = false}) {
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
    // The status bar asks for a map; pairing links and the rest get nothing.
    (call) async => call.method == 'status' ? {'use24Hour': true} : null,
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

/// Reads shows.json and decodes every committed picture once.
void readShows() {
  if (shows.isNotEmpty) return;
  final list =
      jsonDecode(File('$artwork/shows.json').readAsStringSync()) as List;
  for (final j in list) {
    shows.add(fromJson(j as Map));
  }
}

Future<void> loadArt() async {
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
}
