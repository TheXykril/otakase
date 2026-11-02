import 'dart:async';
import 'dart:math';

import 'package:flutter/material.dart';

import '../data/models.dart';
import '../data/episode_metadata.dart';
import '../data/synced_resume.dart';
import '../data/tracking.dart';
import '../services.dart';
import 'app_shell.dart' show Brand, TVTopBar, rootTabs;
import 'components.dart';
import 'find_show.dart';
import 'downloads_page.dart';
import 'device_status.dart';
import 'home_page.dart';
import 'status_editor.dart';
import 'progress_thread.dart';
import '../desktop/desktop_core.dart';
import '../desktop/top_bar.dart';
import '../desktop/window_frame.dart';
import '../desktop/external_mpv.dart';

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

  /// Which block of 50 episodes the desktop grid shows.
  int? range;
  final episodeScroll = ScrollController();
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
    episodeScroll.dispose();
    super.dispose();
  }

  Future<void> play(int ep) => Navigator.push(
    context,
    OtakaseRoute<void>(
      builder: (_) => playerFor(
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
      AppButton(
        key: const Key('show-find'),
        label: 'Wrong show?',
        icon: LucideIcons.search,
        isTV: widget.isTV,
        onPressed: () =>
            findShowMyself(context, widget.services, anime, widget.isTV),
      ),
      // Downloads are for phones and computers; a TV streams.
      if (!widget.isTV || isDesktop)
        AppButton(
          key: const Key('show-download'),
          label: 'Download',
          icon: LucideIcons.download,
          isTV: widget.isTV,
          onPressed: anime.status == 'NOT_YET_RELEASED'
              ? null
              : () => downloadAnimeEpisode(
                  context,
                  widget.services,
                  anime,
                  last,
                  mode,
                ),
        ),
    ];
    return widget.isTV
        ? Wrap(spacing: 12, runSpacing: 8, children: buttons)
        : Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              for (var i = 0; i < buttons.length; i++) ...[
                if (i > 0) const SizedBox(height: 6),
                buttons[i],
              ],
            ],
          );
  }

  Widget header(int last, int position) {
    final tv = widget.isTV;
    // On TV the poster stays left of the episode badge and native title.
    final heroPoster = tv
        ? const EdgeInsets.fromLTRB(0, 28, 240, 28)
        : EdgeInsets.zero;
    final scale = MediaQuery.textScalerOf(context).scale(1);
    // On TV the top bar floats over the art, like on desktop.
    final top = tv ? tvHeaderExtent(context) : 0.0;
    final height = (tv ? 310.0 : 346.0) * max(1.0, scale) + top;
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
              child: HeroArt(anime, height: height, posterPadding: heroPoster),
            )
          else
            HeroArt(anime, height: height, posterPadding: heroPoster),
          if (!tv)
            Positioned(
              top: max(25, MediaQuery.viewPaddingOf(context).top),
              left: 14,
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
              top: tv ? top + 16 : 80,
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
                      if (thumbnail.isNotEmpty)
                        CoverImage(
                          thumbnail,
                          width: card ? 92 : 73,
                          height: card ? 86 : 47,
                        )
                      else
                        EpisodeArtFallback(
                          key: ValueKey('episode-cover-fallback-$ep'),
                          anime.cover,
                          ep,
                          width: card ? 92 : 73,
                          height: card ? 86 : 47,
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

  /// The main tabs in the top bar; picking one leaves the show for it.
  Widget rootTabRow() => Row(
    mainAxisSize: MainAxisSize.min,
    children: [
      for (var i = 0; i < rootTabs.length; i++) ...[
        DesktopTab(
          key: ValueKey('show-nav-${rootTabs[i]}'),
          label: rootTabs[i],
          selected: widget.services.root == i,
          onPressed: () {
            Navigator.popUntil(context, (r) => r.isFirst);
            widget.services.selectRoot?.call(i);
          },
        ),
        if (i < rootTabs.length - 1) const SizedBox(width: 14),
      ],
    ],
  );

  Widget tvNavigation() => TVTopBar(
    leading: IconButton(
      key: const Key('show-back'),
      tooltip: 'Back',
      onPressed: () => Navigator.pop(context),
      icon: const Icon(LucideIcons.arrowLeft, color: fgBright),
    ),
    tabs: rootTabRow(),
  );

  // ---------------------------------------------------------------- desktop

  Widget desktopTopBar() => WindowDragArea(
    child: DesktopTopBar(
      leading: IconButton(
        key: const Key('show-back'),
        tooltip: 'Back',
        onPressed: () => Navigator.pop(context),
        icon: const Icon(LucideIcons.arrowLeft, color: fgBright),
      ),
      brand: const Brand(),
      tabs: rootTabRow(),
    ),
  );

  static String titleCase(String value) => value
      .split('_')
      .where((w) => w.isNotEmpty)
      .map((w) => w[0] + w.substring(1).toLowerCase())
      .join(' ');

  Widget chip(String label, {IconData? icon}) => Container(
    padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 5),
    decoration: BoxDecoration(
      color: bgDeep.withValues(alpha: .45),
      border: Border.all(color: fgBright.withValues(alpha: .22)),
      borderRadius: BorderRadius.circular(999),
    ),
    child: Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (icon != null) ...[
          Icon(icon, size: 16, color: warn),
          const SizedBox(width: 6),
        ],
        Text(label, style: uiStyle(context, size: 17)),
      ],
    ),
  );

  Widget desktopHeader(int last, int position) {
    const height = 600.0;
    final cover = anime.coverLarge.isNotEmpty ? anime.coverLarge : anime.cover;
    final facts = [
      if (anime.format.isNotEmpty) anime.formatLabel,
      if (anime.year > 0) '${anime.year}',
      if (anime.episodes > 0) '${anime.episodes} episodes',
      if (anime.status.isNotEmpty) titleCase(anime.status),
    ];
    return SizedBox(
      height: height,
      child: Stack(
        fit: StackFit.expand,
        children: [
          if (widget.heroTag != null)
            Hero(
              tag: widget.heroTag!,
              child: HeroArt(anime, height: height, poster: false),
            )
          else
            HeroArt(anime, height: height, poster: false),
          Positioned(
            left: 48,
            right: 48,
            bottom: 36,
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
                Container(
                  width: 260,
                  height: 370,
                  decoration: BoxDecoration(
                    borderRadius: BorderRadius.circular(14),
                    boxShadow: const [
                      BoxShadow(
                        color: Color(0x99000000),
                        blurRadius: 30,
                        offset: Offset(0, 14),
                      ),
                    ],
                  ),
                  child: ClipRRect(
                    borderRadius: BorderRadius.circular(14),
                    child: CoverImage(cover, width: 260, height: 370),
                  ),
                ),
                const SizedBox(width: 40),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(
                        facts.join(' · ').toUpperCase(),
                        style: mono.copyWith(
                          fontSize: 17,
                          color: muted,
                          letterSpacing: 3,
                        ),
                      ),
                      const SizedBox(height: 10),
                      Text(
                        anime.title,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: display.copyWith(
                          fontSize: anime.title.length > 38 ? 44 : 56,
                        ),
                      ),
                      const SizedBox(height: 16),
                      Wrap(
                        spacing: 10,
                        runSpacing: 8,
                        children: [
                          if (anime.score > 0)
                            chip(
                              anime.score.toStringAsFixed(1),
                              icon: LucideIcons.star,
                            ),
                          for (final g in anime.genres.take(4)) chip(g),
                          if (anime.studio.isNotEmpty) chip(anime.studio),
                        ],
                      ),
                      if (anime.description.isNotEmpty) ...[
                        const SizedBox(height: 16),
                        ConstrainedBox(
                          constraints: const BoxConstraints(maxWidth: 900),
                          child: Text(
                            anime.description,
                            maxLines: 3,
                            overflow: TextOverflow.ellipsis,
                            style: uiStyle(context, color: muted),
                          ),
                        ),
                      ],
                      const SizedBox(height: 22),
                      Wrap(
                        spacing: 12,
                        runSpacing: 8,
                        crossAxisAlignment: WrapCrossAlignment.center,
                        children: [
                          actions(last, position),
                          Container(
                            padding: const EdgeInsets.all(4),
                            decoration: BoxDecoration(
                              color: fgBright.withValues(alpha: .08),
                              borderRadius: BorderRadius.circular(999),
                            ),
                            child: Row(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                for (final value in ['sub', 'dub'])
                                  AppTab(
                                    key: ValueKey('show-mode-$value'),
                                    label: value == 'sub' ? 'Sub' : 'Dub',
                                    selected: mode == value,
                                    isTV: true,
                                    onPressed: () =>
                                        setState(() => mode = value),
                                  ),
                              ],
                            ),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget episodeCard(int ep, double width) {
    final h = widget.services.store.entry(anime);
    final seen =
        (h?.watched.contains(ep) ?? false) ||
        ep <= widget.services.tracking.progress(anime);
    final seconds = seen
        ? 0
        : max(h?.episode == ep ? h?.position ?? 0 : 0, remote.forEpisode(ep));
    final length = h?.episode == ep && (h?.duration ?? 0) > 0
        ? h!.duration
        : anime.duration * 60;
    final current = started && ep == nextEpisode;
    final metadata = art(ep);
    final thumbnail = metadata?['thumbnail'] as String? ?? '';
    final image = thumbnail.isNotEmpty
        ? thumbnail
        : anime.banner.isNotEmpty
        ? anime.banner
        : anime.cover;
    final title = metadata?['title'] as String?;
    final height = width * 9 / 16;
    String clock(int s) =>
        '${(s ~/ 60).toString().padLeft(2, '0')}:${(s % 60).toString().padLeft(2, '0')}';
    return SizedBox(
      key: ValueKey('episode-$ep'),
      width: width,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          FocusBox(
            isTV: true,
            outsideFrame: true,
            lift: true,
            radius: 12,
            onPressed: () => play(ep),
            child: ClipRRect(
              borderRadius: BorderRadius.circular(12),
              child: SizedBox(
                width: width,
                height: height,
                child: Stack(
                  fit: StackFit.expand,
                  children: [
                    if (thumbnail.isEmpty && anime.banner.isEmpty)
                      EpisodeArtFallback(
                        anime.coverLarge.isNotEmpty
                            ? anime.coverLarge
                            : anime.cover,
                        ep,
                        width: width,
                        height: height,
                      )
                    else
                      CoverImage(
                        image,
                        width: width,
                        height: height,
                        banner: thumbnail.isEmpty && anime.banner.isNotEmpty,
                      ),
                    if (seen) ColoredBox(color: bgDeep.withValues(alpha: .35)),
                    if (length > 0)
                      Positioned(
                        left: 12,
                        bottom: 12,
                        child: Container(
                          padding: const EdgeInsets.symmetric(
                            horizontal: 7,
                            vertical: 1,
                          ),
                          decoration: BoxDecoration(
                            color: bgDeep.withValues(alpha: .8),
                            borderRadius: BorderRadius.circular(6),
                          ),
                          child: Text(
                            clock(length),
                            style: mono.copyWith(fontSize: 15, color: fgBright),
                          ),
                        ),
                      ),
                    if (seen)
                      Positioned(
                        top: 10,
                        right: 10,
                        child: Container(
                          width: 28,
                          height: 28,
                          decoration: const BoxDecoration(
                            color: ok,
                            shape: BoxShape.circle,
                          ),
                          child: const Icon(
                            LucideIcons.check,
                            size: 17,
                            color: bgDeep,
                          ),
                        ),
                      ),
                    if (current)
                      Center(
                        child: Container(
                          width: 56,
                          height: 56,
                          decoration: const BoxDecoration(
                            color: shu,
                            shape: BoxShape.circle,
                          ),
                          child: const Icon(
                            LucideIcons.play,
                            size: 24,
                            color: bgDeep,
                          ),
                        ),
                      ),
                    if (seconds > 0 && length > 0)
                      Positioned(
                        left: 0,
                        right: 0,
                        bottom: 0,
                        child: LinearProgressIndicator(
                          value: (seconds / length).clamp(0.0, 1.0),
                          minHeight: 4,
                          color: shu,
                          backgroundColor: fgBright.withValues(alpha: .18),
                        ),
                      ),
                  ],
                ),
              ),
            ),
          ),
          const SizedBox(height: 10),
          Text(
            title == null ? 'Episode $ep' : '$ep. $title',
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: uiStyle(
              context,
              weight: FontWeight.w600,
              color: current ? fgBright : fg,
            ),
          ),
          Text(
            seen
                ? 'Watched'
                : seconds > 0
                ? '${max(1, (length - seconds) ~/ 60)} min left'
                : 'Not watched',
            style: uiStyle(
              context,
              size: 17,
              color: !seen && seconds > 0 ? accentText : muted,
            ),
          ),
        ],
      ),
    );
  }

  static String rangeLabel(int block, int total) =>
      '${block * 50 + 1}–${min(total, block * 50 + 50)}';

  Widget desktopEpisodes() {
    final total = anime.airedEpisodes;
    final blocks = (total / 50).ceil();
    final block = (range ?? ((nextEpisode - 1) ~/ 50))
        .clamp(0, max(0, blocks - 1))
        .toInt();
    final first = block * 50 + 1, end = min(total, first + 49);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Text(
              'Episodes',
              style: uiStyle(context, size: 28, weight: FontWeight.w700),
            ),
            const SizedBox(width: 20),
            if (blocks > 1 && blocks <= 4) ...[
              for (var b = 0; b < blocks; b++) ...[
                AppTab(
                  key: ValueKey('episode-range-$b'),
                  label: rangeLabel(b, total),
                  selected: b == block,
                  isTV: true,
                  onPressed: () => setState(() => range = b),
                ),
                const SizedBox(width: 10),
              ],
              const Spacer(),
            ] else if (blocks > 4) ...[
              // Long shows get one menu of ranges instead of a wall of
              // buttons, with arrows for the block before and after.
              IconButton(
                key: const Key('episode-range-previous'),
                tooltip: 'Previous episodes',
                onPressed: block > 0
                    ? () => setState(() => range = block - 1)
                    : null,
                icon: const Icon(LucideIcons.chevronLeft, size: 20),
              ),
              PopupMenuButton<int>(
                key: const Key('episode-range-menu'),
                tooltip: 'Choose episodes',
                initialValue: block,
                color: panelRaised,
                constraints: const BoxConstraints(maxHeight: 520),
                position: PopupMenuPosition.under,
                onSelected: (b) => setState(() => range = b),
                itemBuilder: (_) => [
                  for (var b = 0; b < blocks; b++)
                    PopupMenuItem(
                      value: b,
                      child: Text(
                        'Episodes ${rangeLabel(b, total)}',
                        style: uiStyle(
                          context,
                          size: 17,
                          color: b == block ? accentText : fg,
                          weight: b == block
                              ? FontWeight.w600
                              : FontWeight.w400,
                        ),
                      ),
                    ),
                ],
                child: Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 16,
                    vertical: 8,
                  ),
                  decoration: BoxDecoration(
                    color: fgBright.withValues(alpha: .08),
                    borderRadius: BorderRadius.circular(999),
                  ),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(
                        rangeLabel(block, total),
                        style: uiStyle(context, weight: FontWeight.w600),
                      ),
                      const SizedBox(width: 8),
                      const Icon(LucideIcons.chevronDown, size: 18),
                    ],
                  ),
                ),
              ),
              IconButton(
                key: const Key('episode-range-next'),
                tooltip: 'Next episodes',
                onPressed: block < blocks - 1
                    ? () => setState(() => range = block + 1)
                    : null,
                icon: const Icon(LucideIcons.chevronRight, size: 20),
              ),
              const Spacer(),
            ] else
              const Spacer(),
            const SizedBox(width: 16),
            Text(
              '$watched watched',
              style: uiStyle(context, size: 17, color: muted),
            ),
          ],
        ),
        const SizedBox(height: 20),
        if (total == 0)
          Text(
            'Episode availability is checked when you press Play.',
            style: uiStyle(context, color: muted),
          )
        else
          LayoutBuilder(
            builder: (context, box) {
              const gap = 22.0;
              final width = fittedCardWidth(
                box.maxWidth,
                300,
                padding: 0,
                gap: gap,
              );
              return Wrap(
                spacing: gap,
                runSpacing: 26,
                children: [
                  for (var ep = first; ep <= end; ep++) episodeCard(ep, width),
                ],
              );
            },
          ),
      ],
    );
  }

  Widget desktopSidebar() {
    final also = anime.titles.where((t) => t != anime.title).take(2).toList();
    final related = anime.related.where(widget.services.store.visible).take(6);
    Widget fact(String label, String value) => Padding(
      padding: const EdgeInsets.only(bottom: 18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            label.toUpperCase(),
            style: mono.copyWith(fontSize: 15, letterSpacing: 2),
          ),
          const SizedBox(height: 2),
          Text(value, style: uiStyle(context, color: fgBright)),
        ],
      ),
    );
    return Container(
      key: const Key('show-sidebar'),
      padding: const EdgeInsets.fromLTRB(24, 24, 24, 8),
      decoration: BoxDecoration(
        color: panel,
        border: Border.all(color: line),
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (anime.studio.isNotEmpty) fact('Studio', anime.studio),
          if (anime.format.isNotEmpty) fact('Format', anime.formatLabel),
          if (anime.status.isNotEmpty) fact('Status', titleCase(anime.status)),
          if (anime.year > 0) fact('Year', '${anime.year}'),
          if (anime.duration > 0)
            fact('Length', '${anime.duration} min per episode'),
          if (anime.genres.isNotEmpty) fact('Genres', anime.genres.join(', ')),
          if (also.isNotEmpty) fact('Also known as', also.join('\n')),
          if (related.isNotEmpty) ...[
            Divider(color: line, height: 12),
            const SizedBox(height: 12),
            Text(
              'RELATED',
              style: mono.copyWith(fontSize: 15, letterSpacing: 2),
            ),
            const SizedBox(height: 10),
            Wrap(
              spacing: 10,
              runSpacing: 10,
              children: [
                for (final a in related)
                  Tooltip(
                    message: a.title,
                    child: FocusBox(
                      isTV: true,
                      outsideFrame: true,
                      lift: true,
                      radius: 8,
                      onPressed: () => Navigator.push(
                        context,
                        OtakaseRoute<void>(
                          builder: (_) => ShowPage(
                            services: widget.services,
                            anime: a,
                            isTV: widget.isTV,
                          ),
                        ),
                      ),
                      child: ClipRRect(
                        borderRadius: BorderRadius.circular(8),
                        child: CoverImage(a.cover, width: 92, height: 130),
                      ),
                    ),
                  ),
              ],
            ),
            const SizedBox(height: 16),
          ],
        ],
      ),
    );
  }

  Widget desktopBody(int last, int position) => Stack(
    children: [
      CustomScrollView(
        slivers: [
          SliverToBoxAdapter(child: desktopHeader(last, position)),
          SliverToBoxAdapter(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(48, 12, 48, 0),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (loading) const SkeletonBlock(height: 3),
                  if (error != null)
                    Text(error!, style: uiStyle(context, color: muted)),
                  Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Expanded(child: desktopEpisodes()),
                      const SizedBox(width: 40),
                      SizedBox(width: 380, child: desktopSidebar()),
                    ],
                  ),
                ],
              ),
            ),
          ),
          if (anime.recommended.where(widget.services.store.visible).isNotEmpty)
            SliverToBoxAdapter(
              child: PosterRow(
                title: 'Recommended',
                items: anime.recommended
                    .where(widget.services.store.visible)
                    .toList(),
                isTV: true,
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
          const SliverToBoxAdapter(child: SizedBox(height: 40)),
        ],
      ),
      Positioned(top: 0, left: 0, right: 0, child: desktopTopBar()),
    ],
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
          if (isDesktop) return desktopBody(last, position);
          return Stack(
            children: [
              Positioned.fill(
                child: Column(
                  children: [
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
                                      style: uiStyle(
                                        context,
                                        size: 15,
                                        height: 1.5,
                                      ),
                                    ),
                                    const SizedBox(height: 16),
                                    actions(last, position),
                                    const SizedBox(height: 24),
                                  ],
                                  Row(
                                    children: [
                                      const Expanded(
                                        child: SectionLabel('Episodes'),
                                      ),
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
                                    style: uiStyle(
                                      context,
                                      size: 15,
                                      color: muted,
                                    ),
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
                            // The same episode cards as on a computer, in a
                            // row the remote moves along.
                            SliverToBoxAdapter(
                              child: LayoutBuilder(
                                builder: (context, box) {
                                  final width = ((box.maxWidth - 82 - 66) / 4)
                                      .clamp(280.0, 440.0);
                                  final text = MediaQuery.textScalerOf(context);
                                  final height =
                                      width * 9 / 16 +
                                      10 +
                                      (text.scale(20) + text.scale(17)) * 1.4 +
                                      24;
                                  return SizedBox(
                                    height: height,
                                    child: RemoteFocusRow(
                                      isTV: true,
                                      controller: episodeScroll,
                                      itemCount: anime.airedEpisodes,
                                      itemWidth: width,
                                      spacing: 22,
                                      padding: 41,
                                      child: ListView.separated(
                                        controller: episodeScroll,
                                        scrollDirection: Axis.horizontal,
                                        padding: const EdgeInsets.symmetric(
                                          horizontal: 41,
                                          vertical: 8,
                                        ),
                                        itemCount: anime.airedEpisodes,
                                        separatorBuilder: (_, i) =>
                                            const SizedBox(width: 22),
                                        itemBuilder: (_, i) => RemoteFocusItem(
                                          index: i,
                                          child: episodeCard(i + 1, width),
                                        ),
                                      ),
                                    ),
                                  );
                                },
                              ),
                            )
                          else
                            SliverPadding(
                              padding: const EdgeInsets.symmetric(
                                horizontal: 14,
                              ),
                              sliver: SliverList.builder(
                                itemCount: anime.airedEpisodes.clamp(0, 10000),
                                itemBuilder: (_, i) => episodeRow(i + 1),
                              ),
                            ),
                          if (widget.isTV)
                            SliverToBoxAdapter(
                              child: Padding(
                                padding: const EdgeInsets.symmetric(
                                  horizontal: 48,
                                ),
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
                                        body: LayoutBuilder(
                                          builder: (context, box) {
                                            final width =
                                                (box.maxWidth - 96 - 66) / 4;
                                            final text =
                                                MediaQuery.textScalerOf(
                                                  context,
                                                );
                                            return GridView.builder(
                                              padding:
                                                  const EdgeInsets.fromLTRB(
                                                    48,
                                                    8,
                                                    48,
                                                    27,
                                                  ),
                                              gridDelegate:
                                                  SliverGridDelegateWithFixedCrossAxisCount(
                                                    crossAxisCount: 4,
                                                    crossAxisSpacing: 22,
                                                    mainAxisSpacing: 26,
                                                    mainAxisExtent:
                                                        width * 9 / 16 +
                                                        10 +
                                                        (text.scale(20) +
                                                                text.scale(
                                                                  17,
                                                                )) *
                                                            1.4 +
                                                        8,
                                                  ),
                                              itemCount: anime.airedEpisodes,
                                              itemBuilder: (_, i) =>
                                                  episodeCard(i + 1, width),
                                            );
                                          },
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
                ),
              ),
              if (widget.isTV)
                Positioned(top: 0, left: 0, right: 0, child: tvNavigation()),
            ],
          );
        },
      ),
    ),
  );
}
