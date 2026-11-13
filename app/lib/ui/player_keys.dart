import 'dart:async';
import 'dart:io';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:media_kit/media_kit.dart';

import 'components.dart';
import 'mpv.dart';
import '../host.dart';

/// What a key does in the desktop player. The keys are mpv's own, as the
/// old command-line app left them in its mpv skin, so muscle memory from
/// mpv keeps working.
enum PlayerAction {
  playPause,
  seekBack,
  seekForward,
  stepBack,
  stepForward,
  volumeUp,
  volumeDown,
  volumeUpSmall,
  volumeDownSmall,
  mute,
  fullscreen,
  leaveFullscreen,
  skipSegment,
  nextSubtitle,
  previousSubtitle,
  toggleSubtitles,
  nextAudio,
  subtitleEarlier,
  subtitleLater,
  previousEpisode,
  nextEpisode,
  previousChapter,
  nextChapter,
  slower,
  faster,
  halfSpeed,
  doubleSpeed,
  normalSpeed,
  frameBack,
  frameForward,
  screenshot,
  keys,
  quit,
  markOpening,
  markEnding,
  sendMarks,
  forgetMarks,
  upvoteSkip,
  downvoteSkip,
  partySkip,
  partyNext,
}

/// The action for a key press, or null when the key is not a player key.
/// [character] is what the key types, so symbol keys (#, <, ?, [) work on
/// every keyboard layout.
PlayerAction? playerActionFor(
  LogicalKeyboardKey key,
  String? character, {
  bool shift = false,
  bool alt = false,
  bool control = false,
}) {
  if (control) return null;
  if (alt) {
    return switch (key) {
      LogicalKeyboardKey.keyO => PlayerAction.markOpening,
      LogicalKeyboardKey.keyE => PlayerAction.markEnding,
      LogicalKeyboardKey.keyS => PlayerAction.sendMarks,
      LogicalKeyboardKey.keyR => PlayerAction.forgetMarks,
      LogicalKeyboardKey.keyU => PlayerAction.upvoteSkip,
      LogicalKeyboardKey.keyD => PlayerAction.downvoteSkip,
      LogicalKeyboardKey.keyK => PlayerAction.partySkip,
      LogicalKeyboardKey.keyN => PlayerAction.partyNext,
      _ => null,
    };
  }
  switch (key) {
    case LogicalKeyboardKey.space:
    case LogicalKeyboardKey.mediaPlayPause:
      return PlayerAction.playPause;
    case LogicalKeyboardKey.arrowLeft:
      return shift ? PlayerAction.stepBack : PlayerAction.seekBack;
    case LogicalKeyboardKey.arrowRight:
      return shift ? PlayerAction.stepForward : PlayerAction.seekForward;
    case LogicalKeyboardKey.arrowUp:
      return PlayerAction.volumeUp;
    case LogicalKeyboardKey.arrowDown:
      return PlayerAction.volumeDown;
    case LogicalKeyboardKey.pageUp:
      return PlayerAction.previousChapter;
    case LogicalKeyboardKey.pageDown:
      return PlayerAction.nextChapter;
    case LogicalKeyboardKey.enter:
    case LogicalKeyboardKey.numpadEnter:
      return PlayerAction.skipSegment;
    case LogicalKeyboardKey.escape:
      return PlayerAction.leaveFullscreen;
    case LogicalKeyboardKey.backspace:
      return PlayerAction.normalSpeed;
    case LogicalKeyboardKey.mediaTrackNext:
      return PlayerAction.nextEpisode;
    case LogicalKeyboardKey.mediaTrackPrevious:
      return PlayerAction.previousEpisode;
  }
  // Symbols differ between layouts: go by what the key types.
  switch (character) {
    case '#':
      return PlayerAction.nextAudio;
    case '<':
      return PlayerAction.previousEpisode;
    case '>':
      return PlayerAction.nextEpisode;
    case '[':
      return PlayerAction.slower;
    case ']':
      return PlayerAction.faster;
    case '{':
      return PlayerAction.halfSpeed;
    case '}':
      return PlayerAction.doubleSpeed;
    case ',':
      return PlayerAction.frameBack;
    case '.':
      return PlayerAction.frameForward;
    case '?':
      return PlayerAction.keys;
    case '9':
      return PlayerAction.volumeDownSmall;
    case '0':
      return PlayerAction.volumeUpSmall;
  }
  return switch (key) {
    LogicalKeyboardKey.keyP => PlayerAction.playPause,
    LogicalKeyboardKey.keyM => PlayerAction.mute,
    LogicalKeyboardKey.keyF => PlayerAction.fullscreen,
    LogicalKeyboardKey.keyJ =>
      shift ? PlayerAction.previousSubtitle : PlayerAction.nextSubtitle,
    LogicalKeyboardKey.keyV => PlayerAction.toggleSubtitles,
    LogicalKeyboardKey.keyZ => PlayerAction.subtitleEarlier,
    LogicalKeyboardKey.keyX => PlayerAction.subtitleLater,
    LogicalKeyboardKey.keyS => PlayerAction.screenshot,
    LogicalKeyboardKey.keyQ => PlayerAction.quit,
    _ => null,
  };
}

