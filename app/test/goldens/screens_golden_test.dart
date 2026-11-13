// Golden screenshots of the main screens on phone, TV and computer.
//
// They catch unintended visual changes: any screen that draws differently
// from its committed PNG in test/goldens/<device>/ fails. After an intended
// change, refresh them with
//
//   flutter test test/goldens --update-goldens
//
// and look at the new PNGs before committing. They are made on Linux, where
// text is rasterized the same way every time; other systems skip them.
//
// Built on the website fixture (test/site_fixture.dart): real covers, a short
// watch history and no network. The clock and battery header shows the time
// of the run, so it is covered by a plain block before the capture.
import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/desktop/player_bar.dart';
import 'package:otakase/main.dart';
import 'package:otakase/services.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/ui/device_status.dart';
import 'package:otakase/ui/phone_player.dart';
import 'package:otakase/ui/show_page.dart';

import '../site_fixture.dart';

enum Device { phone, tv, desktop }

const sizes = {
  Device.phone: Size(412, 915),
  Device.tv: Size(1280, 720),
  Device.desktop: Size(1280, 800),
};

/// Screens reached from the app shell, by how to get there.
const screens = ['home', 'search', 'more', 'settings', 'accounts', 'show'];

final masks = ValueNotifier<List<Rect>>(const []);

Widget framed(Widget app) => art(
  Stack(
    textDirection: TextDirection.ltr,
    children: [
      Positioned.fill(child: app),
      Positioned.fill(
        child: IgnorePointer(
          child: ValueListenableBuilder<List<Rect>>(
            valueListenable: masks,
            builder: (_, rects, _) => Stack(
              textDirection: TextDirection.ltr,
              children: [
                for (final r in rects)
                  Positioned.fromRect(
                    rect: r,
                    child: const ColoredBox(color: Color(0xff3a3a3a)),
                  ),
              ],
            ),
          ),
        ),
      ),
    ],
  ),
);

Future<void> settle(WidgetTester tester) async {
  await tester.pumpAndSettle();
  await tester.pumpAndSettle();
}

Future<void> capture(WidgetTester tester, Device device, String name) async {
  await settle(tester);
  final root = tester.getTopLeft(find.byKey(const Key('site-render')));
  masks.value = [
    for (final e in find.byType(DeviceStatus).hitTestable().evaluate())
      tester.getRect(find.byWidget(e.widget)).shift(-root).inflate(2),
  ];
  await tester.pump();
  expect(tester.takeException(), isNull);
  await expectLater(
    find.byKey(const Key('site-render')),
    matchesGoldenFile('${device.name}/$name.png'),
  );
  masks.value = const [];
}

Future<void> open(WidgetTester tester, Key key) async {
  final target = find.byKey(key).hitTestable();
  if (target.evaluate().isEmpty) {
    await tester.scrollUntilVisible(
      find.byKey(key),
      200,
      scrollable: find.byType(Scrollable).last,
    );
    await settle(tester);
  }
  await tester.tap(find.byKey(key).first);
  await settle(tester);
}

Future<void> reach(
  WidgetTester tester,
  AppServices app,
  Anime main,
  Device device,
  String screen,
) async {
  final tv = device != Device.phone;
  switch (screen) {
    case 'search':
      await open(tester, const ValueKey('nav-Search'));
    case 'more':
      await open(tester, const ValueKey('nav-More'));
    case 'settings':
      await open(tester, const ValueKey('nav-More'));
      await open(tester, const ValueKey('more-Subtitles & languages'));
    case 'accounts':
      await open(tester, const ValueKey('nav-More'));
      await open(tester, const Key('more-Accounts'));
    case 'show':
      unawaited(
        tester
            .state<NavigatorState>(find.byType(Navigator).first)
            .push(
              OtakaseRoute<void>(
                builder: (_) => ShowPage(services: app, anime: main, isTV: tv),
              ),
            ),
      );
  }
}

Widget player(Device device, Anime main) {
  final ep = main.episodeArt.firstWhere(
    (e) => e['episode'] == resumeAt,
    orElse: () => main.episodeArt.first,
  );
  final frame = Image(
    image: DecodedImage(images[Uri.parse(ep['thumbnail'] as String).path]!),
    fit: BoxFit.cover,
  );
  const spans = [SkipSpan('op', 40, 130), SkipSpan('ed', 1290, 1380)];
  final controls = device == Device.desktop
      ? DesktopPlayerBar(
          title: main.title,
          subtitle: 'Ep $resumeAt · SUB',
          centerLabel: 'Episode $resumeAt',
          position: const Duration(minutes: 1, seconds: 12),
          duration: const Duration(minutes: 24),
          buffered: const Duration(minutes: 4),
          playing: true,
          buffering: false,
          silent: false,
          fullscreen: false,
          trackLabel: 'SUB',
          stripes: spans,
          onBack: () {},
          onPlayPause: () {},
          onSeekBy: (_) {},
          onSeekStart: (_) {},
          onSeekChanged: (_) {},
          onSeekEnd: (_) {},
          onNext: () {},
          onMute: () {},
          onTracks: () {},
          onQuality: () {},
          onFullscreen: () {},
        )
      : PhonePlayerControls(
          isTV: device == Device.tv,
          title: main.title,
          subtitle: 'Episode $resumeAt · sub',
          position: const Duration(minutes: 1, seconds: 12),
          duration: const Duration(minutes: 24),
          spans: spans,
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
        );
  return MaterialApp(
    debugShowCheckedModeBanner: false,
    home: Scaffold(
      backgroundColor: Colors.black,
      body: TVViewport(
        child: Stack(fit: StackFit.expand, children: [frame, controls]),
      ),
    ),
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  // Text rasterizes differently elsewhere; the PNGs are made on Linux.
  final skip = !Platform.isLinux;
  readShows();
  final main = shows.first;
  setUpAll(loadArt);

  for (final device in Device.values) {
    void view(WidgetTester tester) {
      setUpView(tester, sizes[device]!, desktop: device == Device.desktop);
      if (device == Device.phone) {
        tester.view.padding = const FakeViewPadding(top: 24, bottom: 24);
        addTearDown(tester.view.resetPadding);
      }
    }

    for (final screen in screens) {
      testWidgets('${device.name} $screen', skip: skip, (tester) async {
        view(tester);
        final app = await services(main);
        await tester.pumpWidget(
          framed(OtakaseApp(services: app, isTV: device != Device.phone)),
        );
        await settle(tester);
        await reach(tester, app, main, device, screen);
        await capture(tester, device, screen);
        await tester.pumpWidget(const SizedBox());
      });
    }

    testWidgets('${device.name} player', skip: skip, (tester) async {
      view(tester);
      // Phones play in landscape.
      if (device == Device.phone) {
        tester.view.physicalSize = sizes[device]!.flipped;
        tester.view.padding = FakeViewPadding.zero;
      }
      await tester.pumpWidget(framed(player(device, main)));
      await capture(tester, device, 'player');
      await tester.pumpWidget(const SizedBox());
    });
  }
}
