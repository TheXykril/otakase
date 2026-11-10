import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';

import '../core.dart';
import 'models.dart';
import 'store.dart';
import 'trakt.dart';

/// What a profile shows: anime, films and series, or both.
enum ContentMode { anime, movies, both }

ContentMode contentModeOf(AppStore store) => switch (store.text('Content')) {
  'movies' => ContentMode.movies,
  'both' => ContentMode.both,
  _ => ContentMode.anime,
};

const contentLabels = {
  'both': 'Anime, movies & shows',
  'anime': 'Anime only',
  'movies': 'Movies & shows only',
};

/// Source languages, as the core names them, and how settings name them.
const movieLanguageNames = {'en': 'english', 'lt': 'lithuanian'};

String languageTitle(String name) =>
    name.isEmpty ? name : name[0].toUpperCase() + name.substring(1);

class MovieSource {
  MovieSource.fromJson(Json json)
    : name = json['name'] as String? ?? '',
      label = json['label'] as String? ?? '',
      language = json['language'] as String? ?? '',
      series = json['series'] == true;
  const MovieSource(this.name, this.label, this.language, {this.series = true});
  final String name, label, language;
  final bool series;
  String get languageName => movieLanguageNames[language] ?? language;
}

/// The sources the core has, shown in settings before it answers.
const knownMovieSources = [
  MovieSource('vidsrc', 'vidsrc', 'en'),
  MovieSource('8filmai', '8Filmai', 'lt'),
  MovieSource('filmukas', 'Filmukas', 'lt', series: false),
];

/// Where a title was left: the episode for a series, the position, and what
/// was watched.
class MovieEntry {
  const MovieEntry({
    required this.title,
    this.season = 0,
    this.episode = 0,
    this.position = 0,
    this.duration = 0,
    this.percent = 0,
    this.watched = const {},
    this.updated = 0,
    this.pushed = 0,
  });
  final MediaTitle title;
  final int season, episode, position, duration, updated;

  /// The [updated] time Trakt last heard about, so a spot saved here but
  /// never sent (the app was closed mid-film) goes up on the next sync.
  final int pushed;

  /// Progress Trakt reported, used until the duration is known here.
  final double percent;

  /// 'movie' for a watched film, S/E codes for watched episodes.
  final Set<String> watched;
  bool get finished => title.isShow ? false : watched.contains('movie');
  String get code => 'S${season}E$episode';
  bool get started => position > 0 || percent > 0;
  double get progress => position > 0 && duration > 0
      ? (position / duration).clamp(0, 1).toDouble()
      : (percent / 100).clamp(0, 1).toDouble();
  int resumeAt(int knownDuration) => position > 0
      ? position
      : percent > 0 && knownDuration > 0
      ? (knownDuration * percent / 100).round()
      : 0;

  MovieEntry copyWith({
    MediaTitle? title,
    int? season,
    int? episode,
    int? position,
    int? duration,
    double? percent,
    Set<String>? watched,
    int? updated,
    int? pushed,
  }) => MovieEntry(
    title: title ?? this.title,
    season: season ?? this.season,
    episode: episode ?? this.episode,
    position: position ?? this.position,
    duration: duration ?? this.duration,
    percent: percent ?? this.percent,
    watched: watched ?? this.watched,
    updated: updated ?? this.updated,
    pushed: pushed ?? this.pushed,
  );

  factory MovieEntry.fromJson(Json json) => MovieEntry(
    title: MediaTitle.fromJson((json['title'] as Map).cast<String, dynamic>()),
    season: number(json['season']),
    episode: number(json['episode']),
    position: number(json['position']),
    duration: number(json['duration']),
    percent: (json['percent'] as num?)?.toDouble() ?? 0,
    watched: (json['watched'] as List? ?? []).whereType<String>().toSet(),
    updated: number(json['updated']),
    pushed: number(json['pushed']),
  );
  Json toJson() => {
    'title': title.toJson(),
    'season': season,
    'episode': episode,
    'position': position,
    'duration': duration,
    'percent': percent,
    'watched': watched.toList()..sort(),
    'updated': updated,
    'pushed': pushed,
  };
}

