import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../data/models.dart';
import '../data/trakt.dart' show MediaTitle;
import '../services.dart';
import 'components.dart';
import 'movies_pages.dart' show shelvesOf, titleCardsOf;

class DiscoveryHome extends StatefulWidget {
  const DiscoveryHome({
    super.key,
    required this.services,
    required this.isTV,
    required this.hasResume,
    required this.onOpen,
    required this.onSearch,
    required this.onAccounts,
    this.topInset = 0,
    this.showIntro = true,
    this.titles = false,
  });
  final AppServices services;
  final bool isTV, hasResume;

  /// Also browse films and series, for a profile that shows both.
  final bool titles;

  /// False on desktop, where the home hero already features a show.
  final bool showIntro;
  final double topInset;
  final ValueChanged<Anime> onOpen;
  final VoidCallback onSearch, onAccounts;
  @override
  State<DiscoveryHome> createState() => DiscoveryHomeState();
}

class DiscoveryHomeState extends State<DiscoveryHome> {
  HomeData? data;
  Map<String, List<MediaTitle>> titleRows = {};
  String? error;
  bool busy = true;
  int generation = 0;

  @override
  void initState() {
    super.initState();
    unawaited(load());
  }

  Future<void> loadTitles({bool refresh = false}) async {
    if (!widget.titles) return;
    try {
      final rows = await shelvesOf(widget.services).load(refresh: refresh);
      if (mounted) setState(() => titleRows = rows);
    } catch (_) {
      // Anime still shows; the film rows come back on the next refresh.
    }
  }

  @override
  void didUpdateWidget(DiscoveryHome oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.titles && !oldWidget.titles) unawaited(loadTitles());
  }

  Future<void> load({bool refresh = false}) async {
    unawaited(loadTitles(refresh: refresh));
    final request = ++generation;
    setState(() {
      busy = true;
      error = null;
    });
    try {
      if (refresh) widget.services.catalog.clear();
      // Home never shows 18+ titles, whatever the account allows.
      final result = await widget.services.catalog.home(adult: false);
      if (mounted && request == generation) setState(() => data = result);
    } catch (e) {
      if (mounted && request == generation) {
        setState(() => error = e.toString());
      }
    } finally {
      if (mounted && request == generation) setState(() => busy = false);
    }
  }

  /// A row of films or series from Trakt, opened like anime cards.
  Widget titleRow(String name) {
    final items = widget.titles ? titleRows[name] ?? const [] : const [];
    final cards = titleCardsOf(widget.services);
    return PosterRow(
      title: name,
      items: [for (final t in items) cards.card(t)],
      isTV: widget.isTV,
      onOpen: widget.onOpen,
    );
  }

  @override
  Widget build(BuildContext context) {
    bool visible(Anime a) => !a.isAdult;
    final trending = data?.trending.where(visible).toList() ?? <Anime>[];
    final seasonal = data?.season.where(visible).toList() ?? <Anime>[];
    final recent = <String, Airing>{};
    for (final airing in data?.recent ?? <Airing>[]) {
      if (visible(airing.anime)) {
        recent.putIfAbsent(airing.anime.key, () => airing);
      }
    }
    final featured = trending.firstOrNull ?? seasonal.firstOrNull;
    final side = widget.isTV ? 48.0 : 20.0;
    final introduction = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SizedBox(height: widget.topInset),
        Padding(
          padding: EdgeInsets.fromLTRB(
            side,
            widget.hasResume ? 28 : 12,
            side,
            12,
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const PageHeading('Find your next story', '探'),
              const SizedBox(height: 8),
              Text(
                widget.titles
                    ? 'Browse anime, movies and shows, or link AniList, MAL or Trakt to see your lists and sync progress.'
                    : 'Browse anime, or link AniList or MAL to see your lists and sync progress.',
                style: uiStyle(context, color: muted),
              ),
              const SizedBox(height: 12),
              Wrap(
                spacing: 12,
                runSpacing: 4,
                children: [
                  AppButton(
                    label: widget.titles ? 'Search' : 'Search anime',
                    icon: LucideIcons.search,
                    isTV: widget.isTV,
                    autofocus: !widget.hasResume,
                    onPressed: widget.onSearch,
                  ),
                  AppButton(
                    label: 'Accounts',
                    isTV: widget.isTV,
                    onPressed: widget.onAccounts,
                  ),
                ],
              ),
            ],
          ),
        ),
        if (busy && data == null)
          Padding(
            padding: EdgeInsets.symmetric(horizontal: side),
            child: const SkeletonBlock(height: 292),
          ),
        if (featured != null && !widget.hasResume)
          _FeaturedAnime(
            anime: featured,
            isTV: widget.isTV,
            showArtwork: !widget.isTV,
            onOpen: () => widget.onOpen(featured),
          ),
      ],
    );
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (!widget.showIntro)
          const SizedBox.shrink()
        else if (widget.isTV && !widget.hasResume)
          TVHomeHero(
            key: const Key('tv-home-hero'),
            anime: featured,
            child: introduction,
          )
        else
          introduction,
        if (error != null)
          FailureView(
            message: error!,
            onRetry: () => load(refresh: true),
            isTV: widget.isTV,
          ),
        if (!busy && error == null && featured == null && recent.isEmpty)
          Padding(
            padding: EdgeInsets.symmetric(horizontal: side, vertical: 20),
            child: Text(
              'No featured anime right now. Search for a title to start watching.',
              style: uiStyle(context, color: muted),
            ),
          ),
        PosterRow(
          title: 'Trending',
          items: trending,
          isTV: widget.isTV,
          onOpen: widget.onOpen,
        ),
        titleRow('Trending movies'),
        titleRow('Trending shows'),
        PosterRow(
          title: 'Popular this season',
          items: seasonal,
          isTV: widget.isTV,
          onOpen: widget.onOpen,
        ),
        titleRow('Popular movies'),
        titleRow('Popular shows'),
        PosterRow(
          title: 'Recently aired',
          items: recent.values.map((e) => e.anime).toList(),
          isTV: widget.isTV,
          onOpen: widget.onOpen,
          captions: (anime) => 'Episode ${recent[anime.key]!.episode}',
        ),
      ],
    );
  }
}

