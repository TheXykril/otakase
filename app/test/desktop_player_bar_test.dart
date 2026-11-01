import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/desktop/desktop_core.dart';
import 'package:otakase/desktop/player_bar.dart';

void main() {
  setUp(() {
    debugDesktopOverride = true;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(
          const MethodChannel('window_manager'),
          (call) async => call.method == 'isMaximized' ? false : null,
        );
  });

  tearDown(() {
    debugDesktopOverride = null;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(const MethodChannel('window_manager'), null);
  });

  Widget bar(List<String> calls, {bool canNext = true, bool silent = false}) =>
      MaterialApp(
        home: Scaffold(
          backgroundColor: Colors.black,
          body: DesktopPlayerBar(
            title: 'Frieren',
            subtitle: 'Ep 8 · SUB',
            position: const Duration(minutes: 7, seconds: 12),
            duration: const Duration(minutes: 24),
            buffered: const Duration(minutes: 10),
            playing: true,
            buffering: false,
            silent: silent,
            fullscreen: false,
            trackLabel: 'SUB',
            stripes: const [
              SkipSpan('op', 60, 150),
              SkipSpan('ed', 1300, 1390),
            ],
            onBack: () => calls.add('back'),
            onPlayPause: () => calls.add('play'),
            onSeekBy: (s) => calls.add('seek $s'),
            onSeekStart: (_) {},
            onSeekChanged: (_) {},
            onSeekEnd: (_) => calls.add('scrub'),
            onNext: canNext ? () => calls.add('next') : null,
            onMute: () => calls.add('mute'),
            onTracks: () => calls.add('tracks'),
            onQuality: () => calls.add('quality'),
            onFullscreen: () => calls.add('fullscreen'),
          ),
        ),
      );

  testWidgets('desktop player bar shows times and runs every control', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1920, 1080);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final calls = <String>[];
    await tester.pumpWidget(bar(calls));
    await tester.pump();

    expect(find.text('Frieren'), findsOneWidget);
    expect(find.text('07:12'), findsOneWidget);
    expect(find.text('-16:48'), findsOneWidget);
    expect(find.byKey(const Key('window-close')), findsOneWidget);

    for (final key in [
      'player-back',
      'player-play',
      'player-next',
      'player-mute',
      'player-tracks',
      'player-quality',
      'player-fullscreen',
    ]) {
      await tester.tap(find.byKey(Key(key)));
      await tester.pump();
    }
    await tester.tap(find.byTooltip('Back 10 seconds'));
    await tester.tap(find.byTooltip('Forward 10 seconds'));
    await tester.pump();
    expect(calls, [
      'back',
      'play',
      'next',
      'mute',
      'tracks',
      'quality',
      'fullscreen',
      'seek -10',
      'seek 10',
    ]);

    await tester.tap(find.byKey(const Key('player-timeline')));
    await tester.pump();
    expect(calls.last, 'scrub');
  });

  testWidgets('next is off on the last episode and mute shows unmute', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1920, 1080);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final calls = <String>[];
    await tester.pumpWidget(bar(calls, canNext: false, silent: true));
    await tester.pump();
    await tester.tap(find.byKey(const Key('player-next')));
    await tester.pump();
    expect(calls, isEmpty);
    expect(find.byTooltip('Unmute'), findsOneWidget);
  });

  test('time labels use hours only when needed', () {
    expect(DesktopPlayerBar.time(const Duration(seconds: 65)), '01:05');
    expect(DesktopPlayerBar.time(const Duration(seconds: 3725)), '1:02:05');
  });
}
