import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/desktop/desktop_core.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/ui/home_page.dart';
import 'package:otakase/ui/more_page.dart';
import 'package:otakase/ui/settings.dart';

import 'widget_test.dart' as fixture;

void desktop() {
  debugDesktopOverride = true;
  addTearDown(() => debugDesktopOverride = null);
  final m = TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
  m.setMockMethodCallHandler(
    const MethodChannel('window_manager'),
    (call) async => call.method == 'isMaximized' ? false : null,
  );
  addTearDown(
    () =>
        m.setMockMethodCallHandler(const MethodChannel('window_manager'), null),
  );
}

int root(WidgetTester tester) => tester
    .widget<HomePage>(find.byType(HomePage, skipOffstage: false))
    .services
    .root;

void main() {
  testWidgets('desktop tabs open on click, not on hover or focus', (
    tester,
  ) async {
    desktop();
    await fixture.launch(tester, tv: true);
    final search = find.byKey(const Key('nav-Search'));
    final mouse = await tester.createGesture(kind: PointerDeviceKind.mouse);
    await mouse.addPointer(location: Offset.zero);
    addTearDown(mouse.removePointer);
    await mouse.moveTo(tester.getCenter(search));
    await tester.pumpAndSettle();
    expect(root(tester), 0);
    tester.widget<FocusBox>(search).focusNode!.requestFocus();
    await tester.pumpAndSettle();
    expect(root(tester), 0);
    await tester.tap(search);
    await tester.pumpAndSettle();
    expect(root(tester), 1);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('TV tabs still open as the remote moves over them', (
    tester,
  ) async {
    await fixture.launch(tester, tv: true);
    tester
        .widget<FocusBox>(find.byKey(const Key('nav-Search')))
        .focusNode!
        .requestFocus();
    await tester.pumpAndSettle();
    expect(root(tester), 1);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('Trakt sits in Accounts with AniList and MAL', (tester) async {
    await fixture.launch(tester);
    final services = tester.widget<HomePage>(find.byType(HomePage)).services;
    await tester.pumpWidget(
      MaterialApp(home: AccountsPage(services: services, isTV: false)),
    );
    await tester.pumpAndSettle();
    expect(find.text('AniList'), findsOneWidget);
    expect(find.text('MAL'), findsOneWidget);
    expect(find.text('Trakt'), findsOneWidget);
    expect(find.text('Sign in to Trakt'), findsOneWidget);
    await tester.pumpWidget(
      MaterialApp(
        home: SettingsPage(
          services: services,
          isTV: false,
          section: 'Subtitles & languages',
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('Trakt'), findsNothing);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('settings read in plain words', (tester) async {
    await fixture.launch(tester);
    final services = tester.widget<HomePage>(find.byType(HomePage)).services;
    Future<void> open(String section) async {
      await tester.pumpWidget(
        MaterialApp(
          home: SettingsPage(services: services, isTV: false, section: section),
        ),
      );
      await tester.pumpAndSettle();
    }

    await open('Display');
    expect(find.text('This profile shows'), findsOneWidget);
    expect(find.text('Anime only'), findsOneWidget);

    await open('Subtitles & languages');
    // Movie languages sit with the anime ones once movies are on, and
    // movie sources have no switches.
    expect(find.text('Movie & show languages'), findsNothing);
    await services.store.set('Content', 'both');
    await tester.pumpAndSettle();
    expect(find.text('Movie & show languages'), findsOneWidget);
    expect(find.textContaining('Movie source'), findsNothing);
    expect(find.text('vidsrc'), findsNothing);
    await services.store.set('Content', 'anime');
    await tester.pumpAndSettle();
    expect(find.text('White'), findsOneWidget);
    expect(find.text('None'), findsOneWidget);
    expect(find.text('Original styling'), findsOneWidget);
    expect(find.text('Subtitled'), findsOneWidget);
    expect(find.text('#FFFFFF'), findsNothing);

    await open('Playback');
    expect(find.text('Best available'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
  });
}
