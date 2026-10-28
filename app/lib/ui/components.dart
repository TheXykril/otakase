import 'dart:async';
import 'dart:math' as math;
import 'dart:ui';

import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:lucide_icons_flutter/lucide_icons.dart';

import '../data/models.dart';

export 'package:lucide_icons_flutter/lucide_icons.dart';

const bg = Color(0xff0e0d0c),
    bgDeep = Color(0xff080707),
    panel = Color(0xff161412),
    line = Color(0xff2a2724),
    fg = Color(0xffece4d6),
    fgBright = Color(0xfffff8ec),
    muted = Color(0xff8a8276),
    shu = Color(0xffd2492f),
    accentDeep = Color(0xffa83a22),
    ok = Color(0xff9bb67a),
    warn = Color(0xffe3b45a),
    info = Color(0xff7fa2c4);
const mono = TextStyle(
  fontFamily: 'JetBrainsMono',
  fontFamilyFallback: ['NotoSansJP'],
  fontSize: 10,
  letterSpacing: 2,
  color: muted,
  height: 1.5,
);
const prose = TextStyle(
  fontFamily: 'CormorantGaramond',
  fontFamilyFallback: ['NotoSansJP'],
  fontSize: 20,
  height: 1.5,
  color: fg,
);
const display = TextStyle(
  fontFamily: 'ArchivoBlack',
  fontFamilyFallback: ['NotoSansJP'],
  fontSize: 30,
  height: 1.12,
  letterSpacing: -.6,
  color: fgBright,
);
const phoneBarHeight = 64.0, phoneBarMargin = 24.0;

class PageInsets extends InheritedWidget {
  const PageInsets({super.key, required this.bottom, required super.child});
  final double bottom;
  static double bottomOf(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<PageInsets>()?.bottom ??
      MediaQuery.paddingOf(context).bottom;
  @override
  bool updateShouldNotify(PageInsets old) => old.bottom != bottom;
}

double contentBottom(BuildContext context) => PageInsets.bottomOf(context) + 24;

class FocusBox extends StatefulWidget {
  const FocusBox({
    super.key,
    required this.child,
    required this.onPressed,
    this.isTV = false,
    this.autofocus = false,
    this.focusNode,
    this.padding = EdgeInsets.zero,
    this.radius = 6,
    this.scale = 1.08,
    this.onFocused,
  });
  final Widget child;
  final VoidCallback? onPressed, onFocused;
  final bool isTV, autofocus;
  final FocusNode? focusNode;
  final EdgeInsets padding;
  final double radius, scale;
  @override
  State<FocusBox> createState() => _FocusBoxState();
}

class _FocusBoxState extends State<FocusBox> {
  bool focused = false;
  void modeChanged(FocusHighlightMode _) {
    if (mounted) setState(() {});
  }

  @override
  void initState() {
    super.initState();
    FocusManager.instance.addHighlightModeListener(modeChanged);
  }

  @override
  void dispose() {
    FocusManager.instance.removeHighlightModeListener(modeChanged);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final visible =
        focused &&
        FocusManager.instance.highlightMode == FocusHighlightMode.traditional;
    return AnimatedScale(
      scale: visible && widget.isTV ? widget.scale : 1,
      duration: const Duration(milliseconds: 200),
      curve: Curves.easeOut,
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 200),
        curve: Curves.easeOut,
        padding: const EdgeInsets.all(3),
        decoration: BoxDecoration(
          borderRadius: BorderRadius.circular(widget.radius + 3),
          border: Border.all(
            color: visible ? shu : Colors.transparent,
            width: 2,
          ),
        ),
        child: Material(
          color: Colors.transparent,
          child: InkWell(
            autofocus: widget.autofocus,
            focusNode: widget.focusNode,
            onTap: widget.onPressed,
            focusColor: Colors.transparent,
            hoverColor: line.withValues(alpha: .4),
            splashColor: accentDeep.withValues(alpha: .2),
            borderRadius: BorderRadius.circular(widget.radius),
            onFocusChange: (value) {
              setState(() => focused = value);
              if (value) {
                widget.onFocused?.call();
                WidgetsBinding.instance.addPostFrameCallback((_) {
                  if (mounted && Scrollable.maybeOf(context) != null) {
                    unawaited(
                      Scrollable.ensureVisible(
                        context,
                        duration: const Duration(milliseconds: 200),
                        alignment: .15,
                      ),
                    );
                  }
                });
              }
            },
            child: Padding(padding: widget.padding, child: widget.child),
          ),
        ),
      ),
    );
  }
}

