import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/main.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/ui/home_page.dart';

import 'auth_test.dart' show MemoryTokens;
import 'widget_test.dart' as fixture;
import 'phone_screens_test.dart' as screen;

class PreferenceCatalog extends fixture.TestCatalog {
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
  }) async => CatalogPage([
    fixture.anime,
    if (adult) const Anime(id: 99, title: 'Restricted story', isAdult: true),
  ]);
}

void main() {
  testWidgets(
    'account category has seven real groups and survives navigation',
    (tester) async {
      await fixture.launch(tester);
      final home = tester.widget<HomePage>(find.byType(HomePage));
      await home.services.store.set('AniListUsername', 'viewer');
      await tester.pumpAndSettle();
      expect(find.text('AniList · viewer · Public, read only'), findsOneWidget);
      await tester.tap(find.text('Watching'));
      await tester.pumpAndSettle();
      for (final label in [
        'All titles',
        'Watching',
        'Planning',
        'Completed',
        'Paused',
        'Dropped',
        'Repeating',
      ]) {
        expect(find.text(label), findsWidgets);
      }
      await tester.tap(find.text('Planning'));
      await tester.pumpAndSettle();
      expect(
        find.byKey(const ValueKey('AniList-PLANNING-al:2')),
        findsOneWidget,
      );
      expect(home.services.store.text('ListCategory.AniList'), 'PLANNING');
      await tester.tap(find.byKey(const Key('nav-More')));
      await tester.pumpAndSettle();
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(find.text('Planning'), findsOneWidget);
      await tester.tap(find.text('Planning'));
      await tester.pumpAndSettle();
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(find.text('Planning'), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'AniList logout removes already-loaded 18+ search and Continue entries',
    (tester) async {
      await fixture.launch(tester);
      await tester.pumpWidget(const SizedBox());
      final base = await fixture.services();
      final auth = AuthService(storage: MemoryTokens());
      auth.accounts['AniList'] = Account(
        service: 'AniList',
        token: 'token',
        expires: DateTime.now().add(const Duration(days: 30)),
        username: 'viewer',
        userId: 7,
        displayAdultContent: true,
      );
      final services = AppServices(
        catalog: PreferenceCatalog(),
        store: base.store,
        core: base.core,
        auth: auth,
      );
      await services.store.progress(
        const Anime(id: 99, title: 'Restricted story', isAdult: true),
        1,
        20,
        100,
      );
      await tester.pumpWidget(OtakaseApp(services: services, isTV: false));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('nav-Search')));
      await tester.pumpAndSettle();
      expect(find.text('Restricted story'), findsOneWidget);
      await auth.logout('AniList');
      await tester.pumpAndSettle();
      expect(find.text('Restricted story'), findsNothing);
      expect(services.store.continuing, isEmpty);
      expect(services.store.adultAllowed, isFalse);
      await tester.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets(
    'TV remote reaches Schedule through More and returns to its row',
    (tester) async {
      await fixture.launch(tester, tv: true);
      tester
          .widget<FocusBox>(find.byKey(const Key('nav-Home')))
          .focusNode!
          .requestFocus();
      await tester.pump();
      for (var i = 0; i < 2; i++) {
        await tester.sendKeyEvent(LogicalKeyboardKey.arrowRight);
        await tester.pump();
      }
      await tester.sendKeyEvent(LogicalKeyboardKey.select);
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('more-Accounts')), findsOneWidget);
      for (var i = 0; i < 3; i++) {
        await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
        await tester.pumpAndSettle();
      }
      await tester.sendKeyEvent(LogicalKeyboardKey.select);
      await tester.pumpAndSettle();
      expect(
        find.text('Airing times, not source availability'),
        findsOneWidget,
      );
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('more-Schedule')), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'large phone text reflows the device header and personal resume',
    (tester) async {
      await screen.fonts();
      await fixture.launch(tester);
      tester.platformDispatcher.textScaleFactorTestValue = 1.6;
      addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);
      tester.view.physicalSize = const Size(360, 800);
      await tester.pumpAndSettle();
      final home = tester.widget<HomePage>(find.byType(HomePage));
      await home.services.store.progress(
        const Anime(
          id: 3,
          title: 'A long English title for a journey across the stars',
          nativeTitle: '星を数える物語',
        ),
        5,
        120,
        1440,
      );
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('resume-card')), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.tap(find.byKey(const Key('nav-More')));
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    },
  );
}
