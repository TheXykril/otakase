import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/store.dart';
import 'package:otakase/main.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/ui/home_page.dart';

import 'widget_test.dart' as fixture;

class DiagnosticCore extends CoreClient {
  @override
  Future<Json> request(
    Json input, {
    void Function(int)? onHandle,
    RequestScope? scope,
    Duration timeout = const Duration(seconds: 40),
  }) async => {'providers': sourceNames};
}

Finder rings() => find.byWidgetPredicate(
  (w) =>
      w is AnimatedContainer &&
      w.decoration is BoxDecoration &&
      ((w.decoration as BoxDecoration).border as Border?)?.top.color == shu,
);
void main() {
  testWidgets(
    'three labelled roots, More settings, sources only in Diagnostics',
    (tester) async {
      await fixture.launch(tester);
      for (final name in ['Home', 'Search', 'More']) {
        expect(find.byKey(ValueKey('nav-$name')), findsOneWidget);
      }
      expect(find.byType(Drawer), findsNothing);
      expect(find.byKey(const Key('nav-Menu')), findsNothing);
      await tester.tap(find.byKey(const Key('nav-More')));
      await tester.pumpAndSettle();
      expect(find.text('18+ follows your AniList setting'), findsOneWidget);
      for (final source in sourceNames) {
        expect(find.text(source), findsNothing);
      }
      await tester.tap(find.byKey(const Key('more-Playback')));
      await tester.pumpAndSettle();
      expect(find.text('Ask before next episode'), findsOneWidget);
      expect(find.text('AdultContent'), findsNothing);
      expect(find.text('Movies'), findsNothing);
      await tester.pumpWidget(const SizedBox());
      final services = await fixture.services();
      await tester.pumpWidget(
        OtakaseApp(
          services: AppServices(
            catalog: services.catalog,
            store: services.store,
            core: DiagnosticCore(),
          ),
          isTV: false,
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('nav-More')));
      await tester.pumpAndSettle();
      await tester.scrollUntilVisible(
        find.byKey(const Key('more-Diagnostics')),
        200,
      );
      await tester.drag(find.byType(ListView).first, const Offset(0, -220));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('more-Diagnostics')));
      await tester.pumpAndSettle();
      for (var i = 0; i < sourceNames.length; i++) {
        expect(find.text('Source ${i + 1}'), findsOneWidget);
      }
      for (final source in sourceNames) {
        expect(find.textContaining(source), findsNothing);
      }
      await tester.pumpWidget(const SizedBox());
    },
  );
  testWidgets('touch clears focus rings and D-pad restores visible focus', (
    tester,
  ) async {
    FocusManager.instance.highlightStrategy = FocusHighlightStrategy.automatic;
    await fixture.launch(tester);
    await tester.tap(find.byKey(const Key('nav-Home')));
    await tester.pumpAndSettle();
    expect(rings(), findsNothing);
    tester
        .widget<FocusBox>(find.byKey(const Key('nav-Home')))
        .focusNode!
        .requestFocus();
    await tester.sendKeyEvent(LogicalKeyboardKey.tab);
    await tester.pumpAndSettle();
    expect(rings(), findsWidgets);
    await tester.tap(find.byKey(const Key('nav-Search')));
    await tester.pumpAndSettle();
    expect(rings(), findsNothing);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets('root content reserves floating bar and real safe bottom', (
    tester,
  ) async {
    await fixture.launch(tester);
    tester.view.padding = const FakeViewPadding(bottom: 34);
    addTearDown(tester.view.resetPadding);
    await tester.pumpAndSettle();
    for (final tab in ['Home', 'Search', 'More']) {
      await tester.tap(find.byKey(ValueKey('nav-$tab')));
      await tester.pumpAndSettle();
      final list = tester.widget<ListView>(find.byType(ListView).first);
      expect((list.padding! as EdgeInsets).bottom, 72 + 7 + 34 + 24);
    }
    await tester.scrollUntilVisible(
      find.byKey(const Key('more-Diagnostics')),
      200,
    );
    await tester.drag(find.byType(ListView).first, const Offset(0, -220));
    await tester.pumpAndSettle();
    expect(
      tester.getRect(find.byKey(const Key('more-Diagnostics'))).bottom,
      lessThan(tester.getTopLeft(find.byKey(const Key('phone-bottom-bar'))).dy),
    );
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets(
    'Kage phone uses readable text, exact tokens and opaque navigation',
    (tester) async {
      await fixture.launch(tester);
      expect(muted, const Color(0xffb9b0a4));
      expect(panel, const Color(0xff191614));
      expect(line, const Color(0xff3c332d));
      expect(find.byType(BackdropFilter), findsNothing);
      final home = tester.widget<Text>(
        find.descendant(
          of: find.byKey(const Key('nav-Home')),
          matching: find.text('Home'),
        ),
      );
      expect(home.style!.fontSize, 15);
      expect(home.style!.fontFamily, 'Inter');
      expect(find.text('Trending'), findsOneWidget);
      expect(find.text('Find your next story'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
    },
  );
  testWidgets('personal resume uses real position and omits missing duration', (
    tester,
  ) async {
    await fixture.launch(tester);
    final home = tester.widget<HomePage>(find.byType(HomePage));
    await home.services.store.progress(fixture.anime, 5, 720, 1440);
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('resume-card')), findsOneWidget);
    expect(find.text('12 min left'), findsOneWidget);
    expect(find.text('EP 05'), findsOneWidget);
    final bar = tester.widget<LinearProgressIndicator>(
      find.byType(LinearProgressIndicator).first,
    );
    expect(bar.value, .5);
    expect(bar.minHeight, 3);
    await home.services.store.progress(fixture.anime, 5, 720, 0);
    await tester.pumpAndSettle();
    expect(find.text('12 min left'), findsNothing);
    expect(find.byType(LinearProgressIndicator), findsNothing);
    expect(find.text('Resume'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets(
    'TV separates selected navigation and focus, uses 20sp settings',
    (tester) async {
      await fixture.launch(tester, tv: true);
      final home = tester.widget<Text>(
        find.descendant(
          of: find.byKey(const Key('nav-Home')),
          matching: find.text('Home'),
        ),
      );
      expect(home.style!.fontSize, greaterThanOrEqualTo(20));
      expect(home.style!.color, fg);
      await tester.tap(find.byKey(const Key('nav-More')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('more-Subtitles & languages')));
      await tester.pumpAndSettle();
      final label = tester.widget<Text>(find.text('Subtitle language'));
      expect(label.style!.fontSize, greaterThanOrEqualTo(20));
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    },
  );
  test(
    'extraLarge art and supplied native title survive local persistence',
    () {
      final a = Anime.fromJson({
        'id': 1,
        'title': {'romaji': 'Story', 'native': '物語'},
        'coverImage': {'large': 'thumb.jpg', 'extraLarge': 'cover.jpg'},
      });
      expect(Anime.fromJson(a.toJson()).coverLarge, 'cover.jpg');
      expect(Anime.fromJson(a.toJson()).nativeTitle, '物語');
    },
  );
}
