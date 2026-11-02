import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';

import '../core.dart';
import '../data/models.dart';
import '../data/store.dart';
import '../desktop/desktop_core.dart';
import '../desktop/player_bar.dart';
import '../desktop/window_frame.dart';
import '../services.dart';
import '../tls_ca.dart';
import 'components.dart';
import 'player_keys.dart';
import 'seal.dart';

/// Something to play: a stream found by a source, or a downloaded file.
class PlayableMedia {
  const PlayableMedia({
    required this.url,
    this.headers = const {},
    this.subtitles = const [],
    this.audioLanguage = '',
    this.copy = '',
    this.label = '',
  });
  final String url;
  final Map<String, String> headers;

  /// Subtitle files as {url, label, language}; a url may be a local path.
  final List<Json> subtitles;

  /// The audio track to pick, as language codes such as "lt" or "en,eng".
  final String audioLanguage;

  /// Names this copy so it can be skipped when it fails, such as
  /// vidsrc:server 1. Empty for a file, which has no other copy.
  final String copy;

  /// Where it plays from, for the title line.
  final String label;
}

/// What the player shows and tells as it plays.
class PlayRequest {
  const PlayRequest({
    required this.title,
    required this.subtitle,
    required this.open,
    this.resumeAt,
    this.onProgress,
    this.onScrobble,
    this.next,
  });
  final String title, subtitle;
  final Future<PlayableMedia> Function(
    Set<String> failed,
    RequestScope scope,
    void Function(String) status,
  )
  open;

  /// The second to start at once the length is known.
  final int Function(int duration)? resumeAt;
  final Future<void> Function(int position, int duration, bool completed)?
  onProgress;

  /// Trakt's start, pause and stop, with the percentage watched.
  final void Function(String action, double percent)? onScrobble;

  /// The episode after this one, or null at the end.
  final PlayRequest? Function()? next;
}

bool languageMatches(String? language, String? title, String preferred) {
  const aliases = <String, List<String>>{
    'english': ['en', 'eng', 'english'],
    'lithuanian': ['lt', 'lit', 'lithuanian', 'lietuvių'],
    'japanese': ['ja', 'jpn', 'japanese'],
    'russian': ['ru', 'rus', 'russian'],
    'spanish': ['es', 'spa', 'spanish'],
    'french': ['fr', 'fra', 'fre', 'french'],
    'german': ['de', 'deu', 'ger', 'german'],
  };
  final p = preferred.toLowerCase().trim();
  final values = aliases[p] ?? [p];
  final lang = (language ?? '').toLowerCase(),
      label = (title ?? '').toLowerCase();
  return values.any((s) => lang == s || (s.length > 2 && label.contains(s)));
}

/// The player for films, series episodes and downloads. It shares the
/// desktop control bar with the anime player and keeps the remote keys:
/// OK plays and pauses, left and right seek, up shows the buttons, Back
/// leaves.
class MoviePlayerPage extends StatefulWidget {
  const MoviePlayerPage({
    super.key,
    required this.services,
    required this.request,
    required this.isTV,
  });
  final AppServices services;
  final PlayRequest request;
  final bool isTV;
  @override
  State<MoviePlayerPage> createState() => _MoviePlayerPageState();
}

class _MoviePlayerPageState extends State<MoviePlayerPage> {
  late final Player player;
  late final VideoController video;
  late PlayRequest request;
  RequestScope scope = RequestScope();
  final failed = <String>{};
  final subscriptions = <StreamSubscription<dynamic>>[];
  final surface = FocusNode(debugLabel: 'Movie surface');
  final playFocus = FocusNode(debugLabel: 'Movie play');
  PlayableMedia? media;
  Timer? hideTimer, saveTimer;
  Duration position = Duration.zero, duration = Duration.zero;
  bool busy = true, overlay = true, scrubbing = false, fullscreen = false;
  bool finished = false, started = false, subPicked = false;
  String status = 'Finding a copy', error = '';
  int generation = 0;

