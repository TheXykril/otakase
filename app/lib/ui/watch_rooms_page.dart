import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';

import '../data/models.dart';
import '../data/watch_rooms.dart';
import '../services.dart';
import 'components.dart';
import 'device_status.dart';

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
  int generation = 0, readySent = 0;
  String media = '', subs = '';
  String? error;
  double behind = 0;
  Future<void>? closing;
  @override
  void initState() {
    super.initState();
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
        if (done && rooms.state?.host == true) {
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
    final native = player.platform;
    if (native is! NativePlayer) return;
    final s = widget.services.store;
    for (final entry in {
      'sub-font': s.text('SubtitleFont'),
      'sub-font-size': '${s.integer('SubtitleSize')}',
      'sub-color': s.text('SubtitleColour'),
      'sub-back-color': s.text('SubtitleBackground'),
      'sub-border-size': '${s.integer('SubtitleOutline')}',
      'sub-pos': '${s.integer('SubtitlePosition')}',
      'sub-ass-override': s.text('SubStyle') == 'soft' ? 'no' : 'yes',
    }.entries) {
      await native.setProperty(entry.key, entry.value);
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
      if (current.host) {
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
      if (mounted) setState(() => error = e.toString());
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

  Future<void> run(Future<void> Function() action) async {
    try {
      await action();
      if (mounted) setState(() => error = null);
    } catch (e) {
      if (mounted) setState(() => error = e.toString());
    }
  }

  void reveal() {
    if (!mounted || ending) return;
    setState(() => overlay = true);
    hideTimer?.cancel();
    hideTimer = Timer(const Duration(seconds: 6), () {
      if (mounted && surface.hasPrimaryFocus) setState(() => overlay = false);
    });
  }

  Future<void> tracks() async {
    final tracks = (view?.json['subs'] as List? ?? [])
        .whereType<Map>()
        .toList();
    final choice = await chooseValue(context, 'Subtitles', [
      'Off',
      ...tracks.map((t) => t['label'] as String? ?? 'Subtitle'),
    ]);
    if (choice == null || ending) return;
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
    reveal();
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
    unawaited((closing ?? Future<void>.value()).whenComplete(player.dispose));
    unawaited(
      SystemChrome.setEnabledSystemUIMode(
        SystemUiMode.manual,
        overlays: [SystemUiOverlay.bottom],
      ),
    );
    if (!widget.isTV) unawaited(SystemChrome.setPreferredOrientations([]));
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final current = view;
    return PopScope(
      canPop: false,
      onPopInvokedWithResult: (popped, result) {
        if (!popped) unawaited(exit());
      },
      child: Scaffold(
        backgroundColor: bgDeep,
        body: Focus(
          focusNode: surface,
          autofocus: true,
          onKeyEvent: (node, e) {
            if (e is! KeyDownEvent || !node.hasPrimaryFocus) {
              return KeyEventResult.ignored;
            }
            if (e.logicalKey == LogicalKeyboardKey.arrowLeft) {
              unawaited(run(() => rooms.jumpBy(-10)));
              reveal();
              return KeyEventResult.handled;
            }
            if (e.logicalKey == LogicalKeyboardKey.arrowRight) {
              unawaited(run(() => rooms.jumpBy(10)));
              reveal();
              return KeyEventResult.handled;
            }
            if ([
              LogicalKeyboardKey.select,
              LogicalKeyboardKey.enter,
              LogicalKeyboardKey.mediaPlayPause,
              LogicalKeyboardKey.space,
            ].contains(e.logicalKey)) {
              unawaited(run(() => control('toggle', {})));
              return KeyEventResult.handled;
            }
            reveal();
            return KeyEventResult.ignored;
          },
          child: GestureDetector(
            onTap: reveal,
            child: Stack(
              fit: StackFit.expand,
              children: [
                Video(controller: video, controls: NoVideoControls),
                if (busy)
                  const Center(child: CircularProgressIndicator(color: shu)),
                if (overlay || busy || error != null || behind > 0)
                  SafeArea(
                    child: ColoredBox(
                      color: bgDeep.withValues(alpha: .82),
                      child: ListView(
                        padding: EdgeInsets.all(widget.isTV ? 32 : 20),
                        children: [
                          Row(
                            children: [
                              Expanded(
                                child: Text(
                                  current?.anime?.title ?? 'Watch room',
                                  style: uiStyle(
                                    context,
                                    size: 24,
                                    weight: FontWeight.w600,
                                  ),
                                ),
                              ),
                              DeviceStatus(isTV: widget.isTV),
                            ],
                          ),
                          Text(
                            'Episode ${current?.episode ?? 0} · ${current?.host == true ? 'Hosting' : 'Following the host'}',
                            style: uiStyle(context, color: muted),
                          ),
                          if (busy)
                            Text(
                              current?.json['load_step'] as String? ??
                                  'Waiting for the room',
                              style: uiStyle(context),
                            ),
                          const SizedBox(height: 24),
                          LinearProgressIndicator(
                            value: player.state.duration.inSeconds > 0
                                ? (player.state.position.inSeconds /
                                          player.state.duration.inSeconds)
                                      .clamp(0, 1)
                                : 0,
                            minHeight: 3,
                            color: shu,
                            backgroundColor: line,
                          ),
                          const SizedBox(height: 20),
                          Wrap(
                            spacing: 12,
                            runSpacing: 12,
                            children: [
                              AppButton(
                                label: current?.host == true
                                    ? (current?.playing == true
                                          ? 'Pause'
                                          : 'Play')
                                    : (current?.playing == true
                                          ? 'Ask to pause'
                                          : 'Ask to play'),
                                isTV: widget.isTV,
                                onPressed: () =>
                                    run(() => control('toggle', {})),
                              ),
                              AppButton(
                                label: current?.host == true
                                    ? '+10 s'
                                    : 'Ask +10 s',
                                isTV: widget.isTV,
                                onPressed: () => run(() => rooms.jumpBy(10)),
                              ),
                              AppButton(
                                label: current?.host == true
                                    ? 'Next episode'
                                    : 'Ask next episode',
                                isTV: widget.isTV,
                                onPressed: () =>
                                    run(() => rooms.control('next')),
                              ),
                              AppButton(
                                label: 'Tracks',
                                isTV: widget.isTV,
                                onPressed: tracks,
                              ),
                              AppButton(
                                label: 'Hide controls',
                                isTV: widget.isTV,
                                onPressed: () {
                                  setState(() => overlay = false);
                                  surface.requestFocus();
                                },
                              ),
                              AppButton(
                                label: 'Leave room',
                                isTV: widget.isTV,
                                onPressed: exit,
                              ),
                              if (error != null && current?.phase == 'play')
                                AppButton(
                                  label: 'Retry stream',
                                  isTV: widget.isTV,
                                  onPressed: () =>
                                      load(effectiveMedia(current!)),
                                ),
                              if (behind > 0)
                                AppButton(
                                  label:
                                      '${behind.round()} s behind · Catch up',
                                  isTV: widget.isTV,
                                  onPressed: () => apply(force: true),
                                ),
                            ],
                          ),
                          if (current != null && current.hold > 0)
                            Padding(
                              padding: const EdgeInsets.only(top: 16),
                              child: Text(
                                'Waiting for everyone to be ready',
                                style: uiStyle(context, color: muted),
                              ),
                            ),
                          if (current?.json['sync'] case final bool sync)
                            SwitchListTile(
                              contentPadding: EdgeInsets.zero,
                              title: Text(
                                'Sync room progress${(current?.json['linked'] as List? ?? []).isEmpty ? '' : ' · ${(current!.json['linked'] as List).join(' + ')}'}',
                              ),
                              value: sync,
                              onChanged: (v) => run(() => rooms.sync(v)),
                            ),
                          if (number(current?.json['count_ask']) > 0)
                            Wrap(
                              spacing: 12,
                              children: [
                                AppButton(
                                  label:
                                      'Count episode ${current!.json['count_ask']} as watched',
                                  isTV: widget.isTV,
                                  onPressed: () => run(
                                    () => rooms.count(
                                      number(current.json['count_ask']),
                                      true,
                                    ),
                                  ),
                                ),
                                AppButton(
                                  label: 'Do not count',
                                  isTV: widget.isTV,
                                  onPressed: () => run(
                                    () => rooms.count(
                                      number(current.json['count_ask']),
                                      false,
                                    ),
                                  ),
                                ),
                              ],
                            ),
                          for (final ask
                              in (current?.json['asks'] as List? ?? [])
                                  .whereType<Map>())
                            Padding(
                              padding: const EdgeInsets.only(top: 16),
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  Text(
                                    '${ask['name'] ?? 'Someone'} asks to ${ask['kind']}',
                                    style: uiStyle(context),
                                  ),
                                  if (current?.host == true)
                                    Wrap(
                                      spacing: 12,
                                      children: [
                                        AppButton(
                                          label: 'Accept',
                                          isTV: widget.isTV,
                                          onPressed: () => run(
                                            () => rooms.answer(
                                              number(ask['id']),
                                              true,
                                            ),
                                          ),
                                        ),
                                        AppButton(
                                          label: 'Decline',
                                          isTV: widget.isTV,
                                          onPressed: () => run(
                                            () => rooms.answer(
                                              number(ask['id']),
                                              false,
                                            ),
                                          ),
                                        ),
                                      ],
                                    ),
                                ],
                              ),
                            ),
                          if (error != null || rooms.error != null)
                            Padding(
                              padding: const EdgeInsets.only(top: 16),
                              child: Text(
                                error ?? rooms.error!,
                                style: uiStyle(context, color: warn),
                              ),
                            ),
                        ],
                      ),
                    ),
                  ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
