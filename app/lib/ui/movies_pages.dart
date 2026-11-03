import 'dart:async';

import 'package:flutter/material.dart';
import 'package:qr_flutter/qr_flutter.dart';

import '../core.dart';
import '../data/movies.dart';
import '../data/models.dart';
import '../data/store.dart';
import '../data/trakt.dart';
import '../desktop/desktop_core.dart';
import '../services.dart';
import 'components.dart';
import 'downloads_page.dart';
import 'movie_player.dart';

/// Poster rows and cards are drawn for anime; a film or series is shown in
/// them under an id no AniList entry can have, and mapped back on open.
class TitleCards {
  final _byId = <int, MediaTitle>{};
  Anime card(MediaTitle t) {
    final id = -(t.trakt * 2 + (t.isShow ? 1 : 0)) - 10;
    _byId[id] = t;
    return Anime(
      id: id,
      malId: id,
      title: t.title,
      cover: t.poster,
      coverLarge: t.poster,
      banner: t.fanart,
      description: t.overview,
      format: t.isShow ? 'TV' : 'MOVIE',
      year: t.year,
      duration: t.runtime,
      genres: t.genres.map(languageTitle).toList(),
    );
  }

  MediaTitle? title(Anime a) => _byId[a.id];
}

Future<void> openTitle(
  BuildContext context,
  AppServices services,
  MediaTitle title,
  bool isTV,
) => Navigator.push(
  context,
  OtakaseRoute<void>(
    builder: (_) => TitlePage(services: services, title: title, isTV: isTV),
  ),
);

/// The shelves of the Movies & shows home: the viewer's own rows first,
/// then what is trending and popular on Trakt.
class MovieShelves {
  MovieShelves(this.trakt);
  final Trakt trakt;
  Future<Map<String, List<MediaTitle>>>? _loading;
  DateTime _loaded = DateTime(0);
  static const rows = {
    'Trending movies': ('movie', 'trending'),
    'Trending shows': ('show', 'trending'),
    'Popular movies': ('movie', 'popular'),
    'Popular shows': ('show', 'popular'),
    'Most watched this week': ('movie', 'watched'),
  };

  Future<Map<String, List<MediaTitle>>> load({bool refresh = false}) {
    if (refresh ||
        DateTime.now().difference(_loaded) > const Duration(hours: 3)) {
      _loading = null;
    }
    return _loading ??= () async {
      final results = await Future.wait([
        for (final row in rows.values)
          trakt.list(row.$1, row.$2).catchError((_) => <MediaTitle>[]),
      ]);
      _loaded = DateTime.now();
      final out = Map.fromIterables(rows.keys, results);
      if (out.values.every((r) => r.isEmpty)) {
        _loading = null;
        throw const TraktFailure('Trakt could not be reached.');
      }
      return out;
    }();
  }
}

final _shelves = Expando<MovieShelves>();
MovieShelves shelvesOf(AppServices services) =>
    _shelves[services] ??= MovieShelves(services.trakt);

class MoviesHome extends StatefulWidget {
  const MoviesHome({
    super.key,
    required this.services,
    required this.isTV,
    this.searchButton = true,
  });
  final AppServices services;
  final bool isTV;

  /// Off when the Search tab already searches films and series.
  final bool searchButton;
  @override
  State<MoviesHome> createState() => _MoviesHomeState();
}

class _MoviesHomeState extends State<MoviesHome> {
  final cards = TitleCards();
  late Future<Map<String, List<MediaTitle>>> shelves;
  MovieService get movies => widget.services.movies;

  @override
  void initState() {
    super.initState();
    shelves = shelvesOf(widget.services).load();
    unawaited(widget.services.trakt.load().then((_) => movies.sync()));
  }

  void open(Anime a, Object _) {
    final t = cards.title(a);
    if (t != null) {
      unawaited(openTitle(context, widget.services, t, widget.isTV));
    }
  }

