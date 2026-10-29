import 'dart:async';
import 'dart:math';

import 'package:flutter/material.dart';

import '../data/models.dart';
import '../data/episode_metadata.dart';
import '../data/synced_resume.dart';
import '../data/tracking.dart';
import '../services.dart';
import '../player.dart';
import 'app_shell.dart' show Brand;
import 'components.dart';
import 'device_status.dart';
import 'home_page.dart';
import 'status_editor.dart';
import 'progress_thread.dart';

class ShowPage extends StatefulWidget {
  const ShowPage({
    super.key,
    required this.services,
    required this.anime,
    required this.isTV,
    this.heroTag,
  });
  final AppServices services;
  final Anime anime;
  final bool isTV;
  final Object? heroTag;
  @override
  State<ShowPage> createState() => _ShowPageState();
}

class _ShowPageState extends State<ShowPage> {
  late Anime anime;
  late String mode;
  bool loading = true;
  String? error;
  SyncedResume remote = noResume;
  final playFocus = FocusNode(debugLabel: 'Resume or Play');
  @override
  void initState() {
    super.initState();
    anime = widget.anime;
    mode = widget.services.store.text('SubOrDub');
    unawaited(load());
  }

  Future<void> load() async {
    setState(() {
      loading = true;
      error = null;
    });
    try {
      final a = await widget.services.catalog.detail(
        widget.anime,
        adult: widget.services.store.flag('AdultContent'),
      );
      if (mounted) setState(() => anime = a);
    } catch (_) {
      if (mounted) {
        setState(
          () =>
              error = 'Show details are unavailable. Your saved data is shown.',
        );
      }
    }
    final p = await widget.services.tracking.resume(anime);
    if (mounted) {
      setState(() {
        remote = p;
        loading = false;
      });
    }
  }

  @override
  void dispose() {
    playFocus.dispose();
    super.dispose();
  }

  Future<void> play(int ep) => Navigator.push(
    context,
    OtakaseRoute<void>(
      builder: (_) => PlayerPage(
        services: widget.services,
        anime: anime,
        episode: ep,
        isTV: widget.isTV,
        mode: mode,
      ),
    ),
  );
  int get nextEpisode {
    final h = widget.services.store.entry(anime);
    var ep = h == null
        ? widget.services.tracking.progress(anime) + 1
        : h.watched.contains(h.episode)
        ? h.episode + 1
        : h.episode;
    if (remote.valid && remote.episode > ep) ep = remote.episode;
    return anime.airedEpisodes > 0
        ? ep.clamp(1, anime.airedEpisodes)
        : max(1, ep);
  }

  Json? art(int ep) {
    for (final e in anime.episodeArt) {
      if (episodeNumber(e) == ep) return e;
    }
    return null;
  }

  int get watched => max(
    widget.services.store.entry(anime)?.watched.length ?? 0,
    widget.services.tracking.progress(anime),
  );
  bool get started {
    final h = widget.services.store.entry(anime);
    return (h?.position ?? 0) > 0 ||
        (h?.watched.isNotEmpty ?? false) ||
        widget.services.tracking.progress(anime) > 0 ||
        remote.valid;
  }

