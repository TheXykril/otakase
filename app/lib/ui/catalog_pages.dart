import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../data/models.dart';
import '../services.dart';
import 'components.dart';
import 'movie_page.dart';

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
  });
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
  String? error, format, season, genre, status;
  int? year;
  int page = 1, generation = 0;
  bool busy = true, hasMore = false, voiceAvailable = false;
  bool editing = false;
  List<String> genres = [];
  late bool adultSeen;
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
    final g = ++generation;
    setState(() {
      busy = true;
      error = null;
      if (!more) {
        page = 1;
        items = [];
      }
    });
    try {
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
        page: page,
        format: format,
        season: season,
        year: year,
        genre: genre,
        status: status,
      );
      if (mounted && g == generation) {
        setState(() {
          items = more ? [...items, ...result.items] : result.items;
          hasMore = result.hasMore;
        });
      }
    } catch (e) {
      if (mounted && g == generation) setState(() => error = e.toString());
    } finally {
      if (mounted && g == generation) setState(() => busy = false);
    }
  }

  SearchFilters get filters => SearchFilters(
    format: format,
    season: season,
    year: year,
    genre: genre,
    status: status,
  );

  Future<void> openFilters() async {
    final sheet = SearchFilterSheet(
      initial: filters,
      genres: genres,
      isTV: widget.isTV,
    );
    final result = widget.isTV
        ? await showGeneralDialog<SearchFilters>(
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
          )
        : await showModalBottomSheet<SearchFilters>(
            context: context,
            isScrollControlled: true,
            showDragHandle: true,
            backgroundColor: panel,
            builder: (context) => SafeArea(child: sheet),
          );
    if (result == null || !mounted) return;
    setState(() {
      format = result.format;
      season = result.season;
      year = result.year;
      genre = result.genre;
      status = result.status;
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
      20,
      widget.isTV ? 48 : 20,
      contentBottom(context),
    ),
    children: [
      const PageHeading('Search', '探'),
      const SizedBox(height: 18),
      IntrinsicHeight(
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Expanded(
              child: RemoteTextInput(
                isTV: widget.isTV,
                onUp: widget.onNavigateUp,
                onActivate: widget.isTV ? editQuery : null,
                onFocusChanged: (focused) {
                  if (mounted && !focused && editing) {
                    setState(() => editing = false);
                  }
                },
                builder: (focus) => TextField(
                  focusNode: focus,
                  readOnly: widget.isTV && !editing,
                  onTap: widget.isTV ? () => editQuery(focus) : null,
                  key: const Key('catalog-search'),
                  controller: query,
                  decoration: InputDecoration(
                    hintText: 'Search anime',
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
      const SizedBox(height: 22),
      if (busy) const SkeletonBlock(height: 180),
      if (error != null)
        FailureView(message: error!, onRetry: () => load(), isTV: widget.isTV),
      if (!busy && items.isEmpty && error == null)
        EmptyState(
          message: 'No matching anime.',
          action: AppButton(
            label: 'Clear filters',
            isTV: widget.isTV,
            onPressed: () {
              format = null;
              season = null;
              year = null;
              genre = null;
              status = null;
              unawaited(load());
            },
          ),
        ),
      LayoutBuilder(
        builder: (context, box) {
          final columns =
              (box.maxWidth /
                      (widget.isTV
                          ? 180
                          : (MediaQuery.textScalerOf(context).scale(1) > 1.2
                                ? 150
                                : 116)))
                  .floor()
                  .clamp(2, 9);
          final width = (box.maxWidth - (columns - 1) * 12) / columns;
          return GridView.builder(
            shrinkWrap: true,
            physics: const NeverScrollableScrollPhysics(),
            itemCount: items.length,
            gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
              crossAxisCount: columns,
              crossAxisSpacing: 12,
              mainAxisSpacing: 14,
              mainAxisExtent:
                  width * 1.5 +
                  (widget.isTV ? 108 : 108) *
                      MediaQuery.textScalerOf(context).scale(1),
            ),
            itemBuilder: (_, i) => PosterCard(
              key: ValueKey('search-${items[i].key}'),
              anime: items[i],
              width: width,
              isTV: widget.isTV,
              onOpen: () => widget.onOpen(items[i]),
            ),
          );
        },
      ),
      if (hasMore)
        Padding(
          padding: const EdgeInsets.all(20),
          child: AppButton(
            label: 'Load more',
            onPressed: busy
                ? null
                : () {
                    page++;
                    unawaited(load(more: true));
                  },
            isTV: widget.isTV,
          ),
        ),
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
  bool more = false;
  @override
  void initState() {
    super.initState();
    selected = DateTime(now.year, now.month, now.day);
    unawaited(load());
  }

  Future<void> load({bool append = false}) async {
    final g = ++generation;
    setState(() {
      busy = true;
      error = null;
      if (!append) {
        page = 1;
        rows = [];
      }
    });
    try {
      final result = await widget.services.catalog.schedule(
        selected,
        adult: widget.services.store.flag('AdultContent'),
        page: page,
      );
      if (mounted && generation == g) {
        setState(() {
          rows = append ? [...rows, ...result] : result;
          more = result.length == 50;
        });
      }
    } catch (e) {
      if (mounted && generation == g) setState(() => error = e.toString());
    } finally {
      if (mounted && generation == g) setState(() => busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
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
        if (busy) const SkeletonBlock(height: 180),
        if (error != null)
          FailureView(
            message: error!,
            onRetry: () => load(),
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
        if (more)
          AppButton(
            label: 'Load more',
            onPressed: busy
                ? null
                : () {
                    page++;
                    unawaited(load(append: true));
                  },
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

class SearchFilters {
  const SearchFilters({
    this.format,
    this.season,
    this.year,
    this.genre,
    this.status,
  });
  final String? format, season, genre, status;
  final int? year;
  int get count => [
    format,
    season,
    year,
    genre,
    status,
  ].where((value) => value != null).length;
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

/// Format, season, year, genre and status for Search, edited together and
/// applied at once.
class SearchFilterSheet extends StatefulWidget {
  const SearchFilterSheet({
    super.key,
    required this.initial,
    required this.genres,
    this.isTV = false,
  });
  final SearchFilters initial;
  final List<String> genres;
  final bool isTV;
  @override
  State<SearchFilterSheet> createState() => _SearchFilterSheetState();
}

class _SearchFilterSheetState extends State<SearchFilterSheet> {
  late String? format = widget.initial.format,
      season = widget.initial.season,
      genre = widget.initial.genre,
      status = widget.initial.status;
  late int? year = widget.initial.year;

  Widget section(String title, Widget child) => Padding(
    padding: const EdgeInsets.only(top: 18),
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
    bool autofocus = false,
  }) => Wrap(
    spacing: 8,
    runSpacing: 8,
    children: [
      if (any)
        AppTab(
          label: 'Any',
          selected: selected == null,
          isTV: widget.isTV,
          autofocus: autofocus,
          onPressed: () => setState(() => choose(null)),
        ),
      for (final (i, entry) in values.entries.indexed)
        AppTab(
          label: entry.value,
          selected: selected == entry.key,
          isTV: widget.isTV,
          autofocus: autofocus && !any && i == 0,
          // Choosing the selected value again clears it.
          onPressed: () =>
              setState(() => choose(selected == entry.key ? null : entry.key)),
        ),
    ],
  );

  Widget picker(String label, VoidCallback onPressed) => FocusBox(
    isTV: widget.isTV,
    onPressed: onPressed,
    radius: 12,
    child: Container(
      constraints: BoxConstraints(minHeight: widget.isTV ? 52 : 48),
      padding: const EdgeInsets.symmetric(horizontal: 14),
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: line),
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
    setState(
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
                  style: uiStyle(context, size: 26, weight: FontWeight.w700),
                ),
              ),
              TextButton(
                key: const Key('filters-reset'),
                onPressed: () => setState(() {
                  format = season = genre = status = null;
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
            'Format',
            chips(_formats, format, (v) => format = v, autofocus: widget.isTV),
          ),
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
            'Status',
            picker(
              _statuses[status] ?? 'Any status',
              () => pick(
                'Status',
                _statuses,
                status,
                'Any status',
                (v) => status = v,
              ),
            ),
          ),
          const SizedBox(height: 24),
          AppButton(
            key: const Key('filters-apply'),
            label: 'Apply filters',
            filled: true,
            isTV: widget.isTV,
            onPressed: () => Navigator.pop(
              context,
              SearchFilters(
                format: format,
                season: season,
                year: year,
                genre: genre,
                status: status,
              ),
            ),
          ),
        ],
      ),
    );
  }
}
