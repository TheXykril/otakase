import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../core.dart';
import '../data/episode_completion.dart';
import '../data/models.dart';
import '../data/skip_times.dart';
import '../data/store.dart';
import '../player.dart';
import '../services.dart';
import '../ui/components.dart';
import '../ui/rate_prompt.dart';
import '../ui/seal.dart';
import 'desktop_core.dart';
import '../host.dart';

// External mpv, as the CLI played: the desktop app finds the stream and
// hands it to the viewer's own mpv, so their mpv.conf, scripts and key
// bindings apply. A small Lua script (written next to the app's temp files)
// reports the position on stdout and runs commands the app appends to a
// file, which works the same on Linux, macOS and Windows without sockets or
// named pipes.

/// The player for an episode: the built-in one, or the viewer's own mpv on
/// desktop when ExternalMpv is on.
Widget playerFor({
  required AppServices services,
  required Anime anime,
  required int episode,
  required bool isTV,
  required String mode,
}) => isDesktop && services.store.flag('ExternalMpv')
    ? ExternalPlayerPage(
        services: services,
        anime: anime,
        episode: episode,
        mode: mode,
      )
    : PlayerPage(
        services: services,
        anime: anime,
        episode: episode,
        isTV: isTV,
        mode: mode,
      );

const mpvStatusScript = r'''
-- Otakase: reports the position and runs the app's commands.
local file = mp.get_opt("otakase-commands")
local seen = 0
local seeking = false
mp.register_event("seek", function() seeking = true end)
mp.register_event("end-file", function(event)
  io.stdout:write("otakase-end " .. event.reason .. "\n")
  io.stdout:flush()
end)
mp.add_periodic_timer(0.5, function()
  local pos = mp.get_property_number("time-pos") or -1
  local dur = mp.get_property_number("duration") or -1
  local paused = mp.get_property_bool("pause") and 1 or 0
  local jumped = seeking and 1 or 0
  if pos >= 0 then seeking = false end
  io.stdout:write(string.format("otakase-status %.3f %.3f %d %d\n", pos, dur, paused, jumped))
  io.stdout:flush()
  if not file then return end
  local f = io.open(file, "r")
  if not f then return end
  local n = 0
  for line in f:lines() do
    n = n + 1
    if n > seen then
      seen = n
      mp.command(line)
    end
  end
  f:close()
end)
''';

/// One status line from the script.
class MpvStatus {
  const MpvStatus(
    this.position,
    this.duration,
    this.paused, {
    this.seek = false,
  });
  final double position, duration;
  final bool paused, seek;
  bool get loaded =>
      position.isFinite && duration.isFinite && position >= 0 && duration > 0;
  Duration get at => Duration(milliseconds: (position * 1000).round());
  Duration get total => Duration(milliseconds: (duration * 1000).round());
}

MpvStatus? parseMpvStatus(String line) {
  final parts = line.trim().split(RegExp(r'\s+'));
  final at = parts.indexOf('otakase-status');
  if (at < 0 || parts.length < at + 4) return null;
  final pos = double.tryParse(parts[at + 1]),
      dur = double.tryParse(parts[at + 2]);
  if (pos == null || dur == null) return null;
  return MpvStatus(
    pos,
    dur,
    parts[at + 3] == '1',
    seek: parts.length > at + 4 && parts[at + 4] == '1',
  );
}

/// Quotes [s] for an mpv input command.
String mpvQuote(String s) =>
    '"${s.replaceAll(r'\', r'\\').replaceAll('"', r'\"').replaceAll('\n', ' ')}"';

bool _languageMatch(Json track, String preferred) {
  final p = preferred.trim().toLowerCase();
  if (p.isEmpty) return false;
  final lang = (track['language'] as String? ?? '').toLowerCase();
  final label = (track['label'] as String? ?? '').toLowerCase();
  return lang == p ||
      p.startsWith(lang) && lang.length >= 2 ||
      label.contains(p);
}

