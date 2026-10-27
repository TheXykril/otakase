import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';

import 'core.dart';
import 'probe.dart';

class PlaybackPage extends StatefulWidget {
  const PlaybackPage({super.key, this.streams = const [], this.probe = false});
  final List<ResolvedStream> streams;
  final bool probe;
  @override
  State<PlaybackPage> createState() => PlaybackPageState();
}

class PlaybackPageState extends State<PlaybackPage> {
  late final Player player;
  late final VideoController video;
  final surface = FocusNode(debugLabel: 'Video surface');
  final subscriptions = <StreamSubscription<dynamic>>[];
  HLSProbe? probe;
  String status = 'Opening playback';
  int copy = 0;
  bool assLoaded = false;
  bool firstPosition = false;
  final clock = Stopwatch();

  @override
  void initState() {
    super.initState();
    player = Player(
      configuration: const PlayerConfiguration(
        libass: true,
        libassAndroidFont: 'assets/fonts/Inter-Regular.otf',
        libassAndroidFontName: 'Inter',
      ),
    );
    video = VideoController(
      player,
      configuration: const VideoControllerConfiguration(
        hwdec: 'mediacodec',
        enableHardwareAcceleration: true,
      ),
    );
    subscriptions.add(
      player.stream.error.listen((_) {
        if (mounted) {
          setState(
            () => status = 'Playback failed. Try another copy or source.',
          );
        }
      }),
    );
    subscriptions.add(
      player.stream.position.listen((position) {
        if (!firstPosition && position > Duration.zero && mounted) {
          firstPosition = true;
          setState(
            () => status =
                'Playback position advanced after ${clock.elapsedMilliseconds} ms',
          );
        }
      }),
    );
    unawaited(open());
  }

  Future<void> open() async {
    clock.reset();
    clock.start();
    firstPosition = false;
    assLoaded = false;
    try {
      if (widget.probe) {
        probe ??= HLSProbe();
        await probe!.start();
        if (!mounted) {
          await probe!.close();
          return;
        }
        await player.open(Media(probe!.url, httpHeaders: HLSProbe.headers));
        if (mounted) await loadASS();
      } else {
        final stream = widget.streams[copy];
        final headers = Map<String, String>.of(stream.headers);
        if (stream.referrer.isNotEmpty) headers['Referer'] = stream.referrer;
        await player.open(Media(stream.url, httpHeaders: headers));
      }
    } catch (_) {
      if (mounted) {
        setState(() => status = 'Playback failed. Try another copy or source.');
      }
    }
  }

  Future<void> loadASS() async {
    try {
      final data = await rootBundle.loadString('assets/spike.ass');
      if (!mounted) return;
      await player.setSubtitleTrack(
        SubtitleTrack.data(data, title: 'Otakase ASS probe', language: 'en'),
      );
      if (mounted) setState(() => assLoaded = true);
    } catch (_) {
      if (mounted) {
        setState(
          () => status = 'ASS load failed. Stop and report this result.',
        );
      }
    }
  }

  Future<void> selectSubtitle(SubtitleTrack track) async {
    try {
      await player.setSubtitleTrack(track);
      if (mounted) setState(() => assLoaded = false);
    } catch (_) {
      if (mounted) {
        setState(() => status = 'Subtitle load failed. Choose another track.');
      }
    }
  }

  Future<void> seek(int seconds) async {
    final target = player.state.position.inMilliseconds + seconds * 1000;
    await player.seek(
      Duration(
        milliseconds: math.max(
          0,
          math.min(target, player.state.duration.inMilliseconds),
        ),
      ),
    );
  }

