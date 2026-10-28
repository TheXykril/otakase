import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:flutter/services.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';

import 'core.dart';
import 'data/models.dart';
import 'services.dart';
import 'ui/components.dart';

class PlayerPage extends StatefulWidget {
  const PlayerPage({
    super.key,
    required this.services,
    required this.anime,
    required this.episode,
    required this.isTV,
    required this.mode,
  });
  final AppServices services;
  final Anime anime;
  final int episode;
  final bool isTV;
  final String mode;
  @override
  State<PlayerPage> createState() => _PlayerPageState();
}

class _PlayerPageState extends State<PlayerPage> with WidgetsBindingObserver {
  late final Player player;
  late final VideoController video;
  late int episode;
  PlaybackSource? source;
  RequestScope scope = RequestScope();
  final subscriptions = <StreamSubscription<dynamic>>[];
  final surface = FocusNode(debugLabel: 'Playback surface');
  final playFocus = FocusNode(debugLabel: 'Play pause');
  final skipFocus = FocusNode(debugLabel: 'Skip segment');
  Timer? hideTimer, saveTimer, nextTimer;
  int copy = 0, generation = 0, countdownSeconds = 0;
  bool overlay = true,
      busy = true,
      panelOpen = false,
      skipRequested = false,
      finished = false,
      subSelected = false,
      audioSelected = false;
  String status = 'Finding an episode', error = '';
  List<SkipSpan> spans = [];
  List<int> episodes = [];
  SkipSpan? active;
  final skipped = <String>{};
  Duration position = Duration.zero, duration = Duration.zero;
  bool get canNext => episodes.isNotEmpty
      ? episodes.any((e) => e > episode)
      : widget.anime.airedEpisodes > episode;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    episode = widget.episode;
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
      player.stream.position.listen(tick),
      player.stream.duration.listen((d) {
        if (!mounted || d <= Duration.zero) return;
        final changed = duration == Duration.zero;
        setState(() => duration = d);
        if (changed) unawaited(loadSkips(generation));
      }),
      player.stream.completed.listen((done) {
        if (done && !busy && !finished) {
          finished = true;
          unawaited(save(completed: true));
          offerNext();
        }
      }),
      player.stream.error.listen((_) {
        if (mounted && !busy) {
          setState(() {
            error = 'Playback failed. Try another copy or source.';
            overlay = true;
          });
        }
      }),
      player.stream.tracks.listen((_) {
        if (!busy) unawaited(preferredTracks(generation));
      }),
      player.stream.playing.listen((_) {
        if (mounted) setState(() {});
      }),
    ]);
    saveTimer = Timer.periodic(
      const Duration(seconds: 5),
      (_) => unawaited(save()),
    );
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
    unawaited(open());
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state != AppLifecycleState.resumed) {
      unawaited(save());
      unawaited(player.pause());
    }
  }

  Future<void> property(String name, String value) async {
    final native = player.platform;
    if (native is NativePlayer) await native.setProperty(name, value);
  }

  Future<void> configure() async {
    final s = widget.services.store;
    await property('sub-font', s.text('SubtitleFont'));
    await property('sub-font-size', s.integer('SubtitleSize').toString());
    await property('sub-color', s.text('SubtitleColour'));
    await property('sub-back-color', s.text('SubtitleBackground'));
    await property('sub-border-size', s.integer('SubtitleOutline').toString());
    await property('sub-pos', s.integer('SubtitlePosition').toString());
    // Soft tracks keep their ASS styling. Never rasterize or convert them.
    await property(
      'sub-ass-override',
      s.text('SubStyle') == 'soft' ? 'no' : 'yes',
    );
  }

  Future<void> configureQuality(
    ResolvedStream stream,
    Map<String, String> headers,
  ) async {
    final quality = widget.services.store.text('Quality');
    if (quality == 'best' || quality == 'worst') {
      await property('hls-bitrate', quality == 'best' ? 'max' : 'min');
      return;
    }
    final height = int.tryParse(quality.replaceAll('p', ''));
    await property('hls-bitrate', 'max');
    if (height == null || !(stream.hls || stream.url.contains('.m3u8'))) return;
    try {
      final request = http.Request('GET', Uri.parse(stream.url))
        ..headers.addAll(headers);
      final response = await widget.services.catalog.client
          .send(request)
          .timeout(const Duration(seconds: 8));
      var length = 0;
      final data = <int>[];
      await for (final chunk in response.stream.timeout(
        const Duration(seconds: 8),
      )) {
        length += chunk.length;
        if (length > 1024 * 1024) break;
        data.addAll(chunk);
      }
      final text = utf8.decode(data);
      final choices = <({int height, int bandwidth})>[];
      for (final line
          in text
              .split('\n')
              .where((s) => s.startsWith('#EXT-X-STREAM-INF:'))) {
        final resolution = RegExp(r'RESOLUTION=\d+x(\d+)').firstMatch(line);
        final bandwidth = RegExp(r'(?:^|[,:])BANDWIDTH=(\d+)').firstMatch(line);
        final h = int.tryParse(resolution?.group(1) ?? ''),
            b = int.tryParse(bandwidth?.group(1) ?? '');
        if (h != null && b != null) choices.add((height: h, bandwidth: b));
      }
      choices.sort((a, b) => a.height.compareTo(b.height));
      if (choices.isNotEmpty) {
        final fitting = choices.where((c) => c.height <= height).toList();
        await property(
          'hls-bitrate',
          '${(fitting.isEmpty ? choices.first : fitting.last).bandwidth}',
        );
      }
    } catch (_) {
      /* The original master remains playable at its default quality. */
    }
  }

  Future<void> open({String? provider, int? mirror}) async {
    final g = ++generation;
    scope.cancel();
    scope = RequestScope();
    nextTimer?.cancel();
    countdownSeconds = 0;
    setState(() {
      busy = true;
      error = '';
      active = null;
      spans = [];
      finished = false;
      overlay = true;
      duration = Duration.zero;
      position = Duration.zero;
    });
    skipped.clear();
    subSelected = false;
    audioSelected = false;
    try {
      await player.stop();
      if (mirror == null) {
        await widget.services.core.stopPlayback();
        source = await widget.services.resolver.resolve(
          widget.anime,
          episode,
          scope: scope,
          source: provider,
          mode: widget.mode,
          status: (s) {
            if (mounted && g == generation) setState(() => status = s);
          },
        );
        copy = 0;
      } else {
        copy = mirror;
      }
      if (!mounted || g != generation) return;
      final stream = source!.streams[copy];
      final headers = Map<String, String>.of(stream.headers);
      if (stream.referrer.isNotEmpty) headers['Referer'] = stream.referrer;
      await configure();
      await configureQuality(stream, headers);
      await player.open(Media(stream.url, httpHeaders: headers), play: false);
      if (!mounted || g != generation) return;
      final history = widget.services.store.entry(widget.anime);
      if (widget.services.store.flag('ResumePlayback') &&
          history?.episode == episode &&
          (history?.position ?? 0) > 0) {
        await player.seek(Duration(seconds: history!.position));
      }
      await preferredTracks(g);
      if (!mounted || g != generation) return;
      await player.play();
      setState(() => busy = false);
      unawaited(loadEpisodes(g));
      unawaited(loadSkips(g));
      showControls();
      if (widget.services.store.text('SubStyle') == 'ask') {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted && g == generation) unawaited(trackPanel());
        });
      }
    } catch (_) {
      if (mounted && g == generation) {
        setState(() {
          busy = false;
          error =
              'Unable to play this episode. Retry or choose another source.';
        });
      }
    }
  }

  Future<void> loadEpisodes(int g) async {
    final result = await widget.services.resolver.episodes(source!, scope);
    if (mounted && g == generation) setState(() => episodes = result);
  }

  Future<void> loadSkips(int g) async {
    if (source == null) return;
    final result = await widget.services.resolver.skips(
      source!,
      episode,
      scope,
    );
    final extra = await widget.services.catalog.skipTimes(
      widget.anime.malId,
      episode,
      duration.inMilliseconds / 1000,
    );
    if (!mounted || g != generation) return;
    final merged = [
      ...result,
      ...extra.where((s) => !result.any((v) => v.type == s.type)),
    ];
    setState(
      () => spans =
          merged
              .where(
                (s) => duration == Duration.zero || s.end <= duration.inSeconds,
              )
              .toList()
            ..sort((a, b) => a.start.compareTo(b.start)),
    );
  }

  bool languageMatches(String? language, String? title, String preferred) {
    final aliases = <String, List<String>>{
      'english': ['en', 'eng', 'english'],
      'japanese': ['ja', 'jpn', 'japanese'],
      'chinese': ['zh', 'chi', 'zho', 'chinese'],
      'russian': ['ru', 'rus', 'russian'],
      'spanish': ['es', 'spa', 'spanish'],
      'french': ['fr', 'fra', 'fre', 'french'],
      'german': ['de', 'deu', 'ger', 'german'],
    };
    final values =
        aliases[preferred.toLowerCase()] ?? [preferred.toLowerCase()];
    final lang = (language ?? '').toLowerCase(),
        label = (title ?? '').toLowerCase();
    return values.any((s) => lang == s || (s.length > 2 && label.contains(s)));
  }

  Future<void> preferredTracks(int g) async {
    final s = widget.services.store;
    if (!audioSelected) {
      for (final language in s.text('AudioLanguages').split(',')) {
        final matches = player.state.tracks.audio.where(
          (t) =>
              t.id != 'auto' &&
              t.id != 'no' &&
              languageMatches(t.language, t.title, language.trim()),
        );
        if (matches.isNotEmpty) {
          audioSelected = true;
          await player.setAudioTrack(matches.first);
          break;
        }
      }
    }
    if (subSelected || source == null || !mounted || g != generation) return;
    final preferences = s
        .text('SubsLanguage')
        .toLowerCase()
        .split(',')
        .map((v) => v.trim())
        .where((v) => v.isNotEmpty);
    for (final preferred in preferences) {
      if (preferred == 'none' || preferred == 'off') {
        subSelected = true;
        await player.setSubtitleTrack(SubtitleTrack.no());
        return;
      }
      final embedded = player.state.tracks.subtitle.where(
        (t) =>
            t.id != 'auto' &&
            t.id != 'no' &&
            languageMatches(t.language, t.title, preferred),
      );
      if (embedded.isNotEmpty) {
        subSelected = true;
        await player.setSubtitleTrack(embedded.first);
        return;
      }
      final external = source!.streams[copy].subtitles.where(
        (t) => languageMatches(
          t['language'] as String?,
          t['label'] as String?,
          preferred,
        ),
      );
      if (external.isNotEmpty) {
        subSelected = true;
        await externalSubtitle(external.first, g);
        return;
      }
    }
  }

  Future<void> externalSubtitle(Json track, int g) async {
    try {
      final stream = source!.streams[copy];
      final headers = Map<String, String>.of(stream.headers);
      if (stream.referrer.isNotEmpty) headers['Referer'] = stream.referrer;
      final response = await widget.services.catalog.client
          .get(Uri.parse(track['url'] as String), headers: headers)
          .timeout(const Duration(seconds: 15));
      if (!mounted || g != generation) return;
      if (response.statusCode != 200) throw const FormatException();
      // libass receives the original text, including ASS positions and styles.
      await player.setSubtitleTrack(
        SubtitleTrack.data(
          utf8.decode(response.bodyBytes),
          title: track['label'] as String? ?? 'Provider subtitles',
          language: track['language'] as String?,
        ),
      );
    } catch (_) {
      if (mounted && g == generation) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('Subtitle load failed. Choose another track.'),
          ),
        );
      }
    }
  }

  void tick(Duration p) {
    if (!mounted || busy) return;
    final current = spans
        .where((s) => s.contains(p.inMilliseconds / 1000))
        .firstOrNull;
    final entered =
        active?.type != current?.type || active?.start != current?.start;
    setState(() {
      position = p;
      active = current;
    });
    if (current != null) {
      final setting = switch (current.type) {
        'op' => 'SkipOp',
        'ed' => 'SkipEd',
        _ => 'SkipRecap',
      };
      final key = '${current.type}:${current.start}';
      if (widget.services.store.flag(setting) &&
          !skipped.contains(key) &&
          p.inMilliseconds / 1000 >=
              current.start + widget.services.store.integer('SkipDelay')) {
        skipped.add(key);
        unawaited(
          player.seek(Duration(milliseconds: (current.end * 1000).round())),
        );
      } else if (entered && !widget.services.store.flag(setting)) {
        setState(() => overlay = true);
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted && active != null) skipFocus.requestFocus();
        });
      }
    }
    if (duration.inSeconds > 0 &&
        duration.inSeconds - p.inSeconds <= 15 &&
        !finished &&
        canNext) {
      finished = true;
      offerNext();
    }
  }

  Future<void> save({bool completed = false}) async {
    if (busy || duration <= Duration.zero) return;
    final watched =
        completed ||
        position.inSeconds / duration.inSeconds * 100 >=
            widget.services.store.integer('PercentageToMarkComplete');
    // Keep the resume point even when the watched threshold has been reached.
    await widget.services.store.progress(
      widget.anime,
      episode,
      position.inSeconds,
      duration.inSeconds,
      completed: completed,
    );
    if (watched && !completed) {
      await widget.services.store.markWatched(widget.anime, episode, true);
    }
  }

  void showControls() {
    hideTimer?.cancel();
    if (!mounted) return;
    setState(() => overlay = true);
    hideTimer = Timer(const Duration(seconds: 5), () {
      if (mounted &&
          !panelOpen &&
          active == null &&
          countdownSeconds == 0 &&
          player.state.playing) {
        setState(() => overlay = false);
        surface.requestFocus();
      }
    });
  }

  Future<void> seek(int seconds) async {
    showControls();
    await player.seek(
      Duration(
        milliseconds: (position.inMilliseconds + seconds * 1000).clamp(
          0,
          math.max(0, duration.inMilliseconds),
        ),
      ),
    );
  }

  Future<void> next({bool automatic = false}) async {
    if (!canNext) return;
    nextTimer?.cancel();
    await save(completed: true);
    var target = episodes.where((e) => e > episode).firstOrNull ?? episode + 1;
    if (automatic && widget.services.store.flag('SkipFiller')) {
      // A bounded lookup avoids runaway requests when metadata is incomplete.
      for (var i = 0; i < 12 && target <= widget.anime.airedEpisodes; i++) {
        if (!await widget.services.catalog.isFiller(
          widget.anime.malId,
          target,
        )) {
          break;
        }
        await widget.services.store.markWatched(widget.anime, target, true);
        target++;
      }
      if (widget.anime.airedEpisodes > 0 &&
          target > widget.anime.airedEpisodes) {
        return;
      }
    }
    if (!mounted) return;
    episode = target;
    await open();
  }

  void offerNext() {
    if (!canNext || countdownSeconds > 0) return;
    setState(() {
      countdownSeconds = 10;
      overlay = true;
    });
    if (widget.services.store.flag('NextEpisodePrompt')) return;
    nextTimer = Timer.periodic(const Duration(seconds: 1), (timer) {
      if (!mounted) {
        timer.cancel();
        return;
      }
      setState(() => countdownSeconds--);
      if (countdownSeconds <= 0) {
        timer.cancel();
        unawaited(next(automatic: true));
      }
    });
  }

  Future<void> trackPanel() async {
    panelOpen = true;
    showControls();
    await showDialog<void>(
      context: context,
      builder: (dialog) => AlertDialog(
        title: const Text('Subtitles & audio'),
        content: SizedBox(
          width: 560,
          height: 360,
          child: ListView(
            children: [
              const Text('Subtitles', style: TextStyle(color: shu)),
              for (final track in player.state.tracks.subtitle)
                ListTile(
                  autofocus: track == player.state.tracks.subtitle.first,
                  title: Text(
                    track.id == 'no'
                        ? 'Off'
                        : track.id == 'auto'
                        ? 'Automatic'
                        : track.title ?? track.language ?? 'Track ${track.id}',
                  ),
                  onTap: () {
                    subSelected = true;
                    Navigator.pop(dialog);
                    unawaited(player.setSubtitleTrack(track));
                  },
                ),
              for (final track in source?.streams[copy].subtitles ?? <Json>[])
                ListTile(
                  title: Text(
                    '${track['label'] ?? track['language'] ?? 'Provider subtitles'} · provider',
                  ),
                  onTap: () {
                    subSelected = true;
                    Navigator.pop(dialog);
                    unawaited(externalSubtitle(track, generation));
                  },
                ),
              const Divider(),
              const Text('Audio', style: TextStyle(color: shu)),
              for (final track in player.state.tracks.audio)
                ListTile(
                  title: Text(
                    track.title ??
                        track.language ??
                        (track.id == 'auto'
                            ? 'Automatic'
                            : 'Track ${track.id}'),
                  ),
                  onTap: () {
                    audioSelected = true;
                    Navigator.pop(dialog);
                    unawaited(player.setAudioTrack(track));
                  },
                ),
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialog),
            child: const Text('Back'),
          ),
        ],
      ),
    );
    panelOpen = false;
    if (mounted) {
      playFocus.requestFocus();
      showControls();
    }
  }

  Future<void> sourcePanel() async {
    panelOpen = true;
    final chosen = await chooseValue(
      context,
      'Source',
      widget.services.store.enabledSources,
      selected: source?.provider,
    );
    panelOpen = false;
    if (chosen != null && mounted) {
      await save();
      await open(provider: chosen);
    } else if (mounted) {
      showControls();
    }
  }

  Future<void> qualityPanel() async {
    final values = ['best', '1080', '720', '480', '360', 'worst'];
    panelOpen = true;
    final chosen = await chooseValue(
      context,
      'Quality',
      values,
      selected: widget.services.store.text('Quality'),
    );
    panelOpen = false;
    if (chosen != null && mounted) {
      await save();
      await widget.services.store.set('Quality', chosen);
      await open(mirror: copy);
    } else if (mounted) {
      showControls();
    }
  }

  KeyEventResult remote(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent && event is! KeyRepeatEvent) {
      return KeyEventResult.ignored;
    }
    final key = event.logicalKey;
    if ([
      LogicalKeyboardKey.mediaPlayPause,
      LogicalKeyboardKey.space,
    ].contains(key)) {
      unawaited(player.playOrPause());
      showControls();
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.mediaPlay) {
      unawaited(player.play());
      showControls();
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.mediaPause ||
        key == LogicalKeyboardKey.mediaStop) {
      unawaited(player.pause());
      showControls();
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.mediaTrackNext) {
      unawaited(next());
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.mediaRewind) {
      unawaited(seek(-10));
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.mediaFastForward) {
      unawaited(seek(10));
      return KeyEventResult.handled;
    }
    if (surface.hasPrimaryFocus) {
      if (key == LogicalKeyboardKey.arrowLeft ||
          key == LogicalKeyboardKey.arrowRight) {
        unawaited(seek(key == LogicalKeyboardKey.arrowLeft ? -10 : 10));
        return KeyEventResult.handled;
      }
      if ([LogicalKeyboardKey.select, LogicalKeyboardKey.enter].contains(key)) {
        unawaited(player.playOrPause());
        showControls();
        return KeyEventResult.handled;
      }
      if (key == LogicalKeyboardKey.arrowDown ||
          key == LogicalKeyboardKey.arrowUp) {
        showControls();
        playFocus.requestFocus();
        return KeyEventResult.handled;
      }
    }
    showControls();
    return KeyEventResult.ignored;
  }

  String time(Duration d) =>
      '${d.inMinutes.toString().padLeft(2, '0')}:${(d.inSeconds % 60).toString().padLeft(2, '0')}';
  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    generation++;
    scope.cancel();
    hideTimer?.cancel();
    saveTimer?.cancel();
    nextTimer?.cancel();
    unawaited(save());
    for (final subscription in subscriptions) {
      unawaited(subscription.cancel());
    }
    surface.dispose();
    playFocus.dispose();
    skipFocus.dispose();
    unawaited(
      player.dispose().then((_) => widget.services.core.stopPlayback()),
    );
    unawaited(SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge));
    if (!widget.isTV) unawaited(SystemChrome.setPreferredOrientations([]));
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => PopScope(
    canPop: !overlay || busy || error.isNotEmpty,
    onPopInvokedWithResult: (popped, result) {
      if (!popped) {
        setState(() => overlay = false);
        surface.requestFocus();
      }
    },
    child: Scaffold(
      backgroundColor: Colors.black,
      body: Focus(
        onKeyEvent: remote,
        child: Stack(
          fit: StackFit.expand,
          children: [
            GestureDetector(
              onTap: () {
                if (overlay) {
                  setState(() => overlay = false);
                  surface.requestFocus();
                } else {
                  showControls();
                }
              },
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
                    const CircularProgressIndicator(color: shu),
                    const SizedBox(height: 18),
                    Text(status),
                    const SizedBox(height: 16),
                    AppButton(
                      label: 'Back',
                      onPressed: () => Navigator.pop(context),
                      isTV: widget.isTV,
                    ),
                  ],
                ),
              ),
            if (error.isNotEmpty)
              Center(
                child: Container(
                  color: Colors.black87,
                  padding: const EdgeInsets.all(24),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(error),
                      const SizedBox(height: 18),
                      Wrap(
                        spacing: 12,
                        children: [
                          AppButton(
                            label: 'Retry',
                            onPressed: () => open(),
                            isTV: widget.isTV,
                          ),
                          AppButton(
                            label: 'Source',
                            onPressed: sourcePanel,
                            isTV: widget.isTV,
                          ),
                          AppButton(
                            label: 'Back',
                            onPressed: () => Navigator.pop(context),
                            isTV: widget.isTV,
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
              ),
            if (overlay && !busy && error.isEmpty)
              SafeArea(
                child: Column(
                  children: [
                    Container(
                      decoration: const BoxDecoration(
                        gradient: LinearGradient(
                          begin: Alignment.topCenter,
                          end: Alignment.bottomCenter,
                          colors: [Colors.black87, Colors.transparent],
                        ),
                      ),
                      padding: const EdgeInsets.all(16),
                      child: Row(
                        children: [
                          IconButton(
                            tooltip: 'Back',
                            onPressed: () => Navigator.pop(context),
                            icon: const Icon(Icons.arrow_back),
                          ),
                          const SizedBox(width: 12),
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(
                                  widget.anime.title,
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                  style: const TextStyle(
                                    fontSize: 20,
                                    fontWeight: FontWeight.w700,
                                  ),
                                ),
                                Text(
                                  'Episode $episode · ${source?.provider ?? ''} · ${source?.mode.toUpperCase() ?? ''}',
                                  style: const TextStyle(color: muted),
                                ),
                              ],
                            ),
                          ),
                        ],
                      ),
                    ),
                    const Spacer(),
                    if (active != null)
                      Align(
                        alignment: Alignment.centerRight,
                        child: Padding(
                          padding: const EdgeInsets.all(16),
                          child: AppButton(
                            label: active!.label,
                            filled: true,
                            focusNode: skipFocus,
                            onPressed: () {
                              unawaited(
                                player.seek(
                                  Duration(
                                    milliseconds: (active!.end * 1000).round(),
                                  ),
                                ),
                              );
                              playFocus.requestFocus();
                              showControls();
                            },
                            isTV: widget.isTV,
                          ),
                        ),
                      ),
                    if (countdownSeconds > 0)
                      Container(
                        color: Colors.black87,
                        padding: const EdgeInsets.all(12),
                        child: Wrap(
                          spacing: 16,
                          crossAxisAlignment: WrapCrossAlignment.center,
                          children: [
                            Text(
                              widget.services.store.flag('NextEpisodePrompt')
                                  ? 'Play the next episode?'
                                  : 'Next episode in $countdownSeconds',
                            ),
                            AppButton(
                              label: 'Play next',
                              onPressed: () => next(),
                              isTV: widget.isTV,
                              filled: true,
                            ),
                            AppButton(
                              label: 'Cancel',
                              onPressed: () {
                                nextTimer?.cancel();
                                setState(() => countdownSeconds = 0);
                              },
                              isTV: widget.isTV,
                            ),
                          ],
                        ),
                      ),
                    Container(
                      color: Colors.black87,
                      padding: const EdgeInsets.fromLTRB(20, 8, 20, 16),
                      child: Column(
                        children: [
                          SizedBox(
                            height: 44,
                            child: Stack(
                              alignment: Alignment.center,
                              children: [
                                Padding(
                                  padding: const EdgeInsets.symmetric(
                                    horizontal: 24,
                                  ),
                                  child: CustomPaint(
                                    size: const Size(double.infinity, 8),
                                    painter: SkipPainter(spans, duration),
                                  ),
                                ),
                                Slider(
                                  value: position.inMilliseconds
                                      .clamp(
                                        0,
                                        math.max(1, duration.inMilliseconds),
                                      )
                                      .toDouble(),
                                  max: math
                                      .max(1, duration.inMilliseconds)
                                      .toDouble(),
                                  onChangeStart: (_) {
                                    hideTimer?.cancel();
                                  },
                                  onChanged: (v) {
                                    setState(
                                      () => position = Duration(
                                        milliseconds: v.round(),
                                      ),
                                    );
                                  },
                                  onChangeEnd: (v) {
                                    unawaited(
                                      player.seek(
                                        Duration(milliseconds: v.round()),
                                      ),
                                    );
                                    showControls();
                                  },
                                ),
                              ],
                            ),
                          ),
                          Row(
                            children: [
                              Text(
                                '${time(position)} / ${time(duration)}',
                                style: const TextStyle(fontSize: 12),
                              ),
                              const Spacer(),
                              if (player.state.buffering)
                                const Text(
                                  'Buffering',
                                  style: TextStyle(color: muted, fontSize: 12),
                                ),
                            ],
                          ),
                          const SizedBox(height: 10),
                          Wrap(
                            spacing: 8,
                            runSpacing: 8,
                            alignment: WrapAlignment.center,
                            children: [
                              AppButton(
                                label: '−10 s',
                                onPressed: () => seek(-10),
                                isTV: widget.isTV,
                              ),
                              AppButton(
                                label: player.state.playing ? 'Pause' : 'Play',
                                icon: player.state.playing
                                    ? Icons.pause
                                    : Icons.play_arrow,
                                focusNode: playFocus,
                                onPressed: () {
                                  unawaited(player.playOrPause());
                                  showControls();
                                },
                                isTV: widget.isTV,
                              ),
                              AppButton(
                                label: '+10 s',
                                onPressed: () => seek(10),
                                isTV: widget.isTV,
                              ),
                              AppButton(
                                label: 'Next episode',
                                onPressed: canNext ? () => next() : null,
                                isTV: widget.isTV,
                              ),
                              AppButton(
                                label: 'Tracks',
                                onPressed: trackPanel,
                                isTV: widget.isTV,
                              ),
                              AppButton(
                                label: 'Quality',
                                onPressed: qualityPanel,
                                isTV: widget.isTV,
                              ),
                              AppButton(
                                label: 'Source',
                                onPressed: sourcePanel,
                                isTV: widget.isTV,
                              ),
                              if ((source?.streams.length ?? 0) > 1)
                                AppButton(
                                  label:
                                      'Copy ${copy + 1}/${source!.streams.length}',
                                  onPressed: () async {
                                    await save();
                                    await open(
                                      mirror:
                                          (copy + 1) % source!.streams.length,
                                    );
                                  },
                                  isTV: widget.isTV,
                                ),
                            ],
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
          ],
        ),
      ),
    ),
  );
}

class SkipPainter extends CustomPainter {
  SkipPainter(this.spans, this.duration);
  final List<SkipSpan> spans;
  final Duration duration;
  @override
  void paint(Canvas canvas, Size size) {
    if (duration.inMilliseconds <= 0) return;
    for (final span in spans) {
      final left =
          (span.start * 1000 / duration.inMilliseconds).clamp(0.0, 1.0) *
          size.width;
      final right =
          (span.end * 1000 / duration.inMilliseconds).clamp(0.0, 1.0) *
          size.width;
      canvas.drawRRect(
        RRect.fromRectAndRadius(
          Rect.fromLTRB(left, 0, right, 8),
          const Radius.circular(3),
        ),
        Paint()..color = span.type == 'ed' ? Colors.blueAccent : shu,
      );
    }
  }

  @override
  bool shouldRepaint(SkipPainter old) =>
      old.spans != spans || old.duration != duration;
}
