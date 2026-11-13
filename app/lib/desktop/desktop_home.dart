import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../data/models.dart';
import '../ui/components.dart';

/// Side margin of desktop pages, lined up with the logo in the top bar.
const desktopSide = 48.0;

/// One slide of the desktop home hero.
class HeroPick {
  const HeroPick({
    required this.anime,
    required this.kicker,
    required this.action,
    required this.onPlay,
    required this.onDetails,
    this.entry,
    this.label,
  });
  final Anime anime;
  final String kicker, action;
  final VoidCallback onPlay, onDetails;

  /// Where you left off, for a pick from Continue watching.
  final WatchEntry? entry;

  /// The line above the title on the phone and TV banner, in place of the
  /// episode number.
  final String? label;
}

/// The big banner at the top of the desktop home: the show you are watching
/// first, then trending picks, changing every few seconds unless the pointer
/// rests on it.
class DesktopHero extends StatefulWidget {
  const DesktopHero({super.key, required this.picks, this.topInset = 0});
  final List<HeroPick> picks;
  final double topInset;
  @override
  State<DesktopHero> createState() => _DesktopHeroState();
}

class _DesktopHeroState extends State<DesktopHero> {
  int index = 0;

  /// When the pointer last moved over the hero. Leaving the window does not
  /// always report an exit, so a still pointer stops pausing after a while.
  DateTime? hoveredAt;
  Timer? timer;

  @override
  void initState() {
    super.initState();
    timer = Timer.periodic(const Duration(seconds: 9), (_) {
      final paused =
          hoveredAt != null &&
          DateTime.now().difference(hoveredAt!) < const Duration(seconds: 20);
      if (!mounted || paused || widget.picks.length < 2) return;
      if (MediaQuery.disableAnimationsOf(context)) return;
      setState(() => index = (index + 1) % widget.picks.length);
    });
  }

  @override
  void didUpdateWidget(DesktopHero old) {
    super.didUpdateWidget(old);
    if (index >= widget.picks.length) index = 0;
  }

