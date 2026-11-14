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

/// Why no copy of a film or episode played: no source for the chosen
/// languages, sources that did not answer, or none that has it.
/// [reasons] says per language why its sources failed, so a device that
/// fails where another plays shows why.
String noCopyMessage(
  int sources,
  int unreachable, {
  List<String> reasons = const [],
}) {
  final why = reasons.isEmpty ? '' : '\n${reasons.join('\n')}';
  return _noCopyMessage(sources, unreachable) + why;
}

String _noCopyMessage(int sources, int unreachable) {
  if (sources == 0) {
    return 'No source covers your movie & show languages. Add one in More, '
        'Subtitles & languages.';
  }
  if (unreachable == sources) {
    return "Couldn't reach any source. Check your connection and try again "
        'in a minute.';
  }
  if (unreachable > 0) {
    return "No source we reached has this yet, and some didn't answer. "
        'Try again in a minute.';
  }
  return 'No source has this yet. Try another language in More, '
      'Subtitles & languages.';
}

/// Why one source gave no copy, in a few words: the core's reason when it
/// gives one ("Source request failed (timed out).").
String failureReason(CoreFailure e) {
  switch (e.code) {
    case 'not_found':
      return 'not found';
    case 'no_stream':
      return 'no server played';
    case 'timeout':
      return 'timed out';
    case 'rate_limited':
      return 'too many requests';
  }
  final detail = RegExp(r'\(([^()]+)\)\.?$').firstMatch(e.message);
  return detail?.group(1) ?? 'failed';
}

/// Source languages, as the core names them, and how settings name them.
const movieLanguageNames = {'en': 'english', 'lt': 'lithuanian'};

/// "japanese,chinese,english" as "Japanese, Chinese, English".
String languageListLabel(String value) {
  final names = [
    for (final n in value.split(','))
      if (n.trim().isNotEmpty) languageTitle(n.trim()),
  ];
  return names.isEmpty ? 'Off' : names.join(', ');
}

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

  /// One number for the episode playing, for comparing places.
  int get place => season * 100000 + episode;
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
/// A watched mark Trakt has not heard of yet: a finish or a mark made while
/// Trakt or the network was away. Sent on the next sync.
class UnsentWatch {
  const UnsentWatch({
    required this.title,
    required this.watched,
    required this.at,
    this.season = 0,
    this.episode = 0,
  });
  final MediaTitle title;
  final bool watched;
  final int season, episode;

  /// When it was watched or marked, in milliseconds since the epoch.
  final int at;

  bool sameItem(UnsentWatch o) =>
      o.title.key == title.key && o.season == season && o.episode == episode;

  factory UnsentWatch.fromJson(Json json) => UnsentWatch(
    title: MediaTitle.fromJson((json['title'] as Map).cast<String, dynamic>()),
    watched: json['watched'] == true,
    at: number(json['at']),
    season: number(json['season']),
    episode: number(json['episode']),
  );
  Json toJson() => {
    'title': title.toJson(),
    'watched': watched,
    'at': at,
    'season': season,
    'episode': episode,
  };
}

/// A watchlist change made here. Kept until a Trakt pull that started after
/// it was sent, so a pull already under way cannot undo it, and resent when
/// Trakt or the network was away.
class WatchlistEdit {
  const WatchlistEdit({
    required this.title,
    required this.on,
    required this.at,
    this.sent = 0,
  });
  final MediaTitle title;
  final bool on;

  /// When it was made, and when Trakt took it (0 until then), in
  /// milliseconds since the epoch.
  final int at, sent;

