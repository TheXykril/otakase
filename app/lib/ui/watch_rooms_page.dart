import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';

import '../data/models.dart';
import '../desktop/desktop_core.dart';
import '../desktop/player_bar.dart';
import '../desktop/window_frame.dart';
import '../data/store.dart';
import '../data/watch_rooms.dart';
import '../services.dart';
import '../tls_ca.dart';
import 'components.dart';
import 'device_status.dart';
import 'mpv.dart';
import 'phone_player.dart';
import 'player_keys.dart';
import 'seal.dart';

class WatchRoomsPage extends StatefulWidget {
  const WatchRoomsPage({super.key, required this.services, required this.isTV});
  final AppServices services;
  final bool isTV;
  @override
  State<WatchRoomsPage> createState() => _WatchRoomsPageState();
}

class _WatchRoomsPageState extends State<WatchRoomsPage> {
  bool joining = false;
  String? error;
  @override
  void initState() {
    super.initState();
    if (widget.services.discord.linked) {
      unawaited(widget.services.watchRooms.load());
    }
  }

  Future<void> join(Json room) async {
    setState(() {
      joining = true;
      error = null;
    });
    try {
      await widget.services.stopPlayback?.call();
      await widget.services.watchRooms.join(room);
      if (!mounted ||
          widget.services.closed ||
          widget.services.watchRooms.state == null) {
        return;
      }
      await Navigator.push(
        context,
        OtakaseRoute<void>(
          builder: (_) =>
              WatchRoomPlayer(services: widget.services, isTV: widget.isTV),
        ),
      );
      if (mounted) await widget.services.watchRooms.load();
    } catch (e) {
      if (mounted) setState(() => error = e.toString());
    } finally {
      if (mounted) setState(() => joining = false);
    }
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: widget.services.watchRooms,
    builder: (context, _) {
      final r = widget.services.watchRooms;
      return Scaffold(
        appBar: AppBar(
          title: const Text('Watch rooms'),
          leading: widget.isTV
              ? IconButton(
                  autofocus: !widget.services.discord.linked,
                  tooltip: MaterialLocalizations.of(context).backButtonTooltip,
                  icon: const BackButtonIcon(),
                  onPressed: () => Navigator.maybePop(context),
                )
              : null,
          actions: [
            DeviceStatus(isTV: widget.isTV),
            const SizedBox(width: 16),
          ],
        ),
        body: ListView(
          padding: const EdgeInsets.all(24),
          children: [
            const PageHeading('Together, in step', '共'),
            const SizedBox(height: 20),
            Text(
              widget.services.discord.linked
                  ? 'Join a live room. The host controls playback. Ask for a pause, skip or next episode.'
                  : 'Link this profile in More > Discord to join a watch room.',
              style: uiStyle(context, color: muted),
            ),
            const SizedBox(height: 18),
            if (widget.services.discord.linked)
              AppButton(
                label: r.busy ? 'Looking for rooms…' : 'Refresh rooms',
                autofocus: true,
                isTV: widget.isTV,
                onPressed: r.busy || joining ? null : r.load,
              ),
            if (!r.busy && r.rooms.isEmpty && widget.services.discord.linked)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 20),
                child: Text(
                  'No rooms available to join right now.',
                  style: uiStyle(context),
                ),
              ),
            for (final room in r.rooms)
              Padding(
                padding: const EdgeInsets.only(top: 20),
                child: FocusBox(
                  key: ValueKey('room-${room['id']}'),
                  isTV: widget.isTV,
                  onPressed:
                      joining || number(room['people']) >= number(room['max'])
                      ? null
                      : () => join(room),
                  padding: const EdgeInsets.all(12),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        room['show'] as String? ?? 'Private watch room',
                        style: uiStyle(
                          context,
                          size: 22,
                          weight: FontWeight.w600,
                        ),
                      ),
                      const SizedBox(height: 8),
                      if (number(room['ep']) > 0)
                        Text(
                          'Episode ${room['ep']} · ${room['playing'] == true ? 'Playing' : 'Paused'}',
                          style: uiStyle(context),
                        ),
                      Text(
                        '${room['people'] ?? 0}/${room['max'] ?? 0} watching · ${room['channel_name'] ?? 'Discord'}',
                        style: uiStyle(context, color: muted),
                      ),
                      Text(
                        number(room['people']) >= number(room['max'])
                            ? 'Full'
                            : joining
                            ? 'Joining…'
                            : 'Join room',
                        style: uiStyle(context, color: accentText),
                      ),
                    ],
                  ),
                ),
              ),
            if (error != null || r.error != null)
              Padding(
                padding: const EdgeInsets.only(top: 20),
                child: Text(
                  error ?? r.error!,
                  style: uiStyle(context, color: warn),
                ),
              ),
          ],
        ),
      );
    },
  );
}

