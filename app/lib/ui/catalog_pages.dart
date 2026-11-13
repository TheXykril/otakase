import 'dart:async';
import 'dart:math' show min;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../data/models.dart';
import '../data/catalog.dart' show CatalogFailure;
import '../data/movies.dart' show ContentMode, contentModeOf;
import '../data/trakt.dart'
    show
        MediaTitle,
        findTitles,
        titleAges,
        titleLanguages,
        titleLengths,
        titleOnlySorts,
        traktFilterRequests,
        traktGenreNames;
import '../desktop/desktop_core.dart';
import '../services.dart';
import 'components.dart';
import 'movie_page.dart';
import 'movies_pages.dart' show titleCardsOf;

// Browse messages describe the failed task, never an upstream name, URL or
// arbitrary exception. Keep the useful rate-limit instruction without naming it.
String _catalogMessage(Object error, String fallback) =>
    error is CatalogFailure && error.statusCode == 429
    ? 'Too many requests. Wait a moment and retry.'
    : fallback;

class SearchPage extends StatefulWidget {
  const SearchPage({
    super.key,
    required this.services,
    required this.isTV,
    required this.onOpen,
    required this.onVoice,
    this.onNavigateUp,
    this.initialQuery = '',
    this.initialFormat,
    this.fieldFocus,
    this.titles = false,
  });

  /// Also searches films and series, for a profile that shows both.
  final bool titles;

  /// Focus of the search field, so the shell can move into it.
  final FocusNode? fieldFocus;
  final AppServices services;
  final bool isTV;
  final void Function(Anime) onOpen;
  final Future<String?> Function() onVoice;
  final VoidCallback? onNavigateUp;
  final String initialQuery;
  final String? initialFormat;
  @override
  State<SearchPage> createState() => _SearchPageState();
}

class _SearchPageState extends State<SearchPage> {
  final query = TextEditingController();
  Timer? debounce;
  List<Anime> items = [];
  String? error, format, season, genre, status, sort;
  int? year;

  /// Film and series filters (see [SearchFilters]); the anime use the
  /// decade and score too.
  int? decade, minScore;
  String? language, age, length;
  int page = 1, generation = 0;
  bool busy = true, hasMore = false, voiceAvailable = false;
  bool retryMore = false;
  bool editing = false;
  List<String> genres = [];
  late bool adultSeen;

  /// Films and series matching the query, when [SearchPage.titles] is on.
  List<MediaTitle> titleResults = [];
  bool titlesBusy = false, titlesFailed = false;
  String titlesQuery = '';
  int titleGeneration = 0;

  /// Nothing typed and no filters: the page shows what is trending.
  bool get browsing => query.text.trim().isEmpty && filters.count == 0;

  /// Anime and films share the page, so each shows about this many until
  /// its More button opens the rest.
  static const share = 12;
  bool animeOpen = false, titlesOpen = false;

  /// Whether the anime catalog knows [genre]; a films-only genre like Crime
  /// has no anime to look for.
  bool get animeGenre =>
      genre == null || genres.isEmpty || genres.contains(genre);

  /// Whether anime can match at all: not for a films-only genre, the box
  /// office chart, or a language, age rating or length (anime has none).
  bool get animeMatch =>
      animeGenre &&
      sort != 'BOXOFFICE' &&
      language == null &&
      age == null &&
      length == null;

  /// Genres for the filters: with films on, theirs join the anime ones.
  List<String> get filterGenres => widget.titles
      ? ({...genres, ...traktGenreNames}.toList()..sort())
      : genres;

  @override
  void initState() {
    super.initState();
    adultSeen = widget.services.store.adultAllowed;
    unawaited(
      const MethodChannel('otakase/device')
          .invokeMapMethod<dynamic, dynamic>('status')
          .then((status) {
            if (mounted) {
              setState(
                () => voiceAvailable = status?['voiceAvailable'] == true,
              );
            }
          })
          .catchError((Object _) {}),
    );
    query.text = widget.initialQuery;
    format = widget.initialFormat;
    unawaited(load());
    unawaited(
      widget.services.catalog
          .genres(adult: widget.services.store.flag('AdultContent'))
          .then((v) {
            if (mounted) setState(() => genres = v);
          })
          .catchError((_) {}),
    );
  }

  @override
  void didUpdateWidget(SearchPage old) {
    super.didUpdateWidget(old);
    if (old.titles != widget.titles) unawaited(searchTitles(retry: true));
    if (adultSeen != widget.services.store.adultAllowed) {
      adultSeen = widget.services.store.adultAllowed;
      debounce?.cancel();
      if (!adultSeen && genre == 'Hentai') genre = null;
      unawaited(load());
      unawaited(
        widget.services.catalog
            .genres(adult: adultSeen)
            .then((value) {
              if (mounted) setState(() => genres = value);
            })
            .catchError((Object _) {}),
      );
    }
  }

  Future<void> load({bool more = false}) async {
    if (!more) unawaited(searchTitles());
    final g = ++generation;
    final requestedPage = more ? page + 1 : 1;
    setState(() {
      busy = true;
      error = null;
      if (!more) {
        page = 1;
        hasMore = false;
        items = [];
        animeOpen = false;
      }
    });
    try {
      if (!animeMatch) {
        // Only films and series can match; they show below.
        return;
      }
      if (otakaseMovieEnabled && query.text.trim().toLowerCase() == 'otakase') {
        if (mounted && g == generation) {
          setState(() {
            items = [otakaseMovieEntry];
            hasMore = false;
          });
        }
        return;
      }
      final result = await widget.services.catalog.search(
        query.text,
        adult: widget.services.store.flag('AdultContent'),
        page: requestedPage,
        format: format,
        season: season,
        year: year,
        genre: genre,
        status: status,
        sort: sort ?? (browsing ? 'TRENDING' : null),
        decade: decade,
        minScore: minScore,
      );
      if (mounted && g == generation) {
        setState(() {
          page = requestedPage;
          items = more ? [...items, ...result.items] : result.items;
          hasMore = result.hasMore;
        });
      }
    } catch (e) {
      if (mounted && g == generation) {
        setState(() {
          error = _catalogMessage(
            e,
            'Anime could not be loaded. Check your connection and retry.',
          );
          retryMore = more;
        });
      }
    } finally {
      if (mounted && g == generation) setState(() => busy = false);
    }
  }