  Widget row(
    String title,
    List<MediaTitle> items, {
    double? Function(Anime)? progress,
    String? Function(Anime)? captions,
  }) => items.isEmpty
      ? const SizedBox.shrink()
      : Padding(
          padding: const EdgeInsets.only(bottom: 8),
          child: PosterRow(
            title: title,
            items: [for (final t in items) cards.card(t)],
            isTV: widget.isTV,
            onOpen: (a) => open(a, ''),
            progress: progress,
            captions: captions,
          ),
        );

  @override
  Widget build(BuildContext context) => PosterNavigation(
    onOpen: open,
    child: ListenableBuilder(
      listenable: Listenable.merge([movies, widget.services.trakt]),
      builder: (context, _) {
        final continuing = movies.continuing;
        final byCard = {for (final e in continuing) e.title.key: e};
        final pad = widget.isTV ? 48.0 : 20.0;
        return RefreshIndicator(
          onRefresh: () async {
            setState(
              () => shelves = shelvesOf(widget.services).load(refresh: true),
            );
            await movies.sync();
          },
          child: ListView(
            key: const PageStorageKey('movies-home'),
            padding: EdgeInsets.only(bottom: contentBottom(context)),
            children: [
              Padding(
                padding: EdgeInsets.fromLTRB(pad, 12, pad, 8),
                child: Row(
                  children: [
                    const Expanded(child: PageHeading('Movies & shows', '映')),
                    if (widget.searchButton)
                      IconButton(
                        key: const Key('movies-search'),
                        tooltip: 'Search movies and shows',
                        onPressed: () => Navigator.push(
                          context,
                          OtakaseRoute<void>(
                            builder: (_) => Scaffold(
                              appBar: AppBar(leading: const BackButton()),
                              body: MovieSearchPage(
                                services: widget.services,
                                isTV: widget.isTV,
                                autofocus: true,
                              ),
                            ),
                          ),
                        ),
                        icon: const Icon(LucideIcons.search),
                      ),
                    IconButton(
                      key: const Key('movies-downloads'),
                      tooltip: 'Downloads',
                      onPressed: () =>
                          openDownloads(context, widget.services, widget.isTV),
                      icon: const Icon(LucideIcons.download),
                    ),
                  ],
                ),
              ),
              if (!widget.services.trakt.signedIn)
                Padding(
                  padding: EdgeInsets.fromLTRB(pad, 0, pad, 16),
                  child: FocusBox(
                    key: const Key('movies-trakt'),
                    isTV: widget.isTV,
                    radius: 14,
                    onPressed: () =>
                        traktSignIn(context, widget.services, widget.isTV),
                    child: Container(
                      padding: const EdgeInsets.all(14),
                      decoration: BoxDecoration(
                        color: panel,
                        border: Border.all(color: line),
                        borderRadius: BorderRadius.circular(14),
                      ),
                      child: Row(
                        children: [
                          const Icon(LucideIcons.listVideo, color: accentText),
                          const SizedBox(width: 14),
                          Expanded(
                            child: Text(
                              'Sign in to Trakt to keep your watchlist and what you watched in sync.',
                              style: uiStyle(context, size: 15, color: muted),
                            ),
                          ),
                          Text(
                            'Sign in',
                            style: uiStyle(
                              context,
                              size: 15,
                              color: accentText,
                            ),
                          ),
                        ],
                      ),
                    ),
                  ),
                ),
              row(
                'Continue watching',
                [for (final e in continuing) e.title],
                progress: (a) {
                  final t = cards.title(a);
                  final e = t == null ? null : byCard[t.key];
                  return e == null || e.progress <= 0 ? null : e.progress;
                },
                captions: (a) {
                  final t = cards.title(a);
                  final e = t == null ? null : byCard[t.key];
                  return e != null && t!.isShow && e.episode > 0
                      ? e.code
                      : null;
                },
              ),
              row('Your watchlist', movies.watchlist),
              FutureBuilder(
                future: shelves,
                builder: (context, snap) {
                  if (snap.hasError) {
                    return FailureView(
                      message: 'Movies and shows could not be loaded.',
                      isTV: widget.isTV,
                      onRetry: () => setState(
                        () =>
                            shelves = shelvesOf(widget.services)
                                .load(refresh: true),
                      ),
                    );
                  }
                  if (!snap.hasData) {
                    return const Padding(
                      padding: EdgeInsets.all(40),
                      child: Center(child: CircularProgressIndicator()),
                    );
                  }
                  return Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      for (final e in snap.data!.entries) row(e.key, e.value),
                    ],
                  );
                },
              ),
            ],
          ),
        );
      },
    ),
  );
}

