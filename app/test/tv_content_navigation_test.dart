import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/main.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/ui/show_page.dart';

import 'tv_focus_test.dart' show focusPoster, key, navigation;
import 'tv_navigation_audit_test.dart' show focusedWithin;
import 'widget_test.dart' as fixture;
import 'auth_test.dart' show MemoryTokens;

final stories = List.generate(
  24,
  (i) => Anime(id: i + 1, title: 'Story ${i + 1}', episodes: 24),
);

class BrowseCatalog extends fixture.TestCatalog {
  List<Anime> homeItems = stories;
  @override
  Future<HomeData> home({required bool adult}) async =>
      HomeData(trending: homeItems, season: stories.reversed.toList());
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
  }) async => CatalogPage(stories);
}

Future<AppServices> launchBrowse(WidgetTester tester) async {
  await fixture.launch(tester, tv: true);
  await tester.pumpWidget(const SizedBox());
  final base = await fixture.services();
  final services = AppServices(
    catalog: BrowseCatalog(),
    store: base.store,
    core: base.core,
    auth: AuthService(storage: MemoryTokens()),
  );
  await tester.pumpWidget(OtakaseApp(services: services, isTV: true));
  await tester.pumpAndSettle();
  addTearDown(() async {
    await tester.pumpWidget(const SizedBox());
    await services.close();
  });
  return services;
}

Finder result(int i) => find.byKey(ValueKey('search-${stories[i].key}'));

