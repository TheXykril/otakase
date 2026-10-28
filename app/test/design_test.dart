import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/store.dart';
import 'package:otakase/main.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/components.dart';

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
    'phone has five nav actions and one drawer settings entry; sources only in Diagnostics',
    (tester) async {
      await fixture.launch(tester);
      expect(find.byKey(const Key('nav-Settings')), findsNothing);
      for (final name in ['Menu', 'Home', 'Search', 'MyList', 'Schedule']) {
        expect(find.byKey(ValueKey('nav-$name')), findsOneWidget);
      }
      await tester.tap(find.byKey(const Key('nav-Menu')));
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('drawer-settings')), findsOneWidget);
      for (final source in sourceNames) {
        expect(find.text(source), findsNothing);
      }
      expect(find.text('Sources'), findsNothing);
      await tester.tap(find.byKey(const Key('drawer-settings')));
      await tester.pumpAndSettle();
      expect(find.text('SOURCE'), findsNothing);
      expect(find.text('PROVIDER ORDER'), findsNothing);
      for (final source in sourceNames) {
        expect(find.text(source), findsNothing);
      }
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
      await tester.tap(find.byKey(const Key('nav-Menu')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('drawer-settings')));
      await tester.pumpAndSettle();
      await tester.scrollUntilVisible(find.text('Diagnostics'), 500);
      await tester.pumpAndSettle();
      await tester.tap(find.text('Diagnostics'));
      await tester.pumpAndSettle();
      for (final source in sourceNames) {
        expect(find.text(source), findsOneWidget);
      }
      await tester.pumpWidget(const SizedBox());
    },
  );
  testWidgets('touch clears focus rings; D-pad restores them', (tester) async {
    FocusManager.instance.highlightStrategy = FocusHighlightStrategy.automatic;
    await fixture.launch(tester);
    await tester.tap(find.byKey(const ValueKey('nav-Home')));
    await tester.pumpAndSettle();
    expect(FocusManager.instance.highlightMode, FocusHighlightMode.touch);
    expect(rings(), findsNothing);
    final home = tester.widget<FocusBox>(
      find.byKey(const ValueKey('nav-Home')),
    );
    home.focusNode!.requestFocus();
    await tester.sendKeyEvent(LogicalKeyboardKey.tab);
    await tester.pumpAndSettle();
    expect(FocusManager.instance.highlightMode, FocusHighlightMode.traditional);
    expect(rings(), findsWidgets);
    await tester.tap(find.byKey(const ValueKey('nav-Search')));
    await tester.pumpAndSettle();
    expect(FocusManager.instance.highlightMode, FocusHighlightMode.touch);
    expect(rings(), findsNothing);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets(
    'all phone pages reserve bar height plus safe area and the last home title clears it',
    (tester) async {
      await fixture.launch(tester);
      tester.view.padding = const FakeViewPadding(bottom: 34);
      addTearDown(tester.view.resetPadding);
      await tester.pumpAndSettle();
      for (final tab in ['Home', 'Search', 'MyList', 'Schedule']) {
        await tester.tap(find.byKey(ValueKey('nav-$tab')));
        await tester.pumpAndSettle();
        final list = tester.widget<ListView>(find.byType(ListView).first);
        final padding = list.padding! as EdgeInsets;
        expect(
          padding.bottom,
          phoneBarHeight + phoneBarMargin + 34 + 24,
          reason: tab,
        );
      }
      await tester.tap(find.byKey(const ValueKey('nav-Home')));
      await tester.pumpAndSettle();
      await tester.drag(find.byType(ListView).first, const Offset(0, -2400));
      await tester.pumpAndSettle();
      final last = find.byKey(
        ValueKey('Popular this season-${fixture.anime.key}'),
      );
      expect(
        tester.getRect(last).bottom,
        lessThan(
          tester.getTopLeft(find.byKey(const Key('phone-bottom-bar'))).dy,
        ),
      );
      await tester.pumpWidget(const SizedBox());
    },
  );
  test('hero retains extraLarge artwork across local persistence', () {
    final a = Anime.fromJson({
      'id': 1,
      'title': {'romaji': 'Story'},
      'bannerImage': 'https://image.example/banner.jpg',
      'coverImage': {
        'large': 'https://image.example/thumb.jpg',
        'extraLarge': 'https://image.example/cover.jpg',
      },
    });
    expect(a.cover, endsWith('thumb.jpg'));
    expect(a.coverLarge, endsWith('cover.jpg'));
    expect(Anime.fromJson(a.toJson()).coverLarge, a.coverLarge);
  });
}