  /// Films and series for the query and filters: Trakt search with the
  /// filters it understands, or its popular lists when only filters are set.
  Future<void> searchTitles({bool retry = false}) async {
    final q = query.text.trim();
    // With nothing asked, trending films and series fill the page instead.
    final key = !widget.titles
        ? ''
        : browsing
        ? 'trending'
        : [
            q,
            format,
            season,
            year,
            genre,
            status,
            sort,
            decade,
            minScore,
            language,
            age,
            length,
          ].join('|');
    if (key == titlesQuery && !retry) return;
    titlesQuery = key;
    final g = ++titleGeneration;
    final requests = traktFilterRequests(
      format: format,
      season: season,
      year: year,
      genre: genre,
      status: status,
      decade: decade,
      minScore: minScore,
      language: language,
      age: age,
      length: length,
      sort: sort,
    );
    setState(() {
      titleResults = [];
      titlesOpen = false;
      titlesFailed = false;
      titlesBusy = key.isNotEmpty && requests.isNotEmpty;
    });
    if (!titlesBusy) return;
    try {
      final found = await findTitles(
        widget.services.trakt,
        q,
        format: format,
        season: season,
        year: year,
        genre: genre,
        status: status,
        sort: browsing ? 'TRENDING' : sort,
        decade: decade,
        minScore: minScore,
        language: language,
        age: age,
        length: length,
      );
      if (mounted && g == titleGeneration) {
        // Anime has its own row above; the same show twice only confuses.
        setState(() => titleResults = found.where((t) => !t.isAnime).toList());
      }
    } catch (_) {
      if (mounted && g == titleGeneration) setState(() => titlesFailed = true);
    } finally {
      if (mounted && g == titleGeneration) setState(() => titlesBusy = false);
    }
  }

