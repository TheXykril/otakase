import 'dart:async';
import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';

import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/main.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/services.dart';
import 'package:otakase/data/catalog.dart';
import 'package:otakase/data/store.dart';
import 'package:shared_preferences/shared_preferences.dart';

// Opt-in renders of production widgets. Image bytes and PNGs stay outside Git.
const output = String.fromEnvironment('PHONE_SCREENSHOTS');
const artwork = String.fromEnvironment('SCREENSHOT_ART');
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

class ScreenCatalog extends Catalog {
  @override
  Future<HomeData> home({required bool adult}) async => HomeData(
    trending: [story],
    season: [story],
    recent: [Airing(story, 8, DateTime(2026, 10, 28, 19, 30))],
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
  }) async => const CatalogPage([story]);
  @override
  Future<List<String>> genres({required bool adult}) async => [
    'Adventure',
    'Fantasy',
  ];
  @override
  Future<Anime> detail(Anime anime, {required bool adult}) async => story;
  @override
  Future<List<Airing>> schedule(
    DateTime day, {
    required bool adult,
    int page = 1,
  }) async => [Airing(story, 8, day)];
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

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUpAll(() async {
    if (output.isEmpty) return;
    await Directory(output).create(recursive: true);
    await fonts();
    for (final name in ['cover.jpg', 'banner.jpg']) {
      final codec = await ui.instantiateImageCodec(
        File('$artwork/$name').readAsBytesSync(),
      );
      images['/$name'] = (await codec.getNextFrame()).image;
      codec.dispose();
    }
  });
  for (final screen in ['home', 'mylist', 'search', 'settings']) {
    testWidgets('phone render: $screen', (tester) async {
      tester.view.physicalSize = const Size(412, 915);
      tester.view.devicePixelRatio = 1;
      tester.view.padding = const FakeViewPadding(top: 24, bottom: 24);
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      addTearDown(tester.view.resetPadding);
      tester.platformDispatcher.accessibilityFeaturesTestValue =
          FakeAccessibilityFeatures(disableAnimations: true);
      addTearDown(
        tester.platformDispatcher.clearAccessibilityFeaturesTestValue,
      );
      SharedPreferences.setMockInitialValues({});
      final store = AppStore(await SharedPreferences.getInstance());
      await store.progress(story, 8, 426, 1440);
      await tester.runAsync(() async {
        final stream = const AssetImage('assets/mark.png')
            .resolve(const ImageConfiguration(devicePixelRatio: 1));
        final ready = Completer<void>();
        final listener = ImageStreamListener((image, synchronous) {
          if (!ready.isCompleted) ready.complete();
        });
        stream.addListener(listener);
        await ready.future;
        stream.removeListener(listener);
      });
      await tester.pumpWidget(
        RepaintBoundary(
          key: const Key('phone-render'),
          child: ImageSources(
            provider: (url) => DecodedImage(images[Uri.parse(url).path]!),
            child: OtakaseApp(
              services: AppServices(
                catalog: ScreenCatalog(),
                store: store,
                core: CoreClient(),
              ),
              isTV: false,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      if (screen == 'mylist') {
        await tester.tap(find.byKey(const ValueKey('nav-MyList')));
      } else if (screen == 'search') {
        await tester.tap(find.byKey(const ValueKey('nav-Search')));
      } else if (screen == 'settings') {
        await tester.tap(find.byKey(const Key('nav-Menu')));
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const Key('drawer-settings')));
      }
      await tester.pumpAndSettle();
      await tester.pumpAndSettle();
      for (final element in find.byType(RawImage).evaluate()) {
        expect(
          (element.renderObject! as RenderImage).image,
          isNotNull,
          reason: 'Artwork must be decoded before capture',
        );
      }
      await expectLater(
        find.byKey(const Key('phone-render')),
        matchesGoldenFile(Uri.file('$output/$screen.png')),
      );
      await tester.pumpWidget(const SizedBox());
    }, skip: output.isEmpty);
  }
}
