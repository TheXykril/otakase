// Phone accessibility on the main screens, built from the website fixture:
// they must lay out at 1.3x system text, every touch target is at least
// 48 x 48, every tappable thing has a label for screen readers, and the
// decorative kanji and seal are left out of what screen readers say.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/main.dart';
import 'package:otakase/ui/phone_player.dart';

import 'goldens/screens_golden_test.dart' show Device, reach, screens, settle;
import 'site_fixture.dart';

// A kanji accent: one or two kanji standing alone on a line of a label.
final kanjiOnly = RegExp(r'(^|\n)[\u3040-\u30ff\u4e00-\u9fff]{1,2}(\n|$)');

Future<void> check(WidgetTester tester) async {
  expect(tester.takeException(), isNull);
  await expectLater(tester, meetsGuideline(androidTapTargetGuideline));
  await expectLater(tester, meetsGuideline(labeledTapTargetGuideline));
  // Kanji accents are decorative.
  expect(find.bySemanticsLabel(kanjiOnly), findsNothing);
}

void phoneView(WidgetTester tester, {bool landscape = false}) {
  setUpView(tester, landscape ? const Size(915, 412) : const Size(412, 915));
  if (!landscape) {
    tester.view.padding = const FakeViewPadding(top: 24, bottom: 24);
    addTearDown(tester.view.resetPadding);
  }
  tester.platformDispatcher.textScaleFactorTestValue = 1.3;
  addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);
}

void main() {
  readShows();
  final main = shows.first;
  setUpAll(loadArt);

  for (final screen in screens) {
    testWidgets('phone $screen is accessible at 1.3x text', (tester) async {
      phoneView(tester);
      final semantics = tester.ensureSemantics();
      final app = await services(main);
      await tester.pumpWidget(art(OtakaseApp(services: app, isTV: false)));
      await settle(tester);
      await reach(tester, app, main, Device.phone, screen);
      await settle(tester);
      await check(tester);
      semantics.dispose();
      await tester.pumpWidget(const SizedBox());
    });
  }

  testWidgets('phone player is accessible at 1.3x text', (tester) async {
    phoneView(tester, landscape: true);
    final semantics = tester.ensureSemantics();
    // A phone draws the player at its own size, without the TV canvas.
    await tester.pumpWidget(
      art(
        MaterialApp(
          home: Scaffold(
            backgroundColor: Colors.black,
            body: PhonePlayerControls(
              isTV: false,
              title: main.title,
              subtitle: 'Episode $resumeAt · sub',
              position: const Duration(minutes: 1, seconds: 12),
              duration: const Duration(minutes: 24),
              spans: const [SkipSpan('op', 40, 130)],
              playing: true,
              buffering: false,
              onBack: () {},
              onPlayPause: () {},
              onSeekBy: (_) {},
              onSeekStart: (_) {},
              onSeekChanged: (_) {},
              onSeekEnd: (_) {},
              topActions: const [],
              actions: const [],
            ),
          ),
        ),
      ),
    );
    await settle(tester);
    await check(tester);
    semantics.dispose();
    await tester.pumpWidget(const SizedBox());
  });
}