  /// Films and series under the anime: trending ones with nothing asked,
  /// else those matching the query and filters. Capped like the anime so
  /// both get room, until More movies & shows opens the rest.
  List<Widget> titleSection() {
    if (titlesQuery.isEmpty) return const [];
    final cards = titleCardsOf(widget.services);
    final heading = browsing ? 'Trending movies & shows' : 'Movies & shows';
    return [
      if (titlesFailed)
        FailureView(
          message: browsing
              ? 'Trending movies and shows could not be loaded. Check your connection and retry.'
              : 'Movies and shows could not be searched. Check your connection and retry.',
          onRetry: () => unawaited(searchTitles(retry: true)),
          isTV: widget.isTV,
        )
      else if (titlesBusy)
        Semantics(
          liveRegion: true,
          label: browsing
              ? 'Loading trending movies and shows'
              : 'Searching movies and shows',
          child: const SkeletonBlock(height: 180),
        )
      else if (titleResults.isNotEmpty)
        KeyedSubtree(
          key: const Key('search-titles'),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const SizedBox(height: 26),
              Padding(
                padding: const EdgeInsets.only(bottom: 14),
                child: SectionLabel(heading),
              ),
              posterGrid(
                [for (final t in titleResults) cards.card(t)],
                keyPrefix: 'title',
                cap: titlesOpen ? null : share,
                caption: (a) => switch (cards.title(a)) {
                  final t? when t.isShow => 'Series',
                  final t? when t.year > 0 => '${t.year}',
                  _ => 'Movie',
                },
              ),
              if (!titlesOpen && titleResults.length > share)
                moreButton(
                  'More movies & shows',
                  () => setState(() => titlesOpen = true),
                ),
            ],
          ),
        ),
    ];
  }

  Widget moreButton(String label, VoidCallback onPressed) => Padding(
    padding: const EdgeInsets.only(top: 16),
    child: Center(
      child: AppButton(label: label, isTV: widget.isTV, onPressed: onPressed),
    ),
  );

  /// Columns, card width and grid layout for poster cards across [width].
  (int, double, SliverGridDelegate) gridFor(
    BuildContext context,
    double width,
  ) {
    final gap = isDesktop ? 26.0 : 12.0;
    final columns =
        (width /
                (isDesktop
                    ? 226
                    : widget.isTV
                    ? 180
                    : (MediaQuery.textScalerOf(context).scale(1) > 1.2
                          ? 150
                          : 116)))
            .floor()
            .clamp(2, 9);
    final card = (width - (columns - 1) * gap) / columns;
    return (
      columns,
      card,
      SliverGridDelegateWithFixedCrossAxisCount(
        crossAxisCount: columns,
        crossAxisSpacing: gap,
        mainAxisSpacing: isDesktop ? 26 : 14,
        mainAxisExtent:
            card * 1.5 + 108 * MediaQuery.textScalerOf(context).scale(1),
      ),
    );
  }

  Widget poster(
    Anime anime,
    double width,
    String keyPrefix,
    String? Function(Anime)? caption,
  ) => PosterCard(
    key: ValueKey('$keyPrefix-${anime.key}'),
    anime: anime,
    width: width,
    isTV: widget.isTV,
    caption: caption?.call(anime),
    onOpen: () => widget.onOpen(anime),
  );

  /// Whole rows of [cap] cards (rounded up so a cap of 12 never hides a
  /// 12th card), or all of [list] without a cap.
  int capped(List<Anime> list, int columns, int? cap) => cap == null
      ? list.length
      : min(list.length, (cap + columns - 1) ~/ columns * columns);

  /// Poster cards in as many columns as fit, inside a box. With [cap], only
  /// that many show, rounded to whole rows (at least one).
  Widget posterGrid(
    List<Anime> list, {
    required String keyPrefix,
    int? cap,
    String? Function(Anime)? caption,
  }) => LayoutBuilder(
    builder: (context, box) {
      final (columns, width, layout) = gridFor(context, box.maxWidth);
      return GridView.builder(
        shrinkWrap: true,
        physics: const NeverScrollableScrollPhysics(),
        itemCount: capped(list, columns, cap),
        gridDelegate: layout,
        itemBuilder: (_, i) => poster(list[i], width, keyPrefix, caption),
      );
    },
  );

  /// The same grid as a sliver, so only cards near the screen are built.
  Widget posterSliver(
    List<Anime> list, {
    required String keyPrefix,
    int? cap,
  }) => SliverLayoutBuilder(
    builder: (context, box) {
      final (columns, width, layout) = gridFor(context, box.crossAxisExtent);
      return SliverGrid.builder(
        itemCount: capped(list, columns, cap),
        gridDelegate: layout,
        itemBuilder: (_, i) => poster(list[i], width, keyPrefix, null),
      );
    },
  );

  /// Whether the next anime page loads by itself near the end. TV keeps the
  /// Load more button, since paging under the remote can trap focus.
  bool get pagesOn =>
      hasMore &&
      !busy &&
      error == null &&
      (!widget.titles || animeOpen) &&
      !(widget.isTV && !isDesktop);

  bool nearEnd(ScrollNotification note) {
    if (note.depth == 0 &&
        note is ScrollUpdateNotification &&
        note.metrics.extentAfter < 900 &&
        pagesOn) {
      unawaited(load(more: true));
    }
    return false;
  }

  SearchFilters get filters => SearchFilters(
    format: format,
    season: season,
    year: year,
    genre: genre,
    status: status,
    sort: sort,
    decade: decade,
    minScore: minScore,
    language: language,
    age: age,
    length: length,
  );

  Future<void> openFilters() async {
    final result = await showSearchFilters(
      context,
      initial: filters,
      genres: filterGenres,
      films: widget.titles,
      isTV: widget.isTV,
      // TV applies each choice at once; phones confirm with Apply filters.
      onChanged: widget.isTV ? apply : null,
    );
    if (result != null && mounted) await apply(result);
  }

  Future<void> apply(SearchFilters next) async {
    setState(() {
      format = next.format;
      season = next.season;
      year = next.year;
      genre = next.genre;
      status = next.status;
      sort = next.sort;
      decade = next.decade;
      minScore = next.minScore;
      language = next.language;
      age = next.age;
      length = next.length;
    });
    await load();
  }

  void editQuery(FocusNode focus) {
    if (!widget.isTV) return;
    setState(() => editing = true);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && focus.hasFocus && editing) {
        focus.context
            ?.findAncestorStateOfType<EditableTextState>()
            ?.requestKeyboard();
      }
    });
  }

  @override
  void dispose() {
    debounce?.cancel();
    query.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final side = widget.isTV ? 48.0 : 20.0;
    return NotificationListener<ScrollNotification>(
      onNotification: nearEnd,
      child: CustomScrollView(
        slivers: [
          SliverPadding(
            padding: EdgeInsets.fromLTRB(side, isDesktop ? 16 : 20, side, 0),
            sliver: SliverList.list(
              children: [
                if (!isDesktop) ...[
                  const PageHeading('Search', '探'),
                  const SizedBox(height: 18),
                ],
                IntrinsicHeight(
                  child: Row(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      Expanded(
                        child: RemoteTextInput(
                          isTV: widget.isTV,
                          focusNode: widget.fieldFocus,
                          onUp: widget.onNavigateUp,
                          onActivate: widget.isTV && !isDesktop
                              ? editQuery
                              : null,
                          onFocusChanged: (focused) {
                            if (mounted && !focused && editing) {
                              setState(() => editing = false);
                            }
                          },
                          // Rebuilt as the text changes, for the clear button.
                          builder: (focus) => ListenableBuilder(
                            listenable: query,
                            builder: (context, _) => TextField(
                              focusNode: focus,
                              // Desktop has a keyboard, so the field is always editable.
                              readOnly: widget.isTV && !isDesktop && !editing,
                              onTap: widget.isTV && !isDesktop
                                  ? () => editQuery(focus)
                                  : null,
                              key: const Key('catalog-search'),
                              controller: query,
                              decoration: InputDecoration(
                                hintText: widget.titles
                                    ? 'Search anime, movies and shows'
                                    : 'Search anime',
                                prefixIcon: const Icon(LucideIcons.search),
                                suffixIcon:
                                    query.text.isNotEmpty &&
                                        !(widget.isTV && !isDesktop)
                                    ? IconButton(
                                        key: const Key('catalog-search-clear'),
                                        tooltip: 'Clear search',
                                        icon: const Icon(LucideIcons.x),
                                        onPressed: () {
                                          debounce?.cancel();
                                          query.clear();
                                          unawaited(load());
                                          focus.requestFocus();
                                        },
                                      )
                                    : widget.isTV && voiceAvailable
                                    ? IconButton(
                                        tooltip: 'Voice search',
                                        icon: const Icon(LucideIcons.mic),
                                        onPressed: () async {
                                          final text = await widget.onVoice();
                                          if (text != null && mounted) {
                                            query.text = text;
                                            await load();
                                          }
                                        },
                                      )
                                    : null,
                              ),
                              textInputAction: TextInputAction.search,
                              onEditingComplete: widget.isTV ? () {} : null,
                              onSubmitted: (_) {
                                debounce?.cancel();
                                unawaited(load());
                                if (widget.isTV) {
                                  setState(() => editing = false);
                                  if (!focus.focusInDirection(
                                    TraversalDirection.down,
                                  )) {
                                    focus.nextFocus();
                                  }
                                }
                              },
                              onChanged: (_) {
                                debounce?.cancel();
                                debounce = Timer(
                                  const Duration(milliseconds: 450),
                                  () => load(),
                                );
                              },
                            ),
                          ),
                        ),
                      ),
                      SizedBox(width: widget.isTV ? 14 : 10),
                      FilterButton(
                        count: filters.count,
                        isTV: widget.isTV,
                        onPressed: openFilters,
                      ),
                    ],
                  ),
                ),
                if (isDesktop)
                  ActiveFilters(
                    filters: filters,
                    onChanged: (next) => unawaited(apply(next)),
                  ),
                const SizedBox(height: 22),
                // Always one slot, so the results below keep their place (and TV
                // focus) when the heading comes and goes.
                if (items.isNotEmpty && (browsing || titlesQuery.isNotEmpty))
                  Padding(
                    padding: const EdgeInsets.only(bottom: 14),
                    child: SectionLabel(browsing ? 'Trending anime' : 'Anime'),
                  )
                else
                  const SizedBox.shrink(),
                if (busy && items.isEmpty)
                  Semantics(
                    liveRegion: true,
                    label: 'Searching anime',
                    child: const SkeletonBlock(height: 180),
                  ),
                if (error != null && items.isEmpty)
                  FailureView(
                    message: error!,
                    onRetry: () => load(more: retryMore),
                    isTV: widget.isTV,
                  ),
                if (!busy &&
                    items.isEmpty &&
                    error == null &&
                    (titlesBusy || titleResults.isEmpty))
                  EmptyState(
                    message: query.text.trim().isEmpty
                        ? (filters.count == 0
                              ? 'No anime to display right now.'
                              : 'No anime are available for these filters.')
                        : 'No matching anime. Try another title or spelling.',
                    action: filters.count == 0
                        ? Text(
                            'Change the search above to try again.',
                            style: uiStyle(context, color: muted),
                          )
                        : AppButton(
                            label: 'Clear filters',
                            isTV: widget.isTV,
                            onPressed: () =>
                                unawaited(apply(const SearchFilters())),
                          ),
                  ),
              ],
            ),
          ),
          SliverPadding(
            padding: EdgeInsets.symmetric(horizontal: side),
            sliver: posterSliver(
              items,
              keyPrefix: 'search',
              // Anime and films share the page; anime alone pages on.
              cap: widget.titles && !animeOpen ? share : null,
            ),
          ),
          SliverPadding(
            padding: EdgeInsets.fromLTRB(side, 0, side, contentBottom(context)),
            sliver: SliverList.list(
              children: [
                if (widget.titles &&
                    !animeOpen &&
                    (hasMore || items.length > share) &&
                    error == null)
                  moreButton(
                    'More anime',
                    () => setState(() => animeOpen = true),
                  ),
                if (busy && items.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.all(16),
                    child: Semantics(
                      liveRegion: true,
                      child: Text(
                        'Loading more anime…',
                        style: uiStyle(context, color: muted),
                      ),
                    ),
                  ),
                if (error != null && items.isNotEmpty)
                  FailureView(
                    message: 'More results could not be loaded. Your results are still here.',
                    onRetry: () => load(more: retryMore),
                    isTV: widget.isTV,
                  ),
                if (hasMore && error == null && (!widget.titles || animeOpen))
                  Padding(
                    padding: const EdgeInsets.all(20),
                    child: AppButton(
                      label: 'Load more',
                      disabledReason: busy ? 'More results are loading.' : null,
                      onPressed: busy
                          ? null
                          : () => unawaited(load(more: true)),
                      isTV: widget.isTV,
                    ),
                  ),
                ...titleSection(),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class SchedulePage extends StatefulWidget {
  const SchedulePage({
    super.key,
    required this.services,
    required this.isTV,
    required this.onOpen,
  });
  final AppServices services;
  final bool isTV;
  final void Function(Anime) onOpen;
  @override
  State<SchedulePage> createState() => _SchedulePageState();
}

class _SchedulePageState extends State<SchedulePage> {
  final now = DateTime.now();
  late DateTime selected;
  List<Airing> rows = [];
  String? error;
  bool busy = true;
  int generation = 0, page = 1;
  bool retryAppend = false;
  bool more = false;
  // Desktop shows the whole week at once.
  int week = 0;
  bool mineOnly = false;
  Map<int, List<Airing>> days = {};

  /// Anime airing times come from AniList; series from the viewer's Trakt
  /// calendar, with movies & shows on and Trakt signed in.
  bool get anime => contentModeOf(widget.services.store) != ContentMode.movies;
  bool get shows =>
      contentModeOf(widget.services.store) != ContentMode.anime &&
      widget.services.trakt.signedIn;

  /// Series episodes airing in the [count] days from [from], as rows.
  Future<List<Airing>> showRows(DateTime from, int count) async {
    if (!shows) return const [];
    final cards = titleCardsOf(widget.services);
    final found = await widget.services.trakt.calendar(from, count);
    return [
      for (final e in found)
        Airing(
          cards.card(e.show),
          e.episode.number,
          e.at,
          label: e.episode.code,
        ),
    ];
  }

  /// [rows] with series rows mixed in by time.
  List<Airing> mixed(List<Airing> rows, List<Airing> series) => series.isEmpty
      ? rows
      : ([...rows, ...series]..sort((a, b) => a.at.compareTo(b.at)));

  @override
  void initState() {
    super.initState();
    selected = DateTime(now.year, now.month, now.day);
    unawaited(isDesktop ? loadWeek() : load());
  }

  DateTime get monday =>
      DateTime(now.year, now.month, now.day - (now.weekday - 1) + week * 7);

  Future<void> loadWeek() async {
    final g = ++generation;
    final start = monday;
    setState(() {
      busy = true;
      error = null;
      days = {};
    });
    try {
      final adult = widget.services.store.flag('AdultContent');
      final series = showRows(start, 7);
      final result = await Future.wait([
        for (var i = 0; i < 7; i++)
          anime
              ? widget.services.catalog.schedule(
                  DateTime(start.year, start.month, start.day + i),
                  adult: adult,
                )
              : Future.value(const <Airing>[]),
      ]);
      final episodes = await series;
      int dayOf(DateTime at) =>
          DateTime(at.year, at.month, at.day).difference(start).inDays;
      if (mounted && generation == g) {
        setState(
          () => days = {
            for (var i = 0; i < 7; i++)
              i: mixed(result[i], [
                for (final e in episodes)
                  if (dayOf(e.at) == i) e,
              ]),
          },
        );
      }
    } catch (e) {
      if (mounted && generation == g) {
        setState(
          () => error = _catalogMessage(
            e,
            'Airing times could not be loaded. Check your connection and retry.',
          ),
        );
      }
    } finally {
      if (mounted && generation == g) setState(() => busy = false);
    }
  }

  bool onList(Anime anime) =>
      // Series come from the viewer's own Trakt calendar.
      titleCardsOf(widget.services).title(anime) != null ||
      widget.services.store.entry(anime) != null ||
      widget.services.tracking.mergedCached(anime) != null;

  Widget weekItem(Airing row) {
    final mine = onList(row.anime);
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: FocusBox(
        key: ValueKey('week-${row.anime.key}-${row.episode}'),
        isTV: true,
        radius: KageRadius.md,
        outsideFrame: true,
        onPressed: () => widget.onOpen(row.anime),
        child: Container(
          padding: const EdgeInsets.all(10),
          decoration: BoxDecoration(
            color: mine ? selectedPanel : panel,
            border: Border.all(color: mine ? shu : line),
            borderRadius: BorderRadius.circular(KageRadius.md),
          ),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              ClipRRect(
                borderRadius: BorderRadius.circular(KageRadius.sm),
                child: CoverImage(row.anime.cover, width: 56, height: 80),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      '${clockTime(row.at)}${mine ? ' · ON YOUR LIST' : ''}',
                      style: mono.copyWith(
                        fontSize: 14,
                        color: mine ? accentText : muted,
                        height: 1.3,
                      ),
                    ),
                    const SizedBox(height: 3),
                    Text(
                      row.anime.title,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: uiStyle(
                        context,
                        size: 17,
                        weight: FontWeight.w600,
                        color: fgBright,
                        height: 1.25,
                      ),
                    ),
                    Text(
                      row.episodeLabel,
                      style: uiStyle(context, size: 15, color: muted),
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

  Widget desktopWeek() {
    const names = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];
    const months = [
      'Jan',
      'Feb',
      'Mar',
      'Apr',
      'May',
      'Jun',
      'Jul',
      'Aug',
      'Sep',
      'Oct',
      'Nov',
      'Dec',
    ];
    final start = monday, end = start.add(const Duration(days: 6));
    final today = DateTime(now.year, now.month, now.day);
    String day(DateTime d) => '${months[d.month - 1]} ${d.day}';
    Widget arrow(IconData icon, String tip, int step) => IconButton(
      tooltip: tip,
      onPressed: () {
        setState(() => week += step);
        unawaited(loadWeek());
      },
      style: IconButton.styleFrom(
        backgroundColor: fgBright.withValues(alpha: .08),
      ),
      icon: Icon(icon, size: 20),
    );
    return ListView(
      padding: EdgeInsets.fromLTRB(48, 16, 48, contentBottom(context)),
      children: [
        Row(
          crossAxisAlignment: CrossAxisAlignment.center,
          children: [
            Text(
              week == 0
                  ? 'This week'
                  : week == 1
                  ? 'Next week'
                  : week == -1
                  ? 'Last week'
                  : 'Week of ${day(start)}',
              style: uiStyle(context, size: 32, weight: FontWeight.w700),
            ),
            const SizedBox(width: 12),
            const ExcludeSemantics(
              child: Text(
                '暦',
                style: TextStyle(
                  fontFamily: 'NotoSansJP',
                  fontSize: 26,
                  color: accentText,
                ),
              ),
            ),
            const SizedBox(width: 20),
            Text(
              '${day(start)} – ${day(end)}',
              style: uiStyle(context, size: 17, color: muted),
            ),
            const Spacer(),
            AppSegmented<bool>(
              isTV: true,
              options: const [(true, 'My list only'), (false, 'Everything')],
              selected: mineOnly,
              onChanged: (value) => setState(() => mineOnly = value),
            ),
            const SizedBox(width: 16),
            arrow(LucideIcons.chevronLeft, 'Previous week', -1),
            const SizedBox(width: 8),
            arrow(LucideIcons.chevronRight, 'Next week', 1),
          ],
        ),
        const SizedBox(height: 6),
        Text(
          'Airing times, not source availability',
          style: uiStyle(context, size: 15, color: muted),
        ),
        const SizedBox(height: 22),
        if (error != null)
          FailureView(message: error!, onRetry: loadWeek, isTV: true),
        Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            for (var i = 0; i < 7; i++) ...[
              if (i > 0) const SizedBox(width: 16),
              Expanded(
                child: Builder(
                  builder: (context) {
                    final date = DateTime(
                      start.year,
                      start.month,
                      start.day + i,
                    );
                    final isToday = date == today;
                    final rows = (days[i] ?? const <Airing>[])
                        .where((r) => widget.services.store.visible(r.anime))
                        .where((r) => !mineOnly || onList(r.anime))
                        .toList();
                    return Column(
                      key: ValueKey('week-day-$i'),
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        Container(
                          padding: const EdgeInsets.fromLTRB(4, 0, 4, 10),
                          margin: const EdgeInsets.only(bottom: 14),
                          decoration: BoxDecoration(
                            border: Border(
                              bottom: BorderSide(
                                color: isToday ? shu : line,
                                width: 2,
                              ),
                            ),
                          ),
                          child: Row(
                            children: [
                              Expanded(
                                child: Text(
                                  '${names[i]} ${date.day}',
                                  style: uiStyle(
                                    context,
                                    weight: FontWeight.w700,
                                    color: isToday ? fgBright : fg,
                                  ),
                                ),
                              ),
                              Text(
                                isToday
                                    ? 'TODAY'
                                    : busy
                                    ? ''
                                    : '${rows.length} eps',
                                style: mono.copyWith(
                                  fontSize: 14,
                                  color: isToday ? accentText : muted,
                                ),
                              ),
                            ],
                          ),
                        ),
                        if (busy && days.isEmpty)
                          const SkeletonBlock(height: 100)
                        else if (rows.isEmpty && error == null)
                          Text(
                            mineOnly ? 'Nothing from your list' : 'Quiet day',
                            style: uiStyle(context, size: 15, color: muted),
                          ),
                        for (final row in rows) weekItem(row),
                      ],
                    );
                  },
                ),
              ),
            ],
          ],
        ),
      ],
    );
  }

  Future<void> load({bool append = false}) async {
    final g = ++generation;
    final requestedPage = append ? page + 1 : 1;
    setState(() {
      busy = true;
      error = null;
      if (!append) {
        page = 1;
        more = false;
        rows = [];
      }
    });
    try {
      final series = append ? null : showRows(selected, 1);
      final result = anime
          ? await widget.services.catalog.schedule(
              selected,
              adult: widget.services.store.flag('AdultContent'),
              page: requestedPage,
            )
          : const <Airing>[];
      final episodes = [
        for (final e in await series ?? const <Airing>[])
          if (DateTime(e.at.year, e.at.month, e.at.day) == selected) e,
      ];
      if (mounted && generation == g) {
        setState(() {
          page = requestedPage;
          rows = append ? [...rows, ...result] : mixed(result, episodes);
          more = result.length == 50;
        });
      }
    } catch (e) {
      if (mounted && generation == g) {
        setState(() {
          error = _catalogMessage(
            e,
            'Airing times could not be loaded. Check your connection and retry.',
          );
          retryAppend = append;
        });
      }
    } finally {
      if (mounted && generation == g) setState(() => busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    if (isDesktop) return desktopWeek();
    final shown = [
      for (final row in rows)
        if (!mineOnly || onList(row.anime)) row,
    ];
    final monday = DateTime(now.year, now.month, now.day - (now.weekday - 1));
    return ListView(
      padding: EdgeInsets.fromLTRB(
        widget.isTV ? 48 : 20,
        20,
        widget.isTV ? 48 : 20,
        contentBottom(context),
      ),
      children: [
        const PageHeading('Schedule', '暦'),
        const SizedBox(height: 8),
        const Text(
          'Airing times, not source availability',
          style: TextStyle(color: muted),
        ),
        const SizedBox(height: 20),
        // The whole week fits: one cell per day, no scrolling.
        WeekStrip(
          monday: monday,
          selected: selected,
          today: DateTime(now.year, now.month, now.day),
          isTV: widget.isTV,
          onChanged: (date) {
            selected = date;
            unawaited(load());
          },
        ),
        const SizedBox(height: 14),
        Align(
          alignment: Alignment.centerLeft,
          child: AppSegmented<bool>(
            isTV: widget.isTV,
            expand: !widget.isTV,
            options: const [(true, 'My list only'), (false, 'Everything')],
            selected: mineOnly,
            onChanged: (value) => setState(() => mineOnly = value),
          ),
        ),
        const SizedBox(height: 20),
        if (busy && rows.isEmpty) const SkeletonBlock(height: 180),
        if (error != null)
          FailureView(
            message: error!,
            onRetry: () => load(append: retryAppend),
            isTV: widget.isTV,
          ),
        if (!busy && shown.isEmpty && error == null)
          EmptyState(
            message: mineOnly && rows.isNotEmpty
                ? 'Nothing from your list airs this day.'
                : 'A quiet day on the calendar.',
          ),
        for (final row in shown)
          Padding(
            padding: const EdgeInsets.only(bottom: 12),
            child: FocusBox(
              isTV: widget.isTV,
              onPressed: () => widget.onOpen(row.anime),
              padding: const EdgeInsets.all(10),
              child: Row(
                children: [
                  SizedBox(
                    width: widget.isTV ? 88 : 64,
                    child: Text(
                      clockTime(row.at),
                      style: uiStyle(context, size: 15, color: muted),
                    ),
                  ),
                  ClipRRect(
                    borderRadius: BorderRadius.circular(KageRadius.md),
                    child: CoverImage(row.anime.cover, width: 58, height: 84),
                  ),
                  const SizedBox(width: 18),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        if (onList(row.anime))
                          Text(
                            'ON YOUR LIST',
                            style: mono.copyWith(
                              fontSize: 12,
                              letterSpacing: 1.5,
                              color: accentText,
                            ),
                          ),
                        Text(
                          row.anime.title,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          style: uiStyle(context, weight: FontWeight.w600),
                        ),
                        const SizedBox(height: 6),
                        Text(
                          row.episodeLabel,
                          style: const TextStyle(color: muted),
                        ),
                      ],
                    ),
                  ),
                  const Icon(LucideIcons.chevronRight, color: muted),
                ],
              ),
            ),
          ),
        if (more && error == null)
          AppButton(
            label: 'Load more',
            disabledReason: busy ? 'More airing times are loading.' : null,
            onPressed: busy ? null : () => unawaited(load(append: true)),
            isTV: widget.isTV,
          ),
      ],
    );
  }
}

/// The seven days of one week as equal cells: weekday over date, today in
/// Shu, the chosen day raised. Fits a phone without scrolling.
class WeekStrip extends StatelessWidget {
  const WeekStrip({
    super.key,
    required this.monday,
    required this.selected,
    required this.today,
    required this.onChanged,
    this.isTV = false,
  });
  final DateTime monday, selected, today;
  final ValueChanged<DateTime> onChanged;
  final bool isTV;

  static const names = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];

  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.all(2),
    decoration: BoxDecoration(
      color: panel,
      border: Border.all(color: line),
      borderRadius: BorderRadius.circular(KageRadius.input),
    ),
    child: Row(
      children: [
        for (var i = 0; i < 7; i++)
          if (DateTime(monday.year, monday.month, monday.day + i)
              case final date)
            Expanded(
              child: Semantics(
                selected: date == selected,
                button: true,
                label:
                    '${names[i]} ${date.day}${date == today ? ', today' : ''}',
                excludeSemantics: true,
                child: FocusBox(
                  key: ValueKey('schedule-day-$i'),
                  isTV: isTV,
                  radius: KageRadius.segment,
                  scale: 1,
                  onPressed: () => onChanged(date),
                  child: AnimatedContainer(
                    duration: KageMotion.fast,
                    padding: EdgeInsets.symmetric(vertical: isTV ? 10 : 8),
                    decoration: BoxDecoration(
                      color: date == selected
                          ? panelRaised
                          : Colors.transparent,
                      borderRadius: BorderRadius.circular(KageRadius.segment),
                    ),
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Text(
                          names[i].toUpperCase(),
                          style: mono.copyWith(
                            fontSize: isTV ? 14 : 12,
                            letterSpacing: 1,
                            color: date == today ? accentText : muted,
                          ),
                        ),
                        const SizedBox(height: 2),
                        Text(
                          '${date.day}',
                          style: uiStyle(
                            context,
                            weight: FontWeight.w700,
                            color: date == selected
                                ? fgBright
                                : date == today
                                ? accentText
                                : fg,
                          ),
                        ),
                        const SizedBox(height: 4),
                        // Shu mark under the chosen day.
                        Container(
                          width: 16,
                          height: 2,
                          color: date == selected ? shu : Colors.transparent,
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            ),
      ],
    ),
  );
}

const _formats = {
  'TV': 'TV',
  'MOVIE': 'Movie',
  'OVA': 'OVA',
  'ONA': 'ONA',
  'SPECIAL': 'Special',
  'TV_SHORT': 'TV short',
  'MUSIC': 'Music',
};
const _seasons = {
  'WINTER': 'Winter',
  'SPRING': 'Spring',
  'SUMMER': 'Summer',
  'FALL': 'Fall',
};
const _statuses = {
  'RELEASING': 'Releasing',
  'FINISHED': 'Finished',
  'NOT_YET_RELEASED': 'Not yet released',
  'HIATUS': 'Hiatus',
  'CANCELLED': 'Cancelled',
};

/// Movie and show search: the same filter keys as anime, as Trakt names them.
const _titleFormats = {'MOVIE': 'Movies', 'TV': 'Series'};
const _titleStatuses = {
  'RELEASING': 'Returning',
  'FINISHED': 'Ended',
  'NOT_YET_RELEASED': 'Upcoming',
  'CANCELLED': 'Cancelled',
};
const _sorts = {
  'POPULAR': 'Popular',
  'TRENDING': 'Trending',
  'SCORE': 'Top rated',
  'NEWEST': 'Newest',
};

class SearchFilters {
  const SearchFilters({
    this.format,
    this.season,
    this.year,
    this.genre,
    this.status,
    this.sort,
    this.decade,
    this.minScore,
    this.language,
    this.age,
    this.length,
  });
  final String? format, season, genre, status, sort;
  final int? year;

  /// First year of a decade (2010 for the 2010s), used when no [year] is set.
  final int? decade;

  /// Lowest score out of 100, like 70 for 7+.
  final int? minScore;

  /// Films and series only: original language code, age rating up to
  /// (see titleAges) and length (see titleLengths).
  final String? language, age, length;
  int get count => [
    format,
    season,
    year,
    genre,
    status,
    sort,
    decade,
    minScore,
    language,
    age,
    length,
  ].where((value) => value != null).length;

  SearchFilters copyWith({
    String? Function()? format,
    String? Function()? season,
    int? Function()? year,
    String? Function()? genre,
    String? Function()? status,
    String? Function()? sort,
    int? Function()? decade,
    int? Function()? minScore,
    String? Function()? language,
    String? Function()? age,
    String? Function()? length,
  }) => SearchFilters(
    format: format == null ? this.format : format(),
    season: season == null ? this.season : season(),
    year: year == null ? this.year : year(),
    genre: genre == null ? this.genre : genre(),
    status: status == null ? this.status : status(),
    sort: sort == null ? this.sort : sort(),
    decade: decade == null ? this.decade : decade(),
    minScore: minScore == null ? this.minScore : minScore(),
    language: language == null ? this.language : language(),
    age: age == null ? this.age : age(),
    length: length == null ? this.length : length(),
  );
}

/// Decades for the filters, newest first, keyed by their first year.
Map<String, String> _decades() {
  final now = DateTime.now().year ~/ 10 * 10;
  return {for (var d = now; d >= 1950; d -= 10) '$d': '${d}s'};
}

const _scores = {'60': '6+', '70': '7+', '80': '8+'};

/// Opens the Search filters: a menu under the Filters button on desktop, a
/// side panel on TV and a bottom sheet on phones. [titles] shows the movie
/// and show choices instead of the anime ones.
Future<SearchFilters?> showSearchFilters(
  BuildContext context, {
  required SearchFilters initial,
  required List<String> genres,
  required bool isTV,
  bool titles = false,
  bool films = false,
  ValueChanged<SearchFilters>? onChanged,
}) {
  final sheet = SearchFilterSheet(
    initial: initial,
    genres: genres,
    isTV: isTV,
    titles: titles,
    films: films || titles,
    onChanged: onChanged,
  );
  if (isDesktop) {
    return showGeneralDialog<SearchFilters>(
      context: context,
      barrierDismissible: true,
      barrierLabel: 'Close filters',
      barrierColor: Colors.black38,
      transitionDuration: const Duration(milliseconds: 160),
      transitionBuilder: (context, animation, _, child) =>
          FadeTransition(opacity: animation, child: child),
      // A menu under the Filters button instead of a full-height panel.
      pageBuilder: (context, _, _) => Align(
        alignment: Alignment.topRight,
        child: Padding(
          padding: EdgeInsets.only(
            top: tvHeaderExtent(context) + 96,
            right: 48,
          ),
          child: Material(
            key: const Key('search-filter-menu'),
            color: panel,
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(KageRadius.dialog),
              side: const BorderSide(color: line),
            ),
            clipBehavior: Clip.antiAlias,
            child: ConstrainedBox(
              constraints: BoxConstraints(
                maxWidth: 680,
                maxHeight:
                    MediaQuery.sizeOf(context).height -
                    tvHeaderExtent(context) -
                    140,
              ),
              child: sheet,
            ),
          ),
        ),
      ),
    );
  }
  if (isTV) {
    return showGeneralDialog<SearchFilters>(
      context: context,
      barrierDismissible: true,
      barrierLabel: 'Close filters',
      barrierColor: Colors.black54,
      // A plate at the right edge, like the TV pickers, sized to its rows.
      pageBuilder: (context, _, _) => Align(
        alignment: Alignment.centerRight,
        child: Padding(
          padding: const EdgeInsets.all(KageSpace.s6),
          child: Material(
            color: panel,
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(KageRadius.sheet),
              side: const BorderSide(color: line),
            ),
            clipBehavior: Clip.antiAlias,
            child: ConstrainedBox(
              constraints: BoxConstraints(
                maxWidth: 520,
                maxHeight: MediaQuery.sizeOf(context).height * .9,
              ),
              child: sheet,
            ),
          ),
        ),
      ),
    );
  }
  return showModalBottomSheet<SearchFilters>(
    context: context,
    isScrollControlled: true,
    showDragHandle: true,
    backgroundColor: panel,
    builder: (context) => SafeArea(child: sheet),
  );
}

/// The chosen filters as removable chips under the desktop search field.
class ActiveFilters extends StatelessWidget {
  const ActiveFilters({
    super.key,
    required this.filters,
    required this.onChanged,
    this.titles = false,
  });
  final SearchFilters filters;
  final bool titles;
  final ValueChanged<SearchFilters> onChanged;

  @override
  Widget build(BuildContext context) {
    final f = filters;
    final formats = titles ? _titleFormats : _formats;
    final statuses = titles ? _titleStatuses : _statuses;
    final active = <(String, SearchFilters)>[
      if (f.format case final v?)
        (formats[v] ?? v, f.copyWith(format: () => null)),
      if (f.status case final v?)
        (statuses[v] ?? v, f.copyWith(status: () => null)),
      if (f.genre case final v?) (v, f.copyWith(genre: () => null)),
      if (f.season case final v?)
        (_seasons[v] ?? v, f.copyWith(season: () => null)),
      if (f.year case final v?) ('$v', f.copyWith(year: () => null)),
      if (f.sort case final v?)
        (_sorts[v] ?? titleOnlySorts[v] ?? v, f.copyWith(sort: () => null)),
      if (f.decade case final v?) ('${v}s', f.copyWith(decade: () => null)),
      if (f.minScore case final v?)
        (_scores['$v'] ?? '$v+', f.copyWith(minScore: () => null)),
      if (f.language case final v?)
        (titleLanguages[v] ?? v, f.copyWith(language: () => null)),
      if (f.age case final v?) (titleAges[v] ?? v, f.copyWith(age: () => null)),
      if (f.length case final v?)
        (titleLengths[v] ?? v, f.copyWith(length: () => null)),
    ];
    if (active.isEmpty) return const SizedBox.shrink();
    return Padding(
      padding: const EdgeInsets.only(top: 16),
      child: Wrap(
        spacing: 10,
        runSpacing: 8,
        crossAxisAlignment: WrapCrossAlignment.center,
        children: [
          Text('Active:', style: uiStyle(context, size: 17, color: muted)),
          for (final (label, without) in active)
            FocusBox(
              key: ValueKey('active-filter-$label'),
              isTV: true,
              radius: KageRadius.chip,
              outsideFrame: true,
              onPressed: () => onChanged(without),
              child: Container(
                padding: const EdgeInsets.fromLTRB(14, 4, 10, 4),
                decoration: BoxDecoration(
                  border: Border.all(color: lineStrong),
                  borderRadius: BorderRadius.circular(KageRadius.chip),
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(label, style: uiStyle(context, size: 17)),
                    const SizedBox(width: 6),
                    const Icon(LucideIcons.x, size: 16, color: muted),
                  ],
                ),
              ),
            ),
        ],
      ),
    );
  }
}

class FilterButton extends StatelessWidget {
  const FilterButton({
    super.key,
    required this.count,
    required this.onPressed,
    this.isTV = false,
  });
  final int count;
  final bool isTV;
  final VoidCallback onPressed;
  @override
  Widget build(BuildContext context) => FocusBox(
    key: const Key('search-filters'),
    isTV: isTV,
    onPressed: onPressed,
    radius: KageRadius.input,
    outsideFrame: true,
    // Same surface as the search field beside it.
    child: Container(
      constraints: BoxConstraints(minHeight: isTV ? 52 : 48),
      padding: const EdgeInsets.symmetric(horizontal: 16),
      decoration: BoxDecoration(
        color: fgBright.withValues(alpha: .06),
        borderRadius: BorderRadius.circular(KageRadius.input),
        border: Border.all(color: lineStrong),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(LucideIcons.slidersHorizontal, size: isTV ? 22 : 20),
          const SizedBox(width: 8),
          Text('Filters', style: uiStyle(context, weight: FontWeight.w600)),
          if (count > 0) ...[
            const SizedBox(width: 8),
            Container(
              key: const Key('search-filters-count'),
              padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 1),
              decoration: BoxDecoration(
                color: shu,
                borderRadius: BorderRadius.circular(KageRadius.full),
              ),
              child: Text(
                '$count',
                style: uiStyle(
                  context,
                  size: 14,
                  color: bgDeep,
                  weight: FontWeight.w700,
                ),
              ),
            ),
          ],
        ],
      ),
    ),
  );
}

/// Format, season, year, genre, status and sort for Search. With [titles] it
/// offers the movie and show choices: type, year, genre, series status and
/// sort. With [onChanged] each choice applies at once and there is no Apply
/// button.
class SearchFilterSheet extends StatefulWidget {
  const SearchFilterSheet({
    super.key,
    required this.initial,
    required this.genres,
    this.isTV = false,
    this.titles = false,
    this.films = false,
    this.onChanged,
  });
  final SearchFilters initial;
  final List<String> genres;

  /// [films]: the film and series filters (language, age rating, length and
  /// their extra sorts) show too, as when movies are on.
  final bool isTV, titles, films;
  final ValueChanged<SearchFilters>? onChanged;
  @override
  State<SearchFilterSheet> createState() => _SearchFilterSheetState();
}

class _SearchFilterSheetState extends State<SearchFilterSheet> {
  late String? format = widget.initial.format,
      season = widget.initial.season,
      genre = widget.initial.genre,
      status = widget.initial.status,
      sort = widget.initial.sort,
      language = widget.initial.language,
      age = widget.initial.age,
      length = widget.initial.length;
  late int? year = widget.initial.year,
      decade = widget.initial.decade,
      minScore = widget.initial.minScore;

  SearchFilters get current => SearchFilters(
    format: format,
    season: season,
    year: year,
    genre: genre,
    status: status,
    sort: sort,
    decade: decade,
    minScore: minScore,
    language: language,
    age: age,
    length: length,
  );

  void change(VoidCallback update) {
    setState(update);
    widget.onChanged?.call(current);
  }

  /// Genres are the one multi-value-looking group, so they stay chips;
  /// choosing the selected genre again clears it.
  Widget genreChips() => Wrap(
    spacing: widget.isTV ? 12 : 8,
    runSpacing: widget.isTV ? 12 : 8,
    children: [
      for (final g in widget.genres)
        AppChip(
          label: g,
          selected: genre == g,
          isTV: widget.isTV,
          onPressed: () => change(() => genre = genre == g ? null : g),
        ),
    ],
  );

  /// One single-choice filter: its name, the current value on the right,
  /// and a picker with [any] first.
  Widget valueRow(
    String title,
    Map<String, String> values,
    String? selected,
    String any,
    ValueChanged<String?> choose, {
    bool autofocus = false,
  }) => AppRow(
    key: ValueKey('filter-$title'),
    isTV: widget.isTV,
    autofocus: autofocus,
    title: title,
    value: values[selected] ?? any,
    divider: true,
    onTap: () => unawaited(pick(title, values, selected, any, choose)),
  );

  Future<void> pick(
    String title,
    Map<String, String> values,
    String? selected,
    String any,
    ValueChanged<String?> choose,
  ) async {
    final value = await chooseValue(context, title, [
      any,
      ...values.values,
    ], selected: values[selected] ?? any);
    if (value == null || !mounted) return;
    change(
      () => choose(
        values.entries.where((e) => e.value == value).firstOrNull?.key,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final now = DateTime.now().year;
    final years = {
      for (var i = 0; i < 50; i++) '${now + 1 - i}': '${now + 1 - i}',
    };
    final side = widget.isTV ? 28.0 : 20.0;
    final films = widget.films && !widget.titles;
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: EdgeInsets.fromLTRB(side, widget.isTV ? 24 : 0, 12, 8),
          child: Row(
            children: [
              Expanded(
                child: Text(
                  'Filters',
                  style: uiStyle(context, size: 24, weight: FontWeight.w700),
                ),
              ),
              AppButton(
                key: const Key('filters-reset'),
                label: 'Reset',
                variant: ButtonVariant.text,
                isTV: widget.isTV,
                onPressed: () => change(() {
                  format = season = genre = status = sort = null;
                  language = age = length = null;
                  year = decade = minScore = null;
                }),
              ),
              AppButton(
                label: 'Close',
                icon: LucideIcons.x,
                iconOnly: true,
                variant: ButtonVariant.text,
                isTV: widget.isTV,
                onPressed: () => Navigator.pop(context),
              ),
            ],
          ),
        ),
        Flexible(
          child: SingleChildScrollView(
            padding: EdgeInsets.symmetric(horizontal: side - 8),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                valueRow(
                  widget.titles ? 'Type' : 'Format',
                  widget.titles ? _titleFormats : _formats,
                  format,
                  widget.titles ? 'Movies & series' : 'Any format',
                  (v) => format = v,
                  autofocus: widget.isTV,
                ),
                valueRow(
                  'Sort',
                  {..._sorts, if (widget.films) ...titleOnlySorts},
                  sort,
                  'Best match',
                  (v) => sort = v,
                ),
                if (!widget.titles)
                  valueRow(
                    'Season',
                    _seasons,
                    season,
                    'Any season',
                    (v) => season = v,
                  ),
                valueRow('Year', years, year?.toString(), 'Any year', (v) {
                  year = int.tryParse(v ?? '');
                  // A year is narrower than a decade; keep one of them.
                  if (year != null) decade = null;
                }),
                valueRow(
                  'Decade',
                  _decades(),
                  decade?.toString(),
                  'Any decade',
                  (v) {
                    decade = int.tryParse(v ?? '');
                    if (decade != null) year = null;
                  },
                ),
                valueRow(
                  'Rating',
                  _scores,
                  minScore?.toString(),
                  'Any rating',
                  (v) => minScore = int.tryParse(v ?? ''),
                ),
                valueRow(
                  widget.titles ? 'Series status' : 'Status',
                  widget.titles ? _titleStatuses : _statuses,
                  status,
                  'Any status',
                  (v) => status = v,
                ),
                if (widget.films) ...[
                  valueRow(
                    films ? 'Language (movies & shows)' : 'Language',
                    titleLanguages,
                    language,
                    'Any language',
                    (v) => language = v,
                  ),
                  valueRow(
                    films ? 'Age rating (movies & shows)' : 'Age rating',
                    titleAges,
                    age,
                    'Any age',
                    (v) => age = v,
                  ),
                  valueRow(
                    films ? 'Length (movies & shows)' : 'Length',
                    titleLengths,
                    length,
                    'Any length',
                    (v) => length = v,
                  ),
                ],
                if (widget.genres.isNotEmpty) ...[
                  GroupHeading(
                    'Genres',
                    padding: const EdgeInsets.fromLTRB(8, 20, 8, 12),
                  ),
                  Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 8),
                    child: genreChips(),
                  ),
                ],
                const SizedBox(height: 16),
              ],
            ),
          ),
        ),
        // TV applies each choice at once and needs no Apply button.
        if (widget.onChanged == null)
          Padding(
            padding: EdgeInsets.fromLTRB(side, 8, side, 20),
            child: AppButton(
              key: const Key('filters-apply'),
              label: 'Apply filters',
              variant: ButtonVariant.primary,
              isTV: widget.isTV,
              onPressed: () => Navigator.pop(context, current),
            ),
          ),
      ],
    );
  }
}