/// The action for [event] with the modifiers held now.
PlayerAction? playerActionForEvent(KeyEvent event) {
  final keyboard = HardwareKeyboard.instance;
  return playerActionFor(
    event.logicalKey,
    event.character,
    shift: keyboard.isShiftPressed,
    alt: keyboard.isAltPressed,
    control: keyboard.isControlPressed || keyboard.isMetaPressed,
  );
}

/// One line of the keys list.
class PlayerKeyHelp {
  const PlayerKeyHelp(this.keys, this.label);
  final String keys, label;
}

/// The keys list, in sections. [episodes] adds the episode and skip keys
/// that only the anime player has.
List<(String, List<PlayerKeyHelp>)> playerKeyHelp({
  bool episodes = true,
  bool party = false,
}) => [
  (
    'Playback',
    [
      const PlayerKeyHelp('Space / P', 'Play / pause'),
      const PlayerKeyHelp('← / →', 'Seek 5 seconds'),
      const PlayerKeyHelp('Shift + ← / →', 'Seek 1 second'),
      const PlayerKeyHelp('PgUp / PgDn', 'Previous / next chapter'),
      if (episodes) const PlayerKeyHelp('Enter', 'Skip opening or ending'),
      if (episodes) const PlayerKeyHelp('< / >', 'Previous / next episode'),
      const PlayerKeyHelp('[ / ]', 'Slower / faster'),
      const PlayerKeyHelp('{ / }', 'Half / double speed'),
      const PlayerKeyHelp('Backspace', 'Normal speed'),
      const PlayerKeyHelp(', / .', 'Frame back / forward'),
      const PlayerKeyHelp('Q', 'Quit the player'),
    ],
  ),
  (
    'Sound and picture',
    [
      const PlayerKeyHelp('↑ / ↓', 'Volume'),
      const PlayerKeyHelp('9 / 0', 'Volume, small steps'),
      const PlayerKeyHelp('M', 'Mute'),
      const PlayerKeyHelp('F', 'Fullscreen'),
      const PlayerKeyHelp('Esc', 'Leave fullscreen'),
      const PlayerKeyHelp('S', 'Screenshot'),
    ],
  ),
  (
    'Subtitles and audio',
    [
      const PlayerKeyHelp('J / Shift + J', 'Next / previous subtitles'),
      const PlayerKeyHelp('V', 'Subtitles on / off'),
      const PlayerKeyHelp('Z / X', 'Subtitles earlier / later'),
      const PlayerKeyHelp('#', 'Next audio track'),
    ],
  ),
  if (episodes)
    (
      'Skip times',
      [
        const PlayerKeyHelp('Alt + O', 'Mark the opening (again: its end)'),
        const PlayerKeyHelp('Alt + E', 'Mark the ending (again: its end)'),
        const PlayerKeyHelp('Alt + S', 'Send marks to AniSkip'),
        const PlayerKeyHelp('Alt + R', 'Forget marks'),
        const PlayerKeyHelp('Alt + U / Alt + D', 'This skip is right / wrong'),
      ],
    ),
  if (party)
    (
      'Watch party',
      [
        const PlayerKeyHelp('Alt + K', 'Vote to skip'),
        const PlayerKeyHelp('Alt + N', 'Vote for the next episode'),
      ],
    ),
  ('', [const PlayerKeyHelp('?', 'This list')]),
];