  AppStore get store => widget.services.store;

  @override
  void initState() {
    super.initState();
    request = widget.request;
    player = Player(
      configuration: const PlayerConfiguration(
        libass: true,
        libassAndroidFont: 'assets/fonts/NotoSansJP-Regular.otf',
        libassAndroidFontName: 'Noto Sans JP',
      ),
    );
    video = VideoController(
      player,
      configuration: const VideoControllerConfiguration(
        hwdec: 'mediacodec',
        enableHardwareAcceleration: true,
      ),
    );
    subscriptions.addAll([
      player.stream.position.listen((p) {
        if (!mounted || busy || scrubbing) return;
        setState(() => position = p);
        final total = duration.inSeconds;
        if (!finished &&
            total > 0 &&
            p.inSeconds * 100 >=
                total * store.integer('PercentageToMarkComplete') &&
            total - p.inSeconds < 600) {
          finished = true;
          unawaited(save(completed: true));
        }
      }),
      player.stream.duration.listen((d) {
        if (mounted && d > Duration.zero) setState(() => duration = d);
      }),
      player.stream.playing.listen((playing) {
        if (!mounted || busy) return;
        setState(() {});
        if (started) scrobble(playing ? 'start' : 'pause');
        showPresence();
      }),
      player.stream.completed.listen((done) {
        if (done && !busy) unawaited(ended());
      }),
      player.stream.error.listen((message) {
        if (!mounted || busy || duration > Duration.zero) return;
        unawaited(anotherCopy());
      }),
    ]);
    saveTimer = Timer.periodic(const Duration(seconds: 15), (_) {
      if (!busy && duration > Duration.zero) unawaited(save());
      showPresence();
    });
    unawaited(open());
  }

  /// The Discord status on desktop.
  void showPresence() {
    if (busy || duration <= Duration.zero) return;
    unawaited(
      widget.services.presence.movie(
        request.title,
        request.subtitle,
        paused: !player.state.playing,
        position: player.state.position.inMilliseconds / 1000,
        duration: duration.inMilliseconds / 1000,
      ),
    );
  }

  @override
  void dispose() {
    unawaited(widget.services.presence.clear());
    scope.cancel();
    hideTimer?.cancel();
    saveTimer?.cancel();
    if (!busy && duration > Duration.zero) {
      unawaited(save(completed: finished));
      if (!finished) scrobble('pause');
    }
    for (final s in subscriptions) {
      unawaited(s.cancel());
    }
    unawaited(player.dispose());
    unawaited(widget.services.core.stopPlayback().catchError((_) {}));
    if (fullscreen) unawaited(setDesktopFullScreen(false));
    surface.dispose();
    playFocus.dispose();
    flash.dispose();
    super.dispose();
  }

  double get percent => duration.inMilliseconds <= 0
      ? 0
      : position.inMilliseconds * 100 / duration.inMilliseconds;

  void scrobble(String action) =>
      request.onScrobble?.call(action, action == 'stop' ? 100 : percent);

  Future<void> save({bool completed = false}) async {
    await request.onProgress?.call(
      position.inSeconds,
      duration.inSeconds,
      completed,
    );
  }

  Future<void> property(String name, String value) async {
    final native = player.platform;
    if (native is NativePlayer) await native.setProperty(name, value);
  }

  Future<void> configure() async {
    await property('sub-font', store.text('SubtitleFont'));
    await property('sub-font-size', store.integer('SubtitleSize').toString());
    await property('sub-color', store.text('SubtitleColour'));
    await property('sub-back-color', store.text('SubtitleBackground'));
    await property(
      'sub-border-size',
      store.integer('SubtitleOutline').toString(),
    );
    await property('sub-pos', store.integer('SubtitlePosition').toString());
    await property('af', audioFilter(store));
    final quality = store.text('Quality');
    await property('hls-bitrate', quality == 'worst' ? 'min' : 'max');
    for (final e in (await tlsOptions()).entries) {
      await property(e.key, e.value);
    }
  }

