import 'dart:async';

import 'data/tv_remote.dart';
import 'ui/remote_now_playing.dart';
import 'ui/play_on.dart';

import 'dart:convert';
import 'dart:io';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';
import 'package:path_provider/path_provider.dart';

import 'cast/cast.dart';
import 'core.dart';
import 'data/episode_completion.dart';
import 'data/external_subtitles.dart';
import 'data/hls_quality.dart';
import 'data/models.dart';
import 'data/play_request.dart';
import 'desktop/desktop_core.dart';
import 'desktop/player_bar.dart';
import 'desktop/window_frame.dart';
import 'data/skip_times.dart';
import 'data/store.dart';
import 'data/synced_resume.dart';
import 'data/subtitle_fallback.dart';
import 'services.dart';
import 'tls_ca.dart';
import 'ui/components.dart';
import 'ui/screen_orientation.dart';
import 'ui/seal.dart';
import 'ui/skip_marker.dart';
import 'ui/player_keys.dart';
import 'ui/rate_prompt.dart';
import 'ui/phone_player.dart';
import 'ui/playback_report.dart';
import 'ui/watch_party_page.dart';
import 'ui/mpv.dart';
import 'host.dart';

/// The one player for everything: anime episodes, films, series episodes
/// and downloads. What a title has no data for stays hidden: films have no
/// skip times, episode list, markers or watch party, so those buttons and
/// keys never show for them.
class PlayerPage extends StatefulWidget {
  const PlayerPage({
    super.key,
    required this.services,
    required Anime this.anime,
    required this.episode,
    required this.isTV,
    required this.mode,
    this.startAt = 0,
    this.castTo,
  }) : request = null;

  /// A film, a series episode or a download: the request finds the copy and
  /// keeps the watch history.
  const PlayerPage.title({
    super.key,
    required this.services,
    required PlayRequest this.request,
    required this.isTV,
    this.startAt = 0,
    this.castTo,
  }) : anime = null,
       episode = 0,
       mode = 'sub';
  final AppServices services;

  /// Where to start, in seconds, when a phone handed the episode over;
  /// 0 resumes as usual.
  final int startAt;

  /// A cast device picked before the player opened: it casts right away.
  final CastDevice? castTo;
  final Anime? anime;
  final PlayRequest? request;
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

  /// What plays when this is a film, series episode or download; null for
  /// an anime episode. It changes as the next episode starts.
  PlayRequest? request;
  bool get isTitle => request != null;

  /// The copy the request found.
  PlayableMedia? media;

  /// A title counted as watched (the percentage rule or its end), and
  /// whether Trakt was told it stopped, so later events do not put it back
  /// in Trakt's paused list.
  bool markedWatched = false, stopSent = false, started = false;
  bool castPlaying = false;

  /// Where the title's copy started; a spot saved from playing counts as
  /// watched up to it.
  Duration startedAt = Duration.zero;

  /// The episode a title's countdown leads to.
  PlayRequest? upNext;

  /// The audio and subtitles playing when the same copy is opened again
  /// (a quality change or a dropped stream), so they come back.
  ({AudioTrack audio, SubtitleTrack subtitle, Json? external})? keepTracks;

  void rememberTracks() => keepTracks = (
    audio: player.state.track.audio,
    subtitle: player.state.track.subtitle,
    external: externalTrack,
  );

  Future<void> restoreTracks(
    ({AudioTrack audio, SubtitleTrack subtitle, Json? external}) keep,
    int g,
  ) async {
    final tracks = player.state.tracks;
    final audio = tracks.audio.where((t) => t.id == keep.audio.id);
    if (audio.isNotEmpty) {
      audioSelected = true;
      await player.setAudioTrack(audio.first);
    }
    if (!mounted || g != generation) return;
    if (keep.external case final track?) {
      subSelected = true;
      await externalSubtitle(track, g, ++subtitleRequest, manual: false);
      return;
    }
    final subtitle = keep.subtitle.id == 'no'
        ? SubtitleTrack.no()
        : tracks.subtitle.where((t) => t.id == keep.subtitle.id).firstOrNull;
    if (subtitle != null) {
      subSelected = true;
      subtitleRequest++;
      await player.setSubtitleTrack(subtitle);
    }
  }

  bool profileStopped = false;
  late final Future<void> Function() profileStop;
  late final Future<void> Function(String, Json) commandHandler;
  late final Json Function() statusHandler;
  Timer? discordMeter;
  int savedResume = 0;
  final completedEpisodes = <int>{};
  var viewing = EpisodeViewing();
  String? viewingKey;
  bool nextCancelled = false, scrubbing = false;
  PlaybackSource? source;
  RequestScope scope = RequestScope();
  final subscriptions = <StreamSubscription<dynamic>>[];
  final surface = FocusNode(debugLabel: 'Playback surface');
  final playFocus = FocusNode(debugLabel: 'Play pause');
  final skipFocus = FocusNode(debugLabel: 'Skip segment');
  Timer? hideTimer, saveTimer, nextTimer, loadingTimer;
  final stall = PlaybackStallWatchdog();
  bool playRequested = false;
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
  bool retrying = false, reopenedCopy = false, streamDropped = false;

  /// Shown instead of the search and loading steps while a dropped stream
  /// comes back.
  static const reconnecting = 'Reconnecting';
  List<SkipSpan> spans = [];
  List<int> episodes = [];
  List<int> availableQualities = [];
  SkipSpan? active;
  final skipped = <String>{};

  /// The kind of span the viewer is marking, or null.
  String? marking;
  SkipSpan? markingExisting;
  Duration position = Duration.zero, duration = Duration.zero;
  Duration playbackPosition = Duration.zero;
  late final CastClient caster = CastClient(widget.services.core);

  /// The device the episode plays on instead of this screen, or null.
  CastDevice? castDevice;
  CastStatus? castStatus;
  Timer? castTimer;
  int castEnded = -1;
  bool castPolling = false;

  /// The external subtitle track last picked, which a cast takes along.
  Json? externalTrack;
  int subtitleRequest = 0;
  int? selectingTracks;
  bool skipEpisodeSubtitles = false;
  ({String provider, int copy})? unsubtitledCopy;
  bool get casting => castDevice != null;
  bool get isPlaying =>
      casting ? castStatus?.paused != true : player.state.playing;
  bool get canNext => isTitle
      ? upNext != null || request!.next != null
      : episodes.isNotEmpty
      ? episodes.any((e) => e > episode)
      : anime.airedEpisodes > episode;