  factory WatchlistEdit.fromJson(Json json) => WatchlistEdit(
    title: MediaTitle.fromJson((json['title'] as Map).cast<String, dynamic>()),
    on: json['on'] == true,
    at: number(json['at']),
    sent: number(json['sent']),
  );
  Json toJson() => {
    'title': title.toJson(),
    'on': on,
    'at': at,
    if (sent > 0) 'sent': sent,
  };
}

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
  final List<UnsentWatch> _unsent = [];

  /// Watchlist changes made here that a Trakt pull has not confirmed yet,
  /// by title key.
  final Map<String, WatchlistEdit> _watchlistEdits = {};

  /// When each title was last seen on the Trakt watchlist, by key, so a
  /// show Trakt dropped because an episode was watched can be told from
  /// one taken off on purpose.
  final Map<String, int> _listedSeen = {};

  /// Watched marks still to send to Trakt.
  List<UnsentWatch> get unsentWatches => List.unmodifiable(_unsent);

  /// Trakt's last activity seen at the last full pull, per account, so a
  /// sync with nothing new on Trakt skips the big download.
  final Map<String, String> _activity = {};
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
      _unsent.addAll([
        for (final u in (data['unsent'] as List? ?? []).whereType<Map>())
          UnsentWatch.fromJson(u.cast<String, dynamic>()),
      ]);
      for (final w
          in (data['watchlist_edits'] as List? ?? []).whereType<Map>()) {
        final edit = WatchlistEdit.fromJson(w.cast<String, dynamic>());
        _watchlistEdits[edit.title.key] = edit;
      }
      // 'activity' was saved by pulls that missed every watched episode
      // (Trakt's July 2026 change); dropping it makes the next sync pull
      // everything once.
      if (data['listed_seen'] case final Map seen) {
        for (final MapEntry(:key, :value) in seen.entries) {
          _listedSeen['$key'] = number(value);
        }
      }
      if (data['activity2'] case final Map activity) {
        _activity.addAll(activity.cast<String, String>());
      }
    } catch (_) {
      /* A damaged history starts empty rather than blocking the app. */
    }
  }

  Future<void> _persist() => store.preferences.setString(
    _key,
    jsonEncode({
      'entries': [for (final e in _entries.values) e.toJson()],
      'watchlist': [for (final t in watchlist) t.toJson()],
      if (_unsent.isNotEmpty) 'unsent': [for (final u in _unsent) u.toJson()],
      if (_watchlistEdits.isNotEmpty)
        'watchlist_edits': [for (final w in _watchlistEdits.values) w.toJson()],
      if (_activity.isNotEmpty) 'activity2': _activity,
      if (_listedSeen.isNotEmpty) 'listed_seen': _listedSeen,
    }),
  );

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  void _traktChanged() {
    if (trakt.signedIn && !_wasSignedIn) unawaited(sync(force: true));
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
                (e.started || !e.finished && e.title.isShow && e.episode > 0) &&
                !store.continueHidden(e.title.key, e.place, e.updated),
          )
          .toList()
        ..sort((a, b) => b.updated.compareTo(a.updated));

  /// Takes [e] off Continue watching until another episode plays or its
  /// spot moves on.
  Future<void> hideContinue(MovieEntry e) async {
    await store.hideContinue(e.title.key, e.place, e.updated);
    _notify();
  }

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
  ///
  /// Watched marks Trakt missed go up first. Then, unless [force], Trakt's
  /// last activity is checked and the full library is only pulled when
  /// something changed there.
  Future<void> sync({bool force = false}) async {
    await trakt.load();
    if (!trakt.signedIn || syncing) return;
    syncing = true;
    _syncedAt = DateTime.now();
    _notify();
    try {
      await _sendUnsent();
      await _sendWatchlistEdits();
      final account = trakt.username;
      var activity = '';
      try {
        activity = await trakt.lastActivity();
      } catch (_) {
        /* Unknown: pull everything. */
      }
      if (!force && activity.isNotEmpty && _activity[account] == activity) {
        lastError = null;
        await _push(_pendingSpots());
        return;
      }
      final pullStarted = DateTime.now().millisecondsSinceEpoch;
      final lib = await trakt.library();
      final before = watchlist;
      watchlist = _withEdits(lib.watchlist, pullStarted);
      for (final t in lib.watchedMovies.values) {
        final e = _entries[t.key];
        final at = lib.watchedAt[t.key] ?? 0;
        final known = e != null && e.watched.contains('movie');
        // Finished on another device after the spot saved here: the spot is
        // done with. A spot saved here after that watch is a rewatch and
        // stays.
        final done = at > 0 ? e == null || at >= e.updated : !known;
        if (known && !done) continue;
        _entries[t.key] = (e ?? MovieEntry(title: t, updated: 0)).copyWith(
          watched: {...?e?.watched, 'movie'},
          position: done ? 0 : null,
          percent: done ? 0 : null,
          updated: done && at > (e?.updated ?? 0) ? at : null,
          pushed: done && at > (e?.updated ?? 0) ? at : null,
        );
      }
      for (final shown in lib.watchedEpisodes.values) {
        final e =
            _entries[shown.title.key] ??
            MovieEntry(title: shown.title, updated: 0);
        final at =
            lib.watchedAt[watchedKey(
              e.title,
              season: e.season,
              episode: e.episode,
            )] ??
            0;
        // The episode left mid-way here was finished elsewhere since.
        final done = e.started && at > 0 && at >= e.updated;
        _entries[shown.title.key] = e.copyWith(
          watched: {...e.watched, ...shown.codes},
          position: done ? 0 : null,
          percent: done ? 0 : null,
          updated: done ? at : null,
          pushed: done ? at : null,
        );
      }
      await _moveToUpNext(lib.lastEpisodes);
      _keepStartedShows(before, lib, pullStarted);
      await _sendWatchlistEdits();
      final merged = _mergePlayback(lib.playback);
      lastError = null;
      if (activity.isNotEmpty) _activity[account] = activity;
      await _persist();
      _notify();
      await _push(merged.pending);
      await _removeStale(merged.stale);
    } catch (e) {
      lastError = 'Trakt sync failed. $e';
    } finally {
      syncing = false;
      _notify();
    }
  }

  DateTime? _syncedAt;

  /// Syncs again when the app comes back to the front, so a film finished
  /// or paused on another device meanwhile shows here. Not more than once
  /// a minute.
  Future<void> syncIfStale({Duration gap = const Duration(minutes: 1)}) async {
    final last = _syncedAt;
    if (last != null && DateTime.now().difference(last) < gap) return;
    await sync();
  }

  /// Moves each series watched lately on another device or app to the
  /// episode after the one watched there, so Continue offers it. A series
  /// with every aired episode watched leaves Continue.
  Future<void> _moveToUpNext(
    Map<String, ({int season, int episode, int at})> last,
  ) async {
    final cutoff =
        DateTime.now().millisecondsSinceEpoch -
        const Duration(days: 30).inMilliseconds;
    final moved = [
      for (final MapEntry(:key, :value) in last.entries)
        if (value.at > cutoff && _behind(_entries[key], value))
          (key: key, last: value),
    ]..sort((a, b) => b.last.at.compareTo(a.last.at));
    for (final m in moved.take(10)) {
      final e = _entries[m.key]!;
      ({int season, int episode})? next;
      try {
        next = await trakt.nextEpisode(e.title);
      } catch (_) {
        // Without Trakt's answer the watched episode is kept as the spot,
        // and the one after it is offered.
        next = (season: m.last.season, episode: m.last.episode);
      }
      _entries[m.key] = e.copyWith(
        season: next?.season ?? m.last.season,
        episode: next?.episode ?? 0,
        position: 0,
        percent: 0,
        duration: 0,
        updated: m.last.at,
        pushed: m.last.at,
      );
    }
  }

  /// Whether [e] still points before the episode Trakt saw watched last:
  /// saved earlier, or on that very episode, finished, with no spot.
  bool _behind(MovieEntry? e, ({int season, int episode, int at}) last) =>
      e != null &&
      (last.at > e.updated ||
          last.at == e.updated &&
              e.season == last.season &&
              e.episode == last.episode &&
              !e.started);

  /// Trakt's watchlist with the changes made here that it may not show yet:
  /// ones not sent, and ones sent after this pull started. Older sent ones
  /// are settled, and Trakt's list counts, its own removals included.
  List<MediaTitle> _withEdits(List<MediaTitle> pulled, int pullStarted) {
    _watchlistEdits.removeWhere((_, w) => w.sent > 0 && w.sent < pullStarted);
    var list = pulled;
    for (final w in _watchlistEdits.values) {
      list = [if (w.on) w.title, ...list.where((t) => t.key != w.title.key)];
    }
    return list;
  }

  /// Sends watchlist changes Trakt missed. One Trakt refuses outright is
  /// dropped; one that fails because Trakt or the network is away waits.
  Future<void> _sendWatchlistEdits() async {
    final waiting = _watchlistEdits.values.where((w) => w.sent == 0).toList()
      ..sort((a, b) => a.at.compareTo(b.at));
    for (final w in waiting) {
      if (!trakt.signedIn) return;
      try {
        await trakt.setWatchlist(w.title, w.on);
        _watchlistSent(w);
      } on TraktFailure catch (e) {
        if (_retry(e)) break;
        if (_watchlistEdits[w.title.key]?.at == w.at) {
          _watchlistEdits.remove(w.title.key);
        }
      } catch (_) {
        break;
      }
    }
    await _persist();
  }

  void _watchlistSent(WatchlistEdit w) {
    if (_watchlistEdits[w.title.key]?.at != w.at) return;
    _watchlistEdits[w.title.key] = WatchlistEdit(
      title: w.title,
      on: w.on,
      at: w.at,
      sent: DateTime.now().millisecondsSinceEpoch,
    );
  }

  static bool _retry(TraktFailure e) =>
      e.status == 0 ||
      e.status == 401 ||
      e.status == 408 ||
      e.status == 429 ||
      e.status >= 500;

  /// Whether every episode of a show that has ended is watched, or a film
  /// is. A show still airing is never done, as on AniList and MyAnimeList.
  bool _completed(MediaTitle title) {
    final e = _entries[title.key];
    if (e == null) return false;
    if (!title.isShow) return e.watched.contains('movie');
    final status = title.status.toLowerCase();
    if (status != 'ended' && status != 'canceled') return false;
    final seen = e.watched.where((c) => !c.startsWith('S0E')).length;
    return title.airedEpisodes > 0 && seen >= title.airedEpisodes;
  }

  /// The watchlist's own copy of [title], which carries its status and
  /// episode count.
  MediaTitle _listed(MediaTitle title) =>
      watchlist.where((t) => t.key == title.key).firstOrNull ?? title;

  /// Trakt takes a film off the watchlist once it is watched, and a show
  /// once any episode is. A film leaves here too. A show stays listed until
  /// it is completed, as a show being watched stays on an AniList or
  /// MyAnimeList list, so it is put back on Trakt once Trakt has the watch.
  /// Returns the show to put back, or null.
  MediaTitle? _afterWatch(MediaTitle title) {
    if (!onWatchlist(title)) return null;
    final listed = _listed(title);
    if (title.isShow && !_completed(listed)) return listed;
    final edit = _watchlistEdits[title.key];
    if (edit != null && edit.on) _watchlistEdits.remove(title.key);
    watchlist = [...watchlist.where((t) => t.key != title.key)];
    return null;
  }

  /// Puts [show] back on the Trakt watchlist after a watch made Trakt drop
  /// it. Sent again on the next sync when Trakt is away.
  Future<void> _relist(MediaTitle show) async {
    final edit = WatchlistEdit(title: show, on: true, at: _stamp());
    _watchlistEdits[show.key] = edit;
    try {
      await trakt.setWatchlist(show, true);
      _watchlistSent(edit);
    } catch (_) {
      /* Sent on the next sync. */
    }
    await _persist();
  }

  /// Shows that left the Trakt watchlist since the last pull because an
  /// episode was watched (on any device or app), and are not completed,
  /// go back on it. One taken off with no watch since was removed on
  /// purpose and stays off.
  void _keepStartedShows(
    List<MediaTitle> before,
    TraktLibrary lib,
    int pullStarted,
  ) {
    final listedNow = {for (final t in watchlist) t.key};
    for (final t in before) {
      if (!t.isShow || listedNow.contains(t.key)) continue;
      if (_watchlistEdits.containsKey(t.key)) continue;
      final seen = _listedSeen[t.key] ?? 0;
      final watchedAt = lib.lastEpisodes[t.key]?.at ?? 0;
      if (seen == 0 || watchedAt < seen - 60000 || _completed(t)) continue;
      watchlist = [...watchlist, t];
      _watchlistEdits[t.key] = WatchlistEdit(title: t, on: true, at: _stamp());
    }
    _listedSeen
      ..removeWhere((key, _) => !watchlist.any((t) => t.key == key))
      ..addAll({for (final t in watchlist) t.key: pullStarted});
  }

  /// Sends the watched marks Trakt missed, oldest first, at the time they
  /// happened. A mark Trakt refuses outright is dropped; one that fails
  /// because Trakt or the network is away waits for the next sync.
  Future<void> _sendUnsent() async {
    if (_unsent.isEmpty) return;
    for (final u in [..._unsent]) {
      if (!trakt.signedIn) return;
      try {
        await trakt.setWatched(
          u.title,
          u.watched,
          season: u.season,
          episode: u.episode,
          watchedAt: u.at,
        );
      } on TraktFailure catch (e) {
        if (_retry(e)) break;
      } catch (_) {
        break;
      }
      _unsent.remove(u);
    }
    await _persist();
  }

  void _queueWatch(UnsentWatch u) {
    _unsent
      ..removeWhere(u.sameItem)
      ..add(u);
  }

  /// Local spots Trakt is behind on.
  List<String> _pendingSpots() {
    final cutoff =
        DateTime.now().millisecondsSinceEpoch -
        const Duration(days: 30).inMilliseconds;
    return [
      for (final e in _entries.values)
        if (e.updated > e.pushed &&
            e.updated > cutoff &&
            e.position >= 60 &&
            e.duration > 0)
          e.title.key,
    ];
  }

  /// Removes paused spots from Trakt that a finish saved here outdates.
  Future<void> _removeStale(List<int> ids) async {
    for (final id in ids) {
      if (!trakt.signedIn) return;
      try {
        await trakt.removePlayback(id);
      } catch (_) {
        /* Tried again on the next pull. */
      }
    }
  }

  /// Takes Trakt's paused spots where they are newer than the one saved here
  /// and returns the keys of local spots Trakt is behind on, and the ids of
  /// Trakt spots a later finish here made stale.
  ({List<String> pending, List<int> stale}) _mergePlayback(
    List<TraktPaused> paused,
  ) {
    final stale = <int>[];
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
        final done = p.title.isShow
            ? e.watched.contains('S${p.season}E${p.episode}') &&
                  !(e.season == p.season && e.episode == p.episode && e.started)
            : e.finished && !e.started;
        if (done && p.id > 0) stale.add(p.id);
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
    return (pending: _pendingSpots(), stale: stale);
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
      final merged = _mergePlayback(paused);
      await _persist();
      _notify();
      unawaited(_push(merged.pending).then((_) => _removeStale(merged.stale)));
    } catch (_) {
      /* The local spot is used. */
    }
  }

  /// The score this profile gave [title], 0 when none.
  int score(MediaTitle title) => store.integer('TitleScore.${title.key}');

  /// Keeps a score from 1 to 10 for [title] and sends it to Trakt when
  /// signed in, as an anime score goes to AniList and MyAnimeList.
  Future<void> rate(MediaTitle title, int score) async {
    await store.set('TitleScore.${title.key}', score.clamp(1, 10));
    _notify();
    if (trakt.signedIn) await trakt.rate(title, score);
  }

  /// Whether finishing this film, or [episode] of [season] of a show that
  /// has ended, finishes the title, so a score is asked for.
  bool finishes(
    MediaTitle title,
    List<({int season, int number})> episodes, {
    int season = 0,
    int episode = 0,
  }) {
    if (!store.flag('ScoreOnCompletion')) return false;
    if (!title.isShow) return true;
    if (title.status.toLowerCase() != 'ended' || episodes.isEmpty) {
      return false;
    }
    final last = episodes.last;
    return last.season == season && last.number == episode;
  }

  Future<void> setWatchlist(MediaTitle title, bool on) async {
    watchlist = [if (on) title, ...watchlist.where((t) => t.key != title.key)];
    final edit = WatchlistEdit(title: title, on: on, at: _stamp());
    if (trakt.signedIn) _watchlistEdits[title.key] = edit;
    _notify();
    await _persist();
    if (!trakt.signedIn) return;
    try {
      await trakt.setWatchlist(title, on);
      _watchlistSent(edit);
      await _persist();
    } on TraktFailure catch (e) {
      if (!_retry(e) && _watchlistEdits[title.key]?.at == edit.at) {
        _watchlistEdits.remove(title.key);
        await _persist();
      }
      lastError = 'Trakt was not updated. $e';
      _notify();
    } catch (e) {
      lastError = 'Trakt was not updated. It is sent again on the next sync.';
      _notify();
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
    final at = _entries[title.key]!.updated;
    final relist = on && trakt.signedIn ? _afterWatch(title) : null;
    _notify();
    await _persist();
    if (trakt.signedIn) {
      final mark = UnsentWatch(
        title: title,
        watched: on,
        at: at,
        season: season,
        episode: episode,
      );
      // A newer mark replaces an older one still waiting.
      _unsent.removeWhere(mark.sameItem);
      try {
        await trakt.setWatched(
          title,
          on,
          season: season,
          episode: episode,
          watchedAt: at,
        );
        if (relist != null) await _relist(relist);
      } catch (e) {
        if (relist != null) {
          _watchlistEdits[relist.key] = WatchlistEdit(
            title: relist,
            on: true,
            at: _stamp(),
          );
        }
        _queueWatch(mark);
        await _persist();
        lastError = 'Trakt was not updated. It is sent again on the next sync.';
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
    if (completed && trakt.signedIn) {
      final relist = _afterWatch(title);
      // The scrobble that tells Trakt of the watch puts it back; until
      // then a pull keeps it listed here.
      if (relist != null) {
        _watchlistEdits[relist.key] = WatchlistEdit(
          title: relist,
          on: true,
          at: _stamp(),
        );
      }
    }
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
      if (action == 'stop') await _sendWatchlistEdits();
    } on TraktFailure catch (e) {
      // 409: Trakt already has this finish.
      if (e.status != 409) {
        _queueFinish(action, title, percent, season, episode);
      } else if (action == 'stop') {
        await _sendWatchlistEdits();
      }
    } catch (_) {
      // Playback goes on; the progress is saved locally, and a finish is
      // sent again on the next sync.
      _queueFinish(action, title, percent, season, episode);
    }
  }

  void _queueFinish(
    String action,
    MediaTitle title,
    double percent,
    int season,
    int episode,
  ) {
    if (action != 'stop' || percent < 80) return;
    _queueWatch(
      UnsentWatch(
        title: title,
        watched: true,
        at: DateTime.now().millisecondsSinceEpoch,
        season: season,
        episode: episode,
      ),
    );
    unawaited(_persist());
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

  /// Checks one source for Diagnostics: finds a well-known film on it and
  /// asks for a stream, as playback would. Records and returns the result.
  Future<String> check(MovieSource source, RequestScope scope) async {
    final key = 'movie:${source.name}';
    store.health(key, 'Checking');
    String result;
    try {
      final found = await core.request(
        {
          'operation': 'movie_match',
          'provider': source.name,
          // A film every source carries, with its Lithuanian title for the
          // sources that search in Lithuanian.
          'titles': ['Pradžia', 'Inception'],
          'year': 2010,
          'imdb': 'tt1375666',
          'series': false,
          'season': 0,
        },
        scope: scope,
        timeout: const Duration(seconds: 30),
      );
      await core.request(
        {
          'operation': 'movie_resolve',
          'movie': (found['movie'] as Map).cast<String, dynamic>(),
          'season': 0,
          'episode': 0,
          'subtitles': 'english',
          'skip': <String>[],
        },
        scope: scope,
        timeout: const Duration(seconds: 60),
      );
      result = 'Working';
    } on CoreFailure catch (e) {
      if (e.code == 'cancelled') rethrow;
      result = e.code == 'not_found' ? 'No match' : 'Unavailable';
    } catch (_) {
      scope.check();
      result = 'Unavailable';
    }
    store.health(key, result);
    return result;
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
    var unreachable = 0;
    final reasons = <String>[];
    void failed(MovieSource source, String why) {
      // Sources are never named to the viewer; their language is.
      final line = '${languageTitle(source.languageName)}: $why';
      if (!reasons.contains(line)) reasons.add(line);
    }

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
        if (e.code != 'not_found') unreachable++;
        failed(source, failureReason(e));
        store.health(
          'movie:${source.name}',
          e.code == 'not_found' ? 'No match' : 'Unavailable',
        );
      } catch (_) {
        scope.check();
        unreachable++;
        failed(source, 'failed');
        store.health('movie:${source.name}', 'Unavailable');
      }
    }
    throw CoreFailure(
      'no_stream',
      noCopyMessage(order.length, unreachable, reasons: reasons),
    );
  }
}