  Future<void> tracks() async {
    await showDialog<void>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Subtitles'),
        content: SizedBox(
          width: 500,
          height: 300,
          child: ListView(
            children: [
              OutlinedButton(
                autofocus: true,
                onPressed: () {
                  Navigator.pop(context);
                  unawaited(selectSubtitle(SubtitleTrack.no()));
                },
                child: const Text('Off'),
              ),
              OutlinedButton(
                onPressed: () {
                  Navigator.pop(context);
                  unawaited(loadASS());
                },
                child: const Text('Bundled ASS probe'),
              ),
              if (!widget.probe)
                for (final track in widget.streams[copy].subtitles)
                  OutlinedButton(
                    onPressed: () {
                      Navigator.pop(context);
                      unawaited(
                        selectSubtitle(
                          SubtitleTrack.uri(
                            track['url'] as String,
                            title: track['label'] as String,
                            language: track['language'] as String,
                          ),
                        ),
                      );
                    },
                    child: Text(
                      (track['label'] as String).isEmpty
                          ? 'Release subtitles'
                          : track['label'] as String,
                    ),
                  ),
            ],
          ),
        ),
        actions: [
          OutlinedButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('Back'),
          ),
        ],
      ),
    );
  }

  KeyEventResult videoKey(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent && event is! KeyRepeatEvent) {
      return KeyEventResult.ignored;
    }
    if (event.logicalKey == LogicalKeyboardKey.arrowLeft) {
      unawaited(seek(-10));
      return KeyEventResult.handled;
    }
    if (event.logicalKey == LogicalKeyboardKey.arrowRight) {
      unawaited(seek(10));
      return KeyEventResult.handled;
    }
    if (event is KeyDownEvent &&
        [
          LogicalKeyboardKey.select,
          LogicalKeyboardKey.enter,
          LogicalKeyboardKey.space,
          LogicalKeyboardKey.mediaPlayPause,
        ].contains(event.logicalKey)) {
      unawaited(player.playOrPause());
      return KeyEventResult.handled;
    }
    return KeyEventResult.ignored;
  }

  @override
  void dispose() {
    for (final subscription in subscriptions) {
      unawaited(subscription.cancel());
    }
    surface.dispose();
    unawaited(player.dispose());
    unawaited(probe?.close() ?? Future<void>.value());
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: Text(widget.probe ? 'HLS headers + ASS probe' : 'Episode player'),
    ),
    body: SafeArea(
      minimum: const EdgeInsets.all(16),
      child: Column(
        children: [
          Expanded(
            child: Focus(
              focusNode: surface,
              autofocus: true,
              onKeyEvent: videoKey,
              onFocusChange: (_) => setState(() {}),
              child: Container(
                decoration: BoxDecoration(
                  border: Border.all(
                    color: surface.hasFocus
                        ? const Color(0xffd84836)
                        : Colors.transparent,
                    width: 3,
                  ),
                ),
                child: Video(controller: video, controls: NoVideoControls),
              ),
            ),
          ),
          Text(status, key: const Key('playback-status')),
          if (probe != null)
            ValueListenableBuilder<int>(
              valueListenable: probe!.accepted,
              builder: (_, count, child) => Text(
                'Headers accepted: $count, resources: ${probe!.resources.length}, refused: ${probe!.rejected.value}',
                key: const Key('header-status'),
              ),
            ),
          Text(
            assLoaded
                ? 'ASS loaded. Confirm the red heading and moving sign.'
                : 'Subtitles through libass',
          ),
          StreamBuilder<Duration>(
            stream: player.stream.position,
            builder: (_, snapshot) => Text(
              '${(snapshot.data ?? Duration.zero).inSeconds}s / ${player.state.duration.inSeconds}s',
            ),
          ),
          Wrap(
            spacing: 12,
            runSpacing: 8,
            alignment: WrapAlignment.center,
            children: [
              OutlinedButton(
                onPressed: () => seek(-10),
                child: const Text('−10s'),
              ),
              OutlinedButton(
                onPressed: player.playOrPause,
                child: const Text('Play / Pause'),
              ),
              OutlinedButton(
                onPressed: () => seek(10),
                child: const Text('+10s'),
              ),
              OutlinedButton(onPressed: tracks, child: const Text('Subtitles')),
              OutlinedButton(
                onPressed: loadASS,
                child: const Text('Load ASS probe'),
              ),
              if (!widget.probe && widget.streams.length > 1)
                OutlinedButton(
                  onPressed: () {
                    setState(() {
                      copy = (copy + 1) % widget.streams.length;
                      status = 'Opening copy ${copy + 1}';
                    });
                    unawaited(open());
                  },
                  child: Text('Copy ${copy + 1} / ${widget.streams.length}'),
                ),
              OutlinedButton(
                onPressed: () => Navigator.pop(context),
                child: const Text('Back'),
              ),
            ],
          ),
        ],
      ),
    ),
  );
}