/// The room's opening and ending, once the bot has checked them against
/// this episode, as stripes on the player's timeline.
List<SkipSpan> roomSkipSpans(Json room) {
  if (room['skip_ready'] != true || decimal(room['dur']) <= 0) return const [];
  final times = room['skip_times'] as Map? ?? {};
  return [
    for (final type in ['op', 'ed'])
      if (times[type] case final Map span
          when decimal(span['end']) > decimal(span['start']))
        SkipSpan(type, decimal(span['start']), decimal(span['end'])),
  ];
}

/// What a member asked the host for, in words, after "asks to".
String roomAskText(Map ask) => switch (ask['kind']) {
  'forward' when decimal(ask['by']) < 0 =>
    'go back ${-decimal(ask['by']).round()} s',
  'forward' => 'skip ${decimal(ask['by']).round()} s',
  'seek' =>
    'jump to ${DesktopPlayerBar.time(Duration(milliseconds: (decimal(ask['pos']) * 1000).round()))}',
  'next' => 'play the next episode',
  final kind => '$kind',
};

class WatchRoomPlayer extends StatefulWidget {
  const WatchRoomPlayer({
    super.key,
    required this.services,
    required this.isTV,
  });
  final AppServices services;
  final bool isTV;
  @override
  State<WatchRoomPlayer> createState() => _WatchRoomPlayerState();
}