  Future<void> open() async {
    final g = ++generation;
    scope.cancel();
    scope = RequestScope();
    setState(() {
      busy = true;
      error = '';
      overlay = true;
      finished = false;
      started = false;
      subPicked = false;
      position = Duration.zero;
      duration = Duration.zero;
      status = 'Finding a copy';
    });
    try {
      await player.stop();
      final found = await request.open(failed, scope, (s) {
        if (mounted && g == generation) setState(() => status = s);
      });
      if (!mounted || g != generation) return;
      media = found;
      setState(() => status = 'Loading');
      await configure();
      final local = !found.url.startsWith('http');
      await player.open(
        Media(
          local ? Uri.file(found.url).toString() : found.url,
          httpHeaders: found.headers,
        ),
        play: false,
      );
      if (player.state.duration == Duration.zero) {
        try {
          await player.stream.duration
              .firstWhere((d) => d > Duration.zero)
              .timeout(const Duration(seconds: 25));
        } on TimeoutException {
          /* Play anyway; a live stream may never report a length. */
        }
      }
      if (!mounted || g != generation) return;
      final resume =
          request.resumeAt?.call(player.state.duration.inSeconds) ?? 0;
      if (store.flag('ResumePlayback') &&
          resume > 0 &&
          resume < player.state.duration.inSeconds - 30) {
        await player.seek(Duration(seconds: resume));
      }
      await pickTracks(found, g);
      if (!mounted || g != generation) return;
      await player.play();
      started = true;
      scrobble('start');
      setState(() => busy = false);
      showControls();
    } on CoreFailure catch (e) {
      if (e.code == 'cancelled') return;
      if (mounted && g == generation) {
        setState(() {
          busy = false;
          error = e.message;
        });
      }
    } catch (_) {
      if (mounted && g == generation) {
        setState(() {
          busy = false;
          error = 'This title could not be opened.';
        });
      }
    }
  }

  /// The audio the source names (a Lithuanian dub), else the audio language
  /// order; then subtitles in the subtitle language order, unless the audio
  /// is already in the first of them.
  Future<void> pickTracks(PlayableMedia m, int g) async {
    final audio = player.state.tracks.audio
        .where((t) => t.id != 'auto' && t.id != 'no')
        .toList();
    final wanted = [
      ...m.audioLanguage
          .split(',')
          .map((s) => s.trim())
          .where((s) => s.isNotEmpty),
      ...store.text('AudioLanguages').split(','),
    ];
    for (final language in wanted) {
      final match = audio.where(
        (t) => languageMatches(t.language, t.title, language),
      );
      if (match.isNotEmpty) {
        await player.setAudioTrack(match.first);
        break;
      }
    }
    final preferences = store
        .text('SubsLanguage')
        .toLowerCase()
        .split(',')
        .map((v) => v.trim())
        .where((v) => v.isNotEmpty)
        .toList();
    if (preferences.isEmpty ||
        preferences.first == 'off' ||
        preferences.first == 'none') {
      await player.setSubtitleTrack(SubtitleTrack.no());
      return;
    }
    final dubbed =
        m.audioLanguage.isNotEmpty &&
        m.audioLanguage
            .split(',')
            .any((code) => languageMatches(code, '', preferences.first));
    if (dubbed) {
      await player.setSubtitleTrack(SubtitleTrack.no());
      return;
    }
    for (final preferred in preferences) {
      final embedded = player.state.tracks.subtitle.where(
        (t) =>
            t.id != 'auto' &&
            t.id != 'no' &&
            languageMatches(t.language, t.title, preferred),
      );
      if (embedded.isNotEmpty) {
        await player.setSubtitleTrack(embedded.first);
        return;
      }
      for (final track in m.subtitles.where(
        (t) => languageMatches(
          t['language'] as String?,
          t['label'] as String?,
          preferred,
        ),
      )) {
        if (await loadSubtitle(track, g)) return;
      }
    }
  }

