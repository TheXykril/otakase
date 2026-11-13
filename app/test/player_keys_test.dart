import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/ui/player_keys.dart';

void main() {
  test('phone double tap seeks back on the left, forward on the right', () {
    expect(doubleTapSeconds(40, 900), -10);
    expect(doubleTapSeconds(450, 900), 0);
    expect(doubleTapSeconds(860, 900), 10);
  });

  PlayerAction? key(
    LogicalKeyboardKey k, [
    String? character,
    bool shift = false,
    bool alt = false,
    bool control = false,
  ]) => playerActionFor(k, character, shift: shift, alt: alt, control: control);

  test('the old mpv skin keys all do something', () {
    expect(key(LogicalKeyboardKey.space, ' '), PlayerAction.playPause);
    expect(key(LogicalKeyboardKey.arrowLeft), PlayerAction.seekBack);
    expect(key(LogicalKeyboardKey.arrowRight), PlayerAction.seekForward);
    expect(key(LogicalKeyboardKey.arrowUp), PlayerAction.volumeUp);
    expect(key(LogicalKeyboardKey.arrowDown), PlayerAction.volumeDown);
    expect(key(LogicalKeyboardKey.enter), PlayerAction.skipSegment);
    expect(key(LogicalKeyboardKey.keyM, 'm'), PlayerAction.mute);
    expect(key(LogicalKeyboardKey.keyF, 'f'), PlayerAction.fullscreen);
    expect(key(LogicalKeyboardKey.keyJ, 'j'), PlayerAction.nextSubtitle);
    expect(key(LogicalKeyboardKey.numberSign, '#'), PlayerAction.nextAudio);
    expect(key(LogicalKeyboardKey.less, '<'), PlayerAction.previousEpisode);
    expect(key(LogicalKeyboardKey.greater, '>'), PlayerAction.nextEpisode);
    expect(key(LogicalKeyboardKey.question, '?'), PlayerAction.keys);
    expect(key(LogicalKeyboardKey.keyQ, 'q'), PlayerAction.quit);
  });

  test('mpv defaults the skin left alone', () {
    expect(
      key(LogicalKeyboardKey.arrowLeft, null, true),
      PlayerAction.stepBack,
    );
    expect(
      key(LogicalKeyboardKey.keyJ, 'J', true),
      PlayerAction.previousSubtitle,
    );
    expect(key(LogicalKeyboardKey.keyV, 'v'), PlayerAction.toggleSubtitles);
    expect(key(LogicalKeyboardKey.keyZ, 'z'), PlayerAction.subtitleEarlier);
    expect(key(LogicalKeyboardKey.keyX, 'x'), PlayerAction.subtitleLater);
    expect(key(LogicalKeyboardKey.bracketLeft, '['), PlayerAction.slower);
    expect(key(LogicalKeyboardKey.bracketRight, ']'), PlayerAction.faster);
    expect(key(LogicalKeyboardKey.braceLeft, '{'), PlayerAction.halfSpeed);
    expect(key(LogicalKeyboardKey.braceRight, '}'), PlayerAction.doubleSpeed);
    expect(key(LogicalKeyboardKey.backspace), PlayerAction.normalSpeed);
    expect(key(LogicalKeyboardKey.comma, ','), PlayerAction.frameBack);
    expect(key(LogicalKeyboardKey.period, '.'), PlayerAction.frameForward);
    expect(key(LogicalKeyboardKey.digit9, '9'), PlayerAction.volumeDownSmall);
    expect(key(LogicalKeyboardKey.digit0, '0'), PlayerAction.volumeUpSmall);
    expect(key(LogicalKeyboardKey.pageUp), PlayerAction.previousChapter);
    expect(key(LogicalKeyboardKey.pageDown), PlayerAction.nextChapter);
    expect(key(LogicalKeyboardKey.keyS, 's'), PlayerAction.screenshot);
    expect(key(LogicalKeyboardKey.escape), PlayerAction.leaveFullscreen);
  });

  test('symbols follow what the key types, not where it sits', () {
    // On a German layout # is its own key and < sits left of Y.
    expect(key(LogicalKeyboardKey.backslash, '#'), PlayerAction.nextAudio);
    expect(
      key(LogicalKeyboardKey.intlBackslash, '<'),
      PlayerAction.previousEpisode,
    );
    // Shift+, on a US layout types <.
    expect(
      key(LogicalKeyboardKey.comma, '<', true),
      PlayerAction.previousEpisode,
    );
  });

  test('Alt keys mark and vote on skip times, and vote in a party', () {
    PlayerAction? alt(LogicalKeyboardKey k) => key(k, null, false, true);
    expect(alt(LogicalKeyboardKey.keyO), PlayerAction.markOpening);
    expect(alt(LogicalKeyboardKey.keyE), PlayerAction.markEnding);
    expect(alt(LogicalKeyboardKey.keyS), PlayerAction.sendMarks);
    expect(alt(LogicalKeyboardKey.keyR), PlayerAction.forgetMarks);
    expect(alt(LogicalKeyboardKey.keyU), PlayerAction.upvoteSkip);
    expect(alt(LogicalKeyboardKey.keyD), PlayerAction.downvoteSkip);
    expect(alt(LogicalKeyboardKey.keyK), PlayerAction.partySkip);
    expect(alt(LogicalKeyboardKey.keyN), PlayerAction.partyNext);
    expect(alt(LogicalKeyboardKey.keyM), isNull);
  });

  test('Ctrl combinations stay with the app', () {
    expect(key(LogicalKeyboardKey.keyF, 'f', false, false, true), isNull);
    expect(key(LogicalKeyboardKey.keyQ, 'q', false, false, true), isNull);
  });

  test('speed steps stay in range and come back to 1x', () {
    expect(nextSpeed(1, PlayerAction.faster), 1.1);
    expect(nextSpeed(1.1, PlayerAction.slower), 1);
    expect(nextSpeed(1, PlayerAction.doubleSpeed), 2);
    expect(nextSpeed(4, PlayerAction.doubleSpeed), 4);
    expect(nextSpeed(.25, PlayerAction.halfSpeed), .25);
    expect(nextSpeed(2.5, PlayerAction.normalSpeed), 1);
  });

  test('the keys list names every action group', () {
    final anime = playerKeyHelp(party: true).map((s) => s.$1).toList();
    expect(anime, containsAll(['Playback', 'Skip times', 'Watch party']));
    final film = playerKeyHelp(episodes: false).map((s) => s.$1).toList();
    expect(film, isNot(contains('Skip times')));
    final keys = [
      for (final (_, rows) in playerKeyHelp()) ...rows.map((r) => r.keys),
    ];
    expect(keys, containsAll(['Space / P', '#', '< / >', '?', 'Alt + O']));
  });

  testWidgets('the keys list opens and closes', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => TextButton(
            onPressed: () => showPlayerKeys(context),
            child: const Text('open'),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
    expect(find.text('Play / pause'), findsOneWidget);
    await tester.tap(find.text('Close'));
    await tester.pumpAndSettle();
    expect(find.text('Play / pause'), findsNothing);
  });
}