/// The mpv command line for [stream].
List<String> mpvArguments({
  required ResolvedStream stream,
  required String title,
  required AppStore store,
  required String scriptPath,
  required String commandsPath,
  int start = 0,
}) {
  final headers = Map<String, String>.of(stream.headers)
    ..remove('Referer')
    ..remove('referer');
  final referrer = stream.referrer.isNotEmpty
      ? stream.referrer
      : stream.headers['Referer'] ?? stream.headers['referer'] ?? '';
  final subs = <String>[];
  for (final preferred in store.text('SubsLanguage').split(',')) {
    for (final t in stream.subtitles) {
      final url = t['url'] as String? ?? '';
      if (url.isNotEmpty &&
          _languageMatch(t, preferred) &&
          !subs.contains(url)) {
        subs.add(url);
      }
    }
  }
  final af = audioFilter(store);
  return [
    '--force-window=immediate',
    '--title=$title',
    '--force-media-title=$title',
    '--script=$scriptPath',
    '--script-opts=otakase-commands=$commandsPath',
    if (referrer.isNotEmpty) '--referrer=$referrer',
    for (final e in headers.entries)
      '--http-header-fields-append=${e.key}: ${e.value}',
    if (start > 0) '--start=$start',
    '--slang=${store.text('SubsLanguage')}',
    '--alang=${store.text('AudioLanguages')}',
    if (af.isNotEmpty) '--af-append=$af',
    for (final s in subs.take(3)) '--sub-file=$s',
    if (store.text('SubsLanguage').trim().toLowerCase() == 'off') '--sid=no',
    '--',
    stream.url,
  ];
}

typedef ProcessStarter = Future<Process> Function(
  String executable,
  List<String> arguments,
);

class ExternalPlayerPage extends StatefulWidget {
  const ExternalPlayerPage({
    super.key,
    required this.services,
    required this.anime,
    required this.episode,
    required this.mode,
    this.start,
  });
  final AppServices services;
  final Anime anime;
  final int episode;
  final String mode;
  final ProcessStarter? start;
  @override
  State<ExternalPlayerPage> createState() => _ExternalPlayerPageState();
}

class _ExternalPlayerPageState extends State<ExternalPlayerPage> {
  late Anime anime = widget.anime;
  late int episode = widget.episode;
  late final resumePusher = widget.services.tracking.pusher();
  late final Future<void> Function(String, Json) commandHandler = control;
  late final Json Function() statusHandler = remoteStatus;
  late final Future<void> Function() stopHandler = stopForQuit;
  RequestScope scope = RequestScope();
  PlaybackSource? source;
  Process? process;
  File? commands;
  MpvStatus? last;
  var viewing = EpisodeViewing();
  String? viewingKey;
  String? endReason;
  bool completionSaved = false;
  List<SkipSpan> spans = [];
  final skipped = <String>{};
  final failed = <String>{};
  String status = 'Finding an episode', error = '';
  bool busy = true, ended = false;
  int generation = 0, savedResume = 0;
  Timer? saveTimer;

  @override
  void initState() {
    super.initState();
    widget.services.tracking.remember(anime);
    widget.services.playerCommand = commandHandler;
    widget.services.playerStatus = statusHandler;
    widget.services.stopPlayback = stopHandler;
    saveTimer = Timer.periodic(
      const Duration(seconds: 5),
      (_) => unawaited(save()),
    );
    unawaited(open());
  }

  @override
  void dispose() {
    generation++;
    scope.cancel();
    saveTimer?.cancel();
    if (identical(widget.services.playerCommand, commandHandler)) {
      widget.services.playerCommand = null;
    }
    if (identical(widget.services.playerStatus, statusHandler)) {
      widget.services.playerStatus = null;
    }
    if (identical(widget.services.stopPlayback, stopHandler)) {
      widget.services.stopPlayback = null;
    }
    unawaited(save(leaving: true).then((_) => resumePusher.flush()));
    unawaited(widget.services.discord.stopWatching());
    process?.kill();
    unawaited(widget.services.core.stopPlayback());
    super.dispose();
  }

  Json remoteStatus() => {
    'has_player': true,
    'anilist_id': anime.id,
    'title': anime.title,
    'adult': anime.isAdult,
    'episode': episode,
    'episodes': anime.airedEpisodes,
    'loaded': last?.loaded ?? false,
    'position': (last?.position ?? 0).floor(),
    'position_ms': ((last?.position ?? 0) * 1000).round(),
    'duration': (last?.duration ?? 0).floor(),
    'playing': !(last?.paused ?? true),
  };