/// Shows the keys list over the player.
Future<void> showPlayerKeys(
  BuildContext context, {
  bool episodes = true,
  bool party = false,
}) => showDialog<void>(
  context: context,
  builder: (dialog) => AlertDialog(
    title: const Text('Keys'),
    content: SizedBox(
      width: 560,
      height: 460,
      child: ListView(
        children: [
          for (final (title, rows) in playerKeyHelp(
            episodes: episodes,
            party: party,
          )) ...[
            if (title.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 12, bottom: 4),
                child: Text(title, style: const TextStyle(color: accentText)),
              ),
            for (final row in rows)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 3),
                child: Row(
                  children: [
                    SizedBox(
                      width: 170,
                      child: Text(row.keys, style: mono.copyWith(color: fg)),
                    ),
                    Expanded(child: Text(row.label)),
                  ],
                ),
              ),
          ],
        ],
      ),
    ),
    actions: [
      TextButton(
        autofocus: true,
        onPressed: () => Navigator.pop(dialog),
        child: const Text('Close'),
      ),
    ],
  ),
);

/// A short note in the middle top of the player ("Speed 1.25x"), as mpv's
/// on-screen messages did.
class PlayerFlash extends StatelessWidget {
  const PlayerFlash({super.key, required this.text});
  final String text;
  @override
  Widget build(BuildContext context) => IgnorePointer(
    child: Align(
      alignment: const Alignment(0, -.78),
      child: AnimatedOpacity(
        opacity: text.isEmpty ? 0 : 1,
        duration: const Duration(milliseconds: 150),
        child: Container(
          key: const Key('player-flash'),
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
          decoration: BoxDecoration(
            color: const Color(0xcc080707),
            borderRadius: BorderRadius.circular(KageRadius.toast),
            border: Border.all(color: line),
          ),
          child: Text(text, style: uiStyle(context, color: fgBright)),
        ),
      ),
    ),
  );
}

/// Shows short notes for a moment.
/// Seconds a phone double tap at [x] across [width] seeks: back 10 on the
/// left third, forward 10 on the right third, nothing in the middle.
int doubleTapSeconds(double x, double width) => x < width / 3
    ? -10
    : x > width * 2 / 3
    ? 10
    : 0;

class FlashText extends ValueNotifier<String> {
  FlashText() : super('');
  Timer? _timer;

  /// [long] keeps it up long enough to read a sentence, such as an error.
  void show(String text, {bool long = false}) {
    value = text;
    _timer?.cancel();
    _timer = Timer(
      Duration(milliseconds: long ? 4000 : 1400),
      () => value = '',
    );
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }
}

String _trackName(String id, String? title, String? language) {
  if (id == 'no') return 'Off';
  if (title != null && title.isNotEmpty) {
    return language != null && language.isNotEmpty && title != language
        ? '$title · $language'
        : title;
  }
  return language ?? 'Track $id';
}