  Future<bool> loadSubtitle(Json track, int g) async {
    final url = (track['url'] ?? track['path']) as String? ?? '';
    try {
      String text;
      if (url.startsWith('http')) {
        final response = await widget.services.catalog.client
            .get(Uri.parse(url), headers: media?.headers ?? const {})
            .timeout(const Duration(seconds: 15));
        if (response.statusCode != 200) return false;
        text = utf8.decode(response.bodyBytes, allowMalformed: true);
      } else {
        text = utf8.decode(
          await File(url.replaceFirst('file://', '')).readAsBytes(),
          allowMalformed: true,
        );
      }
      if (!mounted || g != generation) return false;
      await player.setSubtitleTrack(
        SubtitleTrack.data(
          text,
          title: track['label'] as String? ?? 'Subtitles',
          language: track['language'] as String?,
        ),
      );
      subPicked = true;
      return true;
    } catch (_) {
      return false;
    }
  }

  Future<void> ended() async {
    if (!finished) {
      finished = true;
      await save(completed: true);
    }
    scrobble('stop');
    final next = request.next?.call();
    if (!mounted) return;
    if (next == null) {
      Navigator.maybePop(context);
      return;
    }
    request = next;
    failed.clear();
    await open();
  }

  Future<void> playNext() async {
    final next = request.next?.call();
    if (next == null) return;
    if (!busy && duration > Duration.zero) {
      await save(completed: finished);
      if (!finished) scrobble('pause');
    }
    request = next;
    failed.clear();
    await open();
  }

  Future<void> anotherCopy() async {
    final copy = media?.copy ?? '';
    if (copy.isEmpty) {
      setState(() {
        busy = false;
        error = 'This file could not be played.';
      });
      return;
    }
    failed.add(copy);
    await open();
  }

  void showControls() {
    hideTimer?.cancel();
    if (!overlay) setState(() => overlay = true);
    hideTimer = Timer(const Duration(seconds: 4), () {
      if (mounted && !busy && error.isEmpty && player.state.playing) {
        setState(() => overlay = false);
        surface.requestFocus();
      }
    });
  }

  Future<void> seekBy(int seconds) async {
    final target = position + Duration(seconds: seconds);
    final clamped = Duration(
      milliseconds: target.inMilliseconds.clamp(
        0,
        math.max(0, duration.inMilliseconds - 1000),
      ),
    );
    setState(() => position = clamped);
    await player.seek(clamped);
    showControls();
  }

  Future<void> toggleFullscreen() async {
    final on = await setDesktopFullScreen(!fullscreen);
    if (mounted) setState(() => fullscreen = on);
  }

