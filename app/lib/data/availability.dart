import 'dart:async';

import 'package:flutter/foundation.dart';

import '../core.dart';
import 'models.dart';
import 'movies.dart';
import 'store.dart';
import 'trakt.dart';

/// What the app knows about whether a title can be played.
enum Playable { unknown, yes, no }

/// Learns which titles some source can play, without asking the sources
/// title by title while the viewer browses:
///
/// * upcoming titles are known at once, with no request;
/// * a few pages of a source's own catalog, fetched once a day, and the
///   same source's results for each search mark many titles at a time;
/// * a title is asked about only when the viewer opens it, or rests on it
///   with the remote;
/// * every play and every search for a copy is remembered.
///
/// Answers are kept on the device. Nothing is ever taken for unplayable
/// because a source could not be reached.
class Availability extends ChangeNotifier {
  Availability({
    required this.store,
    required this.resolver,
    required this.movies,
    this.titleOf,
    DateTime Function()? now,
  }) : _now = now ?? DateTime.now {
    _load();
    resolver.learned = (anime, ok) => remember(anime.key, ok);
    movies.learned = (title, ok) => remember(cardKey(title), ok);
  }

  /// The profile whose cards are on screen. Poster cards read it, as they
  /// are drawn on pages that have no other way to reach the services.
  static Availability? active;

  final AppStore store;
  final SourceResolver resolver;
  final MovieService movies;

  /// The film or series behind a card drawn for it, if any.
  MediaTitle? Function(Anime card)? titleOf;
  final DateTime Function() _now;

  /// How long an answer is kept: a title that plays seldom stops, one that
  /// did not is asked again sooner, since sources add shows every day.
  static const keepPlayable = Duration(days: 30);
  static const keepUnplayable = Duration(days: 3);
  static const keepNames = Duration(days: 30);
  static const catalogEvery = Duration(hours: 20);

  /// The source whose catalog is read, and the pages read from it.
  static const catalogSource = 'anikoto';
  static const catalogPages = [
    ('most-viewed', 1),
    ('most-viewed', 2),
    ('most-viewed', 3),
    ('latest-updated', 1),
    ('latest-updated', 2),
    ('latest-added', 1),
  ];

  static const _answersKey = 'Availability';
  static const _namesKey = 'AvailabilityNames';
  static const _catalogKey = 'AvailabilityCatalog';

  /// Card key → (playable, when learned, seconds since epoch).
  final Map<String, (bool, int)> _answers = {};

  /// Normalized names seen on a source's catalog → day number last seen.
  final Map<String, int> _names = {};
  final Map<String, Future<void>> _asking = {};
  bool _queued = false, _disposed = false;
  bool _catalogRunning = false;

  /// Hide titles known to be unplayable from browse lists, instead of
  /// greying them.
  bool get hide => store.flag('HideUnplayable');

  int get _seconds => _now().millisecondsSinceEpoch ~/ 1000;
  int get _day => _seconds ~/ 86400;

  void _load() {
    final answers = store.cache(_answersKey);
    if (answers is Map) {
      for (final e in answers.entries) {
        final v = e.value;
        if (v is List && v.length == 2 && v[1] is int) {
          _answers[e.key as String] = (v[0] == true, v[1] as int);
        }
      }
    }
    final names = store.cache(_namesKey);
    if (names is Map) {
      final oldest = _day - keepNames.inDays;
      for (final e in names.entries) {
        if (e.value is int && e.value as int >= oldest) {
          _names[e.key as String] = e.value as int;
        }
      }
    }
  }

  /// A kept answer for [key] that is not too old, or null.
  bool? _answer(String key) {
    final answer = _answers[key];
    if (answer == null) return null;
    final age = _seconds - answer.$2;
    return age <= (answer.$1 ? keepPlayable : keepUnplayable).inSeconds
        ? answer.$1
        : null;
  }

  /// Saves and redraws once for a burst of answers, not for each.
  void _changed() {
    if (_queued) return;
    _queued = true;
    scheduleMicrotask(() {
      _queued = false;
      if (_disposed) return;
      _write();
      notifyListeners();
    });
  }

  void _write() {
    final now = _seconds;
    _answers.removeWhere(
      (_, v) => now - v.$2 > (v.$1 ? keepPlayable : keepUnplayable).inSeconds,
    );
    unawaited(
      store.setCache(_answersKey, {
        for (final e in _answers.entries) e.key: [e.value.$1, e.value.$2],
      }),
    );
    unawaited(store.setCache(_namesKey, Map<String, int>.of(_names)));
  }

  /// The key an answer is kept under: the card's own, so a film card and
  /// its title share one.
  static String cardKey(MediaTitle t) =>
      'al:${-(t.trakt * 2 + (t.isShow ? 1 : 0)) - 10}';

  /// Letters and digits only, lower case, as the core compares names.
  static String normalize(String name) => name.toLowerCase().replaceAll(
    RegExp(r'[^\p{L}\p{N}]', unicode: true),
    '',
  );

