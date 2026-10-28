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
        return const Center(child: CircularProgressIndicator(color: shu));
      }
      final data = snapshot.data!;
      final store = widget.services.store;
      final continuing = store.continuing;
      List<Anime> unique(List<Airing> entries) =>
          {for (final e in entries) e.anime.key: e.anime}.values.toList();
      return ListView(
        key: const PageStorageKey('home-scroll'),
        padding: EdgeInsets.only(bottom: widget.isTV ? 36 : 110),
        children: [
          if (data.trending.isNotEmpty)
            HeroCarousel(
              items: data.trending.take(7).toList(),
              isTV: widget.isTV,
              onOpen: widget.onOpen,
            ),
          PosterRow(
            title: 'Continue Watching',
            items: continuing.map((h) => h.anime).toList(),
            isTV: widget.isTV,
            onOpen: widget.onOpen,
            progress: (a) {
              final e = store.entry(a)!;
              return e.duration > 0 ? e.position / e.duration : 0;
            },
            captions: (a) {
              final e = store.entry(a)!;
              return '${a.title} · E${e.episode}';
            },
          ),
          PosterRow(
            title: 'Recently Released',
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
  final controller = PageController();
  Timer? timer;
  int index = 0;
  @override
  void initState() {
    super.initState();
    timer = Timer.periodic(const Duration(seconds: 8), (_) {
      if (mounted && ModalRoute.of(context)?.isCurrent == true) move(1);
    });
  }

  void move(int delta) {
    if (controller.hasClients) {
      unawaited(
        controller.animateToPage(
          (index + delta) % widget.items.length,
          duration: const Duration(milliseconds: 350),
          curve: Curves.easeOut,
        ),
      );
    }
  }

  @override
  void dispose() {
    timer?.cancel();
    controller.dispose();
    super.dispose();
  }

  Widget slide(Anime anime, double height) => Stack(
    fit: StackFit.expand,
    children: [
      CoverImage(
        anime.banner.isEmpty ? anime.cover : anime.banner,
        width: MediaQuery.sizeOf(context).width,
        height: height,
        banner: true,
      ),
      const DecoratedBox(
        decoration: BoxDecoration(
          gradient: LinearGradient(
            colors: [Colors.black, Color(0xb0000000), Color(0x10000000)],
            stops: [0, .45, 1],
          ),
        ),
      ),
      const DecoratedBox(
        decoration: BoxDecoration(
          gradient: LinearGradient(
            begin: Alignment.topCenter,
            end: Alignment.bottomCenter,
            colors: [Colors.transparent, Color(0x30000000), Colors.black],
            stops: [0, .65, 1],
          ),
        ),
      ),
      Align(
        alignment: widget.isTV ? Alignment.centerLeft : Alignment.bottomLeft,
        child: Padding(
          padding: EdgeInsets.fromLTRB(widget.isTV ? 42 : 22, 20, 20, 40),
          child: ConstrainedBox(
            constraints: BoxConstraints(maxWidth: widget.isTV ? 560 : 480),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  anime.title,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(
                    fontSize: widget.isTV ? 38 : 30,
                    height: 1.16,
                    fontWeight: FontWeight.w800,
                  ),
                ),
                const SizedBox(height: 12),
                Text(
                  anime.description,
                  maxLines: widget.isTV ? 3 : 4,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    color: Color(0xffd0d0d0),
                    height: 1.5,
                    fontSize: 13,
                  ),
                ),
                const SizedBox(height: 16),
                AppButton(
                  key: ValueKey('hero-watch-${anime.id}'),
                  label: 'Watch Now',
                  icon: Icons.play_arrow_outlined,
                  filled: true,
                  isTV: widget.isTV,
                  onPressed: () => widget.onOpen(anime),
                ),
                const SizedBox(height: 13),
                MetaLine(anime),
              ],
            ),
          ),
        ),
      ),
    ],
  );
  @override
  Widget build(BuildContext context) {
    final height = (MediaQuery.sizeOf(context).height * .6).clamp(
      widget.isTV ? 330.0 : 360.0,
      widget.isTV ? 620.0 : 500.0,
    );
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
      child: SizedBox(
        height: height,
        child: Stack(
          children: [
            PageView.builder(
              controller: controller,
              itemCount: widget.items.length,
              onPageChanged: (value) => setState(() => index = value),
              itemBuilder: (context, i) => slide(widget.items[i], height),
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
                      onTap: () => controller.animateToPage(
                        i,
                        duration: const Duration(milliseconds: 250),
                        curve: Curves.easeOut,
                      ),
                      child: Container(
                        width: i == index ? 20 : 6,
                        height: 6,
                        margin: const EdgeInsets.all(4),
                        decoration: BoxDecoration(
                          color: i == index ? shu : Colors.white38,
                          borderRadius: BorderRadius.circular(6),
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
      widget.isTV ? 38 : 20,
      20,
      widget.isTV ? 38 : 20,
      110,
    ),
    children: [
      const Text(
        'Search',
        style: TextStyle(fontSize: 28, fontWeight: FontWeight.w700),
      ),
      const SizedBox(height: 18),
      TextField(
        key: const Key('catalog-search'),
        controller: query,
        decoration: InputDecoration(
          hintText: 'Search anime',
          prefixIcon: const Icon(Icons.search),
          suffixIcon: widget.isTV
              ? IconButton(
                  tooltip: 'Voice search',
                  icon: const Icon(Icons.mic_none),
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
          AppButton(
            label: format ?? 'Format',
            onPressed: () => filter(
              'Format',
              ['TV', 'MOVIE', 'ONA', 'OVA', 'SPECIAL', 'TV_SHORT', 'MUSIC'],
              format,
              (v) => format = v,
            ),
            isTV: widget.isTV,
          ),
          AppButton(
            label: season ?? 'Season',
            onPressed: () => filter(
              'Season',
              ['WINTER', 'SPRING', 'SUMMER', 'FALL'],
              season,
              (v) => season = v,
            ),
            isTV: widget.isTV,
          ),
          AppButton(
            label: year?.toString() ?? 'Year',
            onPressed: () => filter(
              'Year',
              List.generate(50, (i) => '${DateTime.now().year + 1 - i}'),
              year?.toString(),
              (v) => year = int.tryParse(v ?? ''),
            ),
            isTV: widget.isTV,
          ),
          AppButton(
            label: genre ?? 'Genre',
            onPressed: () => filter('Genre', genres, genre, (v) => genre = v),
            isTV: widget.isTV,
          ),
          AppButton(
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
      if (busy) const LinearProgressIndicator(color: shu),
      if (error != null)
        FailureView(message: error!, onRetry: () => load(), isTV: widget.isTV),
      if (!busy && items.isEmpty && error == null)
        const Padding(
          padding: EdgeInsets.all(40),
          child: Center(
            child: Text('Nothing here', style: TextStyle(color: muted)),
          ),
        ),
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
              mainAxisExtent: width * 1.5 + (widget.isTV ? 8 : 48),
            ),
            itemBuilder: (_, i) => PosterCard(
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
    required this.onSettings,
  });
  final AppServices services;
  final bool isTV;
  final void Function(Anime) onOpen;
  final VoidCallback onSettings;
  @override
  Widget build(BuildContext context) => ListView(
    padding: const EdgeInsets.only(top: 18, bottom: 120),
    children: [
      const Padding(
        padding: EdgeInsets.symmetric(horizontal: 28),
        child: Text(
          'MyList',
          style: TextStyle(fontSize: 28, fontWeight: FontWeight.w700),
        ),
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
          onSettings: onSettings,
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
    required this.onSettings,
  });
  final String service;
  final AppServices services;
  final bool isTV;
  final void Function(Anime) onOpen;
  final VoidCallback onSettings;
  @override
  State<AccountBlock> createState() => _AccountBlockState();
}

class _AccountBlockState extends State<AccountBlock> {
  String status = '';
  List<ListEntry> items = [];
  String? error;
  bool busy = false, more = false;
  int page = 1, generation = 0;
  String get username => widget.services.store
      .text(widget.service == 'MAL' ? 'MyAnimeListUsername' : 'AniListUsername')
      .trim();
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
    padding: const EdgeInsets.only(top: 28, bottom: 16),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 28),
          child: Text(
            '${widget.service}${username.isEmpty ? '' : '  $username'}',
            style: const TextStyle(fontSize: 22, fontWeight: FontWeight.w700),
          ),
        ),
        const SizedBox(height: 14),
        if (username.isEmpty)
          Padding(
            padding: const EdgeInsets.all(28),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text(
                  'Connect a public list with your username.',
                  style: TextStyle(color: muted),
                ),
                const SizedBox(height: 12),
                AppButton(
                  label: 'Connect ${widget.service}',
                  icon: Icons.person_outline,
                  onPressed: widget.onSettings,
                  isTV: widget.isTV,
                ),
              ],
            ),
          )
        else ...[
          SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            padding: const EdgeInsets.symmetric(horizontal: 22),
            child: Row(
              children: statuses.entries
                  .map(
                    (e) => Padding(
                      padding: const EdgeInsets.only(right: 7),
                      child: AppButton(
                        label: e.value,
                        filled: status == e.key,
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
              padding: EdgeInsets.all(22),
              child: LinearProgressIndicator(color: shu),
            ),
          if (error != null)
            FailureView(
              message: error!,
              onRetry: () => load(),
              isTV: widget.isTV,
            ),
          if (!busy && items.isEmpty && error == null)
            const Padding(
              padding: EdgeInsets.all(50),
              child: Center(
                child: Text('Nothing here', style: TextStyle(color: muted)),
              ),
            ),
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
                    : '${e.progress} / ${a.episodes > 0 ? a.episodes : '?'} watched';
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
        widget.isTV ? 38 : 20,
        20,
        widget.isTV ? 38 : 20,
        110,
      ),
      children: [
        const Text(
          'Schedule',
          style: TextStyle(fontSize: 28, fontWeight: FontWeight.w700),
        ),
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
                child: AppButton(
                  label: '${names[i]} ${date.day}',
                  filled: date == selected,
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
        if (busy) const LinearProgressIndicator(color: shu),
        if (error != null)
          FailureView(
            message: error!,
            onRetry: () => load(),
            isTV: widget.isTV,
          ),
        if (!busy && rows.isEmpty && error == null)
          const Padding(
            padding: EdgeInsets.all(50),
            child: Center(
              child: Text('Nothing here', style: TextStyle(color: muted)),
            ),
          ),
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
                    width: 64,
                    child: Text(
                      clockTime(row.at),
                      style: const TextStyle(
                        color: shu,
                        fontWeight: FontWeight.bold,
                      ),
                    ),
                  ),
                  ClipRRect(
                    borderRadius: BorderRadius.circular(7),
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
                          style: const TextStyle(
                            fontSize: 16,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                        const SizedBox(height: 6),
                        Text(
                          'Episode ${row.episode}',
                          style: const TextStyle(color: muted),
                        ),
                      ],
                    ),
                  ),
                  const Icon(Icons.chevron_right, color: muted),
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
