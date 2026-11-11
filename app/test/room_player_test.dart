import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/desktop/desktop_core.dart';
import 'package:otakase/desktop/player_bar.dart';
import 'package:otakase/ui/components.dart';
import 'package:otakase/ui/watch_rooms_page.dart';

void main() {
  test('room skip stripes come only from checked times', () {
    expect(
      roomSkipSpans({
        'skip_ready': false,
        'dur': 1420,
        'skip_times': {
          'op': {'start': 30, 'end': 120},
        },
      }),
      isEmpty,
    );
    final spans = roomSkipSpans({
      'skip_ready': true,
      'dur': 1420,
      'skip_times': {
        'op': {'start': 30, 'end': 120},
        'ed': {'start': 0, 'end': 0},
      },
    });
    expect(spans.map((s) => '${s.type} ${s.start}-${s.end}'), [
      'op 30.0-120.0',
    ]);
  });

  test('room asks read as plain words', () {
    expect(roomAskText({'kind': 'pause'}), 'pause');
    expect(roomAskText({'kind': 'forward', 'by': 20}), 'skip 20 s');
    expect(roomAskText({'kind': 'forward', 'by': -10}), 'go back 10 s');
    expect(roomAskText({'kind': 'seek', 'pos': 754}), 'jump to 12:34');
    expect(roomAskText({'kind': 'next'}), 'play the next episode');
  });

  testWidgets('desktop bar hides quality and shows the room button', (
    tester,
  ) async {
    debugDesktopOverride = true;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(
          const MethodChannel('window_manager'),
          (call) async => call.method == 'isMaximized' ? false : null,
        );
    addTearDown(() {
      debugDesktopOverride = null;
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(
            const MethodChannel('window_manager'),
            null,
          );
    });
    tester.view.physicalSize = const Size(1920, 1080);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    var room = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: DesktopPlayerBar(
            title: 'Frieren',
            subtitle: 'Episode 8 · Following Reader',
            position: Duration.zero,
            duration: const Duration(minutes: 24),
            buffered: Duration.zero,
            playing: true,
            buffering: false,
            silent: false,
            fullscreen: false,
            trackLabel: 'Subtitles',
            stripes: const [],
            onBack: () {},
            onPlayPause: () {},
            onSeekBy: (_) {},
            onSeekStart: (_) {},
            onSeekChanged: (_) {},
            onSeekEnd: (_) {},
            onNext: null,
            onMute: () {},
            onTracks: () {},
            onQuality: null,
            onFullscreen: () {},
            extras: [
              (
                key: const Key('room-panel'),
                icon: LucideIcons.users,
                tooltip: 'Watch room',
                onPressed: () => room++,
              ),
            ],
          ),
        ),
      ),
    );
    await tester.pump();
    expect(find.byKey(const Key('player-quality')), findsNothing);
    await tester.tap(find.byKey(const Key('room-panel')));
    expect(room, 1);
  });
}