class MovieSearchPage extends StatefulWidget {
  const MovieSearchPage({
    super.key,
    required this.services,
    required this.isTV,
    this.autofocus = false,
    this.fieldFocus,
  });
  final AppServices services;
  final bool isTV, autofocus;
  final FocusNode? fieldFocus;
  @override
  State<MovieSearchPage> createState() => _MovieSearchPageState();
}

class _MovieSearchPageState extends State<MovieSearchPage> {
  final controller = TextEditingController();
  final cards = TitleCards();
  Timer? debounce;
  String query = '', error = '';
  List<MediaTitle> results = [];
  bool loading = false;
  int generation = 0;

  @override
  void dispose() {
    debounce?.cancel();
    controller.dispose();
    super.dispose();
  }

  void changed(String value) {
    debounce?.cancel();
    debounce = Timer(const Duration(milliseconds: 450), () => search(value));
  }

  Future<void> search(String value) async {
    final q = value.trim();
    if (q == query) return;
    query = q;
    final g = ++generation;
    if (q.isEmpty) {
      setState(() => results = []);
      return;
    }
    setState(() {
      loading = true;
      error = '';
    });
    try {
      final found = await widget.services.trakt.search(q);
      if (mounted && g == generation) setState(() => results = found);
    } catch (_) {
      if (mounted && g == generation) {
        setState(() => error = 'Search failed. Try again.');
      }
    } finally {
      if (mounted && g == generation) setState(() => loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final pad = widget.isTV ? 48.0 : 20.0;
    final width = MediaQuery.sizeOf(context).width;
    final cardWidth = fittedCardWidth(
      width,
      widget.isTV ? 180 : 108,
      padding: pad,
      gap: 12,
    );
    return PosterNavigation(
      onOpen: (a, _) {
        final t = cards.title(a);
        if (t != null) {
          unawaited(openTitle(context, widget.services, t, widget.isTV));
        }
      },
      child: ListView(
        padding: EdgeInsets.fromLTRB(pad, 12, pad, contentBottom(context)),
        children: [
          RemoteTextInput(
            isTV: widget.isTV,
            builder: (focus) => TextField(
              key: const Key('movie-search-field'),
              focusNode: widget.fieldFocus ?? focus,
              autofocus: widget.autofocus,
              controller: controller,
              textInputAction: TextInputAction.search,
              decoration: const InputDecoration(
                hintText: 'Search movies and shows',
                prefixIcon: Icon(LucideIcons.search),
              ),
              onChanged: changed,
              onSubmitted: search,
            ),
          ),
          const SizedBox(height: 20),
          if (loading) const LinearProgressIndicator(color: shu),
          if (error.isNotEmpty) EmptyState(message: error, error: true),
          if (!loading && error.isEmpty && query.isNotEmpty && results.isEmpty)
            const EmptyState(message: 'Nothing found.'),
          Wrap(
            spacing: 12,
            runSpacing: 16,
            children: [
              for (final t in results)
                PosterCard(
                  key: ValueKey('movie-${t.key}'),
                  anime: cards.card(t),
                  width: cardWidth,
                  isTV: widget.isTV,
                  caption: t.isShow
                      ? 'Series'
                      : (t.year > 0 ? '${t.year}' : 'Movie'),
                  onOpen: () => unawaited(
                    openTitle(context, widget.services, t, widget.isTV),
                  ),
                ),
            ],
          ),
        ],
      ),
    );
  }
}

/// Plays a film, or one episode of a series with the rest following.
PlayRequest titlePlay(
  AppServices services,
  MediaTitle title, {
  TitleEpisode? episode,
  List<TitleEpisode> episodes = const [],
}) {
  final movies = services.movies;
  final season = episode?.season ?? 0, number = episode?.number ?? 0;
  return PlayRequest(
    title: title.title,
    subtitle: episode == null
        ? (title.year > 0 ? '${title.year}' : '')
        : episode.label,
    open: (failed, scope, status) async {
      // Another device may have paused this further along; look while the
      // stream is found so the spot is fresh when playback starts.
      final pulled = failed.isEmpty ? movies.refreshResume() : null;
      final p = await movies.resolve(
        title,
        season: season,
        episode: number,
        scope: scope,
        status: status,
        skip: failed,
      );
      await pulled;
      final headers = Map<String, String>.of(p.stream.headers);
      if (p.stream.referrer.isNotEmpty) headers['Referer'] = p.stream.referrer;
      return PlayableMedia(
        url: p.stream.url,
        headers: headers,
        subtitles: p.stream.subtitles,
        audioLanguage: p.audioLanguage,
        copy: '${p.source.name}:${p.server}',
        label: '${p.source.label} · ${languageTitle(p.source.languageName)}',
      );
    },
    resumeAt: (duration) {
      final e = movies.entry(title);
      if (e == null) return 0;
      if (title.isShow && (e.season != season || e.episode != number)) return 0;
      return e.resumeAt(duration);
    },
    onProgress: (position, duration, completed) => movies.progress(
      title,
      position,
      duration,
      season: season,
      episode: number,
      completed: completed,
    ),
    onScrobble: (action, percent) => unawaited(
      movies.scrobble(action, title, percent, season: season, episode: number),
    ),
    next: episode == null
        ? null
        : () {
            final i = episodes.indexWhere(
              (e) => e.season == season && e.number == number,
            );
            if (i < 0 || i + 1 >= episodes.length) return null;
            return titlePlay(
              services,
              title,
              episode: episodes[i + 1],
              episodes: episodes,
            );
          },
  );
}

class TitlePage extends StatefulWidget {
  const TitlePage({
    super.key,
    required this.services,
    required this.title,
    required this.isTV,
  });
  final AppServices services;
  final MediaTitle title;
  final bool isTV;
  @override
  State<TitlePage> createState() => _TitlePageState();
}

class _TitlePageState extends State<TitlePage> {
  late MediaTitle title = widget.title;
  List<TitleEpisode> episodes = [];
  bool loadingEpisodes = false;
  String episodeError = '';
  int season = 1;
  MovieService get movies => widget.services.movies;

  @override
  void initState() {
    super.initState();
    unawaited(load());
  }

  Future<void> load() async {
    if (title.overview.isEmpty || title.fanart.isEmpty) {
      try {
        final full = await widget.services.trakt.details(title);
        if (mounted) setState(() => title = full);
      } catch (_) {
        /* The list entry has enough to play from. */
      }
    }
    if (!title.isShow) return;
    setState(() => loadingEpisodes = true);
    try {
      final list = await widget.services.trakt.episodes(title);
      if (!mounted) return;
      final e = movies.entry(title);
      setState(() {
        episodes = list;
        season = e != null && e.season > 0
            ? e.season
            : (list.isEmpty ? 1 : list.first.season);
      });
    } catch (_) {
      if (mounted) {
        setState(() => episodeError = 'Episodes could not be loaded.');
      }
    } finally {
      if (mounted) setState(() => loadingEpisodes = false);
    }
  }

  Future<void> play([TitleEpisode? episode]) => Navigator.push(
    context,
    OtakaseRoute<void>(
      builder: (_) => MoviePlayerPage(
        services: widget.services,
        isTV: widget.isTV,
        request: titlePlay(
          widget.services,
          title,
          episode: episode,
          episodes: episodes,
        ),
      ),
    ),
  );

  /// The episode to carry on with: the one stopped in, else the one after
  /// the last watched.
  TitleEpisode? get upNext {
    if (episodes.isEmpty) return null;
    final e = movies.entry(title);
    if (e != null && e.episode > 0) {
      final i = episodes.indexWhere(
        (x) => x.season == e.season && x.number == e.episode,
      );
      if (i >= 0) {
        final done = movies.watched(
          title,
          season: e.season,
          episode: e.episode,
        );
        if (!done) return episodes[i];
        if (i + 1 < episodes.length) return episodes[i + 1];
      }
    }
    return episodes.firstWhere(
      (x) => !movies.watched(title, season: x.season, episode: x.number),
      orElse: () => episodes.first,
    );
  }

  Future<void> download([TitleEpisode? episode]) =>
      downloadTitle(context, widget.services, title, episode: episode);

  Widget meta(BuildContext context) => Wrap(
    spacing: 10,
    runSpacing: 6,
    children: [
      for (final text in [
        if (title.year > 0) '${title.year}',
        if (title.isShow)
          'Series'
        else if (title.runtime > 0)
          '${title.runtime} min',
        if (title.certification.isNotEmpty) title.certification,
        if (title.rating > 0) '★ ${title.rating.toStringAsFixed(1)}',
        if (title.network.isNotEmpty) title.network,
        ...title.genres.take(3).map(languageTitle),
      ])
        Container(
          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
          decoration: BoxDecoration(
            color: fgBright.withValues(alpha: .08),
            borderRadius: BorderRadius.circular(999),
          ),
          child: Text(text, style: uiStyle(context, size: 15, color: fg)),
        ),
    ],
  );

  Widget actions(BuildContext context) {
    final entry = movies.entry(title);
    final next = upNext;
    final watched = movies.watched(title);
    final resume = title.isShow
        ? (next != null &&
              entry != null &&
              entry.season == next.season &&
              entry.episode == next.number &&
              entry.started)
        : entry?.started ?? false;
    return Wrap(
      spacing: 12,
      runSpacing: 8,
      children: [
        AppButton(
          key: const Key('title-play'),
          label: title.isShow
              ? next == null
                    ? (loadingEpisodes ? 'Loading episodes' : 'No episodes yet')
                    : '${resume ? 'Resume' : 'Play'} ${next.code}'
              : resume
              ? 'Resume'
              : 'Play',
          icon: LucideIcons.play,
          filled: true,
          autofocus: true,
          isTV: widget.isTV,
          onPressed: title.isShow && next == null ? null : () => play(next),
        ),
        AppButton(
          key: const Key('title-watchlist'),
          label: movies.onWatchlist(title) ? 'On watchlist' : 'Watchlist',
          icon: movies.onWatchlist(title)
              ? LucideIcons.bookmarkCheck
              : LucideIcons.bookmarkPlus,
          isTV: widget.isTV,
          onPressed: () =>
              movies.setWatchlist(title, !movies.onWatchlist(title)),
        ),
        if (!title.isShow) ...[
          AppButton(
            key: const Key('title-watched'),
            label: watched ? 'Watched' : 'Mark watched',
            icon: watched ? LucideIcons.circleCheck : LucideIcons.circle,
            isTV: widget.isTV,
            onPressed: () => movies.markWatched(title, !watched),
          ),
          if (!widget.isTV)
            AppButton(
              key: const Key('title-download'),
              label: 'Download',
              icon: LucideIcons.download,
              isTV: widget.isTV,
              onPressed: () => download(),
            ),
        ],
      ],
    );
  }

  Widget episodeList(BuildContext context) {
    final seasons = episodes.map((e) => e.season).toSet().toList()..sort();
    final shown = episodes.where((e) => e.season == season).toList();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (seasons.length > 1)
          SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            child: Row(
              children: [
                for (final s in seasons) ...[
                  AppTab(
                    key: ValueKey('season-$s'),
                    label: 'Season $s',
                    selected: s == season,
                    isTV: widget.isTV,
                    onPressed: () => setState(() => season = s),
                  ),
                  const SizedBox(width: 8),
                ],
              ],
            ),
          ),
        const SizedBox(height: 12),
        for (final e in shown)
          Padding(
            padding: const EdgeInsets.only(bottom: 4),
            child: Row(
              children: [
                Expanded(
                  child: FocusBox(
                    key: ValueKey('episode-${e.code}'),
                    isTV: widget.isTV,
                    radius: 10,
                    padding: const EdgeInsets.symmetric(
                      horizontal: 12,
                      vertical: 10,
                    ),
                    onPressed: () => play(e),
                    child: Row(
                      children: [
                        SizedBox(
                          width: 44,
                          child: Text(
                            '${e.number}',
                            style: mono.copyWith(color: accentText),
                          ),
                        ),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                e.title.isEmpty
                                    ? 'Episode ${e.number}'
                                    : e.title,
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: uiStyle(
                                  context,
                                  weight: FontWeight.w500,
                                ),
                              ),
                              if (e.aired != null)
                                Text(
                                  '${e.aired!.year}-${e.aired!.month.toString().padLeft(2, '0')}-${e.aired!.day.toString().padLeft(2, '0')}',
                                  style: uiStyle(
                                    context,
                                    size: 15,
                                    color: muted,
                                  ),
                                ),
                            ],
                          ),
                        ),
                        if (movies.watched(
                          title,
                          season: e.season,
                          episode: e.number,
                        ))
                          const Icon(
                            LucideIcons.circleCheck,
                            color: ok,
                            size: 20,
                          ),
                      ],
                    ),
                  ),
                ),
                IconButton(
                  tooltip:
                      movies.watched(title, season: e.season, episode: e.number)
                      ? 'Mark unwatched'
                      : 'Mark watched',
                  onPressed: () => movies.markWatched(
                    title,
                    !movies.watched(title, season: e.season, episode: e.number),
                    season: e.season,
                    episode: e.number,
                  ),
                  icon: const Icon(LucideIcons.check, size: 20, color: muted),
                ),
                if (!widget.isTV)
                  IconButton(
                    tooltip: 'Download ${e.code}',
                    onPressed: () => download(e),
                    icon: const Icon(
                      LucideIcons.download,
                      size: 20,
                      color: muted,
                    ),
                  ),
              ],
            ),
          ),
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final pad = widget.isTV ? 48.0 : (isDesktop ? 40.0 : 20.0);
    final size = MediaQuery.sizeOf(context);
    return Scaffold(
      body: ListenableBuilder(
        listenable: movies,
        builder: (context, _) => ListView(
          padding: EdgeInsets.only(bottom: contentBottom(context)),
          children: [
            SizedBox(
              height: (size.height * (widget.isTV || isDesktop ? .55 : .42))
                  .clamp(260, 640),
              child: Stack(
                fit: StackFit.expand,
                children: [
                  if (title.fanart.isNotEmpty || title.poster.isNotEmpty)
                    CoverImage(
                      title.fanart.isNotEmpty ? title.fanart : title.poster,
                      width: size.width,
                      height: size.height * .55,
                      banner: true,
                    ),
                  const DecoratedBox(
                    decoration: BoxDecoration(
                      gradient: LinearGradient(
                        begin: Alignment.topCenter,
                        end: Alignment.bottomCenter,
                        colors: [Color(0x990e0d0c), Color(0x330e0d0c), bg],
                        stops: [0, .45, 1],
                      ),
                    ),
                  ),
                  SafeArea(
                    child: Align(
                      alignment: Alignment.topLeft,
                      child: Padding(
                        padding: EdgeInsets.all(widget.isTV ? 24 : 8),
                        child: IconButton(
                          key: const Key('title-back'),
                          tooltip: 'Back',
                          onPressed: () => Navigator.pop(context),
                          icon: const Icon(
                            LucideIcons.arrowLeft,
                            color: fgBright,
                          ),
                        ),
                      ),
                    ),
                  ),
                  Positioned(
                    left: pad,
                    right: pad,
                    bottom: 12,
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          title.isShow ? 'SERIES' : 'MOVIE',
                          style: mono.copyWith(
                            letterSpacing: 3,
                            color: accentText,
                          ),
                        ),
                        const SizedBox(height: 6),
                        Text(
                          title.title,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          style: display.copyWith(
                            fontSize: widget.isTV || isDesktop ? 48 : 32,
                          ),
                        ),
                        const SizedBox(height: 10),
                        meta(context),
                      ],
                    ),
                  ),
                ],
              ),
            ),
            Padding(
              padding: EdgeInsets.fromLTRB(pad, 12, pad, 0),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  actions(context),
                  if (title.tagline.isNotEmpty) ...[
                    const SizedBox(height: 20),
                    Text(
                      title.tagline,
                      style: uiStyle(
                        context,
                        weight: FontWeight.w600,
                        color: fgBright,
                      ),
                    ),
                  ],
                  if (title.overview.isNotEmpty) ...[
                    const SizedBox(height: 12),
                    ConstrainedBox(
                      constraints: const BoxConstraints(maxWidth: 900),
                      child: Text(
                        title.overview,
                        style: uiStyle(context, color: muted),
                      ),
                    ),
                  ],
                  if (movies.lastError != null) ...[
                    const SizedBox(height: 12),
                    Text(
                      movies.lastError!,
                      style: uiStyle(context, size: 15, color: warn),
                    ),
                  ],
                  if (title.isShow) ...[
                    const SizedBox(height: 28),
                    const SectionLabel('Episodes'),
                    const SizedBox(height: 12),
                    if (loadingEpisodes)
                      const Center(child: CircularProgressIndicator())
                    else if (episodeError.isNotEmpty)
                      EmptyState(message: episodeError, error: true)
                    else
                      episodeList(context),
                  ],
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Trakt sign-in. A computer opens Trakt in its browser and gets the
/// approval straight back, like AniList and MAL; phones and TVs use Trakt's
/// device sign-in: a short code to enter at trakt.tv/activate.
Future<void> traktSignIn(
  BuildContext context,
  AppServices services,
  bool isTV,
) async {
  if (isDesktop) return _traktBrowserSignIn(context, services);
  final trakt = services.trakt;
  TraktDeviceCode code;
  try {
    code = await trakt.startSignIn();
  } catch (_) {
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Trakt could not be reached. Try again.')),
      );
    }
    return;
  }
  if (!context.mounted) return;
  var cancelled = false;
  final done = trakt.finishSignIn(code, cancelled: () => cancelled);
  final ok = await showDialog<bool>(
    context: context,
    builder: (dialog) {
      unawaited(
        done.then((v) {
          if (v && dialog.mounted) Navigator.pop(dialog, true);
        }),
      );
      return AlertDialog(
        title: const Text('Sign in to Trakt'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(
              'Open ${code.verificationUrl} and enter',
              style: uiStyle(dialog, color: muted),
            ),
            const SizedBox(height: 12),
            SelectableText(
              code.userCode,
              style: display.copyWith(letterSpacing: 4),
            ),
            // A TV shows the page as a QR code for a phone; a phone opens it.
            if (isTV) ...[
              const SizedBox(height: 16),
              Container(
                color: fgBright,
                padding: const EdgeInsets.all(8),
                child: QrImageView(data: code.verificationUrl, size: 180),
              ),
            ],
            const SizedBox(height: 12),
            Text(
              'Waiting for Trakt…',
              style: uiStyle(dialog, size: 15, color: muted),
            ),
          ],
        ),
        actions: [
          if (!isTV)
            TextButton(
              onPressed: () => unawaited(
                services.auth
                    .launch(Uri.parse(code.verificationUrl))
                    .catchError((_) {
                      if (dialog.mounted) {
                        ScaffoldMessenger.of(dialog).showSnackBar(
                          const SnackBar(
                            content: Text('No browser could open Trakt.'),
                          ),
                        );
                      }
                    }),
              ),
              child: const Text('Open Trakt'),
            ),
          TextButton(
            autofocus: true,
            onPressed: () => Navigator.pop(dialog, false),
            child: const Text('Cancel'),
          ),
        ],
      );
    },
  );
  cancelled = ok != true;
  if (ok == true && context.mounted) {
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          'Signed in to Trakt${trakt.username.isEmpty ? '' : ' as ${trakt.username}'}.',
        ),
      ),
    );
    unawaited(services.movies.sync());
  }
}