/// The next speed after [rate] for a step, kept between 0.25x and 4x.
double nextSpeed(double rate, PlayerAction action) {
  final next = switch (action) {
    PlayerAction.slower => rate / 1.1,
    PlayerAction.faster => rate * 1.1,
    PlayerAction.halfSpeed => rate / 2,
    PlayerAction.doubleSpeed => rate * 2,
    _ => 1.0,
  };
  // Snap near 1x back to 1x, so [ then ] lands where it started.
  final rounded = (next * 100).round() / 100;
  return (rounded - 1).abs() < .02 ? 1 : rounded.clamp(.25, 4).toDouble();
}

/// The key actions both players run the same way on the media_kit player.
extension PlayerKeyOps on Player {
  Future<void> _command(List<String> args) async {
    await mpvCommand(args);
  }

  /// Picks the next (or previous) subtitle track, Off included; returns its
  /// name.
  Future<String> cycleSubtitles({bool back = false}) async {
    final tracks = state.tracks.subtitle.where((t) => t.id != 'auto').toList();
    if (tracks.isEmpty) return 'No subtitles';
    final now = tracks.indexWhere((t) => t.id == state.track.subtitle.id);
    final step = back ? -1 : 1;
    final next = tracks[(math.max(now, 0) + step) % tracks.length];
    await setSubtitleTrack(next);
    return 'Subtitles: ${_trackName(next.id, next.title, next.language)}';
  }

  /// Turns subtitles off, or back on to the first track.
  Future<String> toggleSubtitles() async {
    if (state.track.subtitle.id != 'no') {
      await setSubtitleTrack(SubtitleTrack.no());
      return 'Subtitles off';
    }
    final first = state.tracks.subtitle
        .where((t) => t.id != 'auto' && t.id != 'no')
        .firstOrNull;
    if (first == null) return 'No subtitles';
    await setSubtitleTrack(first);
    return 'Subtitles: ${_trackName(first.id, first.title, first.language)}';
  }

  Future<String> cycleAudio() async {
    final tracks = state.tracks.audio
        .where((t) => t.id != 'auto' && t.id != 'no')
        .toList();
    if (tracks.length < 2) return 'One audio track';
    final now = tracks.indexWhere((t) => t.id == state.track.audio.id);
    final next = tracks[(math.max(now, 0) + 1) % tracks.length];
    await setAudioTrack(next);
    return 'Audio: ${_trackName(next.id, next.title, next.language)}';
  }

  Future<String> changeSpeed(PlayerAction action) async {
    final rate = nextSpeed(state.rate, action);
    await setRate(rate);
    return 'Speed ${rate.toStringAsFixed(2)}x';
  }

  Future<String> shiftSubtitles(double seconds) async {
    await _command(['add', 'sub-delay', '$seconds']);
    final delay = double.tryParse(await mpvGet('sub-delay') ?? '');
    if (delay != null) {
      return 'Subtitle delay ${(delay * 1000).round()} ms';
    }
    return 'Subtitle delay changed';
  }

  Future<void> stepFrame({bool back = false}) =>
      _command([back ? 'frame-back-step' : 'frame-step']);

  Future<void> stepChapter(int by) => _command(['add', 'chapter', '$by']);

  /// Saves the frame shown now as a PNG in Pictures/Otakase; returns where.
  Future<String> saveScreenshot(String name) async {
    final shot = await screenshot(format: 'image/png');
    if (shot == null) return 'No picture to save';
    final home =
        Host.environment['HOME'] ?? Host.environment['USERPROFILE'] ?? '.';
    final dir = Directory(
      '$home${Host.pathSeparator}Pictures'
      '${Host.pathSeparator}Otakase',
    );
    await dir.create(recursive: true);
    final safe = name.replaceAll(RegExp(r'[^\w\- ]+'), '').trim();
    final stamp = DateTime.now()
        .toIso8601String()
        .replaceAll(RegExp(r'[:.]'), '-')
        .substring(0, 19);
    final file = File(
      '${dir.path}${Host.pathSeparator}${safe.isEmpty ? 'otakase' : safe} $stamp.png',
    );
    await file.writeAsBytes(shot);
    return 'Screenshot saved in Pictures/Otakase';
  }
}
