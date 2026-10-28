import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/catalog.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/store.dart';
import 'package:otakase/main.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/components.dart';
import 'package:shared_preferences/shared_preferences.dart';

const anime = Anime(
  id: 52991,
  malId: 52991,
  title: 'Frieren',
  description: 'A journey after the adventure.',
  format: 'TV',
  year: 2023,
  episodes: 28,
  duration: 24,
  score: 9.2,
);

class TestCatalog extends Catalog {
  @override
  Future<HomeData> home({required bool adult}) async =>
      const HomeData(trending: [anime], season: [anime]);
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
  }) async => const CatalogPage([anime]);
  @override
  Future<List<String>> genres({required bool adult}) async => ['Adventure'];
  @override
  Future<Anime> detail(Anime a, {required bool adult}) async => anime;
  @override
  Future<List<Airing>> schedule(
    DateTime day, {
    required bool adult,
    int page = 1,
  }) async => [Airing(anime, 4, day)];
}

Future<AppServices> services() async {
  SharedPreferences.setMockInitialValues({});
  return AppServices(
    catalog: TestCatalog(),
    store: AppStore(await SharedPreferences.getInstance()),
    core: CoreClient(),
  );
}

Future<void> launch(WidgetTester tester, {bool tv = false}) async {
  tester.view.physicalSize = tv ? const Size(1280, 720) : const Size(412, 915);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  tester.platformDispatcher.accessibilityFeaturesTestValue =
      FakeAccessibilityFeatures(disableAnimations: true);
  addTearDown(tester.platformDispatcher.clearAccessibilityFeaturesTestValue);
  await tester.pumpWidget(OtakaseApp(services: await services(), isTV: tv));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('phone home hero and catalog rows, navigation and Back', (
    tester,
  ) async {
    await launch(tester);
    expect(find.text('WATCH NOW'), findsOneWidget);
    expect(
      find.byKey(const ValueKey('section-Continue Watching')),
      findsNothing,
    );
    await tester.tap(find.byKey(const ValueKey('nav-MyList')));
    await tester.pumpAndSettle();
    expect(find.byKey(const ValueKey('username-AniList')), findsOneWidget);
    expect(find.byKey(const ValueKey('username-MAL')), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('nav-Search')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('catalog-search')), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('nav-Schedule')));
    await tester.pumpAndSettle();
    expect(find.text('Airing times, not source availability'), findsOneWidget);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(find.text('WATCH NOW'), findsOneWidget);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(find.text('Exit Otakase?'), findsOneWidget);
    await tester.tap(find.text('STAY'));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('nav-Settings')), findsNothing);
    await tester.tap(find.byKey(const Key('nav-Menu')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('drawer-settings')));
    await tester.pumpAndSettle();
    expect(find.text('SUBTITLE & LANGUAGES'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets(
    'TV D-pad moves from nav to visible red focus and OK opens show',
    (tester) async {
      await launch(tester, tv: true);
      expect(find.text('OTAKASE'), findsOneWidget);
      expect(FocusManager.instance.primaryFocus?.debugLabel, 'Home navigation');
      await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
      await tester.pumpAndSettle();
      expect(
        FocusManager.instance.primaryFocus?.debugLabel,
        isNot('Home navigation'),
      );
      final focused = find.byWidgetPredicate(
        (w) =>
            w is AnimatedContainer &&
            w.decoration is BoxDecoration &&
            ((w.decoration as BoxDecoration).border as Border?)?.top.color ==
                shu,
      );
      expect(focused, findsWidgets);
      await tester.sendKeyEvent(LogicalKeyboardKey.select);
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('show-play')), findsOneWidget);
      expect(FocusManager.instance.primaryFocus?.debugLabel, 'Resume or Play');
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      await tester.sendKeyEvent(LogicalKeyboardKey.arrowUp);
      await tester.pumpAndSettle();
      expect(FocusManager.instance.primaryFocus?.debugLabel, 'Home navigation');
      await tester.sendKeyEvent(LogicalKeyboardKey.arrowLeft);
      await tester.pumpAndSettle();
      expect(find.text('PROFILE / ACCOUNTS'), findsOneWidget);
      for (final name in sourceNames) {
        expect(find.text(name), findsNothing);
      }
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(find.text('PROFILE / ACCOUNTS'), findsNothing);
      await tester.pumpWidget(const SizedBox());
    },
  );
  testWidgets('phone Watch Now opens show with resume focus', (tester) async {
    await launch(tester);
    await tester.tap(find.text('WATCH NOW'));
    await tester.pumpAndSettle();
    expect(find.text('PLAY EPISODE 1'), findsOneWidget);
    expect(find.text('EPISODES'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
  });
}