  Future<void> trackPanel() async {
    hideTimer?.cancel();
    final tracks = player.state.tracks;
    final external = media?.subtitles ?? const [];
    final choice = await showModalBottomSheet<(String, Object?)>(
      context: context,
      backgroundColor: panel,
      builder: (context) => SafeArea(
        child: ListView(
          shrinkWrap: true,
          padding: const EdgeInsets.symmetric(vertical: 12),
          children: [
            const Padding(
              padding: EdgeInsets.fromLTRB(24, 8, 24, 8),
              child: SectionLabel('Audio'),
            ),
            for (final t in tracks.audio.where(
              (t) => t.id != 'auto' && t.id != 'no',
            ))
              ChoiceRow(
                title: Text(t.title ?? t.language ?? 'Track ${t.id}'),
                trailing: player.state.track.audio.id == t.id
                    ? const Icon(LucideIcons.check, color: shu)
                    : null,
                onTap: () => Navigator.pop(context, ('audio', t)),
              ),
            const Padding(
              padding: EdgeInsets.fromLTRB(24, 16, 24, 8),
              child: SectionLabel('Subtitles'),
            ),
            ChoiceRow(
              title: const Text('Off'),
              trailing: player.state.track.subtitle.id == 'no'
                  ? const Icon(LucideIcons.check, color: shu)
                  : null,
              onTap: () => Navigator.pop(context, ('sub', SubtitleTrack.no())),
            ),
            for (final t in tracks.subtitle.where(
              (t) => t.id != 'auto' && t.id != 'no',
            ))
              ChoiceRow(
                title: Text(t.title ?? t.language ?? 'Track ${t.id}'),
                trailing: player.state.track.subtitle.id == t.id
                    ? const Icon(LucideIcons.check, color: shu)
                    : null,
                onTap: () => Navigator.pop(context, ('sub', t)),
              ),
            for (final t in external)
              ChoiceRow(
                title: Text(
                  [t['language'], t['label']]
                      .whereType<String>()
                      .where((s) => s.isNotEmpty)
                      .toSet()
                      .join(' · '),
                ),
                onTap: () => Navigator.pop(context, ('file', t)),
              ),
          ],
        ),
      ),
    );
    if (choice == null) return;
    switch (choice) {
      case ('audio', final AudioTrack t):
        await player.setAudioTrack(t);
      case ('sub', final SubtitleTrack t):
        await player.setSubtitleTrack(t);
      case ('file', final Json t):
        await loadSubtitle(t, generation);
    }
    showControls();
  }