class AppButton extends StatelessWidget {
  const AppButton({
    super.key,
    required this.label,
    required this.onPressed,
    this.icon,
    this.filled = false,
    this.isTV = false,
    this.autofocus = false,
    this.focusNode,
  });
  final String label;
  final VoidCallback? onPressed;
  final IconData? icon;
  final bool filled, isTV, autofocus;
  final FocusNode? focusNode;
  @override
  Widget build(BuildContext context) => FocusBox(
    isTV: isTV,
    autofocus: autofocus,
    focusNode: focusNode,
    onPressed: onPressed,
    child: Container(
      padding: EdgeInsets.symmetric(horizontal: filled ? 20 : 8, vertical: 12),
      decoration: BoxDecoration(
        color: filled ? shu : Colors.transparent,
        borderRadius: filled ? BorderRadius.circular(6) : null,
        border: filled
            ? null
            : Border(
                bottom: BorderSide(
                  color: onPressed == null ? line : shu,
                  width: 1,
                ),
              ),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (icon != null) ...[
            Icon(icon, size: 18, color: onPressed == null ? muted : fgBright),
            const SizedBox(width: 8),
          ],
          Flexible(
            child: Text(
              label.toUpperCase(),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: mono.copyWith(
                color: onPressed == null ? muted : fgBright,
                fontSize: 11,
                letterSpacing: 1.3,
              ),
            ),
          ),
        ],
      ),
    ),
  );
}

class AppTab extends StatelessWidget {
  const AppTab({
    super.key,
    required this.label,
    required this.selected,
    required this.onPressed,
    this.isTV = false,
  });
  final String label;
  final bool selected, isTV;
  final VoidCallback onPressed;
  @override
  Widget build(BuildContext context) => FocusBox(
    isTV: isTV,
    onPressed: onPressed,
    scale: 1.02,
    child: Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 10),
      decoration: BoxDecoration(
        border: Border(
          bottom: BorderSide(
            color: selected ? shu : line,
            width: selected ? 2 : 1,
          ),
        ),
      ),
      child: Text(
        label.toUpperCase(),
        style: mono.copyWith(
          color: selected ? fgBright : muted,
          letterSpacing: 1.4,
        ),
      ),
    ),
  );
}

class PageHeading extends StatelessWidget {
  const PageHeading(this.title, this.kanji, {super.key});
  final String title, kanji;
  @override
  Widget build(BuildContext context) => SizedBox(
    height: 104,
    child: Stack(
      alignment: Alignment.centerLeft,
      children: [
        Positioned(
          right: 0,
          top: -6,
          child: Text(
            kanji,
            style: TextStyle(
              fontFamily: 'NotoSerifJP',
              fontSize: 88,
              fontWeight: FontWeight.w700,
              height: 1,
              color: fg.withValues(alpha: .09),
            ),
          ),
        ),
        Column(
          mainAxisAlignment: MainAxisAlignment.center,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('OTAKASE / ${title.toUpperCase()}', style: mono),
            const SizedBox(height: 8),
            Text(title, style: display),
          ],
        ),
      ],
    ),
  );
}

class SectionLabel extends StatelessWidget {
  const SectionLabel(this.title, {super.key, this.kanji});
  final String title;
  final String? kanji;
  @override
  Widget build(BuildContext context) => Row(
    children: [
      if (kanji != null) ...[
        Text(
          kanji!,
          style: const TextStyle(
            fontFamily: 'NotoSerifJP',
            fontSize: 24,
            color: shu,
            fontWeight: FontWeight.w700,
          ),
        ),
        const SizedBox(width: 12),
      ],
      Expanded(
        child: Text(
          title.toUpperCase(),
          style: mono.copyWith(color: fg, letterSpacing: 2.2),
        ),
      ),
      const SizedBox(width: 16),
      const SizedBox(width: 28, child: Divider(color: line)),
    ],
  );
}