/// A stream found for a title, and the source it came from.
class MoviePlayback {
  MoviePlayback({
    required this.source,
    required this.movie,
    required this.stream,
    required this.server,
    this.audioLanguage = '',
  });
  final MovieSource source;
  final Json movie;
  final ResolvedStream stream;
  final String server, audioLanguage;
}

/// Films and series for one profile: Trakt for the catalog and the viewer's
/// lists, the core's sources for playing, and the local history.
class MovieService extends ChangeNotifier {
  MovieService({
    required this.trakt,
    required this.core,
    required this.store,
    this.pushGap = const Duration(milliseconds: 1100),
  }) {
    _read();
    trakt.addListener(_traktChanged);
  }
  final Trakt trakt;
  final CoreClient core;
  final AppStore store;

  /// The wait between progress uploads, under Trakt's one write a second.
  final Duration pushGap;
  final Map<String, MovieEntry> _entries = {};
  List<MediaTitle> watchlist = [];
  bool syncing = false;
  String? lastError;
  DateTime? _lastProgressSentAt;
  String? _progressAccount;
  DateTime? get lastProgressSentAt =>
      trakt.signedIn && _progressAccount == trakt.username
      ? _lastProgressSentAt
      : null;
  bool get hasPendingResume => _entries.values.any(
    (e) => e.updated > e.pushed && e.position >= 60 && e.duration > 0,
  );
  void _progressSent(String account) {
    if (_disposed || !trakt.signedIn || trakt.username != account) return;
    _lastProgressSentAt = DateTime.now();
    _progressAccount = trakt.username;
    _notify();
  }

  bool _disposed = false;
  final Map<String, Json> _matches = {};
  final Map<String, String> _translations = {};
  Future<List<MovieSource>>? _sources;
  bool _wasSignedIn = false;

  String get _key => '${store.namespace}movies.v1';

  void _read() {
    try {
      final data = (jsonDecode(
        store.preferences.getString(_key) ?? '{}',
      ) as Map).cast<String, dynamic>();
      for (final e in (data['entries'] as List? ?? []).whereType<Map>()) {
        final entry = MovieEntry.fromJson(e.cast<String, dynamic>());
        _entries[entry.title.key] = entry;
      }
      watchlist = [
        for (final t in (data['watchlist'] as List? ?? []).whereType<Map>())
          MediaTitle.fromJson(t.cast<String, dynamic>()),
      ];
    } catch (_) {
      /* A damaged history starts empty rather than blocking the app. */
    }
  }

  Future<void> _persist() => store.preferences.setString(
    _key,
    jsonEncode({
      'entries': [for (final e in _entries.values) e.toJson()],
      'watchlist': [for (final t in watchlist) t.toJson()],
    }),
  );

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  void _traktChanged() {
    if (trakt.signedIn && !_wasSignedIn) unawaited(sync());
    _wasSignedIn = trakt.signedIn;
    _notify();
  }

  @override
  void dispose() {
    _disposed = true;
    trakt.removeListener(_traktChanged);
    super.dispose();
  }

  MovieEntry? entry(MediaTitle title) => _entries[title.key];

  int _lastStamp = 0;

  /// A time for an update that sorts after every earlier one, even when two
  /// updates land in the same millisecond.
  int _stamp() {
    final now = DateTime.now().millisecondsSinceEpoch;
    _lastStamp = now > _lastStamp ? now : _lastStamp + 1;
    return _lastStamp;
  }

  List<MovieEntry> get continuing =>
      _entries.values
          .where(
            (e) =>
                // A watched film being watched again still has a spot to resume.
                e.started || !e.finished && e.title.isShow && e.episode > 0,
          )
          .toList()
        ..sort((a, b) => b.updated.compareTo(a.updated));

  bool onWatchlist(MediaTitle title) =>
      watchlist.any((t) => t.key == title.key);

  bool watched(MediaTitle title, {int season = 0, int episode = 0}) {
    final e = _entries[title.key];
    if (e == null) return false;
    return title.isShow
        ? e.watched.contains('S${season}E$episode')
        : e.watched.contains('movie');
  }

