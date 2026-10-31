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
  Future<AccountPage> publicList(
    String service,
    String username,
    String status, {
    required bool adult,
    int page = 1,
  }) async => const AccountPage([
    ListEntry(anime, 'CURRENT', 4),
    ListEntry(
      Anime(id: 2, title: 'Planned story', episodes: 12),
      'PLANNING',
      0,
    ),
  ]);
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
  TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
      .setMockMethodCallHandler(
        const MethodChannel('otakase/status'),
        (_) async => null,
      );
  TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
      .setMockMethodCallHandler(
        const MethodChannel('otakase/device'),
        (call) async => call.method == 'status'
            ? {'use24Hour': true, 'battery': 77, 'charging': false}
            : null,
      );
  await tester.pumpWidget(OtakaseApp(services: await services(), isTV: tv));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets(
    'phone personal Home, More accounts and Schedule, Back and exit',
    (tester) async {
      await launch(tester);
      expect(find.text('Find your next story'), findsOneWidget);
      expect(find.text('Trending'), findsOneWidget);
      expect(find.byKey(const Key('resume-card')), findsNothing);
      await tester.tap(find.byKey(const ValueKey('nav-More')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('more-Accounts')));
      await tester.pumpAndSettle();
      expect(find.text('Sign in to AniList'), findsOneWidget);
      await tester.ensureVisible(find.text('Sign in to MAL'));
      expect(find.text('Sign in to MAL'), findsOneWidget);
      expect(find.byType(TextField), findsNothing);
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('more-Schedule')));
      await tester.pumpAndSettle();
      expect(
        find.text('Airing times, not source availability'),
        findsOneWidget,
      );
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('more-Schedule')), findsOneWidget);
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(find.text('Find your next story'), findsOneWidget);
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(find.text('Exit Otakase?'), findsOneWidget);
      expect(FocusManager.instance.primaryFocus?.context?.widget, isNotNull);
      await tester.tap(find.text('Stay'));
      await tester.pumpAndSettle();
      expect(find.byType(Drawer), findsNothing);
      await tester.pumpWidget(const SizedBox());
    },
  );
  testWidgets('TV D-pad opens Search and Show and restores focus on Back', (
    tester,
  ) async {
    await launch(tester, tv: true);
    final nav = tester.widget<FocusBox>(find.byKey(const ValueKey('nav-Home')));
    nav.focusNode!.requestFocus();
    await tester.pump();
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowRight);
    await tester.pump();
    await tester.sendKeyEvent(LogicalKeyboardKey.select);
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('catalog-search')), findsOneWidget);
    final card = find.byKey(ValueKey('search-${anime.key}'));
    await tester.ensureVisible(card);
    await tester.tap(card);
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('show-play')), findsOneWidget);
    expect(FocusManager.instance.primaryFocus?.debugLabel, 'Resume or Play');
    expect(find.text('All episodes'), findsOneWidget);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('catalog-search')), findsOneWidget);
    for (final name in sourceNames) {
      expect(find.text(name), findsNothing);
    }
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets('phone Search opens Show with status editor and resume focus', (
    tester,
  ) async {
    await launch(tester);
    await tester.tap(find.byKey(const ValueKey('nav-Search')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(ValueKey('search-${anime.key}')));
    await tester.pumpAndSettle();
    expect(find.text('Play episode 1'), findsOneWidget);
    expect(find.text('Episodes'), findsOneWidget);
    await tester.ensureVisible(find.byKey(const Key('show-status')));
    await tester.tap(find.byKey(const Key('show-status')));
    await tester.pumpAndSettle();
    expect(
      find.text('Saved on this device. Public lists are read only.'),
      findsOneWidget,
    );
    await tester.tap(find.byTooltip('Increase progress'));
    await tester.tap(find.byTooltip('Increase score'));
    await tester.tap(find.text('Save'));
    await tester.pumpAndSettle();
    expect(find.text('Watched'), findsOneWidget);
    expect(find.text('済'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
  });
}
