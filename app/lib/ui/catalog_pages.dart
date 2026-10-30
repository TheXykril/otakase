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

  Future<void> filter(
    String title,
    List<String> values,
    String? selected,
    void Function(String?) setValue,
  ) async {
    final value = await chooseValue(context, title, [
      'Any',
      ...values,
    ], selected: selected ?? 'Any');
    if (value != null && mounted) {
      setValue(value == 'Any' ? null : value);
      await load();
    }
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
      RemoteTextInput(
        isTV: widget.isTV,
        onUp: widget.onNavigateUp,
        builder: (focus) => TextField(
          focusNode: focus,
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
          onSubmitted: (_) {
            debounce?.cancel();
            unawaited(load());
            if (widget.isTV) focus.nextFocus();
          },
          onChanged: (_) {
            debounce?.cancel();
            debounce = Timer(const Duration(milliseconds: 450), () => load());
          },
        ),
      ),
      const SizedBox(height: 14),
      Wrap(
        spacing: 8,
        runSpacing: 8,
        children: [
          AppTab(
            selected: format != null,
            label: format ?? 'Format',
            onPressed: () => filter(
              'Format',
              ['TV', 'MOVIE', 'ONA', 'OVA', 'SPECIAL', 'TV_SHORT', 'MUSIC'],
              format,
              (v) => format = v,
            ),
            isTV: widget.isTV,
          ),
          AppTab(
            selected: season != null,
            label: season ?? 'Season',
            onPressed: () => filter(
              'Season',
              ['WINTER', 'SPRING', 'SUMMER', 'FALL'],
              season,
              (v) => season = v,
            ),
            isTV: widget.isTV,
          ),
          AppTab(
            selected: year != null,
            label: year?.toString() ?? 'Year',
            onPressed: () => filter(
              'Year',
              List.generate(50, (i) => '${DateTime.now().year + 1 - i}'),
              year?.toString(),
              (v) => year = int.tryParse(v ?? ''),
            ),
            isTV: widget.isTV,
          ),
          AppTab(
            selected: genre != null,
            label: genre ?? 'Genre',
            onPressed: () => filter('Genre', genres, genre, (v) => genre = v),
            isTV: widget.isTV,
          ),
          AppTab(
            selected: status != null,
            label: status ?? 'Status',
            onPressed: () => filter(
              'Status',
              [
                'RELEASING',
                'FINISHED',
                'NOT_YET_RELEASED',
                'HIATUS',
                'CANCELLED',
              ],
              status,
              (v) => status = v,
            ),
            isTV: widget.isTV,
          ),
        ],
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
