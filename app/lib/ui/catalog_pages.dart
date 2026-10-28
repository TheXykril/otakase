import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../data/models.dart';
import '../services.dart';
import 'components.dart';

class HomePage extends StatefulWidget {
  const HomePage({
    super.key,
    required this.services,
    required this.isTV,
    required this.onOpen,
  });
  final AppServices services;
  final bool isTV;
  final void Function(Anime) onOpen;
  @override
  State<HomePage> createState() => _HomePageState();
}

class _HomePageState extends State<HomePage> {
  late Future<HomeData> future;
  @override
  void initState() {
    super.initState();
    load();
  }

  void load() {
    future = widget.services.catalog.home(
      adult: widget.services.store.flag('AdultContent'),
    );
  }

  @override
  Widget build(BuildContext context) => FutureBuilder<HomeData>(
    future: future,
    builder: (context, snapshot) {
      if (snapshot.hasError) {
        return FailureView(
          message: snapshot.error.toString(),
          onRetry: () => setState(load),
          isTV: widget.isTV,
        );
      }
      if (!snapshot.hasData) {
        return const PageSkeleton(hero: true);
      }
      final data = snapshot.data!;
      final store = widget.services.store;
      final continuing = store.continuing;
      List<Anime> unique(List<Airing> entries) =>
          {for (final e in entries) e.anime.key: e.anime}.values.toList();
      return ListView(
        key: const PageStorageKey('home-scroll'),
        padding: EdgeInsets.only(bottom: contentBottom(context)),
        children: [
          if (data.trending.isEmpty &&
              data.season.isEmpty &&
              data.recent.isEmpty &&
              data.today.isEmpty &&
              continuing.isEmpty)
            const EmptyState(message: 'Tonight is still taking shape.'),
          if (data.trending.isNotEmpty)
            HeroCarousel(
              items: data.trending.take(7).toList(),
              isTV: widget.isTV,
              onOpen: widget.onOpen,
            ),
          PosterRow(
            title: 'Continue watching',
            landscape: true,
            items: continuing.map((h) => h.anime).toList(),
            isTV: widget.isTV,
            onOpen: widget.onOpen,
            progress: (a) {
              final e = store.entry(a)!;
              return e.duration > 0 ? e.position / e.duration : 0;
            },
            captions: (a) {
              final e = store.entry(a)!;
              return 'EP ${e.episode} · ${((e.duration - e.position).clamp(0, e.duration) / 60).ceil()} min left';
            },
          ),
          PosterRow(
            title: 'Recently released',
            items: unique(data.recent),
            isTV: widget.isTV,
            onOpen: widget.onOpen,
            captions: (a) {
              final e = data.recent.firstWhere((v) => v.anime.key == a.key);
              return widget.isTV ? 'Episode ${e.episode}' : null;
            },
          ),
          PosterRow(
            title: 'Trending',
            items: data.trending,
            isTV: widget.isTV,
            onOpen: widget.onOpen,
          ),
          PosterRow(
            title: 'Popular this season',
            items: data.season,
            isTV: widget.isTV,
            onOpen: widget.onOpen,
          ),
          PosterRow(
            title: 'Airing today',
            items: unique(data.today),
            isTV: widget.isTV,
            onOpen: widget.onOpen,
            captions: (a) {
              final e = data.today.firstWhere((v) => v.anime.key == a.key);
              return 'E${e.episode} · ${clockTime(e.at)}';
            },
          ),
        ],
      );
    },
  );
}

class HeroCarousel extends StatefulWidget {
  const HeroCarousel({
    super.key,
    required this.items,
    required this.isTV,
    required this.onOpen,
  });
  final List<Anime> items;
  final bool isTV;
  final void Function(Anime) onOpen;
  @override
  State<HeroCarousel> createState() => _HeroCarouselState();
}

class _HeroCarouselState extends State<HeroCarousel> {
  Timer? timer;
  int index = 0;
  @override
  void initState() {
    super.initState();
    timer = Timer.periodic(const Duration(seconds: 8), (_) {
      if (mounted &&
          ModalRoute.of(context)?.isCurrent == true &&
          !MediaQuery.disableAnimationsOf(context)) {
        move(1);
      }
    });
  }

