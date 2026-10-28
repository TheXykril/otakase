import 'dart:async';

import 'package:flutter/material.dart';

import '../data/models.dart';
import '../services.dart';
import '../player.dart';
import 'components.dart';

class ShowPage extends StatefulWidget {
  const ShowPage({
    super.key,
    required this.services,
    required this.anime,
    required this.isTV,
  });
  final AppServices services;
  final Anime anime;
  final bool isTV;
  @override
  State<ShowPage> createState() => _ShowPageState();
}

class _ShowPageState extends State<ShowPage> {
  late Anime anime;
  late String mode;
  bool loading = true;
  String? error;
  final playFocus = FocusNode(debugLabel: 'Resume or Play');
  @override
  void initState() {
    super.initState();
    anime = widget.anime;
    mode = widget.services.store.text('SubOrDub');
    unawaited(load());
  }

  Future<void> load() async {
    setState(() => loading = true);
    try {
      final detail = await widget.services.catalog.detail(
        widget.anime,
        adult: widget.services.store.flag('AdultContent'),
      );
      if (mounted) {
        setState(() {
          anime = detail;
          error = null;
        });
      }
    } catch (_) {
      if (mounted) {
        setState(
          () =>
              error = 'Show details are unavailable. Your saved data is shown.',
        );
      }
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  @override
  void dispose() {
    playFocus.dispose();
    super.dispose();
  }

  void play(int episode) {
    Navigator.push(
      context,
      MaterialPageRoute<void>(
        builder: (_) => PlayerPage(
          services: widget.services,
          anime: anime,
          episode: episode,
          isTV: widget.isTV,
          mode: mode,
        ),
      ),
    );
  }

  int nextEpisode() {
    final h = widget.services.store.entry(anime);
    if (h == null) return 1;
    if (h.position > 0) return h.episode;
    final next = h.watched.contains(h.episode) ? h.episode + 1 : h.episode;
    return anime.airedEpisodes > 0 ? next.clamp(1, anime.airedEpisodes) : next;
  }

  Json? art(int ep) {
    for (final item in anime.episodeArt) {
      final match = RegExp(
        r'(?:Episode|E)\s*(\d+)',
        caseSensitive: false,
      ).firstMatch(item['title'] as String? ?? '');
      if (int.tryParse(match?.group(1) ?? '') == ep) return item;
    }
    return null;
  }

  Widget header(int last, int position, int watched) => SizedBox(
    height: (MediaQuery.sizeOf(context).height * .64).clamp(400, 600),
    child: Stack(
      fit: StackFit.expand,
      children: [
        CoverImage(
          anime.banner.isEmpty ? anime.cover : anime.banner,
          width: MediaQuery.sizeOf(context).width,
          height: 600,
          banner: true,
        ),
        const DecoratedBox(
          decoration: BoxDecoration(
            gradient: LinearGradient(
              begin: Alignment.topCenter,
              end: Alignment.bottomCenter,
              colors: [Colors.black38, Color(0x90000000), Colors.black],
            ),
          ),
        ),
        Positioned(
          top: 10,
          left: 14,
          child: IconButton(
            tooltip: 'Back',
            onPressed: () => Navigator.pop(context),
            icon: const Icon(Icons.arrow_back),
          ),
        ),
        Align(
          alignment: Alignment.bottomLeft,
          child: Padding(
            padding: EdgeInsets.all(widget.isTV ? 38 : 22),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 850),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    anime.title,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      fontSize: widget.isTV ? 36 : 28,
                      fontWeight: FontWeight.w800,
                      height: 1.2,
                    ),
                  ),
                  const SizedBox(height: 12),
                  MetaLine(anime),
                  const SizedBox(height: 8),
                  Wrap(
                    spacing: 6,
                    children: anime.genres
                        .map(
                          (g) => Text(
                            g,
                            style: const TextStyle(color: muted, fontSize: 12),
                          ),
                        )
                        .toList(),
                  ),
                  if (anime.studio.isNotEmpty)
                    Text(anime.studio, style: const TextStyle(color: muted)),
                  const SizedBox(height: 16),
                  Wrap(
                    spacing: 12,
                    runSpacing: 8,
                    children: [
                      AppButton(
                        key: const Key('show-play'),
                        label: position > 0
                            ? 'Resume E$last · ${Duration(seconds: position).inMinutes}m'
                            : 'Play episode $last',
                        icon: Icons.play_arrow_outlined,
                        filled: true,
                        isTV: widget.isTV,
                        autofocus: true,
                        focusNode: playFocus,
                        onPressed: anime.status == 'NOT_YET_RELEASED'
                            ? null
                            : () => play(last),
                      ),
                      AppButton(
                        label: '$watched watched · saved locally',
                        icon: Icons.bookmark_border,
                        isTV: widget.isTV,
                        onPressed: () => showDialog<void>(
                          context: context,
                          builder: (c) => AlertDialog(
                            title: const Text('Local progress'),
                            content: const Text(
                              'Watched episodes and resume positions are saved on this device. Public tracker lists are read only.',
                            ),
                            actions: [
                              TextButton(
                                onPressed: () => Navigator.pop(c),
                                child: const Text('Back'),
                              ),
                            ],
                          ),
                        ),
                      ),
                    ],
                  ),
                  if (anime.nextAiringAt > 0)
                    Padding(
                      padding: const EdgeInsets.only(top: 12),
                      child: Text(
                        'Episode ${anime.nextEpisode} in ${countdown(anime.nextAiringAt)}',
                        style: const TextStyle(color: shu),
                      ),
                    ),
                ],
              ),
            ),
          ),
        ),
      ],
    ),
  );
  @override
  Widget build(BuildContext context) => Scaffold(
    backgroundColor: Colors.black,
    body: SafeArea(
      child: ListenableBuilder(
        listenable: widget.services.store,
        builder: (context, child) {
          final store = widget.services.store, history = store.entry(anime);
          final count = anime.airedEpisodes, last = nextEpisode();
          final position = history?.episode == last
              ? history?.position ?? 0
              : 0;
          if (!store.visible(anime)) {
            return Center(
              child: AppButton(
                label: 'AdultContent is disabled · Back',
                onPressed: () => Navigator.pop(context),
                isTV: widget.isTV,
              ),
            );
          }
          return CustomScrollView(
            slivers: [
              SliverToBoxAdapter(
                child: header(last, position, history?.watched.length ?? 0),
              ),
              SliverToBoxAdapter(
                child: Padding(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 24,
                    vertical: 14,
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      if (loading) const LinearProgressIndicator(color: shu),
                      if (error != null)
                        Row(
                          children: [
                            Expanded(
                              child: Text(
                                error!,
                                style: const TextStyle(color: muted),
                              ),
                            ),
                            TextButton(
                              onPressed: load,
                              child: const Text('Retry'),
                            ),
                          ],
                        ),
                      Text(
                        anime.description,
                        style: const TextStyle(
                          color: Color(0xffc5c5c5),
                          height: 1.6,
                          fontSize: 13,
                        ),
                      ),
                      const SizedBox(height: 24),
                      Row(
                        children: [
                          const Expanded(
                            child: Text(
                              'Episodes',
                              style: TextStyle(
                                fontSize: 23,
                                fontWeight: FontWeight.w700,
                              ),
                            ),
                          ),
                          for (final value in ['sub', 'dub'])
                            Padding(
                              padding: const EdgeInsets.only(left: 8),
                              child: AppButton(
                                label: value.toUpperCase(),
                                filled: mode == value,
                                isTV: widget.isTV,
                                onPressed: () => setState(() => mode = value),
                              ),
                            ),
                        ],
                      ),
                      const SizedBox(height: 16),
                      if (count == 0)
                        const Text(
                          'Episode availability is checked when you press Play.',
                          style: TextStyle(color: muted),
                        ),
                    ],
                  ),
                ),
              ),
              SliverPadding(
                padding: const EdgeInsets.symmetric(horizontal: 20),
                sliver: SliverList.builder(
                  itemCount: count.clamp(0, 10000),
                  itemBuilder: (context, i) {
                    final ep = i + 1,
                        watched = history?.watched.contains(i + 1) ?? false;
                    final resume = history?.episode == ep
                        ? history?.position ?? 0
                        : 0;
                    return Padding(
                      padding: const EdgeInsets.only(bottom: 10),
                      child: Row(
                        children: [
                          Expanded(
                            child: FocusBox(
                              isTV: widget.isTV,
                              onPressed: () => play(ep),
                              padding: const EdgeInsets.all(8),
                              child: Row(
                                children: [
                                  ClipRRect(
                                    borderRadius: BorderRadius.circular(8),
                                    child: CoverImage(
                                      art(ep)?['thumbnail'] as String? ??
                                          anime.cover,
                                      width: 112,
                                      height: 63,
                                    ),
                                  ),
                                  const SizedBox(width: 14),
                                  Expanded(
                                    child: Column(
                                      crossAxisAlignment:
                                          CrossAxisAlignment.start,
                                      children: [
                                        Text(
                                          art(ep)?['title'] as String? ??
                                              'Episode $ep',
                                          maxLines: 2,
                                          overflow: TextOverflow.ellipsis,
                                        ),
                                        if (resume > 0)
                                          Text(
                                            'Resume at ${Duration(seconds: resume).inMinutes}m ${resume % 60}s',
                                            style: const TextStyle(
                                              color: shu,
                                              fontSize: 12,
                                            ),
                                          ),
                                      ],
                                    ),
                                  ),
                                  const Icon(Icons.play_arrow_outlined),
                                ],
                              ),
                            ),
                          ),
                          IconButton(
                            tooltip: watched
                                ? 'Mark episode $ep unwatched'
                                : 'Mark episode $ep watched',
                            onPressed: () =>
                                store.markWatched(anime, ep, !watched),
                            icon: Icon(
                              watched
                                  ? Icons.check_circle
                                  : Icons.radio_button_unchecked,
                              color: watched ? shu : muted,
                            ),
                          ),
                        ],
                      ),
                    );
                  },
                ),
              ),
              for (final section in {
                'Related': anime.related,
                'Recommended': anime.recommended,
              }.entries)
                if (section.value.where(store.visible).isNotEmpty)
                  SliverToBoxAdapter(
                    child: PosterRow(
                      title: section.key,
                      items: section.value.where(store.visible).toList(),
                      isTV: widget.isTV,
                      onOpen: (a) => Navigator.push(
                        context,
                        MaterialPageRoute<void>(
                          builder: (_) => ShowPage(
                            services: widget.services,
                            anime: a,
                            isTV: widget.isTV,
                          ),
                        ),
                      ),
                    ),
                  ),
              const SliverToBoxAdapter(child: SizedBox(height: 40)),
            ],
          );
        },
      ),
    ),
  );
}