Future<void> _traktBrowserSignIn(
  BuildContext context,
  AppServices services,
) async {
  final trakt = services.trakt;
  Uri url;
  try {
    url = await trakt.beginBrowserSignIn();
    await services.auth.launch(url);
  } catch (e) {
    await trakt.cancelBrowserSignIn();
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            e is TraktFailure ? e.message : 'Trakt could not be opened.',
          ),
        ),
      );
    }
    return;
  }
  if (!context.mounted) {
    await trakt.cancelBrowserSignIn();
    return;
  }
  final done = trakt.browserSignIn;
  final ok = await showDialog<bool>(
    context: context,
    builder: (dialog) {
      unawaited(
        done.then((v) {
          if (v && dialog.mounted) Navigator.pop(dialog, true);
        }),
      );
      return AlertDialog(
        title: const Text('Sign in to Trakt'),
        content: SizedBox(
          width: 360,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                'Finish sign in in your browser. Return here after approval.',
                style: uiStyle(dialog),
              ),
              TextButton(
                onPressed: () =>
                    unawaited(services.auth.launch(url).catchError((_) {})),
                child: const Text('Open browser'),
              ),
            ],
          ),
        ),
        actions: [
          TextButton(
            autofocus: true,
            onPressed: () => Navigator.pop(dialog, false),
            child: const Text('Cancel'),
          ),
        ],
      );
    },
  );
  await trakt.cancelBrowserSignIn();
  if (ok == true && context.mounted) {
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          'Signed in to Trakt${trakt.username.isEmpty ? '' : ' as ${trakt.username}'}.',
        ),
      ),
    );
    unawaited(services.movies.sync());
  }
}

/// Plays a downloaded file. With a [store], where it was left is kept per
/// download, so a saved film or episode resumes like a streamed one.
PlayRequest filePlay(
  String title,
  String label,
  String path,
  List<Json> subtitles, {
  AppStore? store,
  String id = '',
}) => PlayRequest(
  title: title,
  subtitle: label,
  resumeAt: store == null || id.isEmpty
      ? null
      : (_) => number((store.value('DownloadSpots') as Map?)?[id]),
  onProgress: store == null || id.isEmpty
      ? null
      : (position, duration, completed) {
          final spots = Map<String, dynamic>.from(
            store.value('DownloadSpots') as Map? ?? {},
          );
          completed ? spots.remove(id) : spots[id] = position;
          return store.set('DownloadSpots', spots);
        },
  open: (failed, scope, status) async {
    if (failed.isNotEmpty) {
      throw const CoreFailure('no_stream', 'This file could not be played.');
    }
    return PlayableMedia(
      url: path,
      subtitles: [
        for (final s in subtitles)
          {'url': s['path'], 'label': s['label'], 'language': s['language']},
      ],
      label: 'Downloaded',
    );
  },
);
