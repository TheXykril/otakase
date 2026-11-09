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
import 'package:otakase/data/models.dart';
import 'package:otakase/data/store.dart';
import 'package:otakase/desktop/desktop_core.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/components.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../demo/catalog.dart';

export '../demo/catalog.dart' show resumeAt;

const artwork = 'test/fixtures/site';

final images = <String, ui.Image>{};
final shows = <Anime>[];

String u(String f) => 'https://fixture.invalid/$f';

Anime fromJson(Map j) => showFromJson(j, u);

class SiteCatalog extends DemoCatalog {
  SiteCatalog(Anime main) : super(shows, main);
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