class EmptyState extends StatelessWidget {
  const EmptyState({
    super.key,
    this.message = 'A little room for the next story.',
    this.action,
    this.error = false,
  });
  final String message;
  final Widget? action;
  final bool error;
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 32, horizontal: 24),
    child: Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(
          error ? '迷' : '空',
          style: TextStyle(
            fontFamily: 'NotoSerifJP',
            fontSize: 88,
            fontWeight: FontWeight.w700,
            height: 1.2,
            color: fg.withValues(alpha: .1),
          ),
        ),
        const SizedBox(height: 16),
        Text(
          message,
          textAlign: TextAlign.center,
          style: prose.copyWith(color: muted),
        ),
        const SizedBox(height: 16),
        if (action != null)
          action!
        else
          Text('LEAVE IT TO US.', style: mono.copyWith(fontSize: 9)),
      ],
    ),
  );
}

class SkeletonBlock extends StatelessWidget {
  const SkeletonBlock({super.key, this.width, this.height = 16});
  final double? width;
  final double height;
  @override
  Widget build(BuildContext context) => Semantics(
    label: 'Loading',
    child: Container(
      width: width,
      height: height,
      decoration: BoxDecoration(
        color: line.withValues(alpha: .65),
        borderRadius: BorderRadius.circular(3),
      ),
    ),
  );
}

class SkeletonShimmer extends StatefulWidget {
  const SkeletonShimmer({super.key, required this.child});
  final Widget child;
  @override
  State<SkeletonShimmer> createState() => _SkeletonShimmerState();
}

class _SkeletonShimmerState extends State<SkeletonShimmer>
    with SingleTickerProviderStateMixin {
  late final controller = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 1500),
  );
  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (MediaQuery.disableAnimationsOf(context)) {
      controller.stop();
    } else if (!controller.isAnimating) {
      controller.repeat();
    }
  }

  @override
  void dispose() {
    controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: controller,
    child: widget.child,
    builder: (context, child) => ShaderMask(
      blendMode: BlendMode.srcATop,
      shaderCallback: (rect) => LinearGradient(
        begin: Alignment(-2 + controller.value * 4, -.3),
        end: Alignment(-1 + controller.value * 4, .3),
        colors: [line, line.withValues(alpha: .3), line],
      ).createShader(rect),
      child: child,
    ),
  );
}

class PageSkeleton extends StatelessWidget {
  const PageSkeleton({super.key, this.hero = false});
  final bool hero;
  @override
  Widget build(BuildContext context) => SkeletonShimmer(
    child: ListView(
      padding: EdgeInsets.fromLTRB(24, 24, 24, contentBottom(context)),
      children: [
        if (hero) const SkeletonBlock(height: 320),
        const SizedBox(height: 24),
        const SkeletonBlock(width: 160, height: 12),
        const SizedBox(height: 20),
        Row(
          children: List.generate(
            3,
            (i) => const Expanded(
              child: Padding(
                padding: EdgeInsets.only(right: 12),
                child: SkeletonBlock(height: 180),
              ),
            ),
          ),
        ),
        const SizedBox(height: 20),
        const SkeletonBlock(width: 220, height: 12),
        const SizedBox(height: 12),
        const SkeletonBlock(height: 12),
      ],
    ),
  );
}

class ImageSources extends InheritedWidget {
  const ImageSources({super.key, required this.provider, required super.child});
  final ImageProvider Function(String) provider;
  static ImageProvider? of(BuildContext context, String url) =>
      context.dependOnInheritedWidgetOfExactType<ImageSources>()?.provider(url);
  @override
  bool updateShouldNotify(ImageSources old) => old.provider != provider;
}