  /// Whether an episode comes before this one: a series episode's request
  /// knows, an anime episode is any after the first.
  bool get canPrevious =>
      isTitle ? request!.previous?.call() != null : episode > 1;
  @override
  void initState() {
    super.initState();
    FullBleed.enter();
    WidgetsBinding.instance.addObserver(this);
    request = widget.request;
    anime = widget.anime ?? Anime(id: 0, title: request!.title);
    if (isTitle) status = 'Finding a copy';
    episode = widget.episode;
    if (!isTitle) widget.services.tracking.remember(anime);
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
      if (!profileStopped && !busy && duration > Duration.zero && isTitle) {
        showPresence();
        // Films and series count towards Otakase watch time on Discord and
        // show in Who's watching too, by name: they have no AniList id.
        final r = request!;
        widget.services.discord.observe(
          Anime(
            id: 0,
            title: [r.title, r.subtitle].where((s) => s.isNotEmpty).join(' · '),
          ),
          0,
          (casting ? position : player.state.position).inMilliseconds / 1000,
        );
      } else if (!profileStopped && !busy && duration > Duration.zero) {
        unawaited(widget.services.party.playing(anime, episode));
        widget.services.discord.observe(
          anime,
          episode,
          (casting ? position : player.state.position).inMilliseconds / 1000,
        );
        unawaited(
          widget.services.presence.episode(
            anime,
            episode,
            paused: !isPlaying,
            position:
                (casting ? position : player.state.position).inMilliseconds /
                1000,
            duration: duration.inMilliseconds / 1000,
            joinUrl: widget.services.party.watchWithMe
                ? widget.services.party.joinUrl ?? ''
                : '',
          ),
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
        if (changed && !busy) unawaited(loadSkips(generation));
      }),
      player.stream.completed.listen((done) {
        if (done && !casting) playbackEnded();
      }),
      player.stream.error.listen((message) {
        if (!casting &&
            player.state.duration == Duration.zero &&
            !message.toLowerCase().contains('sub')) {
          unawaited(recoverPlayback(message, reopen: false));
        }
      }),
      player.stream.tracks.listen((_) {
        if (!busy) unawaited(preferredTracks(generation));
      }),
      player.stream.playing.listen((playing) {
        if (mounted) {
          showControls();
          watchForStall();
          if (isTitle && !busy && !casting && started) {
            scrobble(playing ? 'start' : 'pause');
            showPresence();
          }
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
    castDevice = widget.castTo;
    unawaited(
      open(
        resumeFrom: widget.startAt > 0
            ? Duration(seconds: widget.startAt)
            : null,
      ),
    );
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed &&
        isTitle &&
        started &&
        !busy &&
        !casting &&
        player.state.playing) {
      scrobble('start');
    }
    if (state != AppLifecycleState.resumed) {
      final leaving =
          !casting &&
          (state == AppLifecycleState.hidden ||
              state == AppLifecycleState.paused);
      unawaited(save(leaving: leaving).then((_) => resumePusher.flush()));
      // A cast plays on with this screen off. A computer window that only
      // lost focus (inactive) or was minimised keeps playing, as films do;
      // a phone or TV leaving the app pauses.
      if (!casting && !isDesktop && leaving) unawaited(pausePlayback());
      // A computer keeps playing, but another device should still be able
      // to pick a title up from here.
      if (isTitle && leaving && isDesktop && !casting) scrobble('pause');
    }
  }

  /// The Discord status for a film or series episode.
  void showPresence() {
    final r = request;
    if (r == null || busy || duration <= Duration.zero) return;
    unawaited(
      widget.services.presence.movie(
        r.title,
        r.subtitle,
        paused: !isPlaying,
        position:
            (casting ? position : player.state.position).inMilliseconds / 1000,
        duration: duration.inMilliseconds / 1000,
      ),
    );
  }

  double get percent => duration.inMilliseconds <= 0
      ? 0
      : playbackPosition.inMilliseconds * 100 / duration.inMilliseconds;

  /// Tells Trakt a title started, paused or stopped.
  void scrobble(String action) {
    final r = request;
    if (r == null) return;
    if (action == 'stop') return sendStop();
    if (stopSent || markedWatched) return;
    // Before the first frame the spot is not known yet; 0% would tell
    // Trakt the title was restarted.
    if (action == 'pause' && playbackPosition == Duration.zero) return;
    r.onScrobble?.call(action, percent);
  }

  /// Tells Trakt the title is done, once.
  void sendStop() {
    if (stopSent) return;
    stopSent = true;
    request?.onScrobble?.call('stop', 100);
  }

  /// Starts [next] from its own beginning: nothing of the last title's
  /// watch state carries over.
  void startTitle(PlayRequest next) {
    request = next;
    anime = Anime(id: 0, title: next.title);
    media = null;
    markedWatched = false;
    stopSent = false;
    started = false;
    upNext = null;
    nextCancelled = false;
    completedEpisodes.clear();
    savedResume = 0;
    failedSources.clear();
  }

  /// Whether [m] is a file on this device. The core serves it to a cast
  /// device from here.
  static bool local(PlayableMedia m) => !m.url.startsWith('http');

  /// Whether a download is playing, so its subtitle files can be cast too.
  bool get castsFile => media != null && local(media!);

  /// Whether the copy's own audio is already in the first subtitle language,
  /// such as a Lithuanian dub, so no subtitles are wanted.
  bool get dubbedTitle {
    final m = media;
    if (m == null || m.audioLanguage.isEmpty) return false;
    final first = widget.services.store
        .text('SubsLanguage')
        .toLowerCase()
        .split(',')
        .map((v) => v.trim())
        .where((v) => v.isNotEmpty)
        .firstOrNull;
    if (first == null) return false;
    return m.audioLanguage
        .split(',')
        .any((code) => languageMatches(code.trim(), '', first));
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
    await player.mpvSet(name, value);
  }

  Future<void> configure() async {
    final s = widget.services.store;
    await property('sub-visibility', 'yes');
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
    final choices = await fetchHlsVariants(
      widget.services.catalog.client,
      stream.url,
      headers,
    );
    availableQualities = hlsHeights(choices);
    // Without a readable master the default quality stays.
    if (choices.isNotEmpty && height != null) {
      await property('hls-bitrate', hlsBitrate(quality, choices));
    }
  }

  /// Shows [text] under the loading seal. While a dropped stream is being
  /// brought back the screen keeps saying so instead of listing each step.
  void showStatus(String text) {
    if (!mounted) return;
    setState(() => status = streamDropped ? reconnecting : text);
  }

  /// Shows how much of a torrent copy has arrived while the player waits for
  /// its first frames.
  void watchLoading(int g) {
    loadingTimer?.cancel();
    showStatus('Loading');
    loadingTimer = Timer.periodic(const Duration(seconds: 1), (timer) async {
      if (!mounted || g != generation || !busy) {
        timer.cancel();
        return;
      }
      final p = await widget.services.core.torrentProgress();
      if (p == null || !mounted || g != generation || !busy) return;
      final mb = p.completed ~/ (1 << 20);
      showStatus(mb > 0 ? 'Loading · $mb MB' : 'Loading');
    });
  }

  Future<void> open({
    int? mirror,
    Duration? resumeFrom,
    bool reopening = false,
    String? onlyProvider,
  }) async {
    marking = null;
    if (profileStopped) return;
    final g = ++generation;
    final keep = keepTracks;
    keepTracks = null;
    scope.cancel();
    scope = RequestScope();
    final requestScope = scope;
    unsubtitledCopy = null;
    nextTimer?.cancel();
    stall.cancel();
    playRequested = false;
    reopenedCopy = reopening;
    countdownSeconds = 0;
    playbackPosition = Duration.zero;
    viewing = EpisodeViewing();
    viewingKey = null;
    setState(() {
      busy = true;
      error = '';
      active = null;
      spans = [];
      finished = false;
      overlay = true;
      duration = Duration.zero;
      position = Duration.zero;
      if (streamDropped) {
        status = reconnecting;
      } else if (isTitle && (mirror == null || source == null)) {
        status = 'Finding a copy';
      }
    });
    skipped.clear();
    subSelected = false;
    subtitleRequest++;
    audioSelected = false;
    if (!isTitle &&
        widget.services.auth.accounts.length > 1 &&
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
    if (resumeFrom != null) {
      savedResume = resumeFrom.inSeconds;
    } else if (isTitle) {
      // Known once the copy reports its length.
      savedResume = 0;
    } else {
      final historyBeforeOpen = widget.services.store.entry(anime);
      savedResume = math.max(
        savedResume,
        historyBeforeOpen?.resumeFor(episode) ?? 0,
      );
      final synced = await widget.services.tracking.resume(anime);
      savedResume = math.max(savedResume, synced.forEpisode(episode));
      if ((historyBeforeOpen?.watched.contains(episode) ?? false) ||
          episode <= widget.services.tracking.progress(anime)) {
        savedResume = 0;
      }
    }
    if (!mounted || g != generation) return;
    try {
      await player.stop();
      await clearSubtitleFiles();
      if (isTitle && (mirror == null || source == null)) {
        await widget.services.core.stopPlayback();
        final found = await request!.open(failedSources, requestScope, (s) {
          if (g == generation) showStatus(s);
        });
        if (!mounted || g != generation) return;
        media = found;
        source = mediaSource(found);
        if (casting && local(found)) {
          // A download plays on this screen only.
          await stopCast(resumeHere: false);
          if (!mounted || g != generation) return;
        }
      } else if (mirror == null || onlyProvider != null) {
        await widget.services.core.stopPlayback();
        source = await widget.services.resolver.resolve(
          anime,
          episode,
          scope: requestScope,
          exclude: onlyProvider == null ? failedSources : const {},
          mode: onlyProvider == null ? widget.mode : 'sub',
          onlyProvider: onlyProvider,
          allowAudioFallback: onlyProvider == null,
          status: (s) {
            if (g == generation) showStatus(s);
          },
        );
      }
      if (!mounted || g != generation) return;
      copy = mirror != null && mirror < source!.streams.length ? mirror : 0;
      final stream = source!.streams[copy];
      if (casting) {
        if (externalTrack != null &&
            !externalSubtitleTracks(stream)
                .any((t) => t['url'] == externalTrack!['url'])) {
          externalTrack = null;
        }
        if (isTitle && resumeFrom == null) {
          savedResume = widget.services.store.flag('ResumePlayback')
              ? request!.resumeAt?.call(0) ?? 0
              : 0;
        }
        await castCurrent(
          start:
              resumeFrom != null || widget.services.store.flag('ResumePlayback')
              ? savedResume.toDouble()
              : 0,
        );
        if (!mounted || g != generation) return;
        playbackPosition = Duration(seconds: savedResume);
        startedAt = playbackPosition;
        started = true;
        setState(() => busy = false);
        unawaited(loadEpisodes(g));
        showControls();
        return;
      }
      final initial = source!;
      final initialCopy = copy;
      final failure = await selectSubtitleCopy(
        initial: initial,
        initialCopy: initialCopy,
        // A title's copy carries its own subtitle files; there is no other
        // source to hunt through for one with working subtitles.
        enabled: !isTitle && !skipEpisodeSubtitles && widget.mode != 'dub',
        scope: requestScope,
        openCopy: (candidate, index) => prepareCopy(candidate, index, g),
        nextSource: (tried) async {
          await player.stop();
          requestScope.check();
          await widget.services.core.stopPlayback();
          try {
            return await widget.services.resolver.resolve(
              anime,
              episode,
              scope: requestScope,
              status: (_) {
                if (g == generation) {
                  showStatus('Finding a copy with subtitles');
                }
              },
              exclude: {...failedSources, ...tried, ...torrentSources},
              mode: 'sub',
              allowAudioFallback: false,
            );
          } on CoreFailure catch (e) {
            if (e.code == 'no_stream') return null;
            rethrow;
          }
        },
      );
      if (!mounted || g != generation) return;
      if (failure != null) {
        unsubtitledCopy = (provider: initial.provider, copy: initialCopy);
        setState(() {
          busy = false;
          error =
              'Subtitles could not be loaded from ${failure.host}. '
              'No other copy had working subtitles.';
        });
        return;
      }
      if (keep != null && mirror != null && mirror == initialCopy) {
        await restoreTracks(keep, g);
        if (!mounted || g != generation) return;
      }
      if (isTitle) {
        final length = player.state.duration.inSeconds;
        final resume =
            resumeFrom?.inSeconds ??
            (widget.services.store.flag('ResumePlayback')
                ? request!.resumeAt?.call(length) ?? 0
                : 0);
        // Near the end the title starts over rather than ending at once.
        savedResume = resume > 0 && resume < length - 30 ? resume : 0;
      }
      if (savedResume > 0 &&
          (resumeFrom != null ||
              isTitle ||
              widget.services.store.flag('ResumePlayback'))) {
        await player.seek(Duration(seconds: savedResume));
        playbackPosition = Duration(seconds: savedResume);
      }
      startedAt = Duration(seconds: savedResume);
      if (!mounted || g != generation) return;
      setState(() => busy = false);
      await resumePlayback();
      if (!mounted || g != generation) return;
      if (isTitle) {
        started = true;
        scrobble('start');
      }
      unawaited(loadEpisodes(g));
      unawaited(loadSkips(g));
      showControls();
    } on CoreFailure catch (e) {
      if (e.code == 'cancelled') return;
      if (mounted && g == generation) {
        setState(() {
          busy = false;
          error = isTitle ? e.message : 'This episode could not be opened.';
        });
      }
    } catch (_) {
      if (mounted && g == generation) {
        setState(() {
          busy = false;
          error = isTitle
              ? 'This title could not be opened.'
              : 'This episode could not be opened.';
        });
      }
    }
  }

  Future<SubtitleCopyCheck> prepareCopy(
    PlaybackSource candidate,
    int index,
    int g,
  ) async {
    final requestScope = scope;
    await player.stop();
    requestScope.check();
    await clearSubtitleFiles();
    requestScope.check();
    source = candidate;
    copy = index;
    externalTrack = null;
    subSelected = false;
    audioSelected = false;
    subtitleRequest++;
    setState(() {
      duration = Duration.zero;
      position = Duration.zero;
    });
    final stream = candidate.streams[index];
    if (stream.torrent) watchLoading(g);
    final headers = Map<String, String>.of(stream.headers);
    if (stream.referrer.isNotEmpty) headers['Referer'] = stream.referrer;
    await configure();
    await configureQuality(stream, headers);
    requestScope.check();
    final url = stream.url.startsWith('http')
        ? stream.url
        : Uri.file(stream.url.replaceFirst('file://', '')).toString();
    await player.open(Media(url, httpHeaders: headers), play: false);
    requestScope.check();
    if (player.state.duration == Duration.zero) {
      try {
        await player.stream.duration
            .firstWhere((d) => d > Duration.zero)
            .timeout(Duration(seconds: isTitle ? 25 : 20));
      } on TimeoutException {
        // Live streams may never report a duration.
      }
    }
    requestScope.check();
    if (skipEpisodeSubtitles) {
      subSelected = true;
      await player.setSubtitleTrack(SubtitleTrack.no());
    }
    final failure = await preferredTracks(g);
    return (selected: subSelected, failure: failure);
  }

  Future<void> loadEpisodes(int g) async {
    if (isTitle) return;
    final result = await widget.services.resolver.episodes(source!, scope);
    if (mounted && g == generation) setState(() => episodes = result);
  }

  /// Opening, ending and recap times: from the source and the skip time
  /// services for anime, from the request (TheIntroDB) for series episodes.
  /// Films have none, so their skip button never shows.
  Future<void> loadSkips(int g) async {
    if (source == null) return;
    if (isTitle) {
      final find = request!.skips;
      final seconds = duration.inMilliseconds / 1000;
      if (find == null || seconds <= 0) return;
      final found = await find(seconds, scope);
      if (!mounted || g != generation || !isTitle) return;
      setState(() => spans = selectSkipSpans(const [], found, seconds));
      return;
    }
    final result = await widget.services.resolver.skips(
      source!,
      episode,
      scope,
    );
    if (!mounted || g != generation) return;
    final seconds = duration.inMilliseconds / 1000;
    if (seconds <= 0) return;
    final provider = selectSkipSpans(result, const [], seconds);
    if (provider.any((s) => s.type == 'op') &&
        provider.any((s) => s.type == 'ed')) {
      setState(() => spans = provider);
      return;
    }
    final extra = await widget.services.skipTimes.lookup(
      anime,
      episode,
      seconds,
      scope: scope,
    );
    if (!mounted || g != generation) return;
    setState(() => spans = selectSkipSpans(result, extra, seconds));
  }

  Future<SubtitleFailure?> preferredTracks(int g) async {
    if (!mounted || g != generation || selectingTracks == g) return null;
    selectingTracks = g;
    try {
      return await pickPreferredTracks(g, subtitleRequest);
    } finally {
      if (selectingTracks == g) selectingTracks = null;
    }
  }

  Future<SubtitleFailure?> pickPreferredTracks(int g, int request) async {
    final s = widget.services.store;
    if (!audioSelected) {
      for (final language in [
        // The audio the source names (a Lithuanian dub) comes first.
        ...?media?.audioLanguage
            .split(',')
            .map((v) => v.trim())
            .where((v) => v.isNotEmpty && isTitle),
        ...s.text('AudioLanguages').split(','),
      ]) {
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
    if (subSelected ||
        source == null ||
        !mounted ||
        g != generation ||
        request != subtitleRequest ||
        casting) {
      return null;
    }
    if (isTitle && dubbedTitle) {
      subSelected = true;
      await player.setSubtitleTrack(SubtitleTrack.no());
      return null;
    }
    final stream = source!.streams[copy];
    SubtitleFailure? failure;
    final attempted = <Object?>{};
    void recordFailure(SubtitleFailure problem) {
      if (problem.host != null) failure ??= problem;
    }

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
        return null;
      }
      final candidates = <({SubtitleTrack? embedded, Json? external})>[
        for (final track in player.state.tracks.subtitle)
          if (track.id != 'auto' &&
              track.id != 'no' &&
              languageMatches(track.language, track.title, preferred))
            (embedded: track, external: null),
        for (final track in externalSubtitleTracks(stream))
          if (languageMatches(
            track['language'] as String?,
            track['label'] as String?,
            preferred,
          ))
            (embedded: null, external: track),
      ];
      for (final candidate in dialogueSubtitleOrder(
        candidates,
        language: (t) =>
            t.embedded?.language ?? t.external?['language'] as String?,
        label: (t) => t.embedded?.title ?? t.external?['label'] as String?,
      )) {
        if (candidate.embedded case final track?) {
          subSelected = true;
          await player.setSubtitleTrack(track);
          return null;
        }
        final track = candidate.external!;
        if (!attempted.add(track['url'])) continue;
        if (await externalSubtitle(
          track,
          g,
          request,
          manual: false,
          onFailure: recordFailure,
        )) {
          subSelected = true;
          return null;
        }
        if (!mounted || g != generation || request != subtitleRequest) {
          return null;
        }
      }
    }
    // Some providers only return a single, unlabelled sidecar.
    final legacy = externalSubtitleTracks(stream)
        .where((t) => t['url'] == stream.subtitle && t['language'] == null)
        .firstOrNull;
    if (legacy != null &&
        attempted.add(legacy['url']) &&
        await externalSubtitle(
          legacy,
          g,
          request,
          manual: false,
          onFailure: recordFailure,
        )) {
      subSelected = true;
      return null;
    }
    return failure;
  }

  final subtitleDirectories = <Directory>{};

  Future<void> clearSubtitleFiles() async {
    final directories = subtitleDirectories.toList();
    subtitleDirectories.clear();
    await Future.wait(directories.map(deleteSubtitleDirectory));
  }

  Future<bool> externalSubtitle(
    Json track,
    int g,
    int request, {
    bool manual = true,
    void Function(SubtitleFailure)? onFailure,
  }) async {
    if (casting) {
      externalTrack = track;
      if (manual) unawaited(castCurrent(start: position.inSeconds.toDouble()));
      return true;
    }
    Directory? temporary;
    bool current() => mounted && g == generation && request == subtitleRequest;
    try {
      final link = track['url'] as String? ?? '';
      final subtitle = link.startsWith('http') || !isTitle
          ? await downloadSubtitle(
              widget.services.catalog.client,
              source!.streams[copy],
              link,
              nativeCore: Host.android ? widget.services.core : null,
              scope: scope,
              timeout:
                  !manual &&
                      busy &&
                      widget.mode == 'sub' &&
                      source?.mode == 'sub' &&
                      !isTitle
                  ? const Duration(seconds: 6)
                  : Duration(seconds: Host.android ? 23 : 15),
            )
          : await localSubtitle(link);
      if (!current()) return false;
      final title = track['label'] as String? ?? 'Subtitles';
      final language = track['language'] as String?;
      if (Host.android && player.isMpv) {
        final File file;
        try {
          final cache = await getTemporaryDirectory();
          temporary = await cache.createTemp('otakase-subtitle-');
          file = File('${temporary.path}/track.${subtitle.extension}');
          await file.writeAsBytes(subtitle.bytes, flush: true);
        } on Exception {
          throw const SubtitleFailure('The subtitle file could not be saved.');
        }
        if (!current()) return false;
        await player.setSubtitleTrack(
          SubtitleTrack.uri(file.path, title: title, language: language),
        );
        final active =
            await player.mpvGet('current-tracks/sub/external-filename') ?? '';
        if (!isActiveSubtitleFile(file, active)) {
          throw const SubtitleFailure(
            'The player could not open this subtitle file.',
          );
        }
        if (!current()) return false;
        subtitleDirectories.add(temporary);
        temporary = null;
      } else {
        await player.setSubtitleTrack(
          SubtitleTrack.data(
            utf8.decode(subtitle.bytes, allowMalformed: true),
            title: title,
            language: language,
          ),
        );
      }
      if (!current()) return false;
      externalTrack = track;
      return true;
    } catch (e) {
      if (e is SubtitleFailure && current()) onFailure?.call(e);
      if (manual && mounted && current()) {
        final message = e is SubtitleFailure
            ? e.message
            : 'The player could not open this subtitle file.';
        ScaffoldMessenger.of(context)
            .showSnackBar(SnackBar(content: Text(message)));
      }
      return false;
    } finally {
      if (temporary != null) await deleteSubtitleDirectory(temporary);
    }
  }

  /// A subtitle file kept next to a download.
  Future<({List<int> bytes, String extension})> localSubtitle(
    String link,
  ) async {
    final path = link.replaceFirst('file://', '');
    final List<int> bytes;
    try {
      bytes = await File(path).readAsBytes();
    } on Exception {
      throw const SubtitleFailure('The subtitle file could not be read.');
    }
    try {
      final extension = subtitleExtension(bytes, Uri.file(path));
      return (
        bytes: extension == 'vtt' ? repairWebVtt(bytes) : bytes,
        extension: extension,
      );
    } on FormatException {
      throw const SubtitleFailure('The subtitle file is not readable.');
    }
  }

  void tick(Duration p) {
    if (profileStopped) return;
    if (!mounted ||
        busy ||
        (p == Duration.zero && player.state.completed && !casting)) {
      return;
    }
    final currentSource = source;
    if (currentSource != null && isTitle) {
      final key =
          'title:${request!.title}:${request!.subtitle}:${currentSource.provider}';
      if (viewingKey != key) {
        viewingKey = key;
        viewing = EpisodeViewing();
      }
      viewing.start(duration, [
        duration.inMilliseconds,
        startedAt.inMilliseconds,
      ]);
    } else if (currentSource != null) {
      final key =
          '$episode:${currentSource.provider}:${currentSource.showId}:${currentSource.mode}:$copy';
      if (viewingKey != key) {
        viewingKey = key;
        viewing = EpisodeViewing();
        viewing.start(
          duration,
          widget.services.store.entry(anime)?.viewing[key],
        );
      } else if (viewing.checkpoint == null) {
        viewing.start(
          duration,
          widget.services.store.entry(anime)?.viewing[key],
        );
      }
    }
    viewing.sample(
      p,
      playing: casting ? castStatus?.state == 'playing' : player.state.playing,
      rate: casting ? 1 : player.state.rate,
    );
    if (p != playbackPosition) {
      playbackPosition = p;
      watchForStall();
    }
    if (isTitle) {
      setState(() {
        if (!scrubbing) position = p;
      });
      titleWatched(p);
      return;
    }
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
      if (!scrubbing) position = p;
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
        unawaited(skipSpan(current));
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
  }

  /// Marks a film or series episode watched once the viewer followed it to
  /// the percentage in the settings, within ten minutes of the end. One
  /// skipped to the end is not watched.
  void titleWatched(Duration p) {
    final total = duration.inSeconds;
    if (markedWatched ||
        !copyLengthOk ||
        total <= 0 ||
        p.inSeconds * 100 <
            total * widget.services.store.integer('PercentageToMarkComplete') ||
        total - p.inSeconds >= 600 ||
        !viewing.watchedUpTo(p)) {
      return;
    }
    markedWatched = true;
    unawaited(save(completed: true));
    sendStop();
  }

  /// Whether this copy is about as long as the title usually runs; a short
  /// placeholder clip played to its end is no watched episode.
  /// Episode lengths vary more than film lengths, so for anime only a copy
  /// under a third of the usual length counts as cut short; for a film,
  /// under half.
  bool get copyLengthOk => copyLengthPlausible(
    duration,
    isTitle ? request!.length : anime.duration * 60 * 2 ~/ 3,
  );

  void playbackEnded() {
    if (!mounted || profileStopped || busy || finished) return;
    if (!copyLengthOk ||
        !reachedEpisodeEnd(playbackPosition, duration) &&
            !viewing.endingSkip(playbackPosition, duration, spans)) {
      if (casting) {
        unawaited(save());
        scrobble('pause');
        setState(() {
          error = isTitle
              ? 'The cast stopped before the end.'
              : 'The cast stopped before the end of the episode.';
          overlay = true;
        });
      } else if (!copyLengthOk) {
        // A placeholder clip: the next copy starts the episode afresh.
        playbackPosition = Duration.zero;
        savedResume = 0;
        unawaited(recoverPlayback('This copy is far shorter than the episode'));
      } else {
        unawaited(recoverPlayback('Stream ended early'));
      }
      return;
    }
    finished = true;
    playRequested = false;
    stall.cancel();
    final watched =
        markedWatched || viewing.ended(playbackPosition, duration, spans);
    if (isTitle) {
      unawaited(titleEnded(watched));
      return;
    }
    unawaited(save(completed: watched));
    if (watched) offerNext();
  }

  /// A film or series episode reached its end: one followed to the end is
  /// watched and the next episode is offered; one skipped to the end keeps
  /// its spot and the player closes.
  Future<void> titleEnded(bool watched) async {
    if (!watched) {
      if (mounted) unawaited(Navigator.maybePop(context));
      return;
    }
    if (!markedWatched) {
      markedWatched = true;
      await save(completed: true);
    }
    sendStop();
    final next = request!.next?.call();
    if (!mounted) return;
    if (next == null) {
      unawaited(Navigator.maybePop(context));
      return;
    }
    upNext = next;
    offerNext();
  }

  void watchForStall() {
    stall.cancel();
    if (!mounted ||
        busy ||
        casting ||
        !playRequested ||
        finished ||
        error.isNotEmpty) {
      return;
    }
    stall.arm(() {
      if (!scrubbing) {
        unawaited(recoverPlayback('Playback stopped making progress'));
      } else {
        watchForStall();
      }
    });
  }

  Future<void> recoverPlayback(String reason, {bool reopen = true}) async {
    if (!mounted ||
        profileStopped ||
        casting ||
        busy ||
        retrying ||
        finished ||
        error.isNotEmpty ||
        source == null) {
      return;
    }
    retrying = true;
    // A copy that never started is still being found, not reconnected.
    streamDropped =
        duration > Duration.zero || playbackPosition > Duration.zero;
    final g = generation;
    final resume = playbackPosition == Duration.zero
        ? Duration(seconds: savedResume)
        : playbackPosition;
    try {
      await save();
      if (!mounted || g != generation) return;
      if (reopen && !reopenedCopy) {
        rememberTracks();
        await open(mirror: copy, resumeFrom: resume, reopening: true);
        if (!mounted || generation != g + 1 || error.isEmpty) return;
      }
      if (!isTitle) {
        unawaited(
          widget.services.discord.reportProvider(
            source!.provider,
            false,
            'playback',
            reason,
          ),
        );
      }
      await tryAnotherCopy(automatic: true, resumeFrom: resume);
    } finally {
      retrying = false;
      streamDropped = false;
      if (mounted) {
        if (player.state.completed &&
            !finished &&
            g != generation &&
            error.isEmpty) {
          playbackEnded();
        } else {
          watchForStall();
        }
      }
    }
  }

  Future<void> save({bool completed = false, bool leaving = false}) async {
    if (profileStopped || busy || duration <= Duration.zero) return;
    final at = playbackPosition;
    if (isTitle) {
      final done = completed || markedWatched;
      // Leaving before playback moved must not wipe the saved spot.
      if (!done && at == Duration.zero) return;
      await request!.onProgress?.call(at.inSeconds, duration.inSeconds, done);
      return;
    }
    if (!completed && at == Duration.zero) return;
    final watched =
        completed ||
        completedEpisodes.contains(episode) ||
        (leaving &&
            !retrying &&
            error.isEmpty &&
            copyLengthOk &&
            viewing.credits(at, duration, spans));
    final newlyWatched = watched && completedEpisodes.add(episode);
    final persist = widget.services.store.progress(
      anime,
      episode,
      at.inSeconds,
      duration.inSeconds,
      completed: watched,
      viewingKey: viewingKey,
      viewingCheckpoint: viewing.checkpoint,
    );
    if (newlyWatched) {
      if (finishesShow(widget.services, anime, episode)) {
        widget.services.ratingDue = anime;
      }
      await resumePusher.episodeDone(anime.key, episode, anime.episodes);
    } else if (!watched) {
      resumePusher.note(anime.key, episode, at.inSeconds);
    }
    await persist;
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
          marking == null &&
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
    if (!casting) {
      playRequested = false;
      stall.cancel();
      return player.pause();
    }
    await castControl('pause');
  }

  Future<void> resumePlayback() async {
    if (!casting) {
      playRequested = true;
      watchForStall();
      return player.play();
    }
    await castControl('play');
  }

  Future<void> skipSpan(SkipSpan span) =>
      seekTo(Duration(milliseconds: (span.end * 1000).round()), span: span);

  /// Moves playback here or on the cast device.
  Future<void> seekTo(Duration target, {SkipSpan? span}) async {
    viewing.seek(playbackPosition, target, span: span);
    if (!casting) {
      await player.seek(target);
      watchForStall();
      return;
    }
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
    // A download is cast from this device; another Otakase streams the
    // same episode or film instead, when the download knows which it is.
    panelOpen = true;
    showControls();
    final target = await pickPlayTarget(
      context,
      widget.services,
      here: false,
      // Titles a TV cannot find stay on cast.
      otakase: !widget.isTV && (!isTitle || request!.remote != null),
      caster: caster,
    );
    panelOpen = false;
    if (target == null || !mounted) return;
    switch (target) {
      case PlayOnCast(:final device):
        await startCast(device);
      case PlayOnTV(:final tv):
        await handOff(tv);
      case PlayHere():
        showControls();
    }
  }

  /// Moves this episode to an Otakase TV at the same spot, then turns this
  /// screen into its remote.
  Future<void> handOff(TVDevice tv) async {
    final at = position.inSeconds;
    flash.show('Sending to ${tv.name}…');
    try {
      final link = widget.services.controlTV(tv);
      await link.ready();
      await link.send(
        'start',
        args: isTitle
            ? {...request!.remote!, 'position': at}
            : {
                'anilist_id': anime.id,
                'episode': episode,
                'position': at,
                'mode': source?.mode ?? widget.mode,
              },
      );
    } catch (e) {
      if (mounted) flash.show(e is StateError ? e.message : '$e');
      return;
    }
    if (!mounted) return;
    await save();
    await player.pause();
    if (casting) await stopCast(resumeHere: false);
    if (!mounted) return;
    final navigator = Navigator.of(context);
    navigator.pop();
    unawaited(
      navigator.push(
        OtakaseRoute<void>(
          builder: (_) => RemoteNowPlaying(services: widget.services),
        ),
      ),
    );
  }

  Future<void> startCast(CastDevice device) async {
    final at = position.inSeconds.toDouble();
    await save();
    castDevice = device;
    castStatus = null;
    castEnded = -1;
    castPlaying = false;
    playRequested = false;
    stall.cancel();
    await player.pause();
    if (!mounted) return;
    setState(() => overlay = true);
    if (source == null || busy) return; // open() casts once it resolves.
    await castCurrent(start: at);
  }

  /// Keep an explicit external choice, otherwise prefer dialogue for casting.
  String castSubtitle(ResolvedStream stream) {
    if (skipEpisodeSubtitles) return '';
    if (externalTrack case final Json track) {
      // A file on this device reaches the cast device only beside a
      // download, which the core serves from here.
      final url = track['url'] as String? ?? '';
      return !isTitle || url.startsWith('http') || castsFile ? url : '';
    }
    if ((source?.mode ?? widget.mode) == 'dub') return '';
    // A title's embedded track picked here, or a dub, sends no file.
    if (isTitle && (subSelected || dubbedTitle)) return '';
    final files = isTitle && !castsFile
        ? stream.subtitles
              .where((t) => (t['url'] as String? ?? '').startsWith('http'))
              .toList()
        : stream.subtitles;
    final preferences = widget.services.store
        .text('SubsLanguage')
        .toLowerCase()
        .split(',')
        .map((v) => v.trim())
        .where((v) => v.isNotEmpty);
    for (final preferred in preferences) {
      if (preferred == 'none' || preferred == 'off') return '';
      for (final t in dialogueSubtitleOrder(
        files,
        language: (t) => t['language'] as String?,
        label: (t) => t['label'] as String?,
      )) {
        if (languageMatches(
          t['language'] as String?,
          t['label'] as String?,
          preferred,
        )) {
          return t['url'] as String? ?? '';
        }
      }
    }
    if (isTitle) return '';
    if (files.isNotEmpty) {
      return dialogueSubtitleOrder(
                files,
                language: (t) => t['language'] as String?,
                label: (t) => t['label'] as String?,
              ).first['url']
              as String? ??
          '';
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
        title: isTitle
            ? [
                request!.title,
                request!.subtitle,
              ].where((s) => s.isNotEmpty).join(' · ')
            : '${anime.title} · Episode $episode',
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
        final playing = s.state == 'playing';
        if (isTitle && playing != castPlaying && started) {
          scrobble(playing ? 'start' : 'pause');
          showPresence();
        }
        castPlaying = playing;
      }
      if (s.state == 'ended' && castEnded != s.load) {
        castEnded = s.load;
        final at = Duration(milliseconds: (s.position * 1000).round());
        if (at > Duration.zero) playbackPosition = at;
        playbackEnded();
      }
    } catch (_) {
      // A missed poll is retried in a second.
    } finally {
      castPolling = false;
    }
  }

  /// Opens this computer's firewall and starts the cast again where it was.
  Future<void> allowCast() async {
    final opened = await caster.allow();
    if (!mounted || !casting) return;
    if (!opened) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('The firewall was not opened.')),
      );
      return;
    }
    await castCurrent(start: position.inMilliseconds / 1000);
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
    if (source != null) {
      await open(mirror: copy, resumeFrom: isTitle ? position : null);
    }
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
          await skipSpan(span);
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
        if (busy) break;
        if (isTitle) {
          await switchTitle(request!.previous?.call());
        } else if (episode > 1) {
          await pickEpisode(episode - 1);
        }
      case 'episode':
        if (busy) break;
        if (isTitle) {
          await switchTitle(request!.pick?.call(number(args['episode'])));
        } else {
          await pickEpisode(number(args['episode']));
        }
      case 'subtitle':
        final id = '${args['id']}';
        for (final track in player.state.tracks.subtitle) {
          if (track.id != id) continue;
          subSelected = true;
          subtitleRequest++;
          externalTrack = null;
          await player.setSubtitleTrack(track);
        }
      case 'audio':
        final id = '${args['id']}';
        for (final track in player.state.tracks.audio) {
          if (track.id != id) continue;
          audioSelected = true;
          await player.setAudioTrack(track);
        }
      case 'quality':
        final value = '${args['value']}';
        if (!busy && qualityChoices.contains(value)) await applyQuality(value);
      case 'stop':
        await save();
        if (!busy) scrobble('pause');
        await resumePusher.flush();
        if (casting) await stopCast(resumeHere: false);
        await player.stop();
        if (mounted) Navigator.pop(context);
    }
    if (mounted) showControls();
  }

  Json remoteStatus() => {...baseStatus(), ...remoteExtras()};

  Json baseStatus() => isTitle
      ? {
          'has_player': true,
          'kind': 'movie',
          'anilist_id': 0,
          'loaded': !busy && duration > Duration.zero,
          'position_ms':
              (casting ? position : player.state.position).inMilliseconds,
          'title': [
            request!.title,
            request!.subtitle,
          ].where((s) => s.isNotEmpty).join(' · '),
          'adult': false,
          'episode': request!.number,
          'episodes': request!.pick == null ? 0 : request!.count,
          'position': position.inSeconds,
          'duration': duration.inSeconds,
          'playing': isPlaying,
          'volume': casting
              ? ((castStatus?.volume ?? 1) * 100).round()
              : player.state.volume.round(),
        }
      : animeStatus();

  Json animeStatus() => {
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

  /// Leaves this film or series episode for [target], keeping the spot of
  /// the one left. Nothing happens without a target.
  Future<void> switchTitle(PlayRequest? target) async {
    if (target == null || !isTitle) return;
    nextTimer?.cancel();
    await save(leaving: true);
    if (!busy) scrobble('pause');
    if (!mounted || profileStopped) return;
    setState(() => countdownSeconds = 0);
    startTitle(target);
    await open();
  }

  Future<void> pickEpisode(int target) async {
    if (isTitle) return;
    if (target < 1 || anime.airedEpisodes > 0 && target > anime.airedEpisodes) {
      return;
    }
    await save(leaving: true);
    await resumePusher.flush();
    if (!mounted || profileStopped) return;
    savedResume = 0;
    nextCancelled = false;
    episode = target;
    skipEpisodeSubtitles = false;
    failedSources.clear();
    await open();
  }

  Future<void> next({bool automatic = false}) async {
    if (!canNext) return;
    nextTimer?.cancel();
    if (isTitle) {
      await switchTitle(upNext ?? request!.next?.call());
      return;
    }
    await save(leaving: true);
    var target = episodes.where((e) => e > episode).firstOrNull ?? episode + 1;
    if (automatic && finished && widget.services.store.flag('SkipFiller')) {
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
    skipEpisodeSubtitles = false;
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
                  title: Text(subtitleLabel(track)),
                  onTap: () {
                    subSelected = true;
                    subtitleRequest++;
                    externalTrack = null;
                    Navigator.pop(dialog);
                    unawaited(player.setSubtitleTrack(track));
                  },
                ),
              for (final track
                  in source == null
                      ? <Json>[]
                      : externalSubtitleTracks(source!.streams[copy]))
                ChoiceRow(
                  title: Text(
                    '${track['label'] ?? track['language'] ?? 'Subtitles'}',
                  ),
                  onTap: () {
                    subSelected = true;
                    Navigator.pop(dialog);
                    unawaited(
                      externalSubtitle(track, generation, ++subtitleRequest),
                    );
                  },
                ),
              const Divider(),
              const Text('Audio', style: TextStyle(color: accentText)),
              for (final track in player.state.tracks.audio)
                ChoiceRow(
                  title: Text(audioLabel(track)),
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

  /// The skip times panel: where each span came from, votes, marking.
  Future<void> skipPanel() async {
    panelOpen = true;
    showControls();
    final choice = await showSkipTimesPanel(
      context,
      spans: spans,
      skipTimes: widget.services.skipTimes,
      canSubmit: anime.malId > 0 && duration > Duration.zero,
      isTV: widget.isTV,
    );
    panelOpen = false;
    if (!mounted) return;
    switch (choice) {
      case MarkSpan(:final type):
        setState(() {
          markerKey = GlobalKey();
          marking = type;
          markingExisting = spans.where((s) => s.type == type).firstOrNull;
        });
      case WatchSpan(:final span):
        await seekTo(
          Duration(milliseconds: (math.max(0, span.start - 2) * 1000).round()),
        );
        // Watching a span on purpose: don't skip it.
        skipped.add('${span.type}:${span.start}');
      case null:
        playFocus.requestFocus();
    }
    showControls();
  }

  Future<String?> sendSpan(double start, double end) async {
    final type = marking;
    if (type == null) return 'Nothing is being marked.';
    try {
      final id = await widget.services.skipTimes.submit(
        anime,
        episode,
        type,
        start,
        end,
        duration.inMilliseconds / 1000,
      );
      if (mounted) {
        setState(() {
          spans = [
            ...spans.where((s) => s.type != type),
            SkipSpan(type, start, end, source: 'aniskip', id: id),
          ]..sort((a, b) => a.start.compareTo(b.start));
        });
      }
      return null;
    } on CoreFailure catch (e) {
      return e.message;
    } catch (_) {
      return 'AniSkip could not be reached. Try again later.';
    }
  }

  void closeMarker() {
    setState(() {
      marking = null;
      markingExisting = null;
    });
    playFocus.requestFocus();
    showControls();
  }

  Widget markerCard() => SkipMarkerCard(
    key: markerKey,
    type: marking!,
    existing: markingExisting,
    position: () => position.inMilliseconds / 1000,
    duration: duration.inMilliseconds / 1000,
    seekTo: (seconds) async {
      skipped.addAll(spans.map((s) => '${s.type}:${s.start}'));
      await seekTo(Duration(milliseconds: (seconds * 1000).round()));
      if (!isPlaying) await resumePlayback();
    },
    send: sendSpan,
    onClose: closeMarker,
    isTV: widget.isTV,
  );

  Future<void> tryAnotherCopy({
    bool automatic = false,
    Duration? resumeFrom,
  }) async {
    final g = generation;
    resumeFrom ??= playbackPosition;
    await save();
    if (!mounted || g != generation) return;
    if (source != null && copy + 1 < source!.streams.length) {
      await open(mirror: copy + 1, resumeFrom: resumeFrom);
      return;
    }
    if (isTitle) {
      final name = media?.copy ?? '';
      if (name.isEmpty) {
        // A download has no other copy.
        if (mounted && g == generation) {
          setState(() {
            busy = false;
            error = 'This file could not be played.';
            overlay = true;
          });
        }
        return;
      }
      failedSources.add(name);
      source = null;
      if (mounted && g == generation) {
        await open(resumeFrom: resumeFrom > Duration.zero ? resumeFrom : null);
      }
      return;
    }
    if (source != null) failedSources.add(source!.provider);
    if (failedSources.length >=
        (await widget.services.resolver.sourcesFor(anime)).length) {
      if (automatic) {
        if (mounted && g == generation) {
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
    if (mounted && g == generation) await open(resumeFrom: resumeFrom);
  }

  Future<void> qualityPanel() async {
    const another = 'Try another copy';
    final values = [
      ...qualityChoices,
      // A download has no other copy.
      if (!isTitle || (media?.copy ?? '').isNotEmpty) another,
    ];
    panelOpen = true;
    final chosen = await chooseValue(
      context,
      'Quality',
      values,
      selected: widget.services.store.text('Quality'),
    );
    panelOpen = false;
    if (chosen == another && mounted) {
      await tryAnotherCopy(resumeFrom: playbackPosition);
    } else if (chosen != null && mounted) {
      await applyQuality(chosen);
    } else if (mounted) {
      showControls();
    }
  }

  List<String> get qualityChoices => [
    'best',
    ...availableQualities.map((h) => '$h'),
    if (availableQualities.length > 1) 'worst',
  ];

  Future<void> applyQuality(String chosen) async {
    await save();
    await widget.services.store.set('Quality', chosen);
    rememberTracks();
    await open(mirror: copy, resumeFrom: playbackPosition);
  }

  String subtitleLabel(SubtitleTrack track) => track.id == 'no'
      ? 'Off'
      : track.id == 'auto'
      ? 'Automatic'
      : track.title ?? track.language ?? 'Track ${track.id}';

  String audioLabel(AudioTrack track) =>
      track.title ??
      track.language ??
      (track.id == 'auto' ? 'Automatic' : 'Track ${track.id}');

  /// What a phone remote shows beyond the basics: art, skip marks, the
  /// skip button and the track and quality choices.
  Json remoteExtras() {
    final tracks = player.state.tracks, chosen = player.state.track;
    return {
      'cover': isTitle
          ? request!.cover
          : anime.coverLarge.isNotEmpty
          ? anime.coverLarge
          : anime.cover,
      'spans': [
        for (final s in spans) {'type': s.type, 'start': s.start, 'end': s.end},
      ],
      'skip': active?.label,
      'can_next': canNext,
      'can_previous': canPrevious,
      'subtitles': casting
          ? const []
          : [
              for (final t in tracks.subtitle)
                {
                  'id': t.id,
                  'label': subtitleLabel(t),
                  'on': t.id == chosen.subtitle.id,
                },
            ],
      'audio': casting
          ? const []
          : [
              for (final t in tracks.audio)
                {
                  'id': t.id,
                  'label': audioLabel(t),
                  'on': t.id == chosen.audio.id,
                },
            ],
      'qualities': casting ? const [] : qualityChoices,
      'quality': widget.services.store.text('Quality'),
    };
  }

  KeyEventResult remote(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent && event is! KeyRepeatEvent) {
      return KeyEventResult.ignored;
    }
    if (isDesktop) {
      final action = playerActionForEvent(event);
      if (action != null && desktopKey(action, event is KeyRepeatEvent)) {
        return KeyEventResult.handled;
      }
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
    if (!isTitle &&
        widget.services.party.active &&
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

  final flash = FlashText();
  GlobalKey<SkipMarkerCardState> markerKey = GlobalKey();

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

  /// Runs a desktop key (the old mpv keys); false lets the key go on to
  /// the focused button or the remote handling below.
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
        unawaited(control('stop', {}));
        return true;
      case PlayerAction.keys:
        unawaited(keysPanel());
        return true;
      default:
    }
    if (busy || error.isNotEmpty) return false;
    // Films and series have no skip marking, AniSkip votes or watch party:
    // those keys do nothing for them.
    if (isTitle &&
        const {
          PlayerAction.markOpening,
          PlayerAction.markEnding,
          PlayerAction.sendMarks,
          PlayerAction.forgetMarks,
          PlayerAction.upvoteSkip,
          PlayerAction.downvoteSkip,
          PlayerAction.partySkip,
          PlayerAction.partyNext,
        }.contains(action)) {
      return false;
    }
    switch (action) {
      case PlayerAction.playPause:
        unawaited(playPause());
        showControls();
      case PlayerAction.seekBack || PlayerAction.seekForward:
        unawaited(seek(action == PlayerAction.seekBack ? -5 : 5));
      case PlayerAction.stepBack || PlayerAction.stepForward:
        unawaited(seek(action == PlayerAction.stepBack ? -1 : 1));
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
        final now = casting
            ? (castStatus?.volume ?? 1) * 100
            : player.state.volume;
        final level = (now + step).clamp(0, 100).round();
        unawaited(control('volume', {'volume': level}));
        flash.show('Volume $level%');
      case PlayerAction.mute:
        toggleMute();
        flash.show(player.state.volume > 0 ? 'Muted' : 'Sound on');
      case PlayerAction.skipSegment:
        final span = active;
        if (span == null) return false;
        unawaited(skipSpan(span));
        showControls();
      case PlayerAction.previousEpisode:
        if (!canPrevious) {
          flash.show(
            isTitle && request!.previous == null
                ? 'Films have no episodes'
                : 'This is the first episode',
          );
        } else {
          unawaited(control('previous', {}));
        }
      case PlayerAction.nextEpisode:
        if (!canNext) {
          flash.show(isTitle ? 'Nothing plays next' : 'No next episode yet');
        } else {
          unawaited(control('next', {}));
        }
      case PlayerAction.markOpening || PlayerAction.markEnding:
        markKey(action == PlayerAction.markOpening ? 'op' : 'ed');
      case PlayerAction.sendMarks:
        final card = markerKey.currentState;
        if (marking == null || card == null) {
          flash.show('Mark the opening (Alt+O) or ending (Alt+E) first');
        } else {
          unawaited(card.submit());
        }
      case PlayerAction.forgetMarks:
        if (marking != null) {
          closeMarker();
          flash.show('Marks forgotten');
        }
      case PlayerAction.upvoteSkip || PlayerAction.downvoteSkip:
        unawaited(voteKey(up: action == PlayerAction.upvoteSkip));
      case PlayerAction.partySkip || PlayerAction.partyNext:
        if (!widget.services.party.active) return false;
        widget.services.party.vote(
          action == PlayerAction.partyNext ? 'next' : 'skip',
        );
        flash.show(
          action == PlayerAction.partyNext ? 'Voted next' : 'Voted skip',
        );
      default:
        if (casting) {
          flash.show('Not while casting');
          return true;
        }
        unawaited(mediaKey(action));
    }
    return true;
  }

  /// Keys that work on the picture or sound itself (this screen only).
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
      PlayerAction.screenshot => await player.saveScreenshot(
        isTitle ? request!.title : '${anime.title} E$episode',
      ),
      _ => '',
    };
    switch (action) {
      case PlayerAction.nextSubtitle ||
          PlayerAction.previousSubtitle ||
          PlayerAction.toggleSubtitles:
        subSelected = true;
      case PlayerAction.nextAudio:
        audioSelected = true;
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

  /// Alt+O / Alt+E: the first press marks where the span starts, the next
  /// one where it ends, as in the old mpv player.
  void markKey(String type) {
    if (anime.malId <= 0 || duration <= Duration.zero) {
      flash.show('Skip times cannot be sent for this show');
      return;
    }
    if (marking == type) {
      markerKey.currentState?.setEnd(position.inMilliseconds / 1000);
      flash.show('${skipKindName(type)} ends here');
      return;
    }
    setState(() {
      markerKey = GlobalKey();
      marking = type;
      markingExisting = null;
    });
    flash.show('${skipKindName(type)} starts here');
    showControls();
  }

  /// Alt+U / Alt+D: votes on the skip playing now, else the episode's
  /// first AniSkip time.
  Future<void> voteKey({required bool up}) async {
    final span =
        (active != null && active!.id.isNotEmpty ? active : null) ??
        spans.where((s) => s.id.isNotEmpty).firstOrNull;
    if (span == null) {
      flash.show('No AniSkip time to vote on');
      return;
    }
    if (widget.services.skipTimes.voted(span)) {
      flash.show('You already voted on this one');
      return;
    }
    try {
      await widget.services.skipTimes.vote(span, up: up);
      flash.show('Thanks, your vote is in');
    } catch (_) {
      flash.show('AniSkip could not be reached');
    }
  }

  Future<void> keysPanel() async {
    panelOpen = true;
    await showPlayerKeys(
      context,
      party: !isTitle && widget.services.party.active,
      episodes: !isTitle || request!.previous != null,
    );
    panelOpen = false;
    if (mounted) showControls();
  }

  String time(Duration d) =>
      '${d.inMinutes.toString().padLeft(2, '0')}:${(d.inSeconds % 60).toString().padLeft(2, '0')}';
  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    flash.dispose();
    discordMeter?.cancel();
    unawaited(widget.services.discord.stopWatching());
    unawaited(widget.services.presence.clear());
    generation++;
    scope.cancel();
    hideTimer?.cancel();
    saveTimer?.cancel();
    stall.cancel();
    nextTimer?.cancel();
    loadingTimer?.cancel();
    castTimer?.cancel();
    if (casting) unawaited(caster.control('stop').catchError((_) {}));
    if (!profileStopped) {
      final close = request?.onClose;
      if (!busy) scrobble('pause');
      unawaited(
        save(leaving: true).then((_) async {
          await resumePusher.flush();
          await close?.call();
        }),
      );
    }
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
        await clearSubtitleFiles();
        if (!profileStopped) await widget.services.core.stopPlayback();
      }),
    );
    unawaited(
      SystemChrome.setEnabledSystemUIMode(
        SystemUiMode.manual,
        overlays: [SystemUiOverlay.bottom],
      ),
    );
    if (!widget.isTV) ScreenOrientation.restore();
    FullBleed.leave();
    super.dispose();
  }

  Future<void> stopForProfileSwitch() async {
    if (profileStopped) return;
    hideTimer?.cancel();
    saveTimer?.cancel();
    stall.cancel();
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
    playRequested = false;
    await player.pause();
    if (casting) await stopCast(resumeHere: false);
    await save(leaving: true);
    if (!busy) scrobble('pause');
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
          subtitle: isTitle
              ? titleLine
              : 'Ep $episode · ${source?.mode.toUpperCase() ?? widget.mode.toUpperCase()}',
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
          trackLabel: isTitle
              ? 'Tracks'
              : source?.mode.toUpperCase() ?? widget.mode.toUpperCase(),
          centerLabel: isTitle ? request!.subtitle : 'Episode $episode',
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
          onSkipTimes: isTitle ? null : () => unawaited(skipPanel()),
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
      child: PlayerChip(
        icon: LucideIcons.chevronsRight,
        label: active!.type == 'op' ? 'Skip opening' : active!.label,
        focusNode: skipFocus,
        onPressed: () {
          unawaited(skipSpan(active!));
          playFocus.requestFocus();
          showControls();
        },
        isTV: widget.isTV,
      ),
    ),
  );

  /// The title's second line: the episode and where it plays from.
  String get titleLine => [
    request?.subtitle ?? '',
    media?.label ?? '',
  ].where((s) => s.isNotEmpty).join(' · ');

  Widget nextCard(BuildContext context) => Align(
    alignment: Alignment.centerRight,
    child: Container(
      key: const Key('player-next'),
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
          if (anime.cover.isNotEmpty) ...[
            ClipRRect(
              borderRadius: BorderRadius.circular(12),
              child: CoverImage(anime.cover, width: 54, height: 81),
            ),
            const SizedBox(width: 16),
          ],
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  isTitle
                      ? 'Next · ${upNext?.subtitle ?? ''}'
                      : 'Next · Episode ${episode + 1}',
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
                      autofocus: isTitle,
                      onPressed: () => next(),
                      isTV: widget.isTV,
                    ),
                    AppButton(
                      label: 'Cancel',
                      onPressed: () {
                        nextTimer?.cancel();
                        nextCancelled = true;
                        setState(() {
                          countdownSeconds = 0;
                          upNext = null;
                        });
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
    canPop: marking == null && (!overlay || busy || error.isNotEmpty),
    onPopInvokedWithResult: (popped, result) {
      if (!popped && marking != null) {
        closeMarker();
      } else if (!popped) {
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
                behavior: HitTestBehavior.opaque,
                onDoubleTap: isDesktop
                    ? () => unawaited(toggleFullscreen())
                    : null,
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
                  // The picture never takes taps itself, so a tap anywhere
                  // on it reaches the detector above (on the web the video
                  // is a page element that would otherwise keep them).
                  child: IgnorePointer(
                    child: Video(controller: video, controls: NoVideoControls),
                  ),
                ),
              ),
              if (casting && !busy && error.isEmpty)
                CastPanel(
                  status: castStatus,
                  deviceName: castDevice!.name,
                  isTV: widget.isTV,
                  onStop: () => unawaited(stopCast()),
                  onAllow: () => unawaited(allowCast()),
                ),
              if (marking != null && !busy && error.isEmpty)
                Positioned(
                  top: isDesktop ? 72 : 88,
                  right: 24,
                  left: widget.isTV || isDesktop ? null : 16,
                  child: SafeArea(child: markerCard()),
                ),
              if (!isTitle)
                PartyOverlay(
                  party: widget.services.party,
                  controls: overlay && !busy,
                  isTV: widget.isTV,
                ),
              ValueListenableBuilder<String>(
                valueListenable: flash,
                builder: (_, text, _) => PlayerFlash(text: text),
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
                      action: Wrap(
                        spacing: 8,
                        runSpacing: 8,
                        alignment: WrapAlignment.center,
                        children: [
                          if (unsubtitledCopy case final fallback?)
                            AppButton(
                              label: 'Play without subtitles',
                              onPressed: () {
                                skipEpisodeSubtitles = true;
                                unawaited(
                                  open(
                                    onlyProvider: fallback.provider,
                                    mirror: fallback.copy,
                                  ),
                                );
                              },
                              isTV: widget.isTV,
                            ),
                          if (isTitle)
                            AppButton(
                              label: 'Try again',
                              icon: LucideIcons.refreshCw,
                              filled: true,
                              autofocus: true,
                              onPressed: () {
                                failedSources.clear();
                                source = null;
                                unawaited(
                                  open(
                                    resumeFrom: playbackPosition > Duration.zero
                                        ? playbackPosition
                                        : null,
                                  ),
                                );
                              },
                              isTV: widget.isTV,
                            ),
                          if (!isTitle || (media?.copy ?? '').isNotEmpty)
                            AppButton(
                              label: 'Try another copy',
                              onPressed: tryAnotherCopy,
                              isTV: widget.isTV,
                            ),
                          PlaybackReportButton(
                            services: widget.services,
                            isTV: widget.isTV,
                            title: anime.title,
                            episode: isTitle
                                ? request!.subtitle
                                : 'Episode $episode',
                            problem: error,
                            position: position,
                            provider: source?.provider ?? '',
                          ),
                          // Desktop hides the title bar behind the error,
                          // so there must be a way out besides Esc.
                          AppButton(
                            label: 'Back',
                            icon: LucideIcons.arrowLeft,
                            onPressed: () => Navigator.pop(context),
                            isTV: widget.isTV,
                          ),
                        ],
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
                PhonePlayerControls(
                  isTV: widget.isTV,
                  title: anime.title,
                  subtitle: isTitle
                      ? titleLine
                      : 'Episode $episode · ${source?.mode ?? widget.mode}',
                  position: position,
                  duration: duration,
                  spans: spans,
                  playing: isPlaying,
                  buffering: casting
                      ? castStatus?.state == 'buffering'
                      : player.state.buffering,
                  playFocus: playFocus,
                  onBack: () => Navigator.pop(context),
                  onPlayPause: () {
                    unawaited(playPause());
                    showControls();
                  },
                  onSeekBy: (s) => unawaited(seek(s)),
                  onSeekStart: (_) {
                    scrubbing = true;
                    hideTimer?.cancel();
                  },
                  onSeekChanged: (v) => setState(
                    () => position = Duration(milliseconds: v.round()),
                  ),
                  onSeekEnd: (v) {
                    scrubbing = false;
                    unawaited(seekTo(Duration(milliseconds: v.round())));
                    showControls();
                  },
                  topActions: [
                    if (!widget.isTV)
                      IconButton(
                        key: const Key('player-cast'),
                        tooltip: casting ? 'Casting' : 'Cast',
                        onPressed: () => unawaited(castMenu()),
                        icon: Icon(
                          LucideIcons.cast,
                          color: casting ? accentText : fgBright,
                        ),
                      ),
                  ],
                  above: [
                    if (active != null) skipButton(),
                    if (countdownSeconds > 0) nextCard(context),
                  ],
                  actions: [
                    PlayerButton(
                      icon: LucideIcons.skipForward,
                      label: 'Next',
                      onPressed: canNext ? () => next() : null,
                    ),
                    PlayerButton(
                      icon: LucideIcons.captions,
                      label: 'Audio & subs',
                      onPressed: trackPanel,
                    ),
                    PlayerButton(
                      icon: LucideIcons.gauge,
                      label: 'Quality',
                      onPressed: qualityPanel,
                    ),
                    if (!isTitle)
                      PlayerButton(
                        icon: LucideIcons.chevronsRight,
                        label: 'Skip times',
                        onPressed: skipPanel,
                      ),
                  ],
                ),
            ],
          ),
        ),
      ),
    ),
  );
}