  /// Pulls the watchlist, watched titles and paused progress from Trakt, and
  /// sends up any spot saved here that Trakt has not heard of. For progress
  /// the newer side wins, so a film paused on the phone resumes on the TV and
  /// the other way round.
  Future<void> sync() async {
    await trakt.load();
    if (!trakt.signedIn || syncing) return;
    syncing = true;
    _notify();
    try {
      final lib = await trakt.library();
      watchlist = lib.watchlist;
      for (final t in lib.watchedMovies.values) {
        final e = _entries[t.key];
        if (e == null || !e.watched.contains('movie')) {
          _entries[t.key] = (e ?? MovieEntry(title: t, updated: 0)).copyWith(
            watched: {...?e?.watched, 'movie'},
            position: 0,
            percent: 0,
          );
        }
      }
      for (final shown in lib.watchedEpisodes.values) {
        final e =
            _entries[shown.title.key] ??
            MovieEntry(title: shown.title, updated: 0);
        _entries[shown.title.key] = e.copyWith(
          watched: {...e.watched, ...shown.codes},
        );
      }
      final pending = _mergePlayback(lib.playback);
      lastError = null;
      await _persist();
      _notify();
      await _push(pending);
    } catch (e) {
      lastError = 'Trakt sync failed. $e';
    } finally {
      syncing = false;
      _notify();
    }
  }

  /// Takes Trakt's paused spots where they are newer than the one saved here
  /// and returns the keys of local spots Trakt is behind on.
  List<String> _mergePlayback(List<TraktPaused> paused) {
    final stamp = DateTime.now().millisecondsSinceEpoch;
    final onTrakt = <String, TraktPaused>{};
    for (final p in paused) {
      final had = onTrakt[p.title.key];
      if (had == null || p.pausedAt > had.pausedAt) onTrakt[p.title.key] = p;
    }
    for (final p in onTrakt.values) {
      final e = _entries[p.title.key];
      // Anything newer saved here wins, a finish included: a film watched to
      // the end here must not come back from an older pause on Trakt.
      if (e != null && p.pausedAt > 0 && e.updated > p.pausedAt) {
        continue;
      }
      final when = p.pausedAt > 0 ? p.pausedAt : stamp;
      final same =
          e != null &&
          (!p.title.isShow || e.season == p.season && e.episode == p.episode);
      _entries[p.title.key] = (e ?? MovieEntry(title: p.title)).copyWith(
        season: p.title.isShow ? p.season : null,
        episode: p.title.isShow ? p.episode : null,
        percent: p.progress,
        position: same && e.duration > 0
            ? (e.duration * p.progress / 100).round()
            : 0,
        duration: same ? null : 0,
        updated: e == null || when > e.updated ? when : e.updated,
        pushed: e == null || when > e.updated ? when : e.updated,
      );
    }
    final cutoff = stamp - const Duration(days: 30).inMilliseconds;
    return [
      for (final e in _entries.values)
        if (e.updated > e.pushed &&
            e.updated > cutoff &&
            e.position >= 60 &&
            e.duration > 0)
          e.title.key,
    ];
  }

  /// Sends saved spots up to Trakt as paused playback, newest first.
  Future<void> _push(List<String> keys) async {
    final account = trakt.username;
    final entries = [for (final k in keys) ?_entries[k]]
      ..sort((a, b) => b.updated.compareTo(a.updated));
    var first = true;
    for (final e in entries.take(10)) {
      if (!trakt.signedIn) return;
      if (!first && pushGap > Duration.zero) await Future.delayed(pushGap);
      first = false;
      try {
        await trakt.scrobble(
          'pause',
          e.title,
          e.position * 100 / e.duration,
          season: e.season,
          episode: e.episode,
        );
        _markPushed(e.title.key, e.updated);
        _progressSent(account);
      } catch (_) {
        /* Tried again on the next sync. */
      }
    }
    await _persist();
  }

  void _markPushed(String key, int updated) {
    final now = _entries[key];
    if (now != null && now.updated == updated) {
      _entries[key] = now.copyWith(pushed: updated);
    }
  }

