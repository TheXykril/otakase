import 'dart:async';
import 'dart:math' show min;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../data/models.dart';
import '../data/catalog.dart' show CatalogFailure;
import '../data/trakt.dart'
    show MediaTitle, findTitles, traktFilterRequests, traktGenreNames;
import '../desktop/desktop_core.dart';
import '../services.dart';
import '../data/community_extras.dart';
import 'community_extras_page.dart';
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
    if (isKonamiText(query.text)) {
      // The cheat code spelt out, as it worked in the CLI's rofi menu.
      query.clear();
      unawaited(openCheat(context, widget.services, widget.isTV));
      return;
    }
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
      if (!animeGenre) {
        // Only films and series have this genre; they show below.
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
        : [q, format, season, year, genre, status, sort].join('|');
    if (key == titlesQuery && !retry) return;
    titlesQuery = key;
    final g = ++titleGeneration;
    final requests = traktFilterRequests(
      format: format,
      season: season,
      year: year,
      genre: genre,
      status: status,
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
      );
      if (mounted && g == titleGeneration) {
        setState(() => titleResults = found);
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

  /// Poster cards in as many columns as fit. With [cap], only that many
  /// show, rounded to whole rows (at least one).
  Widget posterGrid(
    List<Anime> list, {
    required String keyPrefix,
    int? cap,
    String? Function(Anime)? caption,
  }) => LayoutBuilder(
    builder: (context, box) {
      final gap = isDesktop ? 26.0 : 12.0;
      final columns =
          (box.maxWidth /
                  (isDesktop
                      ? 226
                      : widget.isTV
                      ? 180
                      : (MediaQuery.textScalerOf(context).scale(1) > 1.2
                            ? 150
                            : 116)))
              .floor()
              .clamp(2, 9);
      final width = (box.maxWidth - (columns - 1) * gap) / columns;
      // Whole rows, rounded up so a cap of 12 never hides a 12th card.
      final count = cap == null
          ? list.length
          : min(list.length, (cap + columns - 1) ~/ columns * columns);
      return GridView.builder(
        shrinkWrap: true,
        physics: const NeverScrollableScrollPhysics(),
        itemCount: count,
        gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
          crossAxisCount: columns,
          crossAxisSpacing: gap,
          mainAxisSpacing: isDesktop ? 26 : 14,
          mainAxisExtent:
              width * 1.5 + 108 * MediaQuery.textScalerOf(context).scale(1),
        ),
        itemBuilder: (_, i) => PosterCard(
          key: ValueKey('$keyPrefix-${list[i].key}'),
          anime: list[i],
          width: width,
          isTV: widget.isTV,
          caption: caption?.call(list[i]),
          onOpen: () => widget.onOpen(list[i]),
        ),
      );
    },
  );

  SearchFilters get filters => SearchFilters(
    format: format,
    season: season,
    year: year,
    genre: genre,
    status: status,
    sort: sort,
  );

  Future<void> openFilters() async {
    final result = await showSearchFilters(
      context,
      initial: filters,
      genres: filterGenres,
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
  Widget build(BuildContext context) => ListView(
    padding: EdgeInsets.fromLTRB(
      widget.isTV ? 48 : 20,
      isDesktop ? 16 : 20,
      widget.isTV ? 48 : 20,
      contentBottom(context),
    ),
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
                onActivate: widget.isTV && !isDesktop ? editQuery : null,
                onFocusChanged: (focused) {
                  if (mounted && !focused && editing) {
                    setState(() => editing = false);
                  }
                },
                builder: (focus) => TextField(
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
                    suffixIcon: widget.isTV && voiceAvailable
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
                      if (!focus.focusInDirection(TraversalDirection.down)) {
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
                  onPressed: () {
                    format = null;
                    season = null;
                    year = null;
                    genre = null;
                    status = null;
                    sort = null;
                    unawaited(load());
                  },
                ),
        ),
      posterGrid(
        items,
        keyPrefix: 'search',
        // Anime and films share the page; anime alone pages on.
        cap: widget.titles && !animeOpen ? share : null,
      ),
      if (widget.titles &&
          !animeOpen &&
          (hasMore || items.length > share) &&
          error == null)
        moreButton('More anime', () => setState(() => animeOpen = true)),
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
          message:
              'More results could not be loaded. Your results are still here.',
          onRetry: () => load(more: retryMore),
          isTV: widget.isTV,
        ),
      if (hasMore && error == null && (!widget.titles || animeOpen))
        Padding(
          padding: const EdgeInsets.all(20),
          child: AppButton(
            label: 'Load more',
            disabledReason: busy ? 'More results are loading.' : null,
            onPressed: busy ? null : () => unawaited(load(more: true)),
            isTV: widget.isTV,
          ),
        ),
      ...titleSection(),
    ],
  );
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
      final result = await Future.wait([
        for (var i = 0; i < 7; i++)
          widget.services.catalog.schedule(
            DateTime(start.year, start.month, start.day + i),
            adult: adult,
          ),
      ]);
      if (mounted && generation == g) {
        setState(() => days = {for (var i = 0; i < 7; i++) i: result[i]});
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
      widget.services.store.entry(anime) != null ||
      widget.services.tracking.mergedCached(anime) != null;

  Widget weekItem(Airing row) {
    final mine = onList(row.anime);
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: FocusBox(
        key: ValueKey('week-${row.anime.key}-${row.episode}'),
        isTV: true,
        radius: 12,
        outsideFrame: true,
        onPressed: () => widget.onOpen(row.anime),
        child: Container(
          padding: const EdgeInsets.all(10),
          decoration: BoxDecoration(
            color: mine ? selectedPanel : panel,
            border: Border.all(color: mine ? shu : line),
            borderRadius: BorderRadius.circular(12),
          ),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              ClipRRect(
                borderRadius: BorderRadius.circular(8),
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
                      'Episode ${row.episode}',
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
    Widget toggle(String label, bool on, VoidCallback onPressed) =>
        AppTab(label: label, selected: on, isTV: true, onPressed: onPressed);
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
            toggle(
              'My list only',
              mineOnly,
              () => setState(() => mineOnly = true),
            ),
            const SizedBox(width: 10),
            toggle(
              'Everything',
              !mineOnly,
              () => setState(() => mineOnly = false),
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
      final result = await widget.services.catalog.schedule(
        selected,
        adult: widget.services.store.flag('AdultContent'),
        page: requestedPage,
      );
      if (mounted && generation == g) {
        setState(() {
          page = requestedPage;
          rows = append ? [...rows, ...result] : result;
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
    const names = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];
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
        SingleChildScrollView(
          scrollDirection: Axis.horizontal,
          child: Row(
            children: List.generate(7, (i) {
              final date = DateTime(monday.year, monday.month, monday.day + i);
              return Padding(
                padding: const EdgeInsets.only(right: 8),
                child: AppTab(
                  label: '${names[i]} ${date.day}',
                  selected: date == selected,
                  isTV: widget.isTV,
                  onPressed: () {
                    selected = date;
                    unawaited(load());
                  },
                ),
              );
            }),
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
        if (!busy && rows.isEmpty && error == null)
          const EmptyState(message: 'A quiet day on the calendar.'),
        for (final row in rows)
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
                    borderRadius: BorderRadius.circular(12),
                    child: CoverImage(row.anime.cover, width: 58, height: 84),
                  ),
                  const SizedBox(width: 18),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          row.anime.title,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          style: uiStyle(context, weight: FontWeight.w600),
                        ),
                        const SizedBox(height: 6),
                        Text(
                          'Episode ${row.episode}',
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
  });
  final String? format, season, genre, status, sort;
  final int? year;
  int get count => [
    format,
    season,
    year,
    genre,
    status,
    sort,
  ].where((value) => value != null).length;

  SearchFilters copyWith({
    String? Function()? format,
    String? Function()? season,
    int? Function()? year,
    String? Function()? genre,
    String? Function()? status,
    String? Function()? sort,
  }) => SearchFilters(
    format: format == null ? this.format : format(),
    season: season == null ? this.season : season(),
    year: year == null ? this.year : year(),
    genre: genre == null ? this.genre : genre(),
    status: status == null ? this.status : status(),
    sort: sort == null ? this.sort : sort(),
  );
}

/// Opens the Search filters: a menu under the Filters button on desktop, a
/// side panel on TV and a bottom sheet on phones. [titles] shows the movie
/// and show choices instead of the anime ones.
Future<SearchFilters?> showSearchFilters(
  BuildContext context, {
  required SearchFilters initial,
  required List<String> genres,
  required bool isTV,
  bool titles = false,
  ValueChanged<SearchFilters>? onChanged,
}) {
  final sheet = SearchFilterSheet(
    initial: initial,
    genres: genres,
    isTV: isTV,
    titles: titles,
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
              borderRadius: BorderRadius.circular(18),
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
      pageBuilder: (context, _, _) => Align(
        alignment: Alignment.centerRight,
        child: Material(
          color: panel,
          child: SizedBox(
            width: 560,
            height: double.infinity,
            child: SafeArea(child: sheet),
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
      if (f.sort case final v?) (_sorts[v] ?? v, f.copyWith(sort: () => null)),
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
              radius: 999,
              outsideFrame: true,
              onPressed: () => onChanged(without),
              child: Container(
                padding: const EdgeInsets.fromLTRB(14, 4, 10, 4),
                decoration: BoxDecoration(
                  border: Border.all(color: lineStrong),
                  borderRadius: BorderRadius.circular(999),
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
    radius: 12,
    outsideFrame: true,
    // Same surface as the search field beside it.
    child: Container(
      constraints: BoxConstraints(minHeight: isTV ? 52 : 48),
      padding: const EdgeInsets.symmetric(horizontal: 16),
      decoration: BoxDecoration(
        color: fgBright.withValues(alpha: .06),
        borderRadius: BorderRadius.circular(12),
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
                borderRadius: BorderRadius.circular(999),
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
    this.onChanged,
  });
  final SearchFilters initial;
  final List<String> genres;
  final bool isTV, titles;
  final ValueChanged<SearchFilters>? onChanged;
  @override
  State<SearchFilterSheet> createState() => _SearchFilterSheetState();
}

class _SearchFilterSheetState extends State<SearchFilterSheet> {
  late String? format = widget.initial.format,
      season = widget.initial.season,
      genre = widget.initial.genre,
      status = widget.initial.status,
      sort = widget.initial.sort;
  late int? year = widget.initial.year;

  SearchFilters get current => SearchFilters(
    format: format,
    season: season,
    year: year,
    genre: genre,
    status: status,
    sort: sort,
  );

  void change(VoidCallback update) {
    setState(update);
    widget.onChanged?.call(current);
  }

  Widget section(String title, Widget child) => Padding(
    padding: const EdgeInsets.only(top: 20),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(title, style: uiStyle(context, weight: FontWeight.w600)),
        const SizedBox(height: 10),
        child,
      ],
    ),
  );

  Widget chips(
    Map<String, String> values,
    String? selected,
    ValueChanged<String?> choose, {
    bool any = false,
    String anyLabel = 'Any',
    bool autofocus = false,
  }) => Wrap(
    spacing: widget.isTV ? 12 : 8,
    runSpacing: widget.isTV ? 12 : 8,
    children: [
      if (any)
        FilterPill(
          label: anyLabel,
          selected: selected == null,
          isTV: widget.isTV,
          autofocus: autofocus,
          onPressed: () => change(() => choose(null)),
        ),
      for (final (i, entry) in values.entries.indexed)
        FilterPill(
          label: entry.value,
          selected: selected == entry.key,
          isTV: widget.isTV,
          autofocus: autofocus && !any && i == 0,
          // Choosing the selected value again clears it.
          onPressed: () =>
              change(() => choose(selected == entry.key ? null : entry.key)),
        ),
    ],
  );

  Widget picker(String label, VoidCallback onPressed) => FocusBox(
    isTV: widget.isTV,
    onPressed: onPressed,
    radius: 12,
    outsideFrame: true,
    child: Container(
      constraints: BoxConstraints(minHeight: widget.isTV ? 52 : 48),
      padding: const EdgeInsets.symmetric(horizontal: 14),
      decoration: BoxDecoration(
        color: fgBright.withValues(alpha: .06),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: lineStrong),
      ),
      child: Row(
        children: [
          Expanded(child: Text(label, style: uiStyle(context))),
          const Icon(LucideIcons.chevronDown, size: 20),
        ],
      ),
    ),
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
    return SingleChildScrollView(
      padding: EdgeInsets.fromLTRB(
        widget.isTV ? 32 : 20,
        widget.isTV ? 28 : 0,
        widget.isTV ? 32 : 20,
        24,
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  'Filters',
                  style: uiStyle(context, size: 28, weight: FontWeight.w700),
                ),
              ),
              TextButton(
                key: const Key('filters-reset'),
                onPressed: () => change(() {
                  format = season = genre = status = sort = null;
                  year = null;
                }),
                child: Text(
                  'Reset',
                  style: uiStyle(context, color: shu, weight: FontWeight.w600),
                ),
              ),
              IconButton(
                tooltip: 'Close',
                icon: const Icon(LucideIcons.x),
                onPressed: () => Navigator.pop(context),
              ),
            ],
          ),
          section(
            widget.titles ? 'Type' : 'Format',
            chips(
              widget.titles ? _titleFormats : _formats,
              format,
              (v) => format = v,
              any: widget.titles,
              autofocus: widget.isTV,
            ),
          ),
          section(
            'Sort',
            chips(
              _sorts,
              sort,
              (v) => sort = v,
              any: true,
              anyLabel: 'Best match',
            ),
          ),
          if (!widget.titles)
            section(
              'Season',
              chips(_seasons, season, (v) => season = v, any: true),
            ),
          section(
            'Year',
            picker(
              year?.toString() ?? 'Any year',
              () => pick(
                'Year',
                years,
                year?.toString(),
                'Any year',
                (v) => year = int.tryParse(v ?? ''),
              ),
            ),
          ),
          if (widget.genres.isNotEmpty)
            section(
              'Genres',
              chips(
                {for (final g in widget.genres) g: g},
                genre,
                (v) => genre = v,
              ),
            ),
          section(
            widget.titles ? 'Series status' : 'Status',
            picker(
              (widget.titles ? _titleStatuses : _statuses)[status] ??
                  'Any status',
              () => pick(
                widget.titles ? 'Series status' : 'Status',
                widget.titles ? _titleStatuses : _statuses,
                status,
                'Any status',
                (v) => status = v,
              ),
            ),
          ),
          if (widget.onChanged == null) ...[
            const SizedBox(height: 24),
            FocusBox(
              key: const Key('filters-apply'),
              isTV: widget.isTV,
              radius: 14,
              outsideFrame: true,
              onPressed: () => Navigator.pop(context, current),
              child: Container(
                height: 52,
                alignment: Alignment.center,
                decoration: BoxDecoration(
                  color: shu,
                  borderRadius: BorderRadius.circular(14),
                ),
                child: Text(
                  'Apply filters',
                  style: uiStyle(
                    context,
                    color: bgDeep,
                    weight: FontWeight.w700,
                  ),
                ),
              ),
            ),
          ],
        ],
      ),
    );
  }
}

/// One choice in the Search filter sheet: a quiet pill, outlined in the
/// accent colour while chosen.
class FilterPill extends StatelessWidget {
  const FilterPill({
    super.key,
    required this.label,
    required this.selected,
    required this.onPressed,
    this.isTV = false,
    this.autofocus = false,
  });
  final String label;
  final bool selected, isTV, autofocus;
  final VoidCallback onPressed;
  @override
  Widget build(BuildContext context) => Semantics(
    selected: selected,
    child: FocusBox(
      isTV: isTV,
      autofocus: autofocus,
      onPressed: onPressed,
      radius: 999,
      outsideFrame: true,
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 180),
        curve: Curves.easeOut,
        padding: EdgeInsets.symmetric(horizontal: isTV ? 18 : 14, vertical: 7),
        decoration: BoxDecoration(
          color: selected
              ? shu.withValues(alpha: .12)
              : fgBright.withValues(alpha: .08),
          borderRadius: BorderRadius.circular(999),
          border: Border.all(
            color: selected ? shu : Colors.transparent,
            width: 1.5,
          ),
        ),
        child: Text(
          label,
          style: uiStyle(
            context,
            size: isTV ? 17 : 14,
            color: selected ? fgBright : fg,
            weight: selected ? FontWeight.w600 : FontWeight.w400,
          ),
        ),
      ),
    ),
  );
}