Future<void> launchShelves(WidgetTester tester) async {
  await fixture.launch(tester, tv: true);
  await tester.pumpWidget(
    AppLayout(
      isTV: true,
      child: MaterialApp(
        home: Scaffold(
          body: ListView(
            children: [
              for (final title in ['First row', 'Second row'])
                PosterRow(
                  title: title,
                  items: stories,
                  isTV: true,
                  onOpen: (_) {},
                ),
            ],
          ),
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('TV Search grid keeps Left and Right inside the current row', (
    tester,
  ) async {
    await launchBrowse(tester);
    navigation(tester, 'Search').requestFocus();
    await tester.pumpAndSettle();
    await focusPoster(tester, result(0));
    await key(tester, LogicalKeyboardKey.arrowLeft);
    expect(focusedWithin(result(0)), isTrue);
    final grid = tester.widget<GridView>(find.byType(GridView));
    final columns =
        (grid.gridDelegate as SliverGridDelegateWithFixedCrossAxisCount)
            .crossAxisCount;
    for (var i = 1; i < columns; i++) {
      await key(tester, LogicalKeyboardKey.arrowRight);
      expect(focusedWithin(result(i)), isTrue);
    }
    await key(tester, LogicalKeyboardKey.arrowRight);
    expect(focusedWithin(result(columns - 1)), isTrue);
    await key(tester, LogicalKeyboardKey.arrowDown);
    expect(focusedWithin(result(columns * 2 - 1)), isTrue);
    await key(tester, LogicalKeyboardKey.arrowUp);
    expect(focusedWithin(result(columns - 1)), isTrue);
  });

  testWidgets('TV shelf edges clamp and long rows visit every title in order', (
    tester,
  ) async {
    await launchShelves(tester);
    Finder card(int i) => find.byKey(ValueKey('First row-${stories[i].key}'));
    await focusPoster(tester, card(0));
    await key(tester, LogicalKeyboardKey.arrowRight);
    expect(focusedWithin(card(1)), isTrue);
    for (var i = 2; i < stories.length; i++) {
      await key(tester, LogicalKeyboardKey.arrowRight);
      expect(focusedWithin(card(i)), isTrue, reason: 'Card $i');
    }
    await key(tester, LogicalKeyboardKey.arrowRight);
    expect(focusedWithin(card(stories.length - 1)), isTrue);
    for (var i = stories.length - 2; i >= 0; i--) {
      await key(tester, LogicalKeyboardKey.arrowLeft);
      expect(focusedWithin(card(i)), isTrue, reason: 'Card $i');
    }
    await key(tester, LogicalKeyboardKey.arrowLeft);
    expect(focusedWithin(card(0)), isTrue);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('TV focus leaves fully visible covers and shelves still', (
    tester,
  ) async {
    await launchShelves(tester);
    final first = find.byKey(const ValueKey('First row-al:1'));
    final second = find.byKey(const ValueKey('First row-al:2'));
    await focusPoster(tester, first);
    final before = tester.getTopLeft(first);
    await key(tester, LogicalKeyboardKey.arrowRight);
    expect(focusedWithin(second), isTrue);
    expect(tester.getTopLeft(first), before);
    await key(tester, LogicalKeyboardKey.arrowDown);
    expect(
      focusedWithin(find.byKey(const ValueKey('Second row-al:2'))),
      isTrue,
    );
    await key(tester, LogicalKeyboardKey.arrowUp);
    expect(focusedWithin(second), isTrue);
    expect(tester.getTopLeft(first), before);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('TV discovery restores the exact card and scroll after Show', (
    tester,
  ) async {
    await launchBrowse(tester);
    Finder card(int i) => find.byKey(ValueKey('Trending-${stories[i].key}'));
    await focusPoster(tester, card(0));
    await tester.sendKeyDownEvent(LogicalKeyboardKey.arrowRight);
    await tester.pumpAndSettle();
    for (var i = 0; i < 15; i++) {
      await tester.sendKeyRepeatEvent(LogicalKeyboardKey.arrowRight);
      await tester.pumpAndSettle();
    }
    await tester.sendKeyUpEvent(LogicalKeyboardKey.arrowRight);
    await tester.pumpAndSettle();
    expect(focusedWithin(card(16)), isTrue);
    final before = tester.getTopLeft(card(16));
    await key(tester, LogicalKeyboardKey.select);
    expect(find.byType(ShowPage), findsOneWidget);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(focusedWithin(card(16)), isTrue);
    expect(tester.getTopLeft(card(16)), before);
    await key(tester, LogicalKeyboardKey.arrowLeft);
    expect(focusedWithin(card(15)), isTrue);
  });

  testWidgets('TV episode shelf clamps at both ends', (tester) async {
    await launchBrowse(tester);
    navigation(tester, 'Search').requestFocus();
    await tester.pumpAndSettle();
    await focusPoster(tester, result(0));
    await key(tester, LogicalKeyboardKey.select);
    final first = find.byKey(const ValueKey('episode-1'));
    await tester.ensureVisible(first);
    Focus.of(
      tester.element(
        find.descendant(of: first, matching: find.byType(CoverImage)),
      ),
    ).requestFocus();
    await tester.pumpAndSettle();
    await key(tester, LogicalKeyboardKey.arrowLeft);
    expect(focusedWithin(first), isTrue);
    for (var i = 2; i <= fixture.anime.episodes; i++) {
      await key(tester, LogicalKeyboardKey.arrowRight);
      expect(focusedWithin(find.byKey(ValueKey('episode-$i'))), isTrue);
    }
    await key(tester, LogicalKeyboardKey.arrowRight);
    expect(
      focusedWithin(find.byKey(ValueKey('episode-${fixture.anime.episodes}'))),
      isTrue,
    );
    await key(tester, LogicalKeyboardKey.arrowDown);
    expect(focusedWithin(find.text('All episodes')), isTrue);
  });

  testWidgets('TV held arrows keep the same order while shelves animate', (
    tester,
  ) async {
    await launchShelves(tester);
    tester.platformDispatcher.accessibilityFeaturesTestValue =
        FakeAccessibilityFeatures(disableAnimations: false);
    await tester.pumpAndSettle();
    final first = find.byKey(const ValueKey('First row-al:1'));
    await focusPoster(tester, first);
    await tester.sendKeyDownEvent(LogicalKeyboardKey.arrowRight);
    await tester.pump(const Duration(milliseconds: 80));
    for (var i = 0; i < 15; i++) {
      await tester.sendKeyRepeatEvent(LogicalKeyboardKey.arrowRight);
      await tester.pump(const Duration(milliseconds: 80));
    }
    await tester.sendKeyUpEvent(LogicalKeyboardKey.arrowRight);
    await tester.pumpAndSettle();
    final last = find.byKey(const ValueKey('First row-al:17'));
    expect(
      focusedWithin(last),
      isTrue,
      reason:
          'Focused ${FocusManager.instance.primaryFocus?.context?.findAncestorWidgetOfExactType<PosterCard>()?.anime.key}',
    );
    expect(tester.getRect(last).left, greaterThanOrEqualTo(0));
    expect(tester.getRect(last).right, lessThanOrEqualTo(1280));
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('TV discovery refresh keeps a moved title focused and visible', (
    tester,
  ) async {
    final services = await launchBrowse(tester);
    final card = find.byKey(const ValueKey('Trending-al:6'));
    await focusPoster(tester, card);
    await key(tester, LogicalKeyboardKey.select);
    expect(find.byType(ShowPage), findsOneWidget);
    (services.catalog as BrowseCatalog).homeItems = stories.reversed.toList();
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(focusedWithin(card), isTrue);
    expect(tester.getRect(card).left, greaterThanOrEqualTo(0));
    expect(tester.getRect(card).right, lessThanOrEqualTo(1280));
  });
}