  /// Picks up a spot paused on another device just before playing. Quick and
  /// quiet: playback starts from the local spot if Trakt is slow or away.
  Future<void> refreshResume() async {
    if (!trakt.signedIn) return;
    try {
      final paused = await trakt.playback().timeout(const Duration(seconds: 6));
      final pending = _mergePlayback(paused);
      await _persist();
      _notify();
      unawaited(_push(pending));
    } catch (_) {
      /* The local spot is used. */
    }
  }

  Future<void> setWatchlist(MediaTitle title, bool on) async {
    watchlist = [if (on) title, ...watchlist.where((t) => t.key != title.key)];
    _notify();
    await _persist();
    if (trakt.signedIn) {
      try {
        await trakt.setWatchlist(title, on);
      } catch (e) {
        lastError = 'Trakt was not updated. $e';
        _notify();
      }
    }
  }

  Future<void> markWatched(
    MediaTitle title,
    bool on, {
    int season = 0,
    int episode = 0,
  }) async {
    final e = _entries[title.key] ?? MovieEntry(title: title);
    final code = title.isShow ? 'S${season}E$episode' : 'movie';
    final watched = {...e.watched};
    on ? watched.add(code) : watched.remove(code);
    _entries[title.key] = e.copyWith(
      watched: watched,
      position:
          on && (!title.isShow || e.season == season && e.episode == episode)
          ? 0
          : null,
      percent: on ? 0 : null,
      updated: _stamp(),
    );
    _notify();
    await _persist();
    if (trakt.signedIn) {
      try {
        await trakt.setWatched(title, on, season: season, episode: episode);
      } catch (e) {
        lastError = 'Trakt was not updated. $e';
        _notify();
      }
    }
  }

  /// Saves where playback is. A finished episode or film is marked watched;
  /// Trakt hears of it through the scrobble the player sends.
  Future<void> progress(
    MediaTitle title,
    int position,
    int duration, {
    int season = 0,
    int episode = 0,
    bool completed = false,
  }) async {
    final e = _entries[title.key] ?? MovieEntry(title: title);
    final watched = {...e.watched};
    if (completed) watched.add(title.isShow ? 'S${season}E$episode' : 'movie');
    _entries[title.key] = e.copyWith(
      title: title,
      season: season,
      episode: episode,
      position: completed ? 0 : position,
      duration: duration,
      percent: 0,
      watched: watched,
      updated: _stamp(),
    );
    _notify();
    await _persist();
  }

  Future<void> forget(MediaTitle title) async {
    _entries.remove(title.key);
    _notify();
    await _persist();
  }

  Future<void> scrobble(
    String action,
    MediaTitle title,
    double percent, {
    int season = 0,
    int episode = 0,
  }) async {
    if (!trakt.signedIn) return;
    final account = trakt.username;
    final sent = _entries[title.key]?.updated;
    try {
      await trakt.scrobble(
        action,
        title,
        percent,
        season: season,
        episode: episode,
      );
      if (sent != null && action != 'start') {
        _markPushed(title.key, sent);
        _progressSent(account);
        await _persist();
      }
    } catch (_) {
      /* Playback goes on; the progress is saved locally. */
    }
  }

  // ------------------------------------------------------------ sources

  Future<List<MovieSource>> sources() => _sources ??= _loadSources();
  Future<List<MovieSource>> _loadSources() async {
    try {
      final d = await core.request({'operation': 'movie_providers'});
      return [
        for (final s in (d['providers'] as List? ?? []).whereType<Map>())
          MovieSource.fromJson(s.cast<String, dynamic>()),
      ];
    } catch (_) {
      _sources = null;
      rethrow;
    }
  }

  /// The language order from settings, then any other language.
  List<String> get languages {
    final chosen = store
        .strings('MovieLanguages')
        .map((l) => l.toLowerCase().trim())
        .where((l) => l.isNotEmpty)
        .toList();
    return [
      ...chosen,
      ...movieLanguageNames.values.where((l) => !chosen.contains(l)),
    ];
  }