  KeyEventResult key(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent && event is! KeyRepeatEvent) {
      return KeyEventResult.ignored;
    }
    if (isDesktop) {
      final action = playerActionForEvent(event);
      if (action != null && desktopKey(action, event is KeyRepeatEvent)) {
        return KeyEventResult.handled;
      }
    }
    final k = event.logicalKey;
    final onSurface = surface.hasPrimaryFocus || !overlay;
    if (k == LogicalKeyboardKey.escape && fullscreen) {
      unawaited(toggleFullscreen());
      return KeyEventResult.handled;
    }
    if (busy || error.isNotEmpty) return KeyEventResult.ignored;
    if (k == LogicalKeyboardKey.space ||
        k == LogicalKeyboardKey.mediaPlayPause ||
        (onSurface &&
            (k == LogicalKeyboardKey.select ||
                k == LogicalKeyboardKey.enter))) {
      unawaited(player.playOrPause());
      showControls();
      return KeyEventResult.handled;
    }
    if (onSurface &&
        (k == LogicalKeyboardKey.arrowLeft ||
            k == LogicalKeyboardKey.arrowRight)) {
      unawaited(seekBy(k == LogicalKeyboardKey.arrowLeft ? -10 : 10));
      return KeyEventResult.handled;
    }
    if (k == LogicalKeyboardKey.keyF && isDesktop) {
      unawaited(toggleFullscreen());
      return KeyEventResult.handled;
    }
    if (onSurface &&
        (k == LogicalKeyboardKey.arrowUp ||
            k == LogicalKeyboardKey.arrowDown)) {
      showControls();
      playFocus.requestFocus();
      return KeyEventResult.handled;
    }
    showControls();
    return KeyEventResult.ignored;
  }

  final flash = FlashText();
  double lastVolume = 100;

  static const _repeatable = {
    PlayerAction.seekBack,
    PlayerAction.seekForward,
    PlayerAction.stepBack,
    PlayerAction.stepForward,
    PlayerAction.volumeUp,
    PlayerAction.volumeDown,
    PlayerAction.volumeUpSmall,
    PlayerAction.volumeDownSmall,
    PlayerAction.frameBack,
    PlayerAction.frameForward,
    PlayerAction.slower,
    PlayerAction.faster,
    PlayerAction.subtitleEarlier,
    PlayerAction.subtitleLater,
  };

  /// The old mpv keys on desktop; false passes the key on.
  bool desktopKey(PlayerAction action, bool repeat) {
    if (repeat && !_repeatable.contains(action)) return true;
    switch (action) {
      case PlayerAction.fullscreen:
        unawaited(toggleFullscreen());
        return true;
      case PlayerAction.leaveFullscreen:
        if (!fullscreen) return false;
        unawaited(toggleFullscreen());
        return true;
      case PlayerAction.quit:
        Navigator.maybePop(context);
        return true;
      case PlayerAction.keys:
        unawaited(keysPanel());
        return true;
      default:
    }
    if (busy || error.isNotEmpty) return false;
    switch (action) {
      case PlayerAction.playPause:
        unawaited(player.playOrPause());
        showControls();
      case PlayerAction.seekBack || PlayerAction.seekForward:
        unawaited(seekBy(action == PlayerAction.seekBack ? -5 : 5));
      case PlayerAction.stepBack || PlayerAction.stepForward:
        unawaited(seekBy(action == PlayerAction.stepBack ? -1 : 1));
      case PlayerAction.volumeUp ||
          PlayerAction.volumeDown ||
          PlayerAction.volumeUpSmall ||
          PlayerAction.volumeDownSmall:
        final step = switch (action) {
          PlayerAction.volumeUp => 5,
          PlayerAction.volumeDown => -5,
          PlayerAction.volumeUpSmall => 2,
          _ => -2,
        };
        final level = (player.state.volume + step).clamp(0, 100).round();
        unawaited(player.setVolume(level.toDouble()));
        flash.show('Volume $level%');
      case PlayerAction.mute:
        final volume = player.state.volume;
        if (volume > 0) lastVolume = volume;
        unawaited(player.setVolume(volume > 0 ? 0 : lastVolume));
        flash.show(volume > 0 ? 'Muted' : 'Sound on');
      case PlayerAction.nextEpisode:
        if (request.next == null) {
          flash.show('Nothing plays next');
        } else {
          unawaited(playNext());
        }
      // Films have no skip times, episode list or party here.
      case PlayerAction.skipSegment ||
          PlayerAction.previousEpisode ||
          PlayerAction.markOpening ||
          PlayerAction.markEnding ||
          PlayerAction.sendMarks ||
          PlayerAction.forgetMarks ||
          PlayerAction.upvoteSkip ||
          PlayerAction.downvoteSkip ||
          PlayerAction.partySkip ||
          PlayerAction.partyNext:
        return false;
      default:
        unawaited(mediaKey(action));
    }
    return true;
  }

  Future<void> mediaKey(PlayerAction action) async {
    final note = switch (action) {
      PlayerAction.nextSubtitle => await player.cycleSubtitles(),
      PlayerAction.previousSubtitle => await player.cycleSubtitles(back: true),
      PlayerAction.toggleSubtitles => await player.toggleSubtitles(),
      PlayerAction.nextAudio => await player.cycleAudio(),
      PlayerAction.subtitleEarlier => await player.shiftSubtitles(-.1),
      PlayerAction.subtitleLater => await player.shiftSubtitles(.1),
      PlayerAction.slower ||
      PlayerAction.faster ||
      PlayerAction.halfSpeed ||
      PlayerAction.doubleSpeed ||
      PlayerAction.normalSpeed => await player.changeSpeed(action),
      PlayerAction.screenshot => await player.saveScreenshot(request.title),
      _ => '',
    };
    switch (action) {
      case PlayerAction.nextSubtitle ||
          PlayerAction.previousSubtitle ||
          PlayerAction.toggleSubtitles:
        subPicked = true;
      case PlayerAction.frameBack || PlayerAction.frameForward:
        await player.stepFrame(back: action == PlayerAction.frameBack);
      case PlayerAction.previousChapter || PlayerAction.nextChapter:
        await player.stepChapter(
          action == PlayerAction.previousChapter ? -1 : 1,
        );
      default:
    }
    if (note.isNotEmpty && mounted) flash.show(note);
  }

  Future<void> keysPanel() async {
    hideTimer?.cancel();
    await showPlayerKeys(context, episodes: false);
    if (mounted) showControls();
  }

  String time(Duration d) => DesktopPlayerBar.time(d);

  Widget mobileControls(BuildContext context) {
    final playing = player.state.playing;
    final total = math.max(1, duration.inMilliseconds).toDouble();
    return Stack(
      children: [
        const Positioned.fill(
          child: IgnorePointer(
            child: DecoratedBox(
              decoration: BoxDecoration(
                gradient: LinearGradient(
                  begin: Alignment.topCenter,
                  end: Alignment.bottomCenter,
                  stops: [0, .25, .6, 1],
                  colors: [
                    Color(0xb3000000),
                    Color(0x00000000),
                    Color(0x00000000),
                    Color(0xd9000000),
                  ],
                ),
              ),
            ),
          ),
        ),
        SafeArea(
          child: Padding(
            padding: EdgeInsets.symmetric(
              horizontal: widget.isTV ? 48 : 16,
              vertical: widget.isTV ? 27 : 8,
            ),
            child: Column(
              children: [
                Row(
                  children: [
                    IconButton(
                      tooltip: 'Back',
                      onPressed: () => Navigator.maybePop(context),
                      icon: const Icon(LucideIcons.arrowLeft, color: fgBright),
                    ),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            request.title,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: uiStyle(
                              context,
                              size: 18,
                              weight: FontWeight.w600,
                              color: fgBright,
                            ),
                          ),
                          Text(
                            [
                              request.subtitle,
                              media?.label ?? '',
                            ].where((s) => s.isNotEmpty).join(' · '),
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: uiStyle(context, size: 15, color: muted),
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
                const Spacer(),
                Row(
                  children: [
                    Text(time(position), style: mono.copyWith(color: fg)),
                    Expanded(
                      child: Slider(
                        value: position.inMilliseconds
                            .clamp(0, total)
                            .toDouble(),
                        max: total,
                        activeColor: shu,
                        inactiveColor: line,
                        onChangeStart: (_) {
                          scrubbing = true;
                          hideTimer?.cancel();
                        },
                        onChanged: (v) => setState(
                          () => position = Duration(milliseconds: v.round()),
                        ),
                        onChangeEnd: (v) {
                          scrubbing = false;
                          unawaited(
                            player.seek(Duration(milliseconds: v.round())),
                          );
                          showControls();
                        },
                      ),
                    ),
                    Text(time(duration), style: mono.copyWith(color: fg)),
                  ],
                ),
                const SizedBox(height: 8),
                Row(
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: [
                    TransportButton(
                      label: 'Back 10 seconds',
                      icon: LucideIcons.rotateCcw,
                      onPressed: () => unawaited(seekBy(-10)),
                    ),
                    const SizedBox(width: 16),
                    TransportButton(
                      label: playing ? 'Pause' : 'Play',
                      icon: playing ? LucideIcons.pause : LucideIcons.play,
                      primary: true,
                      focusNode: playFocus,
                      onPressed: () {
                        unawaited(player.playOrPause());
                        showControls();
                      },
                    ),
                    const SizedBox(width: 16),
                    TransportButton(
                      label: 'Forward 10 seconds',
                      icon: LucideIcons.rotateCw,
                      onPressed: () => unawaited(seekBy(10)),
                    ),
                    const SizedBox(width: 16),
                    TransportButton(
                      label: 'Audio and subtitles',
                      icon: LucideIcons.captions,
                      onPressed: () => unawaited(trackPanel()),
                    ),
                    if ((media?.copy ?? '').isNotEmpty) ...[
                      const SizedBox(width: 16),
                      TransportButton(
                        label: 'Try another copy',
                        icon: LucideIcons.refreshCw,
                        onPressed: () => unawaited(anotherCopy()),
                      ),
                    ],
                    if (request.next != null) ...[
                      const SizedBox(width: 16),
                      TransportButton(
                        label: 'Next episode',
                        icon: LucideIcons.skipForward,
                        onPressed: () => unawaited(playNext()),
                      ),
                    ],
                  ],
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }

  Widget desktopControls() => DesktopPlayerBar(
    title: request.title,
    subtitle: [
      request.subtitle,
      media?.label ?? '',
    ].where((s) => s.isNotEmpty).join(' · '),
    position: position,
    duration: duration,
    buffered: player.state.buffer,
    playing: player.state.playing,
    buffering: player.state.buffering,
    silent: player.state.volume == 0,
    fullscreen: fullscreen,
    trackLabel: 'Tracks',
    centerLabel: request.subtitle,
    stripes: const [],
    playFocus: playFocus,
    onBack: () => Navigator.maybePop(context),
    onPlayPause: () {
      unawaited(player.playOrPause());
      showControls();
    },
    onSeekBy: (s) => unawaited(seekBy(s)),
    onSeekStart: (_) {
      scrubbing = true;
      hideTimer?.cancel();
    },
    onSeekChanged: (v) =>
        setState(() => position = Duration(milliseconds: v.round())),
    onSeekEnd: (v) {
      scrubbing = false;
      unawaited(player.seek(Duration(milliseconds: v.round())));
      showControls();
    },
    onNext: request.next != null ? () => unawaited(playNext()) : null,
    onMute: () =>
        unawaited(player.setVolume(player.state.volume == 0 ? 100 : 0)),
    onTracks: () => unawaited(trackPanel()),
    onQuality: () => unawaited(anotherCopy()),
    onFullscreen: () => unawaited(toggleFullscreen()),
  );

  @override
  Widget build(BuildContext context) => Scaffold(
    backgroundColor: bgDeep,
    body: Focus(
      skipTraversal: true,
      onKeyEvent: key,
      child: MouseRegion(
        onHover: isDesktop ? (_) => showControls() : null,
        cursor: isDesktop && !overlay && !busy && error.isEmpty
            ? SystemMouseCursors.none
            : MouseCursor.defer,
        child: Stack(
          fit: StackFit.expand,
          children: [
            GestureDetector(
              onTap: () {
                if (overlay && !busy) {
                  setState(() => overlay = false);
                  surface.requestFocus();
                } else {
                  showControls();
                }
              },
              onDoubleTap: isDesktop
                  ? () => unawaited(toggleFullscreen())
                  : null,
              child: Focus(
                focusNode: surface,
                autofocus: true,
                child: Video(controller: video, controls: NoVideoControls),
              ),
            ),
            if (busy)
              Center(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    OtakaseSeal(size: widget.isTV ? 96 : 64),
                    const SizedBox(height: 20),
                    Text(status, style: uiStyle(context, color: muted)),
                    const SizedBox(height: 16),
                    TextButton(
                      autofocus: true,
                      onPressed: () => Navigator.maybePop(context),
                      child: const Text('Cancel'),
                    ),
                  ],
                ),
              )
            else if (error.isNotEmpty)
              Center(
                child: Padding(
                  padding: const EdgeInsets.all(32),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(
                        error,
                        textAlign: TextAlign.center,
                        style: uiStyle(context, color: fg),
                      ),
                      const SizedBox(height: 20),
                      Wrap(
                        spacing: 12,
                        children: [
                          AppButton(
                            label: 'Try again',
                            icon: LucideIcons.refreshCw,
                            filled: true,
                            autofocus: true,
                            isTV: widget.isTV,
                            onPressed: () {
                              failed.clear();
                              unawaited(open());
                            },
                          ),
                          AppButton(
                            label: 'Back',
                            isTV: widget.isTV,
                            onPressed: () => Navigator.maybePop(context),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
              )
            else if (overlay)
              FocusTraversalGroup(
                child: isDesktop ? desktopControls() : mobileControls(context),
              ),
            ValueListenableBuilder<String>(
              valueListenable: flash,
              builder: (_, text, _) => PlayerFlash(text: text),
            ),
          ],
        ),
      ),
    ),
  );
}