class _FeaturedAnime extends StatelessWidget {
  const _FeaturedAnime({
    required this.anime,
    required this.isTV,
    required this.onOpen,
    this.showArtwork = true,
  });
  final Anime anime;
  final bool isTV, showArtwork;
  final VoidCallback onOpen;

  @override
  Widget build(BuildContext context) {
    final height = math.max(
      292.0,
      292 * MediaQuery.textScalerOf(context).scale(1),
    );
    return Container(
      key: const Key('discovery-featured'),
      height: height,
      margin: EdgeInsets.symmetric(horizontal: isTV ? 0 : 20),
      child: ClipRRect(
        borderRadius: BorderRadius.circular(isTV ? 0 : 16),
        child: GestureDetector(
          behavior: HitTestBehavior.opaque,
          onTap: onOpen,
          child: Stack(
            fit: StackFit.expand,
            children: [
              if (showArtwork) HeroArt(anime, height: height),
              Align(
                alignment: Alignment.bottomLeft,
                child: Padding(
                  padding: EdgeInsets.fromLTRB(
                    isTV ? 48 : 18,
                    24,
                    isTV ? 300 : 18,
                    20,
                  ),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        'Trending now',
                        style: uiStyle(context, color: muted),
                      ),
                      const SizedBox(height: 8),
                      Text(
                        anime.title,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: uiStyle(
                          context,
                          size: isTV ? 36 : 28,
                          weight: FontWeight.w700,
                          height: 1.2,
                        ),
                      ),
                      const SizedBox(height: 12),
                      AppButton(
                        key: const Key('discovery-open'),
                        label: 'View anime',
                        filled: true,
                        isTV: isTV,
                        onPressed: onOpen,
                      ),
                    ],
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