  /// The sources tried for a title, best first: the viewer's first language,
  /// then the next, each language's sources in the core's order. The source
  /// that played a title before goes first.
  Future<List<MovieSource>> sourcesFor(MediaTitle title) async {
    final all = await sources();
    final order = languages;
    final usable = all.where((s) => !title.isShow || s.series).toList();
    int rank(MovieSource s) {
      final i = order.indexOf(s.languageName);
      return i < 0 ? order.length : i;
    }

    final sorted = [...usable]
      ..sort((a, b) {
        final r = rank(a).compareTo(rank(b));
        return r != 0 ? r : all.indexOf(a).compareTo(all.indexOf(b));
      });
    final remembered = rememberedSource(title);
    final first = sorted.where((s) => s.name == remembered).toList();
    return [...first, ...sorted.where((s) => s.name != remembered)];
  }

  String? rememberedSource(MediaTitle title) {
    final choices = store.value('WorkingMovieSources');
    return choices is Map ? choices[title.key] as String? : null;
  }

  Future<void> rememberSource(MediaTitle title, String source) async {
    if (rememberedSource(title) == source) return;
    final choices = Map<String, dynamic>.from(
      store.value('WorkingMovieSources') as Map? ?? {},
    );
    choices[title.key] = source;
    await store.set('WorkingMovieSources', choices);
  }

  Future<List<String>> _titlesFor(MediaTitle title, MovieSource source) async {
    final names = <String>[];
    if (source.language != 'en' && source.language.isNotEmpty) {
      final key = '${title.key}:${source.language}';
      final translated = _translations[key] ??= await trakt.translation(
        title,
        source.language,
      );
      names.add(translated);
    }
    names.addAll([title.title, title.original]);
    final seen = <String>{};
    return names
        .map((n) => n.trim())
        .where((n) => n.isNotEmpty && seen.add(n.toLowerCase()))
        .toList();
  }

  /// Finds a stream for a film, or for one episode of a series, trying each
  /// source in turn. [skip] holds source:server pairs that already failed.
  Future<MoviePlayback> resolve(
    MediaTitle title, {
    int season = 0,
    int episode = 0,
    required RequestScope scope,
    required void Function(String) status,
    Set<String> skip = const {},
  }) async {
    final order = await sourcesFor(title);
    scope.check();
    final subtitles = store
        .text('SubsLanguage')
        .split(',')
        .map((s) => s.trim())
        .firstWhere(
          (s) => s.isNotEmpty && s != 'off' && s != 'none',
          orElse: () => 'english',
        );
    for (final source in order) {
      scope.check();
      status('Finding a copy');
      try {
        final matchKey =
            '${source.name}:${title.key}:${title.isShow ? season : 0}';
        var movie = _matches[matchKey];
        if (movie == null) {
          final found = await core.request(
            {
              'operation': 'movie_match',
              'provider': source.name,
              'titles': await _titlesFor(title, source),
              'year': title.year,
              'imdb': title.imdb,
              'series': title.isShow,
              'season': season,
            },
            scope: scope,
            timeout: const Duration(seconds: 45),
          );
          movie = (found['movie'] as Map).cast<String, dynamic>();
          _matches[matchKey] = movie;
        }
        final d = await core.request(
          {
            'operation': 'movie_resolve',
            'movie': movie,
            'season': season,
            'episode': episode,
            'subtitles': subtitles,
            'skip': [
              for (final s in skip)
                if (s.startsWith('${source.name}:'))
                  s.substring(source.name.length + 1),
            ],
          },
          scope: scope,
          timeout: const Duration(seconds: 95),
        );
        final stream = (d['stream'] as Map).cast<String, dynamic>();
        store.health('movie:${source.name}', 'Working');
        unawaited(rememberSource(title, source.name));
        return MoviePlayback(
          source: source,
          movie: movie,
          stream: ResolvedStream.fromJson(stream),
          server: stream['server'] as String? ?? '',
          audioLanguage: stream['audio_language'] as String? ?? '',
        );
      } on CoreFailure catch (e) {
        if (e.code == 'cancelled') rethrow;
        store.health(
          'movie:${source.name}',
          e.code == 'not_found' ? 'No match' : 'Unavailable',
        );
      } catch (_) {
        scope.check();
        store.health('movie:${source.name}', 'Unavailable');
      }
    }
    throw const CoreFailure(
      'no_stream',
      'No source has this title in your languages. Check Movies & shows settings.',
    );
  }
}