  Future<void> mpv(String command) async {
    final f = commands;
    if (f == null || process == null) return;
    await f.writeAsString('$command\n', mode: FileMode.append, flush: true);
  }

  Future<void> control(String action, Json args) async {
    if (!mounted) return;
    switch (action) {
      case 'pause':
        await mpv('set pause yes');
      case 'play':
        await mpv('set pause no');
      case 'toggle':
        await mpv('cycle pause');
      case 'skip':
        await mpv('seek 85');
      case 'seek':
        await mpv('seek ${number(args['seconds']).clamp(-86400, 86400)}');
      case 'seek_to':
        final to = (args['position'] as num?)?.toDouble() ?? 0;
        await mpv('seek ${math.max(0, to).toStringAsFixed(2)} absolute');
      case 'party_skip':
        final pos = last?.position ?? 0;
        final span = spans.where((s) => s.contains(pos)).firstOrNull;
        if (span == null) {
          await mpv('seek 85');
        } else {
          await skipSpan(span, last!);
        }
      case 'volume':
        await mpv('set volume ${number(args['volume']).clamp(0, 100)}');
      case 'next':
        await play(episode + 1);
      case 'previous':
        if (episode > 1) await play(episode - 1);
      case 'episode':
        await play(number(args['episode']));
      case 'stop':
        await mpv('quit');
        if (mounted) Navigator.pop(context);
    }
  }

  Future<void> play(int target) async {
    if (target < 1 || anime.airedEpisodes > 0 && target > anime.airedEpisodes) {
      return;
    }
    await save(leaving: true);
    await resumePusher.flush();
    final old = process;
    process = null;
    old?.kill();
    episode = target;
    savedResume = 0;
    await open();
  }

  Future<void> open() async {
    final g = ++generation;
    scope.cancel();
    scope = RequestScope();
    setState(() {
      busy = true;
      ended = false;
      error = '';
      last = null;
      spans = [];
      status = 'Finding an episode';
    });
    skipped.clear();
    viewing = EpisodeViewing();
    viewingKey = null;
    endReason = null;
    completionSaved = false;
    final entry = widget.services.store.entry(anime);
    savedResume = entry?.resumeFor(episode) ?? 0;
    final synced = await widget.services.tracking.resume(anime);
    savedResume = math.max(savedResume, synced.forEpisode(episode));
    if ((entry?.watched.contains(episode) ?? false) ||
        episode <= widget.services.tracking.progress(anime)) {
      savedResume = 0;
    }
    try {
      await widget.services.core.stopPlayback();
      source = await widget.services.resolver.resolve(
        anime,
        episode,
        scope: scope,
        exclude: failed,
        mode: widget.mode,
        status: (s) {
          if (mounted && g == generation) setState(() => status = s);
        },
      );
      if (!mounted || g != generation) return;
      final current = source!;
      viewingKey =
          '$episode:${current.provider}:${current.showId}:${current.mode}:0';
      final dir = await Directory.systemTemp.createTemp('otakase-mpv');
      final script = File('${dir.path}${Host.pathSeparator}otakase.lua');
      await script.writeAsString(mpvStatusScript);
      commands = File('${dir.path}${Host.pathSeparator}commands');
      await commands!.writeAsString('');
      final title = '${anime.title} - Episode $episode';
      final args = mpvArguments(
        stream: source!.streams.first,
        title: title,
        store: widget.services.store,
        scriptPath: script.path,
        commandsPath: commands!.path,
        start: widget.services.store.flag('ResumePlayback') ? savedResume : 0,
      );
      final exe = widget.services.store.text('MpvPath').trim();
      final p = await (widget.start ?? Process.start)(
        exe.isEmpty ? 'mpv' : exe,
        args,
      );
      if (!mounted || g != generation) {
        p.kill();
        return;
      }
      process = p;
      setState(() {
        busy = false;
        status = 'Playing in mpv';
      });
      unawaited(p.stderr.drain<void>());
      final output = p.stdout
          .transform(utf8.decoder)
          .transform(const LineSplitter())
          .forEach((line) => onStatus(line, g));
      unawaited(loadSkips(g));
      unawaited(
        p.exitCode.then((_) async {
          await output;
          if (identical(process, p)) await exited(g);
          try {
            await dir.delete(recursive: true);
          } catch (_) {}
        }),
      );
    } on ProcessException {
      if (mounted && g == generation) {
        setState(() {
          busy = false;
          error = 'mpv could not be started. Install mpv, or set its path in Settings, Playback.';
        });
      }
    } catch (_) {
      if (mounted && g == generation) {
        setState(() {
          busy = false;
          error = 'This episode could not be opened.';
        });
      }
    }
  }

