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
import 'data/synced_resume.dart';
import 'services.dart';
import 'ui/components.dart';
import 'ui/device_status.dart';
import 'ui/seal.dart';

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
  late Anime anime;
  late final ResumePusher resumePusher;
  int savedResume = 0;
  final completedEpisodes = <int>{};
  bool nextCancelled = false, scrubbing = false;
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
  final failedSources = <String>{};
  bool retrying = false;
  List<SkipSpan> spans = [];
  List<int> episodes = [];
  List<int> availableQualities = [];
  SkipSpan? active;
  final skipped = <String>{};
  Duration position = Duration.zero, duration = Duration.zero;
  bool get canNext => episodes.isNotEmpty
      ? episodes.any((e) => e > episode)
      : anime.airedEpisodes > episode;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    anime = widget.anime;
    episode = widget.episode;
    widget.services.tracking.remember(anime);
    resumePusher = widget.services.tracking.pusher();
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
        if (done && !busy && !completedEpisodes.contains(episode)) {
          finished = true;
          unawaited(save(completed: true));
          offerNext();
        }
      }),
      player.stream.error.listen((message) {
        // A subtitle or track error must not throw away a playing copy; only
        // a file that never loaded counts as a broken stream.
        if (message.toLowerCase().contains('sub') ||
            player.state.duration > Duration.zero) {
          return;
        }
        if (mounted && !busy && !retrying) {
          retrying = true;
          unawaited(
            tryAnotherCopy(automatic: true)
                .whenComplete(() => retrying = false),
          );
        }
      }),
      player.stream.tracks.listen((_) {
        if (!busy) unawaited(preferredTracks(generation));
      }),
      player.stream.playing.listen((playing) {
        if (mounted) {
          showControls();
          if (!playing && !busy) {
            unawaited(save().then((_) => resumePusher.flush()));
          }
        }
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
      unawaited(save().then((_) => resumePusher.flush()));
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
    }
    final height = int.tryParse(quality.replaceAll('p', ''));
    if (height != null) await property('hls-bitrate', 'max');
    availableQualities = [];
    if (!(stream.hls || stream.url.contains('.m3u8'))) return;
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
      availableQualities = choices.map((c) => c.height).toSet().toList()
        ..sort((a, b) => b.compareTo(a));
      if (choices.isNotEmpty && height != null) {
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

  Future<void> open({int? mirror}) async {
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
    if (widget.services.auth.accounts.length > 1 &&
        (anime.id <= 0 || anime.malId <= 0)) {
      try {
        anime = await widget.services.catalog
            .detail(anime, adult: widget.services.store.adultAllowed)
            .timeout(const Duration(seconds: 4));
        widget.services.tracking.remember(anime);
      } catch (_) {
        widget.services.tracking.lastError = 'Unable to link this title to both trackers. Retry from Show details.';
      }
    }
    final historyBeforeOpen = widget.services.store.entry(anime);
    savedResume = math.max(
      savedResume,
      historyBeforeOpen?.episode == episode
          ? historyBeforeOpen?.position ?? 0
          : 0,
    );
    final synced = await widget.services.tracking.resume(anime);
    savedResume = math.max(savedResume, synced.forEpisode(episode));
    if (!mounted || g != generation) return;
    try {
      await player.stop();
      if (mirror == null) {
        await widget.services.core.stopPlayback();
        source = await widget.services.resolver.resolve(
          anime,
          episode,
          scope: scope,
          exclude: failedSources,
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
      // Seeking and adding subtitles before mpv has loaded the file fails, so
      // wait until it reports a duration (a live stream may never do so).
      if (player.state.duration == Duration.zero) {
        try {
          await player.stream.duration
              .firstWhere((d) => d > Duration.zero)
              .timeout(const Duration(seconds: 20));
        } on TimeoutException {
          // Play anyway; tracks are picked when they appear.
        }
      }
      if (!mounted || g != generation) return;
      if (widget.services.store.flag('ResumePlayback') && savedResume > 0) {
        await player.seek(Duration(seconds: savedResume));
      }
      await preferredTracks(g);
      if (!mounted || g != generation) return;
      await player.play();
      setState(() => busy = false);
      unawaited(loadEpisodes(g));
      unawaited(loadSkips(g));
      showControls();
    } catch (_) {
      if (mounted && g == generation) {
        setState(() {
          busy = false;
          error = 'This episode could not be opened.';
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
      anime.malId,
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
    if (player.state.duration <= Duration.zero) return;
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
      for (final track in external) {
        if (await externalSubtitle(track, g, manual: false)) {
          subSelected = true;
          return;
        }
        if (!mounted || g != generation) return;
      }
    }
  }

  Future<bool> externalSubtitle(Json track, int g, {bool manual = true}) async {
    try {
      final stream = source!.streams[copy];
      final headers = Map<String, String>.of(stream.headers);
      if (stream.referrer.isNotEmpty) headers['Referer'] = stream.referrer;
      final response = await widget.services.catalog.client
          .get(Uri.parse(track['url'] as String), headers: headers)
          .timeout(const Duration(seconds: 15));
      if (!mounted || g != generation) return false;
      if (response.statusCode != 200) throw const FormatException();
      // libass receives the original text, including ASS positions and styles.
      await player.setSubtitleTrack(
        SubtitleTrack.data(
          utf8.decode(response.bodyBytes, allowMalformed: true),
          title: track['label'] as String? ?? 'Subtitles',
          language: track['language'] as String?,
        ),
      );
      return true;
    } catch (_) {
      if (manual && mounted && g == generation) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('Subtitle load failed. Choose another track.'),
          ),
        );
      }
      return false;
    }
  }

  void tick(Duration p) {
    if (!mounted || busy) return;
    if (p > Duration.zero && source != null) {
      unawaited(widget.services.store.rememberSource(anime, source!.provider));
    }
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
      } else if (entered &&
          !panelOpen &&
          !scrubbing &&
          !widget.services.store.flag(setting)) {
        setState(() => overlay = true);
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted && active != null) skipFocus.requestFocus();
        });
      }
    }
    if (duration.inSeconds > 0 &&
        duration.inSeconds - p.inSeconds <= 15 &&
        !finished &&
        !nextCancelled &&
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
    if (completed) {
      if (completedEpisodes.add(episode)) {
        await resumePusher.episodeDone(anime.key, episode, anime.episodes);
      }
    } else {
      resumePusher.note(anime.key, episode, position.inSeconds);
    }
    // Keep the resume point even when the watched threshold has been reached.
    await widget.services.store.progress(
      anime,
      episode,
      position.inSeconds,
      duration.inSeconds,
      completed: completed,
    );
    if (watched && !completed) {
      await widget.services.store.markWatched(anime, episode, true);
    }
  }

  void showControls() {
    hideTimer?.cancel();
    if (!mounted) return;
    setState(() => overlay = true);
    hideTimer = Timer(const Duration(seconds: 5), () {
      if (mounted &&
          !panelOpen &&
          !scrubbing &&
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
      for (var i = 0; i < 12 && target <= anime.airedEpisodes; i++) {
        if (!await widget.services.catalog.isFiller(anime.malId, target)) {
          break;
        }
        await widget.services.store.markWatched(anime, target, true);
        target++;
      }
      if (anime.airedEpisodes > 0 && target > anime.airedEpisodes) {
        return;
      }
    }
    if (!mounted) return;
    await resumePusher.flush();
    savedResume = 0;
    nextCancelled = false;
    episode = target;
    failedSources.clear();
    await open();
  }

  void offerNext() {
    if (!canNext || nextCancelled || countdownSeconds > 0) return;
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
              const Text('Subtitles', style: TextStyle(color: accentText)),
              for (final track in player.state.tracks.subtitle)
                ChoiceRow(
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
                ChoiceRow(
                  title: Text(
                    '${track['label'] ?? track['language'] ?? 'Subtitles'}',
                  ),
                  onTap: () {
                    subSelected = true;
                    Navigator.pop(dialog);
                    unawaited(externalSubtitle(track, generation));
                  },
                ),
              const Divider(),
              const Text('Audio', style: TextStyle(color: accentText)),
              for (final track in player.state.tracks.audio)
                ChoiceRow(
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

  Future<void> tryAnotherCopy({bool automatic = false}) async {
    await save();
    if (source != null && copy + 1 < source!.streams.length) {
      await open(mirror: copy + 1);
      return;
    }
    if (source != null) failedSources.add(source!.provider);
    if (failedSources.length >=
        (await widget.services.resolver.providerOrder()).length) {
      if (automatic) {
        if (mounted) {
          setState(() {
            busy = false;
            error = 'This episode could not be played.';
            overlay = true;
          });
        }
        return;
      }
      failedSources.clear();
    }
    if (mounted) await open();
  }

  Future<void> qualityPanel() async {
    final values = [
      'best',
      ...availableQualities.map((h) => '$h'),
      if (availableQualities.length > 1) 'worst',
    ];
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
    unawaited(save().then((_) => resumePusher.flush()));
    for (final subscription in subscriptions) {
      unawaited(subscription.cancel());
    }
    surface.dispose();
    playFocus.dispose();
    skipFocus.dispose();
    unawaited(
      player.dispose().then((_) => widget.services.core.stopPlayback()),
    );
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
  Widget build(BuildContext context) => PopScope(
    canPop: !overlay || busy || error.isNotEmpty,
    onPopInvokedWithResult: (popped, result) {
      if (!popped) {
        setState(() => overlay = false);
        surface.requestFocus();
      }
    },
    child: Scaffold(
      backgroundColor: bgDeep,
      body: Focus(
        skipTraversal: true,
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
                child: Padding(
                  padding: const EdgeInsets.all(32),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      OtakaseSeal(size: widget.isTV ? 96 : 64),
                      const SizedBox(height: 24),
                      const SkeletonBlock(width: 160, height: 2),
                      const SizedBox(height: 24),
                      Text(status, style: uiStyle(context)),
                      const SizedBox(height: 16),
                      AppButton(
                        label: 'Back',
                        onPressed: () => Navigator.pop(context),
                        isTV: widget.isTV,
                      ),
                    ],
                  ),
                ),
              ),
            if (error.isNotEmpty)
              Center(
                child: ColoredBox(
                  color: const Color(0xe6080707),
                  child: EmptyState(
                    error: true,
                    message: error,
                    action: AppButton(
                      label: 'Try another copy',
                      onPressed: tryAnotherCopy,
                      isTV: widget.isTV,
                    ),
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
                          colors: [bgDeep, Color(0x00080707)],
                        ),
                      ),
                      padding: const EdgeInsets.all(16),
                      child: Row(
                        children: [
                          IconButton(
                            tooltip: 'Back',
                            onPressed: () => Navigator.pop(context),
                            icon: const Icon(LucideIcons.arrowLeft),
                          ),
                          const SizedBox(width: 12),
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(
                                  anime.title,
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                  style: uiStyle(
                                    context,
                                    size: 20,
                                    weight: FontWeight.w600,
                                  ),
                                ),
                                const SizedBox(height: 4),
                                Text(
                                  'EPISODE $episode · ${source?.mode.toUpperCase() ?? widget.mode.toUpperCase()}',
                                  style: uiStyle(
                                    context,
                                    size: 15,
                                    color: muted,
                                  ),
                                ),
                              ],
                            ),
                          ),
                          DeviceStatus(isTV: widget.isTV),
                          const SizedBox(width: 12),
                          const SizedBox(
                            width: 48,
                            height: 48,
                            child: Center(child: OtakaseSeal()),
                          ),
                        ],
                      ),
                    ),
                    Expanded(
                      child: Center(
                        child: Wrap(
                          alignment: WrapAlignment.center,
                          spacing: 24,
                          crossAxisAlignment: WrapCrossAlignment.center,
                          children: [
                            TransportButton(
                              label: 'Rewind 10 seconds',
                              text: '−10',
                              onPressed: () => seek(-10),
                            ),
                            TransportButton(
                              label: player.state.playing ? 'Pause' : 'Play',
                              icon: player.state.playing
                                  ? LucideIcons.pause
                                  : LucideIcons.play,
                              primary: true,
                              focusNode: playFocus,
                              onPressed: () {
                                unawaited(player.playOrPause());
                                showControls();
                              },
                            ),
                            TransportButton(
                              label: 'Forward 10 seconds',
                              text: '+10',
                              onPressed: () => seek(10),
                            ),
                          ],
                        ),
                      ),
                    ),
                    if (active != null)
                      Align(
                        alignment: Alignment.centerRight,
                        child: Padding(
                          padding: const EdgeInsets.all(16),
                          child: AppButton(
                            label: active!.type == 'op'
                                ? 'Skip opening'
                                : active!.label,
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
                      Align(
                        alignment: Alignment.centerRight,
                        child: Container(
                          width: 500,
                          margin: const EdgeInsets.symmetric(
                            horizontal: 24,
                            vertical: 8,
                          ),
                          padding: const EdgeInsets.all(12),
                          decoration: BoxDecoration(
                            color: panel,
                            border: Border.all(color: line),
                            borderRadius: BorderRadius.circular(12),
                          ),
                          child: Row(
                            children: [
                              ClipRRect(
                                borderRadius: BorderRadius.circular(12),
                                child: CoverImage(
                                  anime.cover,
                                  width: 54,
                                  height: 81,
                                ),
                              ),
                              const SizedBox(width: 16),
                              Expanded(
                                child: Column(
                                  crossAxisAlignment: CrossAxisAlignment.start,
                                  children: [
                                    Text(
                                      'Next · Episode ${episode + 1}',
                                      style: uiStyle(
                                        context,
                                        weight: FontWeight.w600,
                                      ),
                                    ),
                                    const SizedBox(height: 6),
                                    Text(
                                      widget.services.store.flag(
                                            'NextEpisodePrompt',
                                          )
                                          ? 'Continue the story?'
                                          : 'Playing in $countdownSeconds seconds',
                                      style: uiStyle(context),
                                    ),
                                    const SizedBox(height: 8),
                                    LinearProgressIndicator(
                                      minHeight: 2,
                                      value: countdownSeconds / 10,
                                      color: shu,
                                      backgroundColor: line,
                                    ),
                                    Wrap(
                                      spacing: 8,
                                      children: [
                                        AppButton(
                                          label: 'Play next',
                                          onPressed: () => next(),
                                          isTV: widget.isTV,
                                        ),
                                        AppButton(
                                          label: 'Cancel',
                                          onPressed: () {
                                            nextTimer?.cancel();
                                            nextCancelled = true;
                                            setState(
                                              () => countdownSeconds = 0,
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
                      ),
                    Container(
                      decoration: const BoxDecoration(
                        gradient: LinearGradient(
                          begin: Alignment.topCenter,
                          end: Alignment.bottomCenter,
                          colors: [Color(0x00080707), bgDeep],
                        ),
                      ),
                      padding: const EdgeInsets.fromLTRB(24, 8, 24, 16),
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
                                    size: const Size(double.infinity, 6),
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
                                    scrubbing = true;
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
                                    scrubbing = false;
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
                                style: mono.copyWith(
                                  fontSize: widget.isTV ? 20 : 15,
                                  color: fg,
                                  letterSpacing: 0,
                                ),
                              ),
                              const Spacer(),
                              if (player.state.buffering)
                                Text(
                                  'Buffering',
                                  style: uiStyle(
                                    context,
                                    size: 15,
                                    color: muted,
                                  ),
                                ),
                              const Spacer(),
                              Text(
                                '−${time(duration - position)}',
                                style: mono.copyWith(
                                  fontSize: widget.isTV ? 20 : 15,
                                  color: fg,
                                  letterSpacing: 0,
                                ),
                              ),
                            ],
                          ),
                          const SizedBox(height: 4),
                          Wrap(
                            spacing: 12,
                            runSpacing: 0,
                            alignment: WrapAlignment.center,
                            children: [
                              AppButton(
                                label: 'Next',
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
      final region = Rect.fromLTRB(left, 0, right, 7);
      canvas.save();
      canvas.clipRect(region);
      canvas.drawRect(region, Paint()..color = const Color(0xff302923));
      for (double x = left - 7; x < right; x += 10) {
        canvas.drawPath(
          Path()
            ..moveTo(x, 7)
            ..lineTo(x + 7, 0)
            ..lineTo(x + 11, 0)
            ..lineTo(x + 4, 7)
            ..close(),
          Paint()..color = fg,
        );
      }
      canvas.restore();
    }
  }

  @override
  bool shouldRepaint(SkipPainter old) =>
      old.spans != spans || old.duration != duration;
}