  @override
  void dispose() {
    timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (widget.picks.isEmpty) return SizedBox(height: widget.topInset);
    final pick = widget.picks[index];
    final a = pick.anime;
    final height = (MediaQuery.sizeOf(context).height * .6).clamp(520.0, 720.0);
    final facts = [
      if (a.score > 0) '★ ${a.score.toStringAsFixed(1)}',
      ...a.genres.take(3),
      a.studio,
    ];
    return MouseRegion(
      cursor: SystemMouseCursors.click,
      onHover: (_) => hoveredAt = DateTime.now(),
      onExit: (_) => hoveredAt = null,
      child: SizedBox(
        key: const Key('desktop-hero'),
        height: height,
        child: GestureDetector(
          behavior: HitTestBehavior.opaque,
          onTap: pick.onDetails,
          child: Stack(
            fit: StackFit.expand,
            children: [
              AnimatedSwitcher(
                duration: const Duration(milliseconds: 600),
                child: KeyedSubtree(
                  key: ValueKey(a.key),
                  child: HeroArt(
                    a,
                    height: height,
                    posterPadding: EdgeInsets.fromLTRB(
                      0,
                      widget.topInset + 12,
                      desktopSide + 220,
                      48,
                    ),
                  ),
                ),
              ),
              Positioned(
                left: desktopSide,
                right: desktopSide,
                top: widget.topInset,
                bottom: 36,
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.end,
                  children: [
                    Expanded(
                      child: ConstrainedBox(
                        constraints: const BoxConstraints(maxWidth: 900),
                        child: Column(
                          mainAxisAlignment: MainAxisAlignment.end,
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              pick.kicker.toUpperCase(),
                              style: mono.copyWith(
                                fontSize: 17,
                                color: accentText,
                                letterSpacing: 3,
                              ),
                            ),
                            const SizedBox(height: 10),
                            Text(
                              a.title,
                              maxLines: 2,
                              overflow: TextOverflow.ellipsis,
                              style: display.copyWith(fontSize: 60),
                            ),
                            const SizedBox(height: 14),
                            MetaText(facts, size: 17, color: fg),
                            if (a.description.isNotEmpty) ...[
                              const SizedBox(height: 18),
                              ConstrainedBox(
                                constraints: const BoxConstraints(
                                  maxWidth: 760,
                                ),
                                child: Text(
                                  a.description,
                                  maxLines: 2,
                                  overflow: TextOverflow.ellipsis,
                                  style: uiStyle(context, color: muted),
                                ),
                              ),
                            ],
                            const SizedBox(height: 26),
                            Row(
                              children: [
                                AppButton(
                                  key: const Key('desktop-hero-play'),
                                  label: pick.action,
                                  icon: LucideIcons.play,
                                  filled: true,
                                  isTV: true,
                                  onPressed: pick.onPlay,
                                ),
                                const SizedBox(width: 14),
                                AppButton(
                                  key: const Key('desktop-hero-details'),
                                  label: 'Details',
                                  icon: LucideIcons.info,
                                  isTV: true,
                                  onPressed: pick.onDetails,
                                ),
                              ],
                            ),
                          ],
                        ),
                      ),
                    ),
                    if (widget.picks.length > 1)
                      Row(
                        children: [
                          for (var i = 0; i < widget.picks.length; i++)
                            GestureDetector(
                              key: ValueKey('desktop-hero-dot-$i'),
                              onTap: () => setState(() => index = i),
                              child: MouseRegion(
                                cursor: SystemMouseCursors.click,
                                child: AnimatedContainer(
                                  duration: const Duration(milliseconds: 250),
                                  margin: const EdgeInsets.only(left: 8),
                                  width: i == index ? 30 : 10,
                                  height: 10,
                                  decoration: BoxDecoration(
                                    color: i == index
                                        ? shu
                                        : fgBright.withValues(alpha: .3),
                                    borderRadius: BorderRadius.circular(
                                      KageRadius.full,
                                    ),
                                  ),
                                ),
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
      ),
    );
  }
}

/// Wide Continue watching cards that always fill the row: four across on a
/// normal window, scrolling sideways when there are more.
class DesktopContinueRow extends StatefulWidget {
  const DesktopContinueRow({
    super.key,
    required this.entries,
    required this.onResume,
    this.label,
    this.onMenu,
  });
  final List<WatchEntry> entries;
  final ValueChanged<WatchEntry> onResume;

  /// Right click or long press on a card: more for it.
  final ValueChanged<WatchEntry>? onMenu;

  /// Replaces the episode line on a card, for films and series.
  final String? Function(WatchEntry)? label;
  @override
  State<DesktopContinueRow> createState() => _DesktopContinueRowState();
}

class _DesktopContinueRowState extends State<DesktopContinueRow> {
  final scroll = ScrollController();
  @override
  void dispose() {
    scroll.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (widget.entries.isEmpty) return const SizedBox.shrink();
    const gap = 22.0;
    return Padding(
      key: const ValueKey('section-Continue watching'),
      padding: const EdgeInsets.only(top: 12, bottom: 4),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Padding(
            padding: EdgeInsets.symmetric(horizontal: desktopSide),
            child: _ShelfTitle('Continue watching', '続'),
          ),
          const SizedBox(height: 16),
          LayoutBuilder(
            builder: (context, box) {
              // Fewer shows than fit stretch to fill the row instead of
              // leaving a gap at the end; never more than four across, and
              // one or two shows stay a third wide instead of a giant card.
              final room = box.maxWidth - desktopSide * 2;
              final fits = ((room + gap) / (380 + gap)).floor().clamp(1, 4);
              final count = math.max(
                math.min(fits, widget.entries.length),
                math.min(fits, 3),
              );
              final width = (room - gap * (count - 1)) / count;
              final art = width * 9 / 16;
              return SizedBox(
                height: art + 62,
                child: ListView.separated(
                  controller: scroll,
                  key: const PageStorageKey('desktop-continue'),
                  scrollDirection: Axis.horizontal,
                  clipBehavior: Clip.none,
                  padding: const EdgeInsets.symmetric(horizontal: desktopSide),
                  itemCount: widget.entries.length,
                  separatorBuilder: (_, _) => const SizedBox(width: gap),
                  itemBuilder: (context, i) => _ContinueCard(
                    key: ValueKey(
                      'desktop-continue-${widget.entries[i].anime.key}',
                    ),
                    entry: widget.entries[i],
                    label: widget.label?.call(widget.entries[i]),
                    width: width,
                    first: i == 0,
                    onPressed: () => widget.onResume(widget.entries[i]),
                    onMenu: widget.onMenu == null
                        ? null
                        : () => widget.onMenu!(widget.entries[i]),
                  ),
                ),
              );
            },
          ),
        ],
      ),
    );
  }
}

class _ContinueCard extends StatefulWidget {
  const _ContinueCard({
    super.key,
    required this.entry,
    required this.width,
    required this.first,
    required this.onPressed,
    this.label,
    this.onMenu,
  });
  final WatchEntry entry;
  final String? label;
  final double width;
  final bool first;
  final VoidCallback onPressed;
  final VoidCallback? onMenu;
  @override
  State<_ContinueCard> createState() => _ContinueCardState();
}

class _ContinueCardState extends State<_ContinueCard> {
  bool hovered = false;
  @override
  Widget build(BuildContext context) {
    final e = widget.entry, a = e.anime;
    final height = widget.width * 9 / 16;
    final left = e.duration > 0
        ? ((e.duration - e.position) / 60).ceil()
        : a.duration;
    final progress = e.duration > 0
        ? (e.position / e.duration).clamp(0.0, 1.0)
        : 0.0;
    final ep = e.episode.toString().padLeft(2, '0');
    return SizedBox(
      width: widget.width,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          CardMenu(
            onMenu: widget.onMenu,
            child: FocusBox(
              isTV: true,
              outsideFrame: true,
              lift: true,
              radius: KageRadius.md,
              onPressed: widget.onPressed,
              onLongPress: widget.onMenu,
              onHover: (value) => setState(() => hovered = value),
              child: ClipRRect(
                borderRadius: BorderRadius.circular(KageRadius.md),
                child: SizedBox(
                  width: widget.width,
                  height: height,
                  child: Stack(
                    fit: StackFit.expand,
                    children: [
                      if (a.banner.isNotEmpty)
                        CoverImage(
                          a.banner,
                          width: widget.width,
                          height: height,
                          banner: true,
                        )
                      else
                        WideCoverArt(
                          a.coverLarge.isNotEmpty ? a.coverLarge : a.cover,
                          width: widget.width,
                          height: height,
                        ),
                      const DecoratedBox(
                        decoration: BoxDecoration(
                          gradient: LinearGradient(
                            begin: Alignment.topCenter,
                            end: Alignment.bottomCenter,
                            colors: [Color(0x00000000), Color(0xcc000000)],
                            stops: [.45, 1],
                          ),
                        ),
                      ),
                      Positioned(
                        left: 16,
                        bottom: 16,
                        child: Text(
                          widget.label ??
                              'EP $ep${left > 0 ? ' · $left min left' : ''}',
                          style: mono.copyWith(
                            fontSize: 17,
                            color: fgBright,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                      ),
                      AnimatedOpacity(
                        opacity: hovered || widget.first ? 1 : 0,
                        duration: const Duration(milliseconds: 180),
                        child: Center(
                          child: Container(
                            width: 64,
                            height: 64,
                            decoration: const BoxDecoration(
                              color: shu,
                              shape: BoxShape.circle,
                            ),
                            child: const Icon(
                              LucideIcons.play,
                              color: bgDeep,
                              size: 28,
                            ),
                          ),
                        ),
                      ),
                      Positioned(
                        left: 0,
                        right: 0,
                        bottom: 0,
                        child: LinearProgressIndicator(
                          value: progress,
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
          ),
          const SizedBox(height: 12),
          Text(
            a.title,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: uiStyle(
              context,
              weight: FontWeight.w600,
              color: hovered ? fgBright : fg,
            ),
          ),
        ],
      ),
    );
  }
}

class _ShelfTitle extends StatelessWidget {
  const _ShelfTitle(this.title, this.kanji);
  final String title, kanji;
  @override
  Widget build(BuildContext context) => Row(
    crossAxisAlignment: CrossAxisAlignment.baseline,
    textBaseline: TextBaseline.alphabetic,
    children: [
      Text(title, style: uiStyle(context, size: 28, weight: FontWeight.w700)),
      const SizedBox(width: 12),
      ExcludeSemantics(
        child: Text(
          kanji,
          style: const TextStyle(
            fontFamily: 'NotoSansJP',
            fontSize: 24,
            color: accentText,
          ),
        ),
      ),
    ],
  );
}
