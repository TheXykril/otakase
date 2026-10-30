import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../data/models.dart';
import '../services.dart';
import 'components.dart';

class DiscoveryHome extends StatefulWidget {
  const DiscoveryHome({
    super.key,
    required this.services,
    required this.isTV,
    required this.hasResume,
    required this.onOpen,
    required this.onSearch,
    required this.onAccounts,
  });
  final AppServices services;
  final bool isTV, hasResume;
  final ValueChanged<Anime> onOpen;
  final VoidCallback onSearch, onAccounts;
  @override
  State<DiscoveryHome> createState() => DiscoveryHomeState();
}

class DiscoveryHomeState extends State<DiscoveryHome> {
  HomeData? data;
  String? error;
  bool busy = true;
  int generation = 0;

  @override
  void initState() {
    super.initState();
    unawaited(load());
  }

  Future<void> load({bool refresh = false}) async {
    final request = ++generation;
    setState(() {
      busy = true;
      error = null;
    });
    try {
      if (refresh) widget.services.catalog.clear();
      final result = await widget.services.catalog.home(
        adult: widget.services.store.adultAllowed,
      );
      if (mounted && request == generation) setState(() => data = result);
    } catch (e) {
      if (mounted && request == generation) {
        setState(() => error = e.toString());
      }
    } finally {
      if (mounted && request == generation) setState(() => busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final visible = widget.services.store.visible;
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
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
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
                'Browse anime, or link AniList or MAL to see your lists and sync progress.',
                style: uiStyle(context, color: muted),
              ),
              const SizedBox(height: 12),
              Wrap(
                spacing: 12,
                runSpacing: 4,
                children: [
                  AppButton(
                    label: 'Search anime',
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
            onOpen: () => widget.onOpen(featured),
          ),
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
        PosterRow(
          title: 'Popular this season',
          items: seasonal,
          isTV: widget.isTV,
          onOpen: widget.onOpen,
        ),
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
  });
  final Anime anime;
  final bool isTV;
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
        child: Stack(
          fit: StackFit.expand,
          children: [
            HeroArt(anime, height: height),
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
                    Text('Trending now', style: uiStyle(context, color: muted)),
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
    );
  }
}
