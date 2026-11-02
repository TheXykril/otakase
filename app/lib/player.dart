import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:flutter/services.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';

import 'cast/cast.dart';
import 'core.dart';
import 'data/models.dart';
import 'desktop/desktop_core.dart';
import 'desktop/player_bar.dart';
import 'desktop/window_frame.dart';
import 'data/store.dart';
import 'data/synced_resume.dart';
import 'services.dart';
import 'tls_ca.dart';
import 'ui/components.dart';
import 'ui/device_status.dart';
import 'ui/seal.dart';
import 'ui/progress_thread.dart';
import 'ui/watch_party_page.dart';

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
  bool profileStopped = false;
  late final Future<void> Function() profileStop;
  late final Future<void> Function(String, Json) commandHandler;
  late final Json Function() statusHandler;
  Timer? discordMeter;
  int savedResume = 0;
  final completedEpisodes = <int>{};
  bool nextCancelled = false, scrubbing = false;
  PlaybackSource? source;
  RequestScope scope = RequestScope();
  final subscriptions = <StreamSubscription<dynamic>>[];
  final surface = FocusNode(debugLabel: 'Playback surface');
  final playFocus = FocusNode(debugLabel: 'Play pause');
  final skipFocus = FocusNode(debugLabel: 'Skip segment');
  Timer? hideTimer, saveTimer, nextTimer, loadingTimer;
  int copy = 0, generation = 0, countdownSeconds = 0;
  bool overlay = true,
      fullscreen = false,
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
  late final CastClient caster = CastClient(widget.services.core);

  /// The device the episode plays on instead of this screen, or null.
  CastDevice? castDevice;
  CastStatus? castStatus;
  Timer? castTimer;
  int castEnded = -1;
  bool castPolling = false;

  /// The external subtitle track last picked, which a cast takes along.
  Json? externalTrack;
  bool get casting => castDevice != null;
  bool get isPlaying =>
      casting ? castStatus?.paused != true : player.state.playing;
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
    profileStop = stopForProfileSwitch;
    widget.services.stopPlayback = profileStop;
    commandHandler = control;
    widget.services.playerCommand = commandHandler;
    statusHandler = remoteStatus;
    widget.services.playerStatus = statusHandler;
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
    discordMeter = Timer.periodic(const Duration(seconds: 5), (_) {
      if (!profileStopped && !busy && duration > Duration.zero) {
        unawaited(widget.services.party.playing(anime, episode));
        widget.services.discord.observe(
          anime,
          episode,
          (casting ? position : player.state.position).inMilliseconds / 1000,
        );
      }
    });
    subscriptions.addAll([
      player.stream.position.listen((p) {
        if (!casting) tick(p);
      }),
      player.stream.duration.listen((d) {
        if (!mounted || d <= Duration.zero || casting) return;
        final changed = duration == Duration.zero;
        setState(() => duration = d);
        if (changed) unawaited(loadSkips(generation));
      }),
      player.stream.completed.listen((done) {
        if (done && !busy && !casting && !completedEpisodes.contains(episode)) {
          finished = true;
          unawaited(save(completed: true));
          offerNext();
        }
      }),
      player.stream.error.listen((message) {
        // A subtitle or track error must not throw away a playing copy; only
        // a file that never loaded counts as a broken stream.
        if (casting ||
            message.toLowerCase().contains('sub') ||
            player.state.duration > Duration.zero) {
          return;
        }
        if (mounted && !busy && !retrying) {
          if (source != null) {
            unawaited(
              widget.services.discord.reportProvider(
                source!.provider,
                false,
                'playback',
                message,
              ),
            );
          }
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
          if (!playing && !busy && !casting) {
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
      // A cast plays on with this screen off.
      if (!casting) unawaited(player.pause());
    }
  }

  bool? subsLifted;

  /// Moves subtitles above the desktop control bar while it shows, like the
  /// CLI mpv skin does.
  void liftSubtitles(bool up) {
    if (!isDesktop || subsLifted == up) return;
    subsLifted = up;
    final base = widget.services.store.integer('SubtitlePosition');
    unawaited(
      property('sub-pos', (up ? math.max(0, base - 16) : base).toString()),
    );
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
    subsLifted = null;
    await property('af', audioFilter(s));
    for (final e in (await tlsOptions()).entries) {
      await property(e.key, e.value);
    }
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

  /// Shows how much of a torrent copy has arrived while the player waits for
  /// its first frames.
  void watchLoading(int g) {
    loadingTimer?.cancel();
    setState(() => status = 'Loading');
    loadingTimer = Timer.periodic(const Duration(seconds: 1), (timer) async {
      if (!mounted || g != generation || !busy) {
        timer.cancel();
        return;
      }
      final p = await widget.services.core.torrentProgress();
      if (p == null || !mounted || g != generation || !busy) return;
      final mb = p.completed ~/ (1 << 20);
      setState(() => status = mb > 0 ? 'Loading · $mb MB' : 'Loading');
    });
  }

  Future<void> open({int? mirror}) async {
    if (profileStopped) return;
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
    if ((historyBeforeOpen?.watched.contains(episode) ?? false) ||
        episode <= widget.services.tracking.progress(anime)) {
      savedResume = 0;
    }
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
      if (stream.torrent) watchLoading(g);
      if (casting) {
        if (externalTrack != null &&
            !stream.subtitles.any((t) => t['url'] == externalTrack!['url'])) {
          externalTrack = null;
        }
        await castCurrent(
          start: widget.services.store.flag('ResumePlayback')
              ? savedResume.toDouble()
              : 0,
        );
        if (!mounted || g != generation) return;
        setState(() => busy = false);
        unawaited(loadEpisodes(g));
        showControls();
        return;
      }
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
    externalTrack = track;
    if (casting) {
      if (manual) unawaited(castCurrent(start: position.inSeconds.toDouble()));
      return true;
    }
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
    if (profileStopped) return;
    if (!mounted || busy) return;
    if (p > Duration.zero && source != null) {
      widget.services.discord.observe(anime, episode, p.inMilliseconds / 1000);
      unawaited(
        widget.services.discord.reportProvider(
          source!.provider,
          true,
          'playback',
          '',
        ),
      );
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
        unawaited(seekTo(Duration(milliseconds: (current.end * 1000).round())));
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
    if (profileStopped) return;
    if (busy || duration <= Duration.zero) return;
    final watched =
        completed ||
        position.inSeconds / duration.inSeconds * 100 >=
            widget.services.store.integer('PercentageToMarkComplete');
    if (watched) {
      if (completedEpisodes.add(episode)) {
        await resumePusher.episodeDone(anime.key, episode, anime.episodes);
      }
    } else {
      resumePusher.note(anime.key, episode, position.inSeconds);
    }
    // Past the watched threshold the episode counts as seen: no resume point,
    // so it reads "Watched" and opens from the start next time.
    await widget.services.store.progress(
      anime,
      episode,
      watched ? 0 : position.inSeconds,
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
          !casting &&
          player.state.playing) {
        setState(() => overlay = false);
        surface.requestFocus();
      }
    });
  }

  Future<void> seek(int seconds) async {
    showControls();
    await seekTo(
      Duration(
        milliseconds: (position.inMilliseconds + seconds * 1000).clamp(
          0,
          math.max(0, duration.inMilliseconds),
        ),
      ),
    );
  }

  Future<void> playPause() => isPlaying ? pausePlayback() : resumePlayback();

  Future<void> pausePlayback() async {
    if (!casting) return player.pause();
    await castControl('pause');
  }

  Future<void> resumePlayback() async {
    if (!casting) return player.play();
    await castControl('play');
  }

  /// Moves playback here or on the cast device.
  Future<void> seekTo(Duration target) async {
    if (!casting) return player.seek(target);
    setState(() => position = target);
    await castControl('seek', target.inMilliseconds / 1000);
  }

  Future<void> castControl(String action, [double value = 0]) async {
    try {
      await caster.control(action, value);
      await pollCast();
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('The cast device did not answer.')),
        );
      }
    }
  }

  /// Picks a device and moves the episode there, or offers to stop.
  Future<void> castMenu() async {
    panelOpen = true;
    showControls();
    final device = await pickCastDevice(context, caster);
    panelOpen = false;
    if (device == null || !mounted) return;
    await startCast(device);
  }

  Future<void> startCast(CastDevice device) async {
    final at = position.inSeconds.toDouble();
    await save();
    castDevice = device;
    castStatus = null;
    castEnded = -1;
    await player.pause();
    if (!mounted) return;
    setState(() => overlay = true);
    if (source == null || busy) return; // open() casts once it resolves.
    await castCurrent(start: at);
  }

  /// The subtitles a cast takes: the external track picked, or the first one
  /// matching the subtitle languages.
  String castSubtitle(ResolvedStream stream) {
    if (externalTrack case final Json track) {
      return track['url'] as String? ?? '';
    }
    if ((source?.mode ?? widget.mode) == 'dub') return '';
    final preferences = widget.services.store
        .text('SubsLanguage')
        .toLowerCase()
        .split(',')
        .map((v) => v.trim())
        .where((v) => v.isNotEmpty);
    for (final preferred in preferences) {
      if (preferred == 'none' || preferred == 'off') return '';
      for (final t in stream.subtitles) {
        if (languageMatches(
          t['language'] as String?,
          t['label'] as String?,
          preferred,
        )) {
          return t['url'] as String? ?? '';
        }
      }
    }
    if (stream.subtitles.isNotEmpty) {
      return stream.subtitles.first['url'] as String? ?? '';
    }
    return stream.subtitle.startsWith('http') ? stream.subtitle : '';
  }

  /// Sends the current copy of the episode to the cast device.
  Future<void> castCurrent({double start = 0}) async {
    final device = castDevice;
    if (device == null || source == null) return;
    final stream = source!.streams[copy];
    try {
      await caster.start(
        device: device,
        stream: stream,
        title: '${anime.title} · Episode $episode',
        subtitle: castSubtitle(stream),
        start: start,
        duration: duration.inMilliseconds / 1000,
      );
    } on CoreFailure catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context)
          .showSnackBar(SnackBar(content: Text(e.message)));
      castDevice = null;
      setState(() {});
      return;
    }
    castTimer?.cancel();
    castTimer = Timer.periodic(
      const Duration(seconds: 1),
      (_) => unawaited(pollCast()),
    );
  }

  Future<void> pollCast() async {
    if (!casting || castPolling || !mounted) return;
    castPolling = true;
    try {
      final s = await caster.status();
      if (!mounted || !casting) return;
      if (!s.active) {
        // Stopped from the browser page.
        await stopCast(remote: true);
        return;
      }
      final hadDuration = duration > Duration.zero;
      setState(() {
        castStatus = s;
        if (s.duration > 0) {
          duration = Duration(milliseconds: (s.duration * 1000).round());
        }
      });
      if (!hadDuration && duration > Duration.zero) {
        unawaited(loadSkips(generation));
      }
      if (busy) return;
      if (s.state == 'playing' || s.state == 'paused') {
        tick(Duration(milliseconds: (s.position * 1000).round()));
      }
      if (s.state == 'ended' && castEnded != s.load) {
        castEnded = s.load;
        if (!completedEpisodes.contains(episode)) {
          finished = true;
          await save(completed: true);
          offerNext();
        }
      }
    } catch (_) {
      // A missed poll is retried in a second.
    } finally {
      castPolling = false;
    }
  }

  /// Ends the cast. Unless the screen is being left, the episode carries on
  /// here from where the device was.
  Future<void> stopCast({bool resumeHere = true, bool remote = false}) async {
    castTimer?.cancel();
    castTimer = null;
    if (!remote) {
      try {
        await caster.control('stop');
      } catch (_) {
        /* The device may already be gone. */
      }
    }
    castDevice = null;
    castStatus = null;
    if (!resumeHere || !mounted) return;
    await save();
    savedResume = position.inSeconds;
    setState(() {});
    if (source != null) await open(mirror: copy);
  }

  Future<void> control(String action, Json args) async {
    if (!mounted || profileStopped) return;
    switch (action) {
      case 'pause':
        await pausePlayback();
      case 'play':
        await resumePlayback();
      case 'toggle':
        await playPause();
      case 'skip':
        await seek(85);
      case 'seek':
        await seek(number(args['seconds']).clamp(-86400, 86400));
      case 'seek_to':
        // A watch party follows the host: an absolute position.
        final to = (args['position'] as num?)?.toDouble() ?? 0;
        await seekTo(
          Duration(
            milliseconds: (to * 1000).round().clamp(
              0,
              math.max(0, duration.inMilliseconds),
            ),
          ),
        );
      case 'party_skip':
        // What a party voted to skip: the opening or ending when it is on,
        // else 85 s, as in the CLI.
        final span = active;
        if (span != null) {
          await seekTo(Duration(milliseconds: (span.end * 1000).round()));
        } else {
          await seek(85);
        }
      case 'volume':
        final level = number(args['volume']).clamp(0, 100).toDouble();
        if (casting) {
          await castControl('volume', level / 100);
        } else {
          await player.setVolume(level);
        }
      case 'next':
        if (!busy) await next();
      case 'previous':
        if (!busy && episode > 1) await pickEpisode(episode - 1);
      case 'episode':
        if (!busy) await pickEpisode(number(args['episode']));
      case 'stop':
        await save();
        await resumePusher.flush();
        if (casting) await stopCast(resumeHere: false);
        await player.stop();
        if (mounted) Navigator.pop(context);
    }
    if (mounted) showControls();
  }

  Json remoteStatus() => {
    'has_player': true,
    'anilist_id': anime.id,
    'loaded': !busy && duration > Duration.zero,
    'position_ms': (casting ? position : player.state.position).inMilliseconds,
    'title': anime.title,
    'adult': anime.isAdult,
    'episode': episode,
    'episodes': anime.airedEpisodes,
    'position': position.inSeconds,
    'duration': duration.inSeconds,
    'playing': isPlaying,
    'volume': casting
        ? ((castStatus?.volume ?? 1) * 100).round()
        : player.state.volume.round(),
  };

  Future<void> pickEpisode(int target) async {
    if (target < 1 || anime.airedEpisodes > 0 && target > anime.airedEpisodes) {
      return;
    }
    await save();
    await resumePusher.flush();
    if (!mounted || profileStopped) return;
    savedResume = 0;
    nextCancelled = false;
    episode = target;
    failedSources.clear();
    await open();
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
        (await widget.services.resolver.sourcesFor(anime)).length) {
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
      unawaited(playPause());
      showControls();
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.mediaPlay) {
      unawaited(resumePlayback());
      showControls();
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.mediaPause ||
        key == LogicalKeyboardKey.mediaStop) {
      unawaited(pausePlayback());
      showControls();
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.mediaTrackNext) {
      unawaited(next());
      return KeyEventResult.handled;
    }
    if (widget.services.party.active &&
        HardwareKeyboard.instance.isAltPressed &&
        (key == LogicalKeyboardKey.keyK || key == LogicalKeyboardKey.keyN)) {
      widget.services.party.vote(
        key == LogicalKeyboardKey.keyN ? 'next' : 'skip',
      );
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
        unawaited(playPause());
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
    discordMeter?.cancel();
    unawaited(widget.services.discord.stopWatching());
    generation++;
    scope.cancel();
    hideTimer?.cancel();
    saveTimer?.cancel();
    nextTimer?.cancel();
    loadingTimer?.cancel();
    castTimer?.cancel();
    if (casting) unawaited(caster.control('stop').catchError((_) {}));
    if (!profileStopped) unawaited(save().then((_) => resumePusher.flush()));
    if (identical(widget.services.stopPlayback, profileStop)) {
      widget.services.stopPlayback = null;
    }
    if (identical(widget.services.playerCommand, commandHandler)) {
      widget.services.playerCommand = null;
    }
    if (identical(widget.services.playerStatus, statusHandler)) {
      widget.services.playerStatus = null;
    }
    for (final subscription in subscriptions) {
      unawaited(subscription.cancel());
    }
    if (fullscreen) unawaited(setDesktopFullScreen(false));
    surface.dispose();
    playFocus.dispose();
    skipFocus.dispose();
    unawaited(
      player.dispose().then((_) async {
        if (!profileStopped) await widget.services.core.stopPlayback();
      }),
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

  Future<void> stopForProfileSwitch() async {
    if (profileStopped) return;
    hideTimer?.cancel();
    saveTimer?.cancel();
    nextTimer?.cancel();
    loadingTimer?.cancel();
    discordMeter?.cancel();
    await widget.services.discord.stopWatching();
    if (identical(widget.services.playerCommand, commandHandler)) {
      widget.services.playerCommand = null;
    }
    if (identical(widget.services.playerStatus, statusHandler)) {
      widget.services.playerStatus = null;
    }
    generation++;
    scope.cancel();
    await player.pause();
    if (casting) await stopCast(resumeHere: false);
    await save();
    await resumePusher.flush().timeout(
      const Duration(seconds: 5),
      onTimeout: () {},
    );
    profileStopped = true;
    await player.stop();
    await widget.services.core.stopPlayback();
  }

  double lastVolume = 100;

  void toggleMute() {
    final volume = player.state.volume;
    if (volume > 0) lastVolume = volume;
    unawaited(player.setVolume(volume > 0 ? 0 : lastVolume));
    showControls();
  }

  Future<void> toggleFullscreen() async {
    final on = await setDesktopFullScreen(!fullscreen);
    if (mounted) setState(() => fullscreen = on);
    showControls();
  }

  /// The desktop controls: the CLI mpv skin look, with the skip button and
  /// next-episode card above the bar.
  Widget desktopControls(BuildContext context) {
    liftSubtitles(true);
    return Stack(
      fit: StackFit.expand,
      children: [
        DesktopPlayerBar(
          title: anime.title,
          subtitle:
              'Ep $episode · ${source?.mode.toUpperCase() ?? widget.mode.toUpperCase()}',
          position: position,
          duration: duration,
          buffered: player.state.buffer,
          playing: isPlaying,
          buffering: casting
              ? castStatus?.state == 'buffering' ||
                    castStatus?.state == 'loading'
              : player.state.buffering,
          silent: player.state.volume == 0,
          fullscreen: fullscreen,
          trackLabel: source?.mode.toUpperCase() ?? widget.mode.toUpperCase(),
          centerLabel: 'Episode $episode',
          stripes: spans,
          playFocus: playFocus,
          onBack: () => Navigator.pop(context),
          onPlayPause: () {
            unawaited(playPause());
            showControls();
          },
          casting: casting,
          onCast: () => unawaited(castMenu()),
          onSeekBy: (seconds) => unawaited(seek(seconds)),
          onSeekStart: (_) {
            scrubbing = true;
            hideTimer?.cancel();
          },
          onSeekChanged: (v) =>
              setState(() => position = Duration(milliseconds: v.round())),
          onSeekEnd: (v) {
            scrubbing = false;
            unawaited(seekTo(Duration(milliseconds: v.round())));
            showControls();
          },
          onNext: canNext ? () => unawaited(next()) : null,
          onMute: toggleMute,
          onTracks: () => unawaited(trackPanel()),
          onQuality: () => unawaited(qualityPanel()),
          onFullscreen: () => unawaited(toggleFullscreen()),
        ),
        Positioned(
          right: 40,
          bottom: 170,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.end,
            children: [
              if (active != null) skipButton(),
              if (countdownSeconds > 0) nextCard(context),
            ],
          ),
        ),
      ],
    );
  }

  Widget skipButton() => Align(
    alignment: Alignment.centerRight,
    child: Padding(
      padding: const EdgeInsets.all(16),
      child: AppButton(
        label: active!.type == 'op' ? 'Skip opening' : active!.label,
        focusNode: skipFocus,
        onPressed: () {
          unawaited(
            seekTo(Duration(milliseconds: (active!.end * 1000).round())),
          );
          playFocus.requestFocus();
          showControls();
        },
        isTV: widget.isTV,
      ),
    ),
  );

  Widget nextCard(BuildContext context) => Align(
    alignment: Alignment.centerRight,
    child: Container(
      width: 500,
      margin: const EdgeInsets.symmetric(horizontal: 24, vertical: 8),
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
            child: CoverImage(anime.cover, width: 54, height: 81),
          ),
          const SizedBox(width: 16),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'Next · Episode ${episode + 1}',
                  style: uiStyle(context, weight: FontWeight.w600),
                ),
                const SizedBox(height: 6),
                Text(
                  widget.services.store.flag('NextEpisodePrompt')
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
                        setState(() => countdownSeconds = 0);
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
  );

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
        // With a mouse, moving it brings the controls back and keeps them up,
        // and the pointer hides with them.
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
              if (casting && !busy && error.isEmpty)
                CastPanel(
                  status: castStatus,
                  deviceName: castDevice!.name,
                  isTV: widget.isTV,
                  onStop: () => unawaited(stopCast()),
                ),
              PartyOverlay(
                party: widget.services.party,
                controls: overlay && !busy,
                isTV: widget.isTV,
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
              if (overlay && !busy && error.isEmpty && isDesktop)
                desktopControls(context)
              else if (isDesktop)
                Builder(
                  builder: (_) {
                    liftSubtitles(false);
                    return const SizedBox.shrink();
                  },
                ),
              if (overlay && !busy && error.isEmpty && !isDesktop)
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
                            if (!widget.isTV)
                              IconButton(
                                key: const Key('player-cast'),
                                tooltip: casting ? 'Casting' : 'Cast',
                                onPressed: () => unawaited(castMenu()),
                                icon: Icon(
                                  LucideIcons.cast,
                                  color: casting ? accentText : null,
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
                                label: isPlaying ? 'Pause' : 'Play',
                                icon: isPlaying
                                    ? LucideIcons.pause
                                    : LucideIcons.play,
                                primary: true,
                                focusNode: playFocus,
                                onPressed: () {
                                  unawaited(playPause());
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
                      if (active != null) skipButton(),
                      if (countdownSeconds > 0) nextCard(context),
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
                                  KageSeekSlider(
                                    key: const Key('player-timeline'),
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
                                        seekTo(
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
                                if (casting
                                    ? castStatus?.state == 'buffering'
                                    : player.state.buffering)
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
