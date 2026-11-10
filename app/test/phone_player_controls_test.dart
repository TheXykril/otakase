import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/ui/phone_player.dart';

// Opt-in render of the phone player controls; PNGs stay outside Git.
const output = String.fromEnvironment('PHONE_SCREENSHOTS');

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
    if (output.isNotEmpty) await fonts();
  });
  setUp(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(
          const MethodChannel('otakase/device'),
          (_) async => null,
        );
  });

  Widget controls(
    List<String> calls, {
    bool isTV = false,
    bool canNext = true,
    bool skipTimes = true,
  }) => MaterialApp(
    home: Scaffold(
      backgroundColor: const Color(0xff2a3448),
      body: RepaintBoundary(
        key: const Key('player-render'),
        child: DecoratedBox(
          decoration: const BoxDecoration(
            gradient: LinearGradient(
              begin: Alignment.topLeft,
              end: Alignment.bottomRight,
              colors: [Color(0xff2a3448), Color(0xff4a3b3a), Color(0xff1b1a22)],
            ),
          ),
          child: PhonePlayerControls(
            isTV: isTV,
            title: 'Frieren',
            subtitle: 'Episode 8 · sub',
            position: const Duration(minutes: 8, seconds: 12),
            duration: const Duration(minutes: 23, seconds: 50),
            spans: const [SkipSpan('op', 60, 150), SkipSpan('ed', 1300, 1390)],
            playing: true,
            buffering: false,
            onBack: () => calls.add('back'),
            onPlayPause: () => calls.add('play'),
            onSeekBy: (s) => calls.add('seek $s'),
            onSeekStart: (_) {},
            onSeekChanged: (_) {},
            onSeekEnd: (_) => calls.add('scrub'),
            topActions: [
              IconButton(
                tooltip: 'Cast',
                onPressed: () => calls.add('cast'),
                icon: const Icon(LucideIcons.cast),
              ),
            ],
            actions: [
              PlayerButton(
                icon: LucideIcons.skipForward,
                label: 'Next',
                onPressed: canNext ? () => calls.add('next') : null,
              ),
              PlayerButton(
                icon: LucideIcons.captions,
                label: 'Audio & subs',
                onPressed: () => calls.add('tracks'),
              ),
              PlayerButton(
                icon: LucideIcons.gauge,
                label: 'Quality',
                onPressed: () => calls.add('quality'),
              ),
              if (skipTimes)
                PlayerButton(
                  icon: LucideIcons.chevronsRight,
                  label: 'Skip times',
                  onPressed: () => calls.add('skip'),
                ),
            ],
          ),
        ),
      ),
    ),
  );

  void phone(WidgetTester tester) {
    tester.view.physicalSize = const Size(844, 390);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
  }

  testWidgets('phone player shows times and plain icon buttons', (
    tester,
  ) async {
    phone(tester);
    final calls = <String>[];
    await tester.pumpWidget(controls(calls));
    expect(find.text('Frieren'), findsOneWidget);
    expect(find.text('EPISODE 8 · SUB'), findsOneWidget);
    expect(find.text('08:12 / 23:50'), findsOneWidget);
    expect(find.text('−15:38'), findsOneWidget);
    // Icons only on a phone: names live in tooltips, no text buttons.
    expect(find.text('Quality'), findsNothing);
    for (final name in ['Next', 'Audio & subs', 'Quality', 'Skip times']) {
      await tester.tap(find.bySemanticsLabel(name));
    }
    await tester.tap(find.byTooltip('Rewind 10 seconds'));
    await tester.tap(find.byTooltip('Pause'));
    await tester.tap(find.byTooltip('Forward 10 seconds'));
    await tester.tap(find.byTooltip('Cast'));
    await tester.tap(find.byTooltip('Back'));
    expect(calls, [
      'next',
      'tracks',
      'quality',
      'skip',
      'seek -10',
      'play',
      'seek 10',
      'cast',
      'back',
    ]);
    expect(tester.takeException(), isNull);
    if (output.isNotEmpty) {
      await tester.pumpAndSettle();
      await expectLater(
        find.byKey(const Key('player-render')),
        matchesGoldenFile(Uri.file('$output/player-phone.png')),
      );
    }
  });

  testWidgets('holding an icon shows its name', (tester) async {
    phone(tester);
    await tester.pumpWidget(controls([]));
    await tester.longPress(find.bySemanticsLabel('Quality'));
    await tester.pumpAndSettle();
    expect(find.text('Quality'), findsOneWidget);
  });

  testWidgets('missing next episode dims the button and does nothing', (
    tester,
  ) async {
    phone(tester);
    final calls = <String>[];
    await tester.pumpWidget(controls(calls, canNext: false, skipTimes: false));
    await tester.tap(find.bySemanticsLabel('Next'), warnIfMissed: false);
    expect(calls, isEmpty);
    expect(find.bySemanticsLabel('Skip times'), findsNothing);
  });

  testWidgets('TV writes the focused button name under it', (tester) async {
    tester.view.physicalSize = const Size(1600, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(controls([], isTV: true));
    expect(find.text('Quality'), findsNothing);
    final icon = find.descendant(
      of: find.byType(PlayerIconButton).at(2),
      matching: find.byType(Icon),
    );
    Focus.of(tester.element(icon)).requestFocus();
    await tester.pumpAndSettle();
    expect(find.text('Quality'), findsOneWidget);
    expect(tester.takeException(), isNull);
    if (output.isNotEmpty) {
      await expectLater(
        find.byKey(const Key('player-render')),
        matchesGoldenFile(Uri.file('$output/player-tv.png')),
      );
    }
  });
  testWidgets('phone player shading covers a camera cutout', (tester) async {
    phone(tester);
    // Camera cutout on the left edge of a landscape phone.
    tester.view.padding = const FakeViewPadding(left: 50);
    addTearDown(tester.view.resetPadding);
    await tester.pumpWidget(controls([]));
    final shades = find.descendant(
      of: find.byType(PhonePlayerControls),
      matching: find.byWidgetPredicate(
        (w) =>
            w is Container &&
            w.decoration is BoxDecoration &&
            (w.decoration! as BoxDecoration).gradient != null,
      ),
    );
    expect(shades, findsNWidgets(2));
    for (final shade in shades.evaluate()) {
      final box = shade.renderObject! as RenderBox;
      expect(box.localToGlobal(Offset.zero).dx, 0);
      expect(box.size.width, 844);
    }
    // Buttons and text stay beside the cutout.
    expect(tester.getTopLeft(find.byTooltip('Back')).dx, greaterThan(50));
    expect(tester.getTopLeft(find.text('08:12 / 23:50')).dx, greaterThan(50));
  });
}
