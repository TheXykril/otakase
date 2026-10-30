import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/ui/components.dart';

import 'home_discovery_test.dart' show HomeCatalog, launchHome;
import 'tv_focus_test.dart' show key, navigation;
import 'widget_test.dart' as fixture;

void main() {
  for (final personal in [false, true]) {
    testWidgets(
      'TV ${personal ? 'resume' : 'discovery'} art covers header and hero',
      (tester) async {
        final services = await launchHome(
          tester,
          tv: true,
          catalog: HomeCatalog(),
        );
        if (personal) {
          await services.store.progress(fixture.anime, 5, 720, 1440);
          await tester.pumpAndSettle();
        }
        final region = find.byKey(const Key('tv-home-hero'));
        expect(region, findsOneWidget);
        final art = find.descendant(of: region, matching: find.byType(HeroArt));
        expect(art, findsOneWidget);
        final rect = tester.getRect(art);
        expect(rect.top, 0);
        expect(rect.left, 0);
        expect(rect.right, 1280);
        expect(
          rect.contains(tester.getCenter(find.byKey(const Key('nav-Home')))),
          isTrue,
        );
        final hero = find.byKey(
          Key(personal ? 'resume-card' : 'discovery-featured'),
        );
        expect(rect.bottom, closeTo(tester.getRect(hero).bottom, .01));
        expect(find.byKey(const Key('home-top-vignette')), findsOneWidget);
        navigation(tester, 'Home').requestFocus();
        await tester.pumpAndSettle();
        await key(tester, LogicalKeyboardKey.arrowRight);
        expect(navigation(tester, 'Search').hasPrimaryFocus, isTrue);
        await key(tester, LogicalKeyboardKey.arrowDown);
        final field = find.byKey(const Key('catalog-search'));
        expect(
          tester.getRect(field).top,
          greaterThan(
            tester.getRect(find.byKey(const Key('nav-Search'))).bottom,
          ),
        );
        expect(tester.testTextInput.isVisible, isFalse);
      },
    );
  }
  testWidgets('TV header stays reachable above scrolling art and large text', (
    tester,
  ) async {
    await launchHome(tester, tv: true, catalog: HomeCatalog());
    tester.platformDispatcher.textScaleFactorTestValue = 1.8;
    addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);
    await tester.pumpAndSettle();
    final nav = find.byKey(const Key('nav-Home'));
    final before = tester.getRect(nav);
    await tester.drag(
      find.byKey(const PageStorageKey('home-scroll')),
      const Offset(0, -450),
    );
    await tester.pumpAndSettle();
    expect(tester.getRect(nav), before);
    await tester.tap(find.byKey(const Key('nav-Search')));
    await tester.pumpAndSettle();
    expect(
      tester.getRect(find.byKey(const Key('catalog-search'))).top,
      greaterThan(tester.getRect(find.byKey(const Key('nav-Search'))).bottom),
    );
    expect(tester.takeException(), isNull);
  });
  testWidgets('phone keeps artwork within its card', (tester) async {
    await launchHome(tester, catalog: HomeCatalog());
    expect(find.byKey(const Key('tv-home-hero')), findsNothing);
    final hero = find.byKey(const Key('discovery-featured'));
    final art = find.descendant(of: hero, matching: find.byType(HeroArt));
    expect(tester.getRect(art).top, greaterThan(0));
    expect(tester.getRect(art).width, 372);
  });
}