class CoverImage extends StatelessWidget {
  const CoverImage(
    this.url, {
    super.key,
    required this.width,
    required this.height,
    this.banner = false,
    this.fit = BoxFit.cover,
  });
  final String url;
  final double width, height;
  final bool banner;
  final BoxFit fit;
  @override
  Widget build(BuildContext context) {
    final pixels =
        ((banner ? math.max(width, height * 4) : width) *
                MediaQuery.devicePixelRatioOf(context))
            .round()
            .clamp(160, banner ? 2560 : 480);
    if (url.isEmpty) {
      return SizedBox(
        width: width,
        height: height,
        child: const ColoredBox(color: panel),
      );
    }
    final source = ImageSources.of(context, url);
    if (source != null) {
      return RepaintBoundary(
        child: Image(
          image: ResizeImage.resizeIfNeeded(pixels, null, source),
          width: width,
          height: height,
          fit: fit,
        ),
      );
    }
    return RepaintBoundary(
      child: CachedNetworkImage(
        imageUrl: url,
        width: width,
        height: height,
        fit: fit,
        memCacheWidth: pixels,
        maxWidthDiskCache: pixels,
        fadeInDuration: const Duration(milliseconds: 200),
        placeholder: (_, url) => SkeletonBlock(width: width, height: height),
        errorWidget: (_, url, error) => SizedBox(
          width: width,
          height: height,
          child: const ColoredBox(color: panel),
        ),
      ),
    );
  }
}