  Future<void> loadSkips(int g) async {
    final s = source;
    if (s == null) return;
    final found = await widget.services.resolver.skips(s, episode, scope);
    // Online skip times need the length; wait until mpv knows it.
    for (var i = 0; i < 30 && (last?.duration ?? 0) <= 0; i++) {
      await Future<void>.delayed(const Duration(seconds: 1));
      if (!mounted || g != generation) return;
    }
    if (!mounted || g != generation) return;
    final seconds = last?.duration ?? 0;
    if (seconds <= 0) return;
    final provider = selectSkipSpans(found, const [], seconds);
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
    setState(() => spans = selectSkipSpans(found, extra, seconds));
  }

  void onStatus(String line, int g) {
    if (!mounted || g != generation) return;
    final end = RegExp(r'otakase-end (\w+)$').firstMatch(line.trim());
    if (end != null) {
      endReason = end[1];
      if (endReason == 'eof') unawaited(save());
      return;
    }
    final s = parseMpvStatus(line);
    if (s == null || !s.loaded) return;
    final previous = last;
    final first = previous == null;
    setState(() => last = s);
    viewing.start(
      s.total,
      widget.services.store.entry(anime)?.viewing[viewingKey],
    );
    if (s.seek && previous != null) viewing.externalSeek(previous.at, s.at);
    viewing.sample(s.at, playing: !s.paused);
    if (first && source != null) {
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
    widget.services.discord.observe(anime, episode, s.position);
    unawaited(
      widget.services.presence.episode(
        anime,
        episode,
        paused: s.paused,
        position: s.position,
        duration: s.duration,
        joinUrl: widget.services.party.watchWithMe
            ? widget.services.party.joinUrl ?? ''
            : '',
      ),
    );
    unawaited(widget.services.party.playing(anime, episode));
    final store = widget.services.store;
    for (final span in spans) {
      if (!span.contains(s.position)) continue;
      final setting = switch (span.type) {
        'op' => 'SkipOp',
        'ed' => 'SkipEd',
        _ => 'SkipRecap',
      };
      final key = '${span.type}:${span.start}';
      if (store.flag(setting) &&
          !skipped.contains(key) &&
          s.position >= span.start + store.integer('SkipDelay')) {
        skipped.add(key);
        unawaited(skipSpan(span, s));
        unawaited(
          mpv(
            'show-text ${mpvQuote(span.label.replaceFirst('Skip', 'Skipped'))} 2000',
          ),
        );
      }
    }
  }

  Future<void> skipSpan(SkipSpan span, MpvStatus status) {
    viewing.seek(
      status.at,
      Duration(milliseconds: (span.end * 1000).round()),
      span: span,
    );
    return mpv('seek ${span.end.toStringAsFixed(2)} absolute');
  }

  bool get watched {
    final s = last;
    return s != null &&
        (completionSaved ||
            endReason == 'eof' &&
                copyLengthOk &&
                viewing.ended(s.at, s.total, spans));
  }

  /// A copy under a third of the show's usual episode length is a
  /// placeholder clip, never a watched episode.
  bool get copyLengthOk => copyLengthPlausible(
    last?.total ?? Duration.zero,
    anime.duration * 60 * 2 ~/ 3,
  );

  Future<void> save({bool leaving = false}) async {
    final s = last;
    if (s == null) return;
    final done =
        watched ||
        leaving &&
            endReason != 'error' &&
            copyLengthOk &&
            viewing.credits(s.at, s.total, spans);
    // Closing mpv before playback moved must not wipe the saved spot.
    if (!done && s.position <= 0) return;
    final newlyWatched = done && !completionSaved;
    if (newlyWatched) completionSaved = true;
    final persist = widget.services.store.progress(
      anime,
      episode,
      s.position.floor(),
      s.duration.floor(),
      completed: done,
      viewingKey: viewingKey,
      viewingCheckpoint: viewing.checkpoint,
    );
    if (newlyWatched) {
      if (finishesShow(widget.services, anime, episode)) {
        widget.services.ratingDue = anime;
      }
      await resumePusher.episodeDone(anime.key, episode, anime.episodes);
    } else if (!done) {
      resumePusher.note(anime.key, episode, s.position.floor());
    }
    await persist;
  }

  /// The app quits or switches profile: save where mpv is, close it and
  /// stop the helper's stream, so no mpv window outlives the app.
  Future<void> stopForQuit() async {
    generation++;
    scope.cancel();
    saveTimer?.cancel();
    final running = process;
    process = null;
    await save(leaving: true);
    await resumePusher.flush().timeout(
      const Duration(seconds: 5),
      onTimeout: () {},
    );
    await widget.services.discord.stopWatching();
    running?.kill();
    await widget.services.core.stopPlayback();
  }

  bool get canNext => anime.airedEpisodes > episode || anime.airedEpisodes == 0;

  Future<void> exited(int g) async {
    if (!mounted || g != generation) return;
    process = null;
    await save(leaving: true);
    await resumePusher.flush();
    await widget.services.discord.stopWatching();
    if (!mounted) return;
    final loaded = last?.loaded ?? false;
    if (!loaded && source != null) {
      // mpv closed without playing: this copy is broken, try another.
      failed.add(source!.provider);
      unawaited(
        widget.services.discord.reportProvider(
          source!.provider,
          false,
          'playback',
          'mpv closed before playing',
        ),
      );
    }
    if (watched &&
        canNext &&
        !widget.services.store.flag('NextEpisodePrompt') &&
        (last!.duration - last!.position) < 30) {
      await play(episode + 1);
      return;
    }
    setState(() => ended = true);
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    backgroundColor: bgDeep,
    body: Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 560),
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const OtakaseSeal(size: 72),
              const SizedBox(height: 24),
              Text(
                anime.title,
                textAlign: TextAlign.center,
                style: uiStyle(context, size: 24, weight: FontWeight.w700),
              ),
              Text('Episode $episode', style: uiStyle(context, color: muted)),
              const SizedBox(height: 20),
              if (busy) const CircularProgressIndicator(),
              const SizedBox(height: 12),
              Text(
                error.isNotEmpty
                    ? error
                    : ended
                    ? 'mpv closed.'
                    : last?.loaded == true
                    ? '${clock(last!.position)} / ${clock(last!.duration)}${last!.paused ? ' · paused' : ''}'
                    : status,
                textAlign: TextAlign.center,
                style: uiStyle(context, color: error.isNotEmpty ? warn : fg),
              ),
              const SizedBox(height: 24),
              Wrap(
                spacing: 12,
                runSpacing: 12,
                alignment: WrapAlignment.center,
                children: [
                  if (error.isNotEmpty || ended)
                    AppButton(
                      label: 'Play again',
                      isTV: true,
                      onPressed: () => unawaited(open()),
                    ),
                  if (canNext && !busy)
                    AppButton(
                      label: 'Next episode',
                      isTV: true,
                      autofocus: ended,
                      onPressed: () => unawaited(play(episode + 1)),
                    ),
                  AppButton(
                    label: ended || error.isNotEmpty ? 'Back' : 'Stop',
                    filled: true,
                    isTV: true,
                    autofocus: !ended,
                    onPressed: () => unawaited(control('stop', {})),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    ),
  );

  static String clock(double s) {
    final t = Duration(milliseconds: (s * 1000).round());
    final h = t.inHours, m = t.inMinutes % 60, sec = t.inSeconds % 60;
    String two(int v) => v.toString().padLeft(2, '0');
    return h > 0 ? '$h:${two(m)}:${two(sec)}' : '${two(m)}:${two(sec)}';
  }
}