class _WatchRoomPlayerState extends State<WatchRoomPlayer>
    with WidgetsBindingObserver {
  WatchRooms get rooms => widget.services.watchRooms;
  late final Player player;
  late final VideoController video;
  late final Future<void> Function() stopHandler;
  late final Future<void> Function(String, Json) commandHandler;
  late final Json Function() statusHandler;
  final surface = FocusNode(debugLabel: 'Watch room video');
  final playFocus = FocusNode(debugLabel: 'Watch room play');
  final flash = FlashText();
  final subscriptions = <StreamSubscription<dynamic>>[];
  Timer? followTimer, beatTimer, saveTimer, hideTimer;
  RoomState? view;
  bool busy = true,
      ending = false,
      overlay = true,
      applying = false,
      beating = false,
      subSelected = false;
  bool foreground = true, manualSubs = false;
  bool scrubbing = false, panelOpen = false, fullscreen = false;

  /// Where the timeline thumb sits while it is dragged.
  Duration? scrubTo;
  double lastVolume = 100;
  int generation = 0, readySent = 0;
  String media = '', subs = '';
  String? error;
  double behind = 0;
  Future<void>? closing;
  @override
  void initState() {
    super.initState();
    FullBleed.enter();
    WidgetsBinding.instance.addObserver(this);
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
    stopHandler = stopForProfile;
    commandHandler = control;
    statusHandler = status;
    widget.services.stopPlayback = stopHandler;
    widget.services.playerCommand = commandHandler;
    widget.services.playerStatus = statusHandler;
    rooms.addListener(changed);
    subscriptions.addAll([
      player.stream.position.listen((p) {
        if (!ending && !busy && view?.anime != null) {
          widget.services.discord.observe(
            view!.anime!,
            view!.episode,
            p.inMilliseconds / 1000,
          );
          if (mounted && overlay) setState(() {});
        }
      }),
      player.stream.duration.listen((d) {
        if (d > Duration.zero && !subSelected) unawaited(subtitles());
      }),
      player.stream.error.listen((e) {
        if (mounted && player.state.duration == Duration.zero) {
          setState(
            () => error = 'Playback is unavailable. Retry or leave the room.',
          );
        }
      }),
      player.stream.completed.listen((done) {
        if (done && rooms.state?.drives == true) {
          unawaited(run(() => rooms.control('next')));
        }
      }),
    ]);
    followTimer = Timer.periodic(
      const Duration(milliseconds: 500),
      (_) => apply(),
    );
    beatTimer = Timer.periodic(const Duration(seconds: 3), (_) => beat());
    saveTimer = Timer.periodic(const Duration(seconds: 10), (_) => save());
    unawaited(
      SystemChrome.setEnabledSystemUIMode(SystemUiMode.immersiveSticky),
    );
    if (!widget.isTV) {
      unawaited(
        SystemChrome.setPreferredOrientations([
          DeviceOrientation.landscapeLeft,
          DeviceOrientation.landscapeRight,
        ]),
      );
    }
    changed();
    reveal();
  }

  void changed() {
    if (!mounted || ending) return;
    final next = rooms.state;
    if (next == null) {
      unawaited(exit());
      return;
    }
    if (next.phase == 'over') {
      unawaited(save());
      view = next;
      busy = false;
      setState(() => error = 'The watch room is over. Thanks for watching!');
      unawaited(player.pause());
      return;
    }
    final old = view;
    if (old != null &&
        (old.episode != next.episode ||
            old.anime?.key != next.anime?.key ||
            effectiveMedia(old) != effectiveMedia(next))) {
      unawaited(save());
    }
    final force =
        old == null ||
        next.jumpedFrom(old) ||
        old.hold != next.hold ||
        number(old.json['skip_seq']) != number(next.json['skip_seq']);
    view = next;
    if (next.phase != 'play') {
      generation++;
      media = '';
      busy = true;
      unawaited(player.pause());
      setState(() {});
      return;
    }
    final desired = effectiveMedia(next);
    if (desired != media) {
      unawaited(load(desired));
    } else {
      if (subs != jsonEncode(next.json['subs'])) unawaited(subtitles());
      unawaited(apply(force: force));
    }
    setState(() {});
  }

  String effectiveMedia(RoomState state) {
    final audio = (state.json['audio_media'] as Map? ?? {});
    const codes = {
      'japanese': 'ja',
      'english': 'en',
      'chinese': 'zh',
      'korean': 'ko',
      'spanish': 'es',
      'french': 'fr',
      'german': 'de',
    };
    for (final language
        in widget.services.store
            .text('AudioLanguages')
            .split(',')
            .map((s) => s.trim().toLowerCase())) {
      final chosen = audio[codes[language] ?? language];
      if (chosen is String && chosen.isNotEmpty) return chosen;
    }
    return state.media;
  }

  Future<void> configure() async {
    if (!player.isMpv) return;
    final s = widget.services.store;
    for (final entry in {
      'sub-font': s.text('SubtitleFont'),
      'sub-font-size': '${s.integer('SubtitleSize')}',
      'sub-color': s.text('SubtitleColour'),
      'sub-back-color': s.text('SubtitleBackground'),
      'sub-border-size': '${s.integer('SubtitleOutline')}',
      'sub-pos': '${s.integer('SubtitlePosition')}',
      'sub-ass-override': s.text('SubStyle') == 'soft' ? 'no' : 'yes',
      'af': audioFilter(s),
      ...await tlsOptions(),
    }.entries) {
      await player.mpvSet(entry.key, entry.value);
    }
  }

  Future<void> load(String desired) async {
    final current = view;
    if (current == null || desired.isEmpty) return;
    final g = ++generation;
    media = desired;
    busy = true;
    subSelected = false;
    manualSubs = false;
    subs = '';
    error = null;
    if (mounted) setState(() {});
    try {
      await configure();
      await player.open(Media(rooms.mediaUri(desired).toString()), play: false);
      if (player.state.duration <= Duration.zero) {
        await player.stream.duration
            .firstWhere((d) => d > Duration.zero)
            .timeout(const Duration(seconds: 20));
      }
      if (!mounted || ending || g != generation) return;
      busy = false;
      await player.seek(
        Duration(
          milliseconds: ((view ?? current).target(DateTime.now()) * 1000)
              .round(),
        ),
      );
      await subtitles();
      await apply(force: true);
    } catch (e) {
      if (mounted && g == generation) {
        busy = false;
        error = 'The stream could not be opened. Retry or leave the room.';
      }
    }
    if (mounted && g == generation) setState(() {});
  }

  Future<void> subtitles() async {
    final current = view;
    if (current == null ||
        ending ||
        manualSubs ||
        player.state.duration <= Duration.zero) {
      return;
    }
    final tracks = (current.json['subs'] as List? ?? [])
        .whereType<Map>()
        .toList();
    subs = jsonEncode(tracks);
    final track = roomSubtitleTrack(
      tracks.map((t) => t.cast<String, dynamic>()).toList(),
      widget.services.store.text('SubsLanguage'),
    );
    if (track == null) {
      await player.setSubtitleTrack(SubtitleTrack.no());
      subSelected = true;
      return;
    }

    try {
      await player.setSubtitleTrack(
        SubtitleTrack.uri(
          rooms.mediaUri(track['url'] as String).toString(),
          title: track['label'] as String? ?? '',
          language: track['lang'] as String? ?? '',
        ),
      );
      subSelected = true;
    } catch (_) {
      /* Tracks also supports manual selection. */
    }
  }

  Future<void> apply({bool force = false}) async {
    final current = view;
    if (ending ||
        !foreground ||
        busy ||
        applying ||
        current == null ||
        current.phase != 'play' ||
        player.state.duration <= Duration.zero) {
      return;
    }
    applying = true;
    try {
      final target = current.target(DateTime.now());
      if (current.drives) {
        if (force &&
            (player.state.position.inMilliseconds / 1000 - target).abs() > .5) {
          await player.seek(Duration(milliseconds: (target * 1000).round()));
        }
      } else {
        final d = followRoom(
          position: player.state.position.inMilliseconds / 1000,
          target: target,
          playing: current.playing,
          force: force,
          hold: current.hold > 0,
          stalled: player.state.buffering,
        );
        if (d.seek != null) {
          await player.seek(Duration(milliseconds: (d.seek! * 1000).round()));
        }
        if (player.state.rate != d.rate) await player.setRate(d.rate);
        if (mounted && behind != d.behind) setState(() => behind = d.behind);
      }
      if (current.playing && !player.state.playing) {
        await player.play();
      } else if (!current.playing && player.state.playing) {
        await player.pause();
      }
      if (current.hold > 0 &&
          current.json['hold_ready'] != true &&
          readySent != current.hold &&
          !player.state.buffering &&
          (player.state.position.inMilliseconds / 1000 - current.position)
                  .abs() <=
              1.5) {
        readySent = current.hold;
        try {
          await rooms.ready(current.hold);
        } catch (_) {
          readySent = 0;
        }
      }
    } catch (e) {
      if (mounted) flash.show(message(e), long: true);
    } finally {
      applying = false;
    }
  }

  Future<void> beat() async {
    if (ending || busy || beating || view?.phase != 'play') return;
    beating = true;
    try {
      await rooms.beat(
        position: player.state.position.inMilliseconds / 1000,
        duration: player.state.duration.inMilliseconds / 1000,
        playing: player.state.playing,
        buffering: player.state.buffering,
      );
    } catch (_) {
      /* Polling also keeps this member present. */
    } finally {
      beating = false;
    }
  }

  Future<void> save({bool completed = false}) async {
    final a = view?.anime;
    if (a == null ||
        busy ||
        view!.episode < 1 ||
        player.state.duration <= Duration.zero ||
        widget.services.closed) {
      return;
    }
    await widget.services.store.progress(
      a,
      view!.episode,
      player.state.position.inSeconds,
      player.state.duration.inSeconds,
      completed: completed,
    );
  }

  Json status() => {
    'has_player': true,
    'room': true,
    'title': view?.anime?.title ?? 'Watch room',
    'adult': view?.adult ?? false,
    'episode': view?.episode ?? 0,
    'episodes': number(view?.json['last_ep']),
    'position': player.state.position.inSeconds,
    'duration': player.state.duration.inSeconds,
    'playing': player.state.playing,
    'volume': player.state.volume.round(),
  };
  Future<void> control(String action, Json args) async {
    if (ending) return;
    switch (action) {
      case 'play':
        await rooms.control('play');
      case 'pause':
        await rooms.control('pause');
      case 'toggle':
        await rooms.control(view?.playing == true ? 'pause' : 'play');
      case 'skip':
      case 'seek':
        await rooms.jumpBy(
          action == 'skip' ? 10 : number(args['seconds']).toDouble(),
        );
      case 'next':
        await rooms.control('next');
      case 'volume':
        await player.setVolume(number(args['volume']).clamp(0, 100).toDouble());
      case 'stop':
        await exit();
      case 'episode':
      case 'previous':
        throw StateError('Episode changes follow the room host.');
    }
    reveal();
  }

  /// Runs a room action. A failed one is a short note on the picture, not
  /// the error screen: the episode keeps playing.
  Future<bool> run(Future<void> Function() action) async {
    try {
      await action();
      return true;
    } catch (e) {
      if (mounted) flash.show(message(e), long: true);
      return false;
    }
  }

  static String message(Object e) => switch (e) {
    StateError(:final message) => message,
    _ => e.toString(),
  };

  /// Brings the controls up; they hide again after a while of playing.
  void reveal() {
    hideTimer?.cancel();
    if (!mounted || ending) return;
    setState(() => overlay = true);
    hideTimer = Timer(const Duration(seconds: 5), () {
      if (mounted && !panelOpen && !scrubbing && player.state.playing) {
        setState(() => overlay = false);
        surface.requestFocus();
      }
    });
  }

  void hideControls() {
    hideTimer?.cancel();
    setState(() => overlay = false);
    surface.requestFocus();
  }

  bool get hosting => view?.host == true;

  /// Runs a room action; a member only asks the host, so say so.
  Future<void> act(Future<void> Function() action, String asked) async {
    if (await run(action) && mounted && !hosting) flash.show('Asked to $asked');
    reveal();
  }

  Future<void> playPause() => act(
    () => control('toggle', {}),
    view?.playing == true ? 'pause' : 'play',
  );

  Future<void> seekBy(int seconds) => act(
    () => rooms.jumpBy(seconds.toDouble()),
    seconds < 0 ? 'go back ${-seconds} s' : 'skip $seconds s',
  );

  Future<void> seekTo(Duration to) => act(
    () => rooms.control('seek', position: to.inMilliseconds / 1000),
    'jump to ${DesktopPlayerBar.time(to)}',
  );

  Future<void> nextEpisode() =>
      act(() => rooms.control('next'), 'play the next episode');

  bool get canNext {
    final last = number(view?.json['last_ep']);
    return view?.phase == 'play' && (last == 0 || (view?.episode ?? 0) < last);
  }

  List<SkipSpan> get spans =>
      view == null ? const [] : roomSkipSpans(view!.json);

  String get roomLine {
    final current = view;
    final who = current == null
        ? ''
        : current.host
        ? 'Hosting'
        : 'Following ${hostName(current)}';
    return [
      if ((current?.episode ?? 0) > 0) 'Episode ${current!.episode}',
      who,
    ].where((s) => s.isNotEmpty).join(' · ');
  }

  String hostName(RoomState current) =>
      (current.json['members'] as List? ?? [])
              .whereType<Map>()
              .where((m) => m['host'] == true)
              .firstOrNull?['name']
          as String? ??
      'the host';

  Duration get shown => scrubTo ?? player.state.position;

  void toggleMute() {
    final volume = player.state.volume;
    if (volume > 0) lastVolume = volume;
    unawaited(player.setVolume(volume > 0 ? 0 : lastVolume));
    reveal();
  }

  Future<void> toggleFullscreen() async {
    final on = await setDesktopFullScreen(!fullscreen);
    if (mounted) setState(() => fullscreen = on);
    reveal();
  }

  Future<void> tracks() async {
    final tracks = (view?.json['subs'] as List? ?? [])
        .whereType<Map>()
        .toList();
    panelOpen = true;
    reveal();
    final choice = await chooseValue(context, 'Subtitles', [
      'Off',
      ...tracks.map((t) => t['label'] as String? ?? 'Subtitle'),
    ]);
    panelOpen = false;
    if (choice == null || ending) {
      if (mounted) playFocus.requestFocus();
      return;
    }
    manualSubs = true;
    if (choice == 'Off') {
      await player.setSubtitleTrack(SubtitleTrack.no());
      subSelected = true;
    } else {
      final t = tracks.firstWhere((t) => t['label'] == choice);
      if (player.state.duration > Duration.zero) {
        await player.setSubtitleTrack(
          SubtitleTrack.uri(
            rooms.mediaUri(t['url'] as String).toString(),
            title: choice,
          ),
        );
        subSelected = true;
      }
    }
    if (mounted) playFocus.requestFocus();
    reveal();
  }

  /// Who is in the room, progress sync, the host's auto skip switches and
  /// leaving: what the anime player has no button for.
  Future<void> roomPanel() async {
    panelOpen = true;
    reveal();
    var leaving = false;
    await showDialog<void>(
      context: context,
      builder: (dialog) => ListenableBuilder(
        listenable: rooms,
        builder: (dialog, _) {
          final current = rooms.state;
          final people = (current?.json['members'] as List? ?? [])
              .whereType<Map>()
              .toList();
          final linked = (current?.json['linked'] as List? ?? []).join(' + ');
          Widget toggle(String label, bool value, String on, String off) =>
              ChoiceRow(
                title: Text(label, style: uiStyle(dialog)),
                trailing: Switch(
                  value: value,
                  onChanged: (_) =>
                      unawaited(run(() => rooms.control(value ? off : on))),
                ),
                onTap: () =>
                    unawaited(run(() => rooms.control(value ? off : on))),
              );
          return AlertDialog(
            backgroundColor: panel,
            title: Text(
              'Watch room',
              style: uiStyle(dialog, size: 22, weight: FontWeight.w600),
            ),
            content: SizedBox(
              width: 520,
              child: SingleChildScrollView(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(roomLine, style: uiStyle(dialog, color: muted)),
                    const SizedBox(height: 12),
                    const Text('Watching', style: TextStyle(color: accentText)),
                    for (final p in people)
                      Padding(
                        padding: const EdgeInsets.symmetric(vertical: 6),
                        child: Text(
                          [
                            p['name'] as String? ?? 'Someone',
                            if (p['host'] == true) 'host',
                            if (number(p['behind']) > 0)
                              '${p['behind']} s behind'
                            else if (p['state'] == 'loading')
                              'loading',
                          ].join(' · '),
                          style: uiStyle(dialog),
                        ),
                      ),
                    if (current?.json['sync'] case final bool sync) ...[
                      const Divider(),
                      ChoiceRow(
                        autofocus: true,
                        title: Text(
                          'Sync room progress${linked.isEmpty ? '' : ' · $linked'}',
                          style: uiStyle(dialog),
                        ),
                        trailing: Switch(
                          value: sync,
                          onChanged: (v) => unawaited(run(() => rooms.sync(v))),
                        ),
                        onTap: () => unawaited(run(() => rooms.sync(!sync))),
                      ),
                    ],
                    if (current?.host == true &&
                        current?.json['skip_ready'] == true) ...[
                      const Divider(),
                      toggle(
                        'Skip the opening for everyone',
                        current?.json['auto_skip_op'] == true,
                        'op-on',
                        'op-off',
                      ),
                      toggle(
                        'Skip the ending for everyone',
                        current?.json['auto_skip_ed'] == true,
                        'ed-on',
                        'ed-off',
                      ),
                    ],
                  ],
                ),
              ),
            ),
            actions: [
              TextButton(
                key: const Key('room-leave'),
                onPressed: () {
                  leaving = true;
                  Navigator.pop(dialog);
                },
                child: const Text('Leave room'),
              ),
              TextButton(
                autofocus: current?.json['sync'] is! bool,
                onPressed: () => Navigator.pop(dialog),
                child: const Text('Back'),
              ),
            ],
          );
        },
      ),
    );
    panelOpen = false;
    if (leaving) {
      await exit();
      return;
    }
    if (mounted) {
      playFocus.requestFocus();
      reveal();
    }
  }

  Future<void> stopForProfile() => closing ??= closeRoom();
  Future<void> closeRoom() async {
    ending = true;
    generation++;
    followTimer?.cancel();
    beatTimer?.cancel();
    saveTimer?.cancel();
    hideTimer?.cancel();
    await player.pause();
    await save();
    await widget.services.discord.stopWatching();
    try {
      final r = await rooms.leave();
      if (r['done'] == true) await save(completed: true);
    } catch (_) {
      /* The server expires absent members. */
    }
    await player.stop();
    if (identical(widget.services.playerCommand, commandHandler)) {
      widget.services.playerCommand = null;
    }
    if (identical(widget.services.playerStatus, statusHandler)) {
      widget.services.playerStatus = null;
    }
  }

  Future<void> exit() async {
    await stopForProfile();
    if (mounted) Navigator.pop(context);
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    foreground = state == AppLifecycleState.resumed;
    if (state != AppLifecycleState.resumed) {
      unawaited(save());
      unawaited(player.pause());
    } else {
      unawaited(apply(force: true));
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    rooms.removeListener(changed);
    if (!ending) unawaited(stopForProfile());
    if (identical(widget.services.stopPlayback, stopHandler)) {
      widget.services.stopPlayback = null;
    }
    for (final s in subscriptions) {
      unawaited(s.cancel());
    }
    surface.dispose();
    playFocus.dispose();
    flash.dispose();
    if (fullscreen) unawaited(setDesktopFullScreen(false));
    unawaited((closing ?? Future<void>.value()).whenComplete(player.dispose));
    unawaited(
      SystemChrome.setEnabledSystemUIMode(
        SystemUiMode.manual,
        overlays: [SystemUiOverlay.bottom],
      ),
    );
    if (!widget.isTV) unawaited(SystemChrome.setPreferredOrientations([]));
    FullBleed.leave();
    super.dispose();
  }

  KeyEventResult keys(FocusNode node, KeyEvent e) {
    if (e is! KeyDownEvent) return KeyEventResult.ignored;
    final key = e.logicalKey;
    if ([
      LogicalKeyboardKey.mediaPlayPause,
      LogicalKeyboardKey.space,
    ].contains(key)) {
      unawaited(playPause());
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.mediaTrackNext) {
      if (canNext) unawaited(nextEpisode());
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.mediaRewind ||
        key == LogicalKeyboardKey.mediaFastForward) {
      unawaited(seekBy(key == LogicalKeyboardKey.mediaRewind ? -10 : 10));
      return KeyEventResult.handled;
    }
    if (isDesktop && key == LogicalKeyboardKey.keyF) {
      unawaited(toggleFullscreen());
      return KeyEventResult.handled;
    }
    if (isDesktop && key == LogicalKeyboardKey.keyM) {
      toggleMute();
      return KeyEventResult.handled;
    }
    if (surface.hasPrimaryFocus) {
      if (key == LogicalKeyboardKey.arrowLeft ||
          key == LogicalKeyboardKey.arrowRight) {
        unawaited(seekBy(key == LogicalKeyboardKey.arrowLeft ? -10 : 10));
        return KeyEventResult.handled;
      }
      if ([LogicalKeyboardKey.select, LogicalKeyboardKey.enter].contains(key)) {
        unawaited(playPause());
        return KeyEventResult.handled;
      }
      if (key == LogicalKeyboardKey.arrowDown ||
          key == LogicalKeyboardKey.arrowUp) {
        reveal();
        if (!busy && error == null) playFocus.requestFocus();
        return KeyEventResult.handled;
      }
    }
    reveal();
    return KeyEventResult.ignored;
  }

  /// Room notes in the player's corner, above the controls when they show:
  /// asks (the host answers them), the ready check, catching up and
  /// counting the episode.
  Widget notices(BuildContext context, RoomState current) {
    final asks = (current.json['asks'] as List? ?? []).whereType<Map>();
    final countAsk = number(current.json['count_ask']);
    Widget card(String text, [List<Widget> buttons = const []]) => Container(
      margin: const EdgeInsets.only(top: 8),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: const Color(0xe6191614),
        border: Border.all(color: line),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(text, style: uiStyle(context, color: fgBright)),
          if (buttons.isNotEmpty) ...[
            const SizedBox(height: 10),
            Wrap(spacing: 8, runSpacing: 8, children: buttons),
          ],
        ],
      ),
    );
    final items = [
      for (final ask in asks)
        card(
          '${ask['name'] ?? 'Someone'} asks to ${roomAskText(ask)}',
          current.host
              ? [
                  PlayerChip(
                    icon: LucideIcons.check,
                    label: 'Accept',
                    isTV: widget.isTV,
                    onPressed: () => unawaited(
                      run(() => rooms.answer(number(ask['id']), true)),
                    ),
                  ),
                  PlayerChip(
                    icon: LucideIcons.x,
                    label: 'Decline',
                    isTV: widget.isTV,
                    onPressed: () => unawaited(
                      run(() => rooms.answer(number(ask['id']), false)),
                    ),
                  ),
                ]
              : const [],
        ),
      if (rooms.error case final problem?) card(problem),
      if (current.hold > 0) card('Waiting for everyone to be ready'),
      if (behind > 0)
        PlayerChip(
          key: const Key('room-catch-up'),
          icon: LucideIcons.fastForward,
          label: '${behind.round()} s behind · Catch up',
          isTV: widget.isTV,
          onPressed: () => unawaited(apply(force: true)),
        ),
      if (countAsk > 0)
        card('Count episode $countAsk as watched?', [
          PlayerChip(
            icon: LucideIcons.check,
            label: 'Count it',
            isTV: widget.isTV,
            onPressed: () => unawaited(run(() => rooms.count(countAsk, true))),
          ),
          PlayerChip(
            icon: LucideIcons.x,
            label: 'Do not count',
            isTV: widget.isTV,
            onPressed: () => unawaited(run(() => rooms.count(countAsk, false))),
          ),
        ]),
    ];
    if (items.isEmpty) return const SizedBox.shrink();
    final controls = overlay && !busy && error == null;
    return Positioned(
      right: 24,
      left: isDesktop || widget.isTV ? null : 24,
      bottom: !controls
          ? 24
          : isDesktop
          ? 170
          : widget.isTV
          ? 190
          : 120,
      child: SafeArea(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 420),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.end,
            children: items,
          ),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final current = view;
    final title = current?.anime?.title ?? 'Watch room';
    final controls = overlay && !busy && error == null;
    final duration = player.state.duration;
    void seekStart(double _) {
      scrubbing = true;
      hideTimer?.cancel();
    }

    void seekChanged(double v) =>
        setState(() => scrubTo = Duration(milliseconds: v.round()));
    void seekEnd(double v) {
      scrubbing = false;
      final to = Duration(milliseconds: v.round());
      setState(() => scrubTo = null);
      unawaited(seekTo(to));
    }

    return PopScope(
      canPop: false,
      onPopInvokedWithResult: (popped, result) {
        if (popped) return;
        if (overlay && !busy && error == null) {
          hideControls();
        } else {
          unawaited(exit());
        }
      },
      child: Scaffold(
        backgroundColor: bgDeep,
        body: Focus(
          skipTraversal: true,
          onKeyEvent: keys,
          child: MouseRegion(
            onHover: isDesktop ? (_) => reveal() : null,
            cursor: isDesktop && !overlay && !busy && error == null
                ? SystemMouseCursors.none
                : MouseCursor.defer,
            child: Stack(
              fit: StackFit.expand,
              children: [
                GestureDetector(
                  behavior: HitTestBehavior.opaque,
                  onDoubleTap: isDesktop
                      ? () => unawaited(toggleFullscreen())
                      : null,
                  onTap: () => overlay ? hideControls() : reveal(),
                  child: Focus(
                    focusNode: surface,
                    autofocus: true,
                    child: IgnorePointer(
                      child: Video(
                        controller: video,
                        controls: NoVideoControls,
                      ),
                    ),
                  ),
                ),
                ValueListenableBuilder<String>(
                  valueListenable: flash,
                  builder: (_, text, _) => PlayerFlash(text: text),
                ),
                if (busy && error == null)
                  Center(
                    child: Padding(
                      padding: const EdgeInsets.all(32),
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          OtakaseSeal(size: widget.isTV ? 96 : 64),
                          const SizedBox(height: 24),
                          const SkeletonBlock(width: 160, height: 2),
                          const SizedBox(height: 24),
                          Text(
                            current?.json['load_step'] as String? ??
                                'Waiting for the room',
                            style: uiStyle(context),
                          ),
                          const SizedBox(height: 16),
                          AppButton(
                            label: 'Leave room',
                            onPressed: exit,
                            isTV: widget.isTV,
                          ),
                        ],
                      ),
                    ),
                  ),
                if (error != null)
                  Center(
                    child: ColoredBox(
                      color: const Color(0xe6080707),
                      child: EmptyState(
                        error: current?.phase != 'over',
                        message: error!,
                        action: Wrap(
                          spacing: 8,
                          runSpacing: 8,
                          alignment: WrapAlignment.center,
                          children: [
                            if (current?.phase == 'play')
                              AppButton(
                                label: 'Try again',
                                icon: LucideIcons.refreshCw,
                                filled: true,
                                autofocus: true,
                                onPressed: () => load(effectiveMedia(current!)),
                                isTV: widget.isTV,
                              ),
                            AppButton(
                              label: 'Leave room',
                              autofocus: current?.phase != 'play',
                              onPressed: exit,
                              isTV: widget.isTV,
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                if (controls && isDesktop)
                  DesktopPlayerBar(
                    title: title,
                    subtitle: roomLine,
                    position: shown,
                    duration: duration,
                    buffered: player.state.buffer,
                    playing: current?.playing == true,
                    buffering: player.state.buffering,
                    silent: player.state.volume == 0,
                    fullscreen: fullscreen,
                    trackLabel: 'Subtitles',
                    centerLabel: current?.host == true
                        ? 'Hosting'
                        : 'Watch room',
                    stripes: spans,
                    playFocus: playFocus,
                    onBack: () => unawaited(exit()),
                    onPlayPause: () => unawaited(playPause()),
                    onSeekBy: (s) => unawaited(seekBy(s)),
                    onSeekStart: seekStart,
                    onSeekChanged: seekChanged,
                    onSeekEnd: seekEnd,
                    onNext: canNext ? () => unawaited(nextEpisode()) : null,
                    onMute: toggleMute,
                    onTracks: () => unawaited(tracks()),
                    onQuality: null,
                    onFullscreen: () => unawaited(toggleFullscreen()),
                    extras: [
                      (
                        key: const Key('room-panel'),
                        icon: LucideIcons.users,
                        tooltip: 'Watch room',
                        onPressed: () => unawaited(roomPanel()),
                      ),
                    ],
                  ),
                if (controls && !isDesktop)
                  PhonePlayerControls(
                    isTV: widget.isTV,
                    title: title,
                    subtitle: roomLine,
                    position: shown,
                    duration: duration,
                    spans: spans,
                    playing: current?.playing == true,
                    buffering: player.state.buffering,
                    playFocus: playFocus,
                    onBack: () => unawaited(exit()),
                    onPlayPause: () => unawaited(playPause()),
                    onSeekBy: (s) => unawaited(seekBy(s)),
                    onSeekStart: seekStart,
                    onSeekChanged: seekChanged,
                    onSeekEnd: seekEnd,
                    actions: [
                      PlayerButton(
                        icon: LucideIcons.skipForward,
                        label: hosting ? 'Next' : 'Ask for next',
                        onPressed: canNext ? () => nextEpisode() : null,
                      ),
                      PlayerButton(
                        icon: LucideIcons.captions,
                        label: 'Subtitles',
                        onPressed: tracks,
                      ),
                      PlayerButton(
                        key: const Key('room-panel'),
                        icon: LucideIcons.users,
                        label: 'Watch room',
                        onPressed: roomPanel,
                      ),
                    ],
                  ),
                // Above the controls, so the host can answer asks while the
                // controls show.
                if (current != null && error == null) notices(context, current),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