  /// What is known about a card, with no request. Upcoming titles count as
  /// unplayable unless [upcomingOk], for a list the viewer asked to show
  /// upcoming titles.
  Playable of(Anime anime, {bool upcomingOk = false}) {
    if (anime.status == 'NOT_YET_RELEASED') {
      return upcomingOk ? Playable.unknown : Playable.no;
    }
    if (store.entry(anime) != null) return Playable.yes;
    final answer = _answer(anime.key);
    if (answer != null) return answer ? Playable.yes : Playable.no;
    if (anime.id > 0 || anime.malId > 0) {
      for (final name in {anime.title, ...anime.titles}) {
        final n = normalize(name);
        if (n.isNotEmpty && _names.containsKey(n)) return Playable.yes;
      }
    }
    return Playable.unknown;
  }

  /// [items] without the titles known to be unplayable, when the viewer
  /// chose to hide them. Titles still unknown always stay.
  List<Anime> visible(List<Anime> items, {bool upcomingOk = false}) => hide
      ? items
            .where((a) => of(a, upcomingOk: upcomingOk) != Playable.no)
            .toList()
      : items;

  /// Films and series [visible] keeps.
  List<MediaTitle> visibleTitles(
    List<MediaTitle> items, {
    bool upcomingOk = false,
  }) => hide
      ? items
            .where(
              (t) =>
                  (upcomingOk || !upcomingTitle(t)) &&
                  _answer(cardKey(t)) != false,
            )
            .toList()
      : items;

  /// Records what playback or a check found out.
  void remember(String key, bool playable) {
    final old = _answers[key];
    _answers[key] = (playable, _seconds);
    if (old?.$1 != playable) _changed();
  }

  void _learnNames(Iterable<String> names) {
    final day = _day;
    var added = false;
    for (final name in names) {
      final n = normalize(name);
      if (n.isEmpty) continue;
      if (_names[n] != day) added = true;
      _names[n] = day;
    }
    if (added) _changed();
  }

  /// Asks the sources about one anime, unless it is known already. For a
  /// title the viewer opened or rests on.
  Future<void> check(Anime anime) {
    if (anime.id < 0) {
      final title = titleOf?.call(anime);
      return title == null ? Future.value() : checkTitle(title);
    }
    if (of(anime) != Playable.unknown) return Future.value();
    return _asking[anime.key] ??= () async {
      try {
        final ok = await resolver.available(anime, RequestScope());
        if (ok != null) remember(anime.key, ok);
      } catch (_) {
        // Unknown stays unknown.
      } finally {
        _asking.remove(anime.key);
      }
    }();
  }

  /// [check] for a film or series.
  Future<void> checkTitle(MediaTitle title) {
    final key = cardKey(title);
    if (upcomingTitle(title) || _answer(key) != null) return Future.value();
    return _asking[key] ??= () async {
      try {
        final ok = await movies.available(title, RequestScope());
        if (ok != null) remember(key, ok);
      } catch (_) {
        // Unknown stays unknown.
      } finally {
        _asking.remove(key);
      }
    }();
  }

  /// Reads the catalog pages, at most once in [catalogEvery], one page at a
  /// time. Pages that fail are skipped.
  Future<void> refreshCatalog() async {
    if (_catalogRunning) return;
    final last = store.cache(_catalogKey);
    if (last is int && _seconds - last < catalogEvery.inSeconds) return;
    _catalogRunning = true;
    try {
      if (!(await resolver.providerOrder()).contains(catalogSource)) return;
      var read = 0;
      for (final (sort, page) in catalogPages) {
        try {
          _learnNames(
            await resolver.catalog(catalogSource, sort: sort, page: page),
          );
          read++;
        } catch (_) {
          // Another page, or tomorrow.
        }
      }
      if (read > 0) await store.setCache(_catalogKey, _seconds);
    } catch (_) {
      // The source order could not be read; try again later.
    } finally {
      _catalogRunning = false;
    }
  }

  /// Sends a search to the catalog source too: one request marks every
  /// show it finds.
  Future<void> searched(String query) async {
    final q = query.trim();
    if (q.isEmpty || q.length > 300) return;
    try {
      if (!(await resolver.providerOrder()).contains(catalogSource)) return;
      _learnNames(await resolver.catalog(catalogSource, query: q));
    } catch (_) {
      // The search itself is unaffected.
    }
  }

  @override
  void dispose() {
    if (_queued) _write();
    _disposed = true;
    if (identical(active, this)) active = null;
    super.dispose();
  }
}

/// Trakt statuses of a film or series that has not come out yet.
const upcomingTraktStatuses = {
  'planned',
  'rumored',
  'in production',
  'post production',
  'upcoming',
};

bool upcomingTitle(MediaTitle t) =>
    upcomingTraktStatuses.contains(t.status.toLowerCase());