  Widget actions(int last, int position) {
    final entry = widget.services.tracking.mergedCached(anime);
    final label = entry == null
        ? (widget.services.auth.hasAccounts ? 'Add to list' : 'Local progress')
        : (anilistStatuses[entry.status] ??
              malStatuses[entry.status] ??
              'List status');
    final buttons = [
      AppButton(
        key: const Key('show-play'),
        label: position > 0 ? 'Resume episode $last' : 'Play episode $last',
        icon: LucideIcons.play,
        filled: true,
        isTV: widget.isTV,
        autofocus: true,
        focusNode: playFocus,
        onPressed: anime.status == 'NOT_YET_RELEASED' ? null : () => play(last),
      ),
      AppButton(
        key: const Key('show-status'),
        label: label,
        isTV: widget.isTV,
        onPressed: () =>
            editListStatus(context, widget.services, anime, widget.isTV),
      ),
    ];
    return widget.isTV
        ? Wrap(spacing: 12, runSpacing: 8, children: buttons)
        : Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [buttons.first, const SizedBox(height: 6), buttons.last],
          );
  }

  Widget header(int last, int position) {
    final tv = widget.isTV;
    final scale = MediaQuery.textScalerOf(context).scale(1);
    final height = (tv ? 310.0 : 346.0) * max(1.0, scale);
    final titleStyle = anime.title.length > 38
        ? uiStyle(
            context,
            size: tv ? 36 : 28,
            weight: FontWeight.w700,
            height: 1.22,
          )
        : display.copyWith(fontSize: tv ? 45 : 36, height: tv ? 48 / 45 : 1);
    return SizedBox(
      height: height,
      child: Stack(
        fit: StackFit.expand,
        children: [
          if (widget.heroTag != null)
            Hero(
              tag: widget.heroTag!,
              child: HeroArt(anime, height: height),
            )
          else
            HeroArt(anime, height: height),
          Positioned(
            top: tv ? 0 : max(25, MediaQuery.viewPaddingOf(context).top),
            left: tv ? 41 : 14,
            child: IconButton(
              tooltip: 'Back',
              onPressed: () => Navigator.pop(context),
              icon: const Icon(LucideIcons.arrowLeft),
            ),
          ),
          if (!tv)
            Positioned(
              top: max(25, MediaQuery.viewPaddingOf(context).top),
              right: 20,
              child: Container(
                color: bgDeep.withValues(alpha: .95),
                padding: const EdgeInsets.all(8),
                child: const DeviceStatus(),
              ),
            ),
          if (anime.nativeTitle.isNotEmpty)
            Positioned(
              right: tv ? 48 : 12,
              top: tv ? 16 : 80,
              child: NativeTitle(anime.nativeTitle, isTV: tv),
            ),
          if (tv)
            Positioned(
              right: 110,
              bottom: 36,
              child: Container(
                padding: const EdgeInsets.symmetric(
                  horizontal: 10,
                  vertical: 2,
                ),
                color: bgDeep.withValues(alpha: .9),
                child: Text(
                  last.toString().padLeft(2, '0'),
                  style: display.copyWith(fontSize: 54),
                ),
              ),
            ),
          Align(
            alignment: Alignment.bottomLeft,
            child: Padding(
              padding: EdgeInsets.fromLTRB(
                tv ? 48 : 22,
                48,
                tv ? 350 : 48,
                tv ? 16 : 24,
              ),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    [
                      if (anime.studio.isNotEmpty) anime.studio,
                      if (anime.year > 0) '${anime.year}',
                      if (anime.episodes > 0) '${anime.episodes} episodes',
                    ].join(' · '),
                    style: uiStyle(context, size: 15, color: muted),
                  ),
                  const SizedBox(height: 8),
                  Text(
                    anime.title,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: titleStyle,
                  ),
                  if (tv) ...[
                    const SizedBox(height: 8),
                    Text(
                      anime.description,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: uiStyle(context),
                    ),
                    const SizedBox(height: 8),
                    actions(last, position),
                  ],
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget episodeRow(int ep, {bool card = false}) {
    final h = widget.services.store.entry(anime);
    final seen =
        (h?.watched.contains(ep) ?? false) ||
        ep <= widget.services.tracking.progress(anime);
    final seconds = seen
        ? 0
        : max(h?.episode == ep ? h?.position ?? 0 : 0, remote.forEpisode(ep));
    final current = started && ep == nextEpisode;
    final metadata = art(ep);
    final thumbnail = metadata?['thumbnail'] as String? ?? '';
    final airdate = metadata?['airdate'] as String? ?? '';
    return EpisodeProgressThread(
      key: ValueKey('episode-$ep'),
      episode: ep,
      current: nextEpisode,
      started: started,
      x: 7 + (card ? 14 : 8) + 1.5,
      child: FocusBox(
        isTV: widget.isTV,
        radius: card ? 12 : 7,
        onPressed: () => play(ep),
        padding: EdgeInsets.all(card ? 14 : 8),
        child: Container(
          constraints: BoxConstraints(minHeight: card ? 110 : 78),
          child: Row(
            children: [
              SizedBox(
                width: max(24, '$ep'.length * (widget.isTV ? 12.2 : 9.2) + 6),
                child: Padding(
                  padding: const EdgeInsets.only(left: 6),
                  child: Text(
                    '$ep',
                    textAlign: TextAlign.right,
                    style: mono.copyWith(
                      fontSize: widget.isTV ? 20 : 15,
                      color: current ? accentText : muted,
                    ),
                  ),
                ),
              ),
              const SizedBox(width: 8),
              ClipRRect(
                borderRadius: BorderRadius.circular(7),
                child: SizedBox(
                  width: card ? 92 : 73,
                  height: card ? 86 : 47,
                  child: Stack(
                    fit: StackFit.expand,
                    children: [
                      CoverImage(
                        thumbnail.isNotEmpty ? thumbnail : anime.cover,
                        width: card ? 92 : 73,
                        height: card ? 86 : 47,
                      ),
                      if (thumbnail.isEmpty)
                        ColoredBox(
                          key: ValueKey('episode-cover-fallback-$ep'),
                          color: bgDeep.withValues(alpha: .4),
                        ),
                      if (seen) ...[
                        ColoredBox(color: bgDeep.withValues(alpha: .65)),
                        Center(
                          child: Container(
                            padding: const EdgeInsets.symmetric(
                              horizontal: 5,
                              vertical: 1,
                            ),
                            decoration: BoxDecoration(
                              border: Border.all(color: fg),
                            ),
                            child: Text('済', style: uiStyle(context, size: 15)),
                          ),
                        ),
                      ],
                    ],
                  ),
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      metadata?['title'] as String? ?? 'Episode $ep',
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: uiStyle(
                        context,
                        size: card ? 22 : 16,
                        weight: FontWeight.w600,
                      ),
                    ),
                    Text(
                      '${seen
                          ? 'Watched'
                          : seconds > 0
                          ? 'Resume at ${seconds ~/ 60}m ${seconds % 60}s'
                          : 'Not watched'}${airdate.isNotEmpty ? ' · $airdate' : ''}',
                      maxLines: card ? 1 : null,
                      overflow: card ? TextOverflow.ellipsis : null,
                      style: uiStyle(
                        context,
                        size: 15,
                        color: !seen && seconds > 0 ? accentText : muted,
                      ),
                    ),
                  ],
                ),
              ),
              if (!card)
                const Padding(
                  padding: EdgeInsets.all(8),
                  child: Icon(LucideIcons.play, size: 20),
                ),
            ],
          ),
        ),
      ),
    );
  }

  Widget tvNavigation() => Padding(
    padding: const EdgeInsets.fromLTRB(48, 27, 48, 12),
    child: Row(
      children: [
        const Brand(),
        const SizedBox(width: 28),
        for (var i = 0; i < 3; i++)
          TextButton(
            onPressed: () {
              Navigator.popUntil(context, (r) => r.isFirst);
              widget.services.selectRoot?.call(i);
            },
            child: Text(['Home', 'Search', 'More'][i]),
          ),
        const Spacer(),
        const DeviceStatus(isTV: true),
      ],
    ),
  );
  @override
  Widget build(BuildContext context) => PosterNavigation(
    onOpen: (a, tag) => Navigator.push(
      context,
      OtakaseRoute<void>(
        builder: (_) => ShowPage(
          services: widget.services,
          anime: a,
          isTV: widget.isTV,
          heroTag: tag,
        ),
      ),
    ),
    child: Scaffold(
      body: ListenableBuilder(
        listenable: Listenable.merge([
          widget.services.store,
          widget.services.tracking,
        ]),
        builder: (context, _) {
          final last = nextEpisode, h = widget.services.store.entry(anime);
          final position = max(
            h?.episode == last ? h?.position ?? 0 : 0,
            remote.forEpisode(last),
          );
          if (!widget.services.store.visible(anime)) {
            return SafeArea(
              child: Center(
                child: AppButton(
                  label: 'This title is unavailable · Back',
                  isTV: widget.isTV,
                  onPressed: () => Navigator.pop(context),
                ),
              ),
            );
          }
          return Column(
            children: [
              if (widget.isTV) tvNavigation(),
              Expanded(
                child: CustomScrollView(
                  slivers: [
                    SliverToBoxAdapter(child: header(last, position)),
                    SliverToBoxAdapter(
                      child: Padding(
                        padding: EdgeInsets.symmetric(
                          horizontal: widget.isTV ? 48 : 22,
                          vertical: 12,
                        ),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            if (loading) const SkeletonBlock(height: 3),
                            if (error != null)
                              Text(
                                error!,
                                style: uiStyle(context, color: muted),
                              ),
                            if (!widget.isTV) ...[
                              Text(
                                anime.description,
                                maxLines: 3,
                                overflow: TextOverflow.ellipsis,
                                style: uiStyle(context, size: 15, height: 1.5),
                              ),
                              const SizedBox(height: 16),
                              actions(last, position),
                              const SizedBox(height: 24),
                            ],
                            Row(
                              children: [
                                const Expanded(child: SectionLabel('Episodes')),
                                for (final value in ['sub', 'dub'])
                                  AppTab(
                                    label: value == 'sub' ? 'Sub' : 'Dub',
                                    selected: mode == value,
                                    isTV: widget.isTV,
                                    onPressed: () =>
                                        setState(() => mode = value),
                                  ),
                              ],
                            ),
                            Text(
                              '${anime.year > 0 ? '${anime.year} · ' : ''}$watched watched',
                              style: uiStyle(context, size: 15, color: muted),
                            ),
                            if (anime.airedEpisodes == 0)
                              Text(
                                'Episode availability is checked when you press Play.',
                                style: uiStyle(context, color: muted),
                              ),
                          ],
                        ),
                      ),
                    ),
                    if (widget.isTV)
                      SliverToBoxAdapter(
                        child: SizedBox(
                          height:
                              170 * MediaQuery.textScalerOf(context).scale(1),
                          child: LayoutBuilder(
                            builder: (context, box) {
                              final width = ((box.maxWidth - 96 - 44) / 3)
                                  .clamp(360.0, 600.0);
                              return ListView.separated(
                                scrollDirection: Axis.horizontal,
                                padding: const EdgeInsets.symmetric(
                                  horizontal: 41,
                                  vertical: 8,
                                ),
                                itemCount: anime.airedEpisodes,
                                separatorBuilder: (_, i) =>
                                    const SizedBox(width: 22),
                                itemBuilder: (_, i) => SizedBox(
                                  width: width,
                                  child: episodeRow(i + 1, card: true),
                                ),
                              );
                            },
                          ),
                        ),
                      )
                    else
                      SliverPadding(
                        padding: const EdgeInsets.symmetric(horizontal: 14),
                        sliver: SliverList.builder(
                          itemCount: anime.airedEpisodes.clamp(0, 10000),
                          itemBuilder: (_, i) => episodeRow(i + 1),
                        ),
                      ),
                    if (widget.isTV)
                      SliverToBoxAdapter(
                        child: Padding(
                          padding: const EdgeInsets.symmetric(horizontal: 48),
                          child: AppButton(
                            label: 'All episodes',
                            isTV: true,
                            onPressed: () => Navigator.push(
                              context,
                              OtakaseRoute<void>(
                                builder: (_) => Scaffold(
                                  appBar: AppBar(
                                    title: const Text('All episodes'),
                                    leading: const BackButton(),
                                  ),
                                  body: ListView.builder(
                                    padding: const EdgeInsets.fromLTRB(
                                      48,
                                      8,
                                      48,
                                      27,
                                    ),
                                    itemCount: anime.airedEpisodes,
                                    itemBuilder: (_, i) => episodeRow(i + 1),
                                  ),
                                ),
                              ),
                            ),
                          ),
                        ),
                      ),
                    for (final section in {
                      'Related': anime.related,
                      'Recommended': anime.recommended,
                    }.entries)
                      if (section.value
                          .where(widget.services.store.visible)
                          .isNotEmpty)
                        SliverToBoxAdapter(
                          child: PosterRow(
                            title: section.key,
                            items: section.value
                                .where(widget.services.store.visible)
                                .toList(),
                            isTV: widget.isTV,
                            onOpen: (a) => Navigator.push(
                              context,
                              OtakaseRoute<void>(
                                builder: (_) => ShowPage(
                                  services: widget.services,
                                  anime: a,
                                  isTV: widget.isTV,
                                ),
                              ),
                            ),
                          ),
                        ),
                    SliverToBoxAdapter(
                      child: SizedBox(
                        height:
                            MediaQuery.viewPaddingOf(context).bottom +
                            (widget.isTV ? 27 : 24),
                      ),
                    ),
                  ],
                ),
              ),
            ],
          );
        },
      ),
    ),
  );
}
