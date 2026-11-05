import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/main.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/ui/episode_controls.dart';
import 'package:otakase/ui/home_page.dart';
import 'package:otakase/ui/settings.dart';
import 'package:otakase/ui/show_page.dart';

import 'auth_test.dart' show MemoryTokens;
import 'show_details_test.dart' show DetailCatalog;
import 'tv_navigation_audit_test.dart' show focusedWithin;
import 'widget_test.dart' as fixture;
import 'uniform_ui_test.dart' show desktop;

const story = Anime(
  id: 12,
  title: 'Story',
  episodes: 1100,
  duration: 24,
  episodeArt: [
    {
      'episode': 3,
      'title': 'A full episode title that keeps all of its words',
      'thumbnail': 'https://fixture.invalid/3.jpg',
      'airdate': '2026-11-01',
      'description': 'An available episode summary.',
    },
    {
      'episode': 4,
      'title': 'The spoiler title',
      'thumbnail': 'https://fixture.invalid/4.jpg',
    },
  ],
);
Future<AppServices> openEpisodes(
  WidgetTester tester, {
  bool tv = true,
  bool computer = false,
  double scale = 1,
}) async {
  await fixture.launch(tester, tv: tv);
  if (computer) desktop();
  await tester.pumpWidget(const SizedBox());
  if (computer) tester.view.physicalSize = const Size(1600, 900);
  tester.platformDispatcher.textScaleFactorTestValue = scale;
  addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);
  final base = await fixture.services();
  final app = AppServices(
    catalog: DetailCatalog(),
    store: base.store,
    core: base.core,
    auth: AuthService(storage: MemoryTokens()),
  );
  await tester.pumpWidget(
    ImageSources(
      provider: (_) => const AssetImage('assets/otakase.png'),
      child: OtakaseApp(services: app, isTV: tv),
    ),
  );
  await tester.pumpAndSettle();
  Navigator.of(tester.element(find.byType(HomePage))).push(
    MaterialPageRoute<void>(
      builder: (_) => ShowPage(services: app, anime: story, isTV: tv),
    ),
  );
  await tester.pumpAndSettle();
  addTearDown(() async {
    await tester.pumpWidget(const SizedBox());
    await app.close();
  });
  return app;
}

Future<void> openJump(WidgetTester tester) async {
  final button = find.byKey(const Key('episode-jump'));
  await Scrollable.ensureVisible(tester.element(button), alignment: .5);
  await tester.pumpAndSettle();
  await tester.tap(button);
  await tester.pumpAndSettle();
}

Future<void> showCard(WidgetTester tester, int ep) async {
  await tester.scrollUntilVisible(
    find.byKey(ValueKey('episode-$ep')),
    250,
    scrollable: find
        .descendant(
          of: find.byType(ShowPage),
          matching: find.byType(Scrollable),
        )
        .first,
  );
  await tester.pumpAndSettle();
}

