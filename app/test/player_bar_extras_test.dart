import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/desktop/desktop_core.dart';
import 'package:otakase/desktop/player_bar.dart';
import 'package:otakase/ui/components.dart';

void main() {
  testWidgets('desktop bar hides quality and shows extra buttons', (
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
            subtitle: 'Episode 8',
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
                tooltip: 'People',
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