  void move(int delta) {
    setState(() => index = (index + delta) % widget.items.length);
  }

  @override
  void dispose() {
    timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final height = (MediaQuery.sizeOf(context).height * .58).clamp(
          widget.isTV ? 460.0 : 500.0,
          widget.isTV ? 640.0 : 620.0,
        ),
        anime = widget.items[index];
    return Focus(
      skipTraversal: true,
      onKeyEvent: (_, event) {
        if (widget.isTV && event is KeyDownEvent) {
          if (event.logicalKey == LogicalKeyboardKey.arrowRight) {
            move(1);
            return KeyEventResult.handled;
          }
          if (event.logicalKey == LogicalKeyboardKey.arrowLeft) {
            move(-1);
            return KeyEventResult.handled;
          }
        }
        return KeyEventResult.ignored;
      },
      child: GestureDetector(
        onHorizontalDragEnd: (d) {
          if ((d.primaryVelocity ?? 0).abs() > 100) {
            move(d.primaryVelocity! < 0 ? 1 : -1);
          }
        },
        child: SizedBox(
          height: height,
          child: Stack(
            fit: StackFit.expand,
            children: [
              AnimatedSwitcher(
                duration: const Duration(milliseconds: 300),
                child: TweenAnimationBuilder<double>(
                  key: ValueKey(anime.key),
                  tween: Tween(
                    begin: 1,
                    end: MediaQuery.disableAnimationsOf(context) ? 1 : 1.03,
                  ),
                  duration: const Duration(seconds: 8),
                  builder: (context, value, child) =>
                      Transform.scale(scale: value, child: child),
                  child: HeroArt(anime, height: height),
                ),
              ),
              Positioned(
                right: 24,
                top: 12,
                child: Text(
                  '今',
                  style: TextStyle(
                    fontFamily: 'NotoSerifJP',
                    fontSize: 48,
                    fontWeight: FontWeight.w700,
                    color: fg.withValues(alpha: .1),
                  ),
                ),
              ),
              Align(
                alignment: Alignment.bottomLeft,
                child: Padding(
                  padding: EdgeInsets.fromLTRB(
                    widget.isTV ? 48 : 24,
                    24,
                    24,
                    52,
                  ),
                  child: ConstrainedBox(
                    constraints: BoxConstraints(
                      maxWidth: widget.isTV ? 600 : 480,
                    ),
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        AnimatedSwitcher(
                          duration: const Duration(milliseconds: 300),
                          child: Align(
                            key: ValueKey(anime.key),
                            alignment: Alignment.centerLeft,
                            child: Text(
                              anime.title,
                              maxLines: widget.isTV ? 2 : 3,
                              overflow: TextOverflow.ellipsis,
                              style: display.copyWith(
                                fontSize: widget.isTV
                                    ? (MediaQuery.sizeOf(context).height < 600
                                          ? 34
                                          : 42)
                                    : 32,
                                letterSpacing: -.7,
                              ),
                            ),
                          ),
                        ),
                        const SizedBox(height: 12),
                        Text(
                          anime.description,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          style: prose.copyWith(
                            fontSize: widget.isTV ? 20 : 16,
                            color: fg,
                          ),
                        ),
                        const SizedBox(height: 16),
                        MetaLine(anime),
                        const SizedBox(height: 16),
                        Wrap(
                          spacing: 12,
                          children: [
                            AppButton(
                              key: ValueKey('hero-watch-${anime.id}'),
                              label: 'Watch now',
                              icon: LucideIcons.play,
                              filled: true,
                              isTV: widget.isTV,
                              onPressed: () => widget.onOpen(anime),
                            ),
                            AppButton(
                              label: 'Details',
                              isTV: widget.isTV,
                              onPressed: () => widget.onOpen(anime),
                            ),
                          ],
                        ),
                      ],
                    ),
                  ),
                ),
              ),
              Positioned(
                bottom: 16,
                right: 24,
                child: Row(
                  children: List.generate(
                    widget.items.length,
                    (i) => Semantics(
                      label: 'Hero page ${i + 1}',
                      button: true,
                      child: GestureDetector(
                        onTap: () => setState(() => index = i),
                        child: Container(
                          width: i == index ? 22 : 5,
                          height: 6,
                          margin: const EdgeInsets.all(4),
                          decoration: BoxDecoration(
                            color: i == index
                                ? shu
                                : muted.withValues(alpha: .4),
                            borderRadius: BorderRadius.circular(999),
                          ),
                        ),
                      ),
                    ),
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class SearchPage extends StatefulWidget {
  const SearchPage({
    super.key,
    required this.services,
    required this.isTV,
    required this.onOpen,
    required this.onVoice,
    this.initialQuery = '',
    this.initialFormat,
  });
  final AppServices services;
  final bool isTV;
  final void Function(Anime) onOpen;
  final Future<String?> Function() onVoice;
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
  bool busy = true, hasMore = false;
  List<String> genres = [];
  @override
  void initState() {
    super.initState();
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
      widget.isTV ? 48 : 24,
      20,
      widget.isTV ? 48 : 24,
      contentBottom(context),
    ),
    children: [
      const PageHeading('Search', '探'),
      const SizedBox(height: 18),
      TextField(
        key: const Key('catalog-search'),
        controller: query,
        decoration: InputDecoration(
          hintText: 'Search anime',
          prefixIcon: const Icon(LucideIcons.search),
          suffixIcon: widget.isTV
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
        onSubmitted: (_) => load(),
        onChanged: (_) {
          debounce?.cancel();
          debounce = Timer(const Duration(milliseconds: 450), () => load());
        },
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
        const EmptyState(message: 'The next story is waiting to be found.'),
      LayoutBuilder(
        builder: (context, box) {
          final columns = (box.maxWidth / (widget.isTV ? 175 : 130))
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
              mainAxisExtent: width * 1.5 + (widget.isTV ? 24 : 108),
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

class MyListPage extends StatelessWidget {
  const MyListPage({
    super.key,
    required this.services,
    required this.isTV,
    required this.onOpen,
  });
  final AppServices services;
  final bool isTV;
  final void Function(Anime) onOpen;
  @override
  Widget build(BuildContext context) => ListView(
    key: const PageStorageKey('mylist-scroll'),
    padding: EdgeInsets.only(top: 16, bottom: contentBottom(context)),
    children: [
      Padding(
        padding: EdgeInsets.symmetric(horizontal: isTV ? 48 : 24),
        child: const PageHeading('MyList', '録'),
      ),
      for (final service in ['AniList', 'MAL'])
        AccountBlock(
          key: ValueKey(
            '$service-${services.store.text(service == 'MAL' ? 'MyAnimeListUsername' : 'AniListUsername')}',
          ),
          service: service,
          services: services,
          isTV: isTV,
          onOpen: onOpen,
        ),
    ],
  );
}

class AccountBlock extends StatefulWidget {
  const AccountBlock({
    super.key,
    required this.service,
    required this.services,
    required this.isTV,
    required this.onOpen,
  });
  final String service;
  final AppServices services;
  final bool isTV;
  final void Function(Anime) onOpen;
  @override
  State<AccountBlock> createState() => _AccountBlockState();
}

class _AccountBlockState extends State<AccountBlock> {
  final usernameInput = TextEditingController();
  String status = '';
  List<ListEntry> items = [];
  String? error;
  bool busy = false, more = false;
  int page = 1, generation = 0;
  String get setting =>
      widget.service == 'MAL' ? 'MyAnimeListUsername' : 'AniListUsername';
  String get username => widget.services.store.text(setting).trim();
  Map<String, String> get statuses => widget.service == 'MAL'
      ? {
          'watching': 'Watching',
          'plan_to_watch': 'Planning',
          'completed': 'Completed',
          'dropped': 'Dropped',
          'on_hold': 'On Hold',
        }
      : {
          'CURRENT': 'Current',
          'PLANNING': 'Planning',
          'COMPLETED': 'Completed',
          'DROPPED': 'Dropped',
          'PAUSED': 'Paused',
          'REPEATING': 'Repeating',
        };
  @override
  void initState() {
    super.initState();
    status = statuses.keys.first;
    if (username.isNotEmpty) unawaited(load());
  }

  @override
  void dispose() {
    usernameInput.dispose();
    super.dispose();
  }

  Future<void> add() async {
    final name = usernameInput.text.trim();
    if (name.isEmpty) return;
    await widget.services.store.set(setting, name);
  }

  Future<void> load({bool append = false}) async {
    final g = ++generation;
    setState(() {
      busy = true;
      error = null;
      if (!append) {
        page = 1;
        items = [];
      }
    });
    try {
      final data = await widget.services.catalog.publicList(
        widget.service,
        username,
        status,
        adult: widget.services.store.flag('AdultContent'),
        page: page,
      );
      if (mounted && g == generation) {
        setState(() {
          items = append ? [...items, ...data.entries] : data.entries;
          more = data.hasMore;
          if (widget.service == 'AniList') {
            widget.services.store.avatar = data.avatar;
          }
        });
      }
    } catch (e) {
      if (mounted && g == generation) setState(() => error = e.toString());
    } finally {
      if (mounted && g == generation) setState(() => busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(top: 24, bottom: 24),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Padding(
          padding: EdgeInsets.symmetric(horizontal: widget.isTV ? 48 : 24),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Divider(),
              const SizedBox(height: 20),
              SectionLabel(widget.service),
              if (username.isNotEmpty)
                Padding(
                  padding: const EdgeInsets.only(top: 8),
                  child: Text(
                    username,
                    style: uiStyle(context, size: 24, weight: FontWeight.w600),
                  ),
                ),
              if (username.isEmpty) ...[
                const SizedBox(height: 16),
                TextField(
                  key: ValueKey('username-${widget.service}'),
                  controller: usernameInput,
                  style: uiStyle(context),
                  decoration: InputDecoration(
                    hintText: 'Add ${widget.service} username',
                    suffixIcon: IconButton(
                      tooltip: 'Add ${widget.service} username',
                      onPressed: add,
                      icon: const Icon(LucideIcons.arrowRight, size: 18),
                    ),
                  ),
                  textInputAction: TextInputAction.done,
                  onSubmitted: (_) => add(),
                ),
                const SizedBox(height: 12),
                Text(
                  'Read your public list. Progress stays on this device.',
                  style: uiStyle(context, color: muted),
                ),
              ],
            ],
          ),
        ),
        if (username.isNotEmpty) ...[
          const SizedBox(height: 16),
          SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            padding: EdgeInsets.symmetric(horizontal: widget.isTV ? 42 : 18),
            child: Row(
              children: statuses.entries
                  .map(
                    (e) => Padding(
                      padding: const EdgeInsets.only(right: 8),
                      child: AppTab(
                        label: e.value,
                        selected: status == e.key,
                        isTV: widget.isTV,
                        onPressed: () {
                          status = e.key;
                          unawaited(load());
                        },
                      ),
                    ),
                  )
                  .toList(),
            ),
          ),
          if (busy)
            const Padding(
              padding: EdgeInsets.all(24),
              child: SkeletonBlock(height: 180),
            ),
          if (error != null)
            FailureView(
              message: error!,
              onRetry: () => load(),
              isTV: widget.isTV,
            ),
          if (!busy && items.isEmpty && error == null)
            const EmptyState(message: 'A little room for the next story.'),
          if (items.isNotEmpty)
            PosterRow(
              title: '${items.length} titles',
              items: items.map((e) => e.anime).toList(),
              isTV: widget.isTV,
              onOpen: widget.onOpen,
              progress: (a) {
                final e = items.firstWhere((e) => e.anime.key == a.key);
                return a.episodes > 0 ? e.progress / a.episodes : 0;
              },
              captions: (a) {
                final e = items.firstWhere((e) => e.anime.key == a.key);
                return a.nextAiringAt > 0
                    ? 'E${a.nextEpisode} in ${countdown(a.nextAiringAt)}'
                    : '${a.title} · ${e.progress}/${a.episodes > 0 ? a.episodes : '?'}';
              },
            ),
          if (more)
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 24),
              child: AppButton(
                label: 'Load more',
                onPressed: busy
                    ? null
                    : () {
                        page++;
                        unawaited(load(append: true));
                      },
                isTV: widget.isTV,
              ),
            ),
        ],
      ],
    ),
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
        widget.isTV ? 48 : 24,
        20,
        widget.isTV ? 48 : 24,
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