void main() {
  for (final scale in [1.0, 1.8]) {
    testWidgets(
      'remote numbers select lazy episode 1076 and restore focus at $scale text',
      (tester) async {
        final app = await openEpisodes(tester, scale: scale);
        expect(find.byKey(const Key('episode-1076')), findsNothing);
        await openJump(tester);
        for (final key in [
          LogicalKeyboardKey.digit1,
          LogicalKeyboardKey.numpad0,
          LogicalKeyboardKey.digit7,
          LogicalKeyboardKey.digit6,
        ]) {
          expect(await tester.sendKeyDownEvent(key), isTrue);
          expect(await tester.sendKeyUpEvent(key), isTrue);
        }
        await tester.pumpAndSettle();
        expect(
          tester
              .widget<TextField>(find.byKey(const Key('episode-jump-input')))
              .controller!
              .text,
          '1076',
        );
        final confirm = find.byKey(const Key('episode-jump-confirm'));
        expect(focusedWithin(confirm), isTrue);
        await tester.sendKeyEvent(LogicalKeyboardKey.select);
        await tester.pumpAndSettle();
        final target = find.byKey(const Key('episode-1076'));
        expect(focusedWithin(target), isTrue);
        expect(
          app.store.entry(story),
          isNull,
          reason: 'Jump neither plays nor marks watched',
        );
        expect(tester.getTopLeft(target).dy, greaterThan(130));
        await tester.sendKeyEvent(LogicalKeyboardKey.arrowLeft);
        await tester.pumpAndSettle();
        expect(focusedWithin(find.byKey(const Key('episode-1075'))), isTrue);
        await tester.sendKeyEvent(LogicalKeyboardKey.info);
        await tester.pumpAndSettle();
        expect(find.byType(EpisodeInfo), findsOneWidget);
        await tester.binding.handlePopRoute();
        await tester.pumpAndSettle();
        expect(focusedWithin(find.byKey(const Key('episode-1075'))), isTrue);
        await openJump(tester);
        await tester.ensureVisible(find.text('Current · 1'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Current · 1'));
        await tester.pumpAndSettle();
        expect(focusedWithin(find.byKey(const Key('episode-1'))), isTrue);
        expect(tester.takeException(), isNull);
      },
    );
  }
  testWidgets(
    'jump bounds, keypad, repeat suppression, cancellation and event scope',
    (tester) async {
      final app = await openEpisodes(tester);
      await openJump(tester);
      await tester.tap(find.byKey(const Key('episode-jump-0')));
      await tester.pumpAndSettle();
      expect(
        tester
            .widget<AppButton>(find.byKey(const Key('episode-jump-confirm')))
            .onPressed,
        isNull,
      );
      await tester.tap(find.byKey(const Key('episode-jump-Clear')));
      await tester.sendKeyDownEvent(LogicalKeyboardKey.digit1);
      await tester.sendKeyRepeatEvent(LogicalKeyboardKey.digit1);
      await tester.sendKeyUpEvent(LogicalKeyboardKey.digit1);
      for (var i = 0; i < 3; i++) {
        await tester.sendKeyEvent(LogicalKeyboardKey.digit9);
      }
      await tester.pumpAndSettle();
      expect(
        tester
            .widget<TextField>(find.byKey(const Key('episode-jump-input')))
            .controller!
            .text,
        '1999',
      );
      expect(
        tester
            .widget<AppButton>(find.byKey(const Key('episode-jump-confirm')))
            .onPressed,
        isNull,
      );
      await tester.tap(find.byKey(const Key('episode-jump-Delete')));
      await tester.pumpAndSettle();
      expect(
        tester
            .widget<TextField>(find.byKey(const Key('episode-jump-input')))
            .controller!
            .text,
        '199',
      );
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      expect(focusedWithin(find.byKey(const Key('episode-jump'))), isTrue);
      expect(await tester.sendKeyEvent(LogicalKeyboardKey.digit5), isFalse);
      expect(app.store.entry(story), isNull);
    },
  );
  testWidgets('desktop jump switches the 50-episode block without playing', (
    tester,
  ) async {
    final app = await openEpisodes(tester, tv: false, computer: true);
    await openJump(tester);
    for (final key in [
      LogicalKeyboardKey.digit1,
      LogicalKeyboardKey.digit0,
      LogicalKeyboardKey.digit7,
      LogicalKeyboardKey.digit6,
    ]) {
      await tester.sendKeyEvent(key);
    }
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('episode-jump-confirm')));
    await tester.pumpAndSettle();
    expect(focusedWithin(find.byKey(const Key('episode-1076'))), isTrue);
    expect(find.byKey(const Key('episode-1')), findsNothing);
    expect(app.store.entry(story), isNull);
    expect(tester.takeException(), isNull);
  });
  for (final tv in [true, false]) {
    testWidgets(
      'spoilers default off, watched art visible, reveal unwatched on ${tv ? 'TV' : 'phone'}',
      (tester) async {
        final app = await openEpisodes(tester, tv: tv);
        expect(app.store.flag('HideEpisodeSpoilers'), isFalse);
        await app.store.set('HideEpisodeSpoilers', true);
        await app.store.markWatched(story, 3, true);
        await tester.pumpAndSettle();
        await showCard(tester, 3);
        expect(
          tester
              .widget<SpoilerArt>(
                find
                    .descendant(
                      of: find.byKey(const Key('episode-3')),
                      matching: find.byType(SpoilerArt),
                    )
                    .first,
              )
              .hidden,
          isFalse,
        );
        await showCard(tester, 4);
        final card = find.byKey(const Key('episode-4'));
        expect(find.textContaining('The spoiler title'), findsNothing);
        expect(
          tester
              .widget<SpoilerArt>(
                find
                    .descendant(of: card, matching: find.byType(SpoilerArt))
                    .first,
              )
              .hidden,
          isTrue,
        );
        expect(
          find.descendant(of: card, matching: find.byIcon(LucideIcons.eyeOff)),
          findsOneWidget,
        );
        if (tv) {
          Focus.of(
            tester.element(
              find.descendant(of: card, matching: find.byType(ClipRRect)).first,
            ),
          ).requestFocus();
          await tester.pumpAndSettle();
          expect(
            tester
                .widget<Text>(find.byKey(const Key('focused-episode-title')))
                .data,
            'Episode 4',
          );
          await tester.sendKeyEvent(LogicalKeyboardKey.info);
        } else {
          await tester.longPress(card);
        }
        await tester.pumpAndSettle();
        expect(find.byType(EpisodeInfo), findsOneWidget);
        expect(find.textContaining('The spoiler title'), findsNothing);
        await tester.tap(find.text('Reveal this episode'));
        await tester.pumpAndSettle();
        expect(
          find.descendant(
            of: find.byType(EpisodeInfo),
            matching: find.textContaining('The spoiler title'),
          ),
          findsOneWidget,
        );
        expect(app.store.entry(story)!.watched, [3]);
        await tester.binding.handlePopRoute();
        await tester.pumpAndSettle();
        expect(
          tester
              .widget<SpoilerArt>(
                find
                    .descendant(of: card, matching: find.byType(SpoilerArt))
                    .first,
              )
              .hidden,
          isFalse,
        );
        expect(tester.takeException(), isNull);
      },
    );
  }
  testWidgets(
    'episode information includes full title, airdate and per-episode resume',
    (tester) async {
      final app = await openEpisodes(tester);
      await app.store.progress(story, 3, 90, 1440);
      await app.store.progress(story, 4, 10, 1440);
      await showCard(tester, 3);
      final card = find.byKey(const Key('episode-3'));
      Focus.of(
        tester.element(
          find.descendant(of: card, matching: find.byType(ClipRRect)).first,
        ),
      ).requestFocus();
      await tester.pumpAndSettle();
      expect(
        tester
            .widget<Text>(find.byKey(const Key('focused-episode-title')))
            .data,
        '3. A full episode title that keeps all of its words',
      );
      await tester.sendKeyEvent(LogicalKeyboardKey.info);
      await tester.pumpAndSettle();
      expect(
        find.textContaining('2026-11-01 · 24 min · Resume at 1m 30s'),
        findsOneWidget,
      );
      expect(find.text('Resume episode 3'), findsOneWidget);
      expect(find.text('An available episode summary.'), findsOneWidget);
    },
  );
  testWidgets('real Playback spoiler setting defaults off and persists', (
    tester,
  ) async {
    final app = await openEpisodes(tester);
    openSettings(
      tester.element(find.byType(ShowPage)),
      app,
      true,
      section: 'Playback',
    );
    await tester.pumpAndSettle();
    expect(app.store.flag('HideEpisodeSpoilers'), isFalse);
    await tester.tap(find.text('Hide episode spoilers'));
    await tester.pumpAndSettle();
    expect(app.store.flag('HideEpisodeSpoilers'), isTrue);
    expect(
      app.store.preferences.getString('settings.v1'),
      contains('"HideEpisodeSpoilers":true'),
    );
    expect(tester.takeException(), isNull);
  });
  testWidgets(
    'desktop outline waits for keyboard, hides on mouse and retains focus',
    (tester) async {
      desktop();
      DesktopInput.keyboardMode.value = false;
      final focused = FocusNode();
      addTearDown(focused.dispose);
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: FocusBox(
              key: const Key('focus-test'),
              focusNode: focused,
              autofocus: true,
              onPressed: () {},
              child: const SizedBox(width: 150, height: 80),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      Color border() =>
          (tester
                      .widget<AnimatedContainer>(
                        find
                            .descendant(
                              of: find.byKey(const Key('focus-test')),
                              matching: find.byType(AnimatedContainer),
                            )
                            .first,
                      )
                      .decoration!
                  as BoxDecoration)
              .border!
              .top
              .color;
      expect(focused.hasFocus, isTrue);
      expect(border(), Colors.transparent);
      await tester.sendKeyEvent(LogicalKeyboardKey.arrowRight);
      await tester.pumpAndSettle();
      expect(border(), shu);
      final mouse = await tester.createGesture(kind: PointerDeviceKind.mouse);
      await mouse.addPointer(location: const Offset(300, 300));
      await mouse.moveTo(const Offset(310, 310));
      await tester.pumpAndSettle();
      expect(border(), Colors.transparent);
      expect(focused.hasFocus, isTrue);
      await tester.sendKeyEvent(LogicalKeyboardKey.tab);
      await tester.pumpAndSettle();
      expect(border(), shu);
      tester.binding.handlePointerEvent(
        const PointerScrollEvent(
          position: Offset(300, 300),
          scrollDelta: Offset(0, 20),
        ),
      );
      await tester.pumpAndSettle();
      expect(border(), Colors.transparent);
      await mouse.removePointer();
      await tester.pumpWidget(const SizedBox());
    },
  );
}