class HeroArt extends StatelessWidget {
  const HeroArt(this.anime, {super.key, required this.height});
  final Anime anime;
  final double height;
  @override
  Widget build(BuildContext context) {
    final width = MediaQuery.sizeOf(context).width;
    final cover = anime.coverLarge.isNotEmpty ? anime.coverLarge : anime.cover;
    return ClipRect(
      child: Stack(
        fit: StackFit.expand,
        children: [
          const ColoredBox(color: bgDeep),
          if (anime.banner.isNotEmpty)
            CoverImage(anime.banner, width: width, height: height, banner: true)
          else ...[
            ImageFiltered(
              imageFilter: ImageFilter.blur(sigmaX: 22, sigmaY: 22),
              child: Opacity(
                opacity: .45,
                child: CoverImage(
                  cover,
                  width: width,
                  height: height,
                  banner: true,
                ),
              ),
            ),
            Align(
              alignment: Alignment.centerRight,
              child: Opacity(
                opacity: .6,
                child: CoverImage(
                  cover,
                  width: width * .62,
                  height: height,
                  fit: BoxFit.contain,
                  banner: true,
                ),
              ),
            ),
          ],
          const DecoratedBox(
            decoration: BoxDecoration(
              gradient: LinearGradient(
                colors: [bg, Color(0xd90e0d0c), Color(0x000e0d0c)],
                stops: [0, .38, 1],
              ),
            ),
          ),
          const DecoratedBox(
            decoration: BoxDecoration(
              gradient: LinearGradient(
                begin: Alignment.topCenter,
                end: Alignment.bottomCenter,
                colors: [Color(0x000e0d0c), Color(0x500e0d0c), bg],
                stops: [0, .6, 1],
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class OtakaseRoute<T> extends PageRouteBuilder<T> {
  OtakaseRoute({required WidgetBuilder builder})
    : super(
        pageBuilder: (context, a, b) => builder(context),
        transitionDuration: const Duration(milliseconds: 450),
        reverseTransitionDuration: const Duration(milliseconds: 400),
        transitionsBuilder: (context, a, b, child) =>
            FadeTransition(opacity: a, child: child),
      );
}

class PosterNavigation extends InheritedWidget {
  const PosterNavigation({
    super.key,
    required this.onOpen,
    required super.child,
  });
  final void Function(Anime, Object) onOpen;
  static void open(
    BuildContext context,
    Anime anime,
    Object tag,
    VoidCallback fallback,
  ) {
    final scope = context.getInheritedWidgetOfExactType<PosterNavigation>();
    if (scope == null) {
      fallback();
    } else {
      scope.onOpen(anime, tag);
    }
  }

  @override
  bool updateShouldNotify(PosterNavigation old) => old.onOpen != onOpen;
}

class PosterCard extends StatelessWidget {
  const PosterCard({
    super.key,
    required this.anime,
    required this.onOpen,
    required this.isTV,
    this.width = 140,
    this.progress,
    this.caption,
    this.autofocus = false,
    this.onFocused,
  });
  final Anime anime;
  final VoidCallback onOpen;
  final bool isTV, autofocus;
  final double width;
  final double? progress;
  final String? caption;
  final VoidCallback? onFocused;
  @override
  Widget build(BuildContext context) => SizedBox(
    width: width,
    child: Semantics(
      label: anime.title,
      button: true,
      child: FocusBox(
        isTV: isTV,
        autofocus: autofocus,
        onPressed: () => PosterNavigation.open(
          context,
          anime,
          'poster-${key ?? anime.key}',
          onOpen,
        ),
        onFocused: onFocused,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            Hero(
              tag: 'poster-${key ?? anime.key}',
              child: ClipRRect(
                borderRadius: BorderRadius.circular(6),
                child: AspectRatio(
                  aspectRatio: 2 / 3,
                  child: Stack(
                    fit: StackFit.expand,
                    children: [
                      CoverImage(
                        anime.cover,
                        width: width,
                        height: width * 1.5,
                      ),
                      Positioned(
                        top: 6,
                        left: 6,
                        child: Container(
                          padding: const EdgeInsets.symmetric(
                            horizontal: 5,
                            vertical: 3,
                          ),
                          color: bgDeep.withValues(alpha: .8),
                          child: Text(
                            '${anime.formatLabel} · ${anime.airedEpisodes}/${anime.episodes > 0 ? anime.episodes : '?'}',
                            style: mono.copyWith(
                              fontSize: 8,
                              letterSpacing: .4,
                              color: fg,
                            ),
                          ),
                        ),
                      ),
                      if (progress != null)
                        Positioned(
                          left: 0,
                          right: 0,
                          bottom: 0,
                          child: LinearProgressIndicator(
                            value: progress!.clamp(0, 1),
                            backgroundColor: line,
                            color: shu,
                            minHeight: 2,
                          ),
                        ),
                    ],
                  ),
                ),
              ),
            ),
            if (!isTV) ...[
              const SizedBox(height: 7),
              Text(
                '${anime.score > 0 ? anime.score.toStringAsFixed(1) : '—'} · ${anime.year > 0 ? anime.year : '?'} · ${anime.duration} MIN',
                maxLines: 1,
                style: mono.copyWith(
                  fontSize: 8,
                  letterSpacing: .1,
                  color: anime.score >= 8 ? ok : muted,
                ),
              ),
              const SizedBox(height: 3),
              Text(
                caption ?? anime.title,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: prose.copyWith(fontSize: 17, height: 1.15),
              ),
            ],
          ],
        ),
      ),
    ),
  );
}

class PosterRow extends StatefulWidget {
  const PosterRow({
    super.key,
    required this.title,
    required this.items,
    required this.isTV,
    required this.onOpen,
    this.progress,
    this.captions,
    this.trailing,
  });
  final String title;
  final List<Anime> items;
  final bool isTV;
  final void Function(Anime) onOpen;
  final double? Function(Anime)? progress;
  final String? Function(Anime)? captions;
  final Widget? trailing;
  @override
  State<PosterRow> createState() => _PosterRowState();
}

class _PosterRowState extends State<PosterRow> {
  int focused = 0;
  final scroll = ScrollController();
  @override
  void dispose() {
    scroll.dispose();
    super.dispose();
  }

  void focus(int i, double width) {
    setState(() => focused = i);
    if (widget.isTV && scroll.hasClients) {
      unawaited(
        scroll.animateTo(
          ((i - 1) * (width + 12)).clamp(0, scroll.position.maxScrollExtent),
          duration: const Duration(milliseconds: 200),
          curve: Curves.easeOut,
        ),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    if (widget.items.isEmpty) return const SizedBox.shrink();
    final width = widget.isTV ? 154.0 : 130.0;
    final kanji = {
      'Continue Watching': '続',
      'Recently Released': '新',
      'Trending': '熱',
      'Popular this season': '今',
      'Airing today': '暦',
      'Related': '縁',
      'Recommended': '薦',
    }[widget.title];
    return Padding(
      key: ValueKey('section-${widget.title}'),
      padding: const EdgeInsets.only(top: 28, bottom: 4),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: EdgeInsets.symmetric(horizontal: widget.isTV ? 48 : 24),
            child: Row(
              children: [
                Expanded(child: SectionLabel(widget.title, kanji: kanji)),
                ?widget.trailing,
              ],
            ),
          ),
          const SizedBox(height: 14),
          SizedBox(
            height: width * 1.5 + (widget.isTV ? 24 : 78),
            child: ListView.separated(
              controller: scroll,
              key: PageStorageKey('row-${widget.title}'),
              scrollDirection: Axis.horizontal,
              padding: EdgeInsets.symmetric(
                horizontal: widget.isTV ? 42 : 18,
                vertical: 8,
              ),
              clipBehavior: Clip.none,
              itemCount: widget.items.length,
              separatorBuilder: (_, i) => const SizedBox(width: 12),
              itemBuilder: (_, i) => PosterCard(
                key: ValueKey('${widget.title}-${widget.items[i].key}'),
                anime: widget.items[i],
                onOpen: () => widget.onOpen(widget.items[i]),
                onFocused: () => focus(i, width),
                isTV: widget.isTV,
                width: width,
                progress: widget.progress?.call(widget.items[i]),
                caption: widget.captions?.call(widget.items[i]),
              ),
            ),
          ),
          if (widget.isTV)
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 48),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    widget
                        .items[focused.clamp(0, widget.items.length - 1)]
                        .title,
                    style: prose,
                  ),
                  MetaLine(
                    widget.items[focused.clamp(0, widget.items.length - 1)],
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

class MetaLine extends StatelessWidget {
  const MetaLine(this.anime, {super.key});
  final Anime anime;
  @override
  Widget build(BuildContext context) => Text(
    [
      if (anime.score > 0) '★ ${anime.score.toStringAsFixed(1)}',
      anime.formatLabel,
      if (anime.year > 0) '${anime.year}',
      if (anime.duration > 0) '${anime.duration} MIN',
      if (anime.episodes > 0) '${anime.episodes} EP',
    ].where((s) => s.isNotEmpty).join(' · '),
    style: mono.copyWith(color: fg, letterSpacing: 1, fontSize: 9, height: 1.7),
  );
}

class FailureView extends StatelessWidget {
  const FailureView({
    super.key,
    required this.message,
    required this.onRetry,
    this.isTV = false,
  });
  final String message;
  final VoidCallback onRetry;
  final bool isTV;
  @override
  Widget build(BuildContext context) => EmptyState(
    error: true,
    message: message,
    action: AppButton(label: 'Retry', onPressed: onRetry, isTV: isTV),
  );
}

String countdown(int epoch) {
  final left = DateTime.fromMillisecondsSinceEpoch(epoch * 1000)
      .difference(DateTime.now());
  if (left.isNegative) return 'Aired';
  if (left.inDays > 0) return '${left.inDays}d ${left.inHours % 24}h';
  if (left.inHours > 0) return '${left.inHours}h ${left.inMinutes % 60}m';
  return '${left.inMinutes.clamp(0, 59)}m';
}

String clockTime(DateTime at) =>
    '${at.hour.toString().padLeft(2, '0')}:${at.minute.toString().padLeft(2, '0')}';

class ChoiceRow extends StatelessWidget {
  const ChoiceRow({
    super.key,
    required this.title,
    required this.onTap,
    this.autofocus = false,
    this.trailing,
  });
  final Widget title;
  final VoidCallback? onTap;
  final bool autofocus;
  final Widget? trailing;
  @override
  Widget build(BuildContext context) => FocusBox(
    autofocus: autofocus,
    onPressed: onTap,
    padding: const EdgeInsets.symmetric(vertical: 12, horizontal: 8),
    child: Row(
      children: [
        Expanded(child: title),
        ?trailing,
      ],
    ),
  );
}

Future<String?> chooseValue(
  BuildContext context,
  String title,
  List<String> values, {
  String? selected,
}) => showDialog<String>(
  context: context,
  builder: (context) => AlertDialog(
    backgroundColor: panel,
    title: Text(title, style: display.copyWith(fontSize: 22)),
    content: SizedBox(
      width: 460,
      height: (values.length * 56.0).clamp(80, 420),
      child: ListView(
        children: values
            .map(
              (value) => ChoiceRow(
                autofocus:
                    value == selected ||
                    selected == null && value == values.first,
                title: Text(value, style: prose),
                trailing: value == selected
                    ? const Icon(LucideIcons.check, color: shu)
                    : null,
                onTap: () => Navigator.pop(context, value),
              ),
            )
            .toList(),
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('BACK'),
      ),
    ],
  ),
);
