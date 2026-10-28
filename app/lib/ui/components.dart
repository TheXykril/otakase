import 'dart:async';

import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';

import '../data/models.dart';

const shu = Color(0xffd84836);
const panel = Color(0xff151515);
const muted = Color(0xff989898);

class FocusBox extends StatefulWidget {
  const FocusBox({
    super.key,
    required this.child,
    required this.onPressed,
    this.isTV = false,
    this.autofocus = false,
    this.focusNode,
    this.padding = EdgeInsets.zero,
    this.radius = 14,
    this.scale = 1.04,
    this.onFocused,
  });
  final Widget child;
  final VoidCallback? onPressed;
  final bool isTV, autofocus;
  final FocusNode? focusNode;
  final EdgeInsets padding;
  final double radius, scale;
  final VoidCallback? onFocused;
  @override
  State<FocusBox> createState() => _FocusBoxState();
}

class _FocusBoxState extends State<FocusBox> {
  bool focused = false;
  @override
  Widget build(BuildContext context) => AnimatedScale(
    scale: focused && widget.isTV ? widget.scale : 1,
    duration: const Duration(milliseconds: 120),
    child: AnimatedContainer(
      duration: const Duration(milliseconds: 100),
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(widget.radius),
        border: Border.all(
          color: focused ? shu : Colors.transparent,
          width: 2.5,
        ),
      ),
      child: Material(
        color: Colors.transparent,
        borderRadius: BorderRadius.circular(widget.radius),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          autofocus: widget.autofocus,
          focusNode: widget.focusNode,
          onTap: widget.onPressed,
          focusColor: shu.withValues(alpha: .15),
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
                      duration: const Duration(milliseconds: 150),
                      alignmentPolicy:
                          ScrollPositionAlignmentPolicy.keepVisibleAtEnd,
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
    radius: 28,
    onPressed: onPressed,
    child: Container(
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
      decoration: BoxDecoration(
        color: filled ? shu : Colors.white.withValues(alpha: .08),
        borderRadius: BorderRadius.circular(24),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (icon != null) ...[Icon(icon, size: 20), const SizedBox(width: 8)],
          Flexible(
            child: Text(
              label,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                fontWeight: FontWeight.w600,
                color: onPressed == null ? muted : Colors.white,
              ),
            ),
          ),
        ],
      ),
    ),
  );
}

class CoverImage extends StatelessWidget {
  const CoverImage(
    this.url, {
    super.key,
    required this.width,
    required this.height,
    this.banner = false,
  });
  final String url;
  final double width, height;
  final bool banner;
  @override
  Widget build(BuildContext context) {
    final pixels = (width * MediaQuery.devicePixelRatioOf(context))
        .round()
        .clamp(160, banner ? 1920 : 480);
    if (url.isEmpty) {
      return Container(
        color: panel,
        child: const Center(child: Icon(Icons.movie_outlined, color: muted)),
      );
    }
    return RepaintBoundary(
      child: CachedNetworkImage(
        imageUrl: url,
        width: width,
        height: height,
        fit: BoxFit.cover,
        memCacheWidth: pixels,
        maxWidthDiskCache: pixels,
        fadeInDuration: const Duration(milliseconds: 100),
        placeholder: (_, url) => Container(color: panel),
        errorWidget: (_, url, error) => Container(
          color: panel,
          child: const Icon(Icons.broken_image_outlined, color: muted),
        ),
      ),
    );
  }
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
  });
  final Anime anime;
  final VoidCallback onOpen;
  final bool isTV, autofocus;
  final double width;
  final double? progress;
  final String? caption;
  @override
  Widget build(BuildContext context) => SizedBox(
    width: width,
    child: Semantics(
      label: anime.title,
      button: true,
      child: FocusBox(
        isTV: isTV,
        autofocus: autofocus,
        onPressed: onOpen,
        radius: 12,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            ClipRRect(
              borderRadius: BorderRadius.circular(9),
              child: SizedBox(
                width: width,
                height: width * 1.5,
                child: Stack(
                  fit: StackFit.expand,
                  children: [
                    CoverImage(anime.cover, width: width, height: width * 1.5),
                    Positioned(
                      top: 6,
                      right: 6,
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.end,
                        children: [
                          _badge(anime.formatLabel),
                          const SizedBox(height: 3),
                          _badge(
                            '${anime.airedEpisodes > 0 ? anime.airedEpisodes : '?'} / ${anime.episodes > 0 ? anime.episodes : '?'}',
                            icon: Icons.subtitles_outlined,
                          ),
                        ],
                      ),
                    ),
                    if (!isTV)
                      Positioned(
                        left: 0,
                        right: 0,
                        bottom: 0,
                        child: Container(
                          padding: const EdgeInsets.all(5),
                          color: Colors.black.withValues(alpha: .8),
                          child: Text(
                            '${anime.score > 0 ? anime.score.toStringAsFixed(1) : '—'}  ·  ${anime.year > 0 ? anime.year : '?'}  ·  ${anime.duration}m',
                            textAlign: TextAlign.center,
                            style: const TextStyle(
                              fontSize: 9,
                              color: Colors.white,
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
                          backgroundColor: Colors.black54,
                          color: shu,
                          minHeight: 4,
                        ),
                      ),
                  ],
                ),
              ),
            ),
            if (!isTV || caption != null)
              Padding(
                padding: const EdgeInsets.only(top: 8, left: 3, right: 3),
                child: Text(
                  caption ?? anime.title,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(
                    fontSize: isTV ? 12 : 12,
                    fontWeight: FontWeight.w500,
                    color: Colors.white,
                  ),
                ),
              ),
          ],
        ),
      ),
    ),
  );
  Widget _badge(String label, {IconData? icon}) => Container(
    padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 3),
    decoration: BoxDecoration(
      color: Colors.black.withValues(alpha: .8),
      borderRadius: BorderRadius.circular(4),
    ),
    child: Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (icon != null) ...[Icon(icon, size: 10), const SizedBox(width: 3)],
        Text(
          label,
          style: const TextStyle(fontSize: 9, fontWeight: FontWeight.w700),
        ),
      ],
    ),
  );
}

class PosterRow extends StatelessWidget {
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
  Widget build(BuildContext context) {
    final width = isTV ? 154.0 : 122.0;
    return Padding(
      padding: const EdgeInsets.only(top: 22, bottom: 6),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: EdgeInsets.symmetric(horizontal: isTV ? 38 : 20),
            child: Row(
              children: [
                Expanded(
                  child: Text(
                    title,
                    style: TextStyle(
                      fontSize: isTV ? 21 : 19,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
                ?trailing,
              ],
            ),
          ),
          const SizedBox(height: 10),
          if (items.isEmpty)
            const Padding(
              padding: EdgeInsets.all(25),
              child: Text('Nothing here', style: TextStyle(color: muted)),
            )
          else
            SizedBox(
              height: width * 1.5 + (!isTV || captions != null ? 54 : 20),
              child: ListView.separated(
                key: PageStorageKey('row-$title'),
                scrollDirection: Axis.horizontal,
                padding: EdgeInsets.symmetric(
                  horizontal: isTV ? 32 : 14,
                  vertical: 6,
                ),
                clipBehavior: Clip.none,
                itemCount: items.length,
                separatorBuilder: (_, index) => const SizedBox(width: 12),
                itemBuilder: (_, index) => PosterCard(
                  key: ValueKey('$title-${items[index].key}'),
                  anime: items[index],
                  onOpen: () => onOpen(items[index]),
                  isTV: isTV,
                  width: width,
                  progress: progress?.call(items[index]),
                  caption: captions?.call(items[index]),
                ),
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
  Widget build(BuildContext context) => Wrap(
    spacing: 14,
    runSpacing: 6,
    crossAxisAlignment: WrapCrossAlignment.center,
    children: [
      if (anime.score > 0)
        Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.star_outline, size: 17, color: shu),
            const SizedBox(width: 4),
            Text(
              anime.score.toStringAsFixed(1),
              style: const TextStyle(fontWeight: FontWeight.bold),
            ),
          ],
        ),
      Text(
        [
          anime.formatLabel,
          if (anime.year > 0) '${anime.year}',
        ].where((s) => s.isNotEmpty).join(' · '),
      ),
      if (anime.duration > 0) Text('${anime.duration} min'),
      if (anime.episodes > 0) Text('${anime.episodes} episodes'),
    ],
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
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.all(30),
    child: Column(
      mainAxisAlignment: MainAxisAlignment.center,
      children: [
        const Icon(Icons.wifi_off_outlined, size: 36, color: muted),
        const SizedBox(height: 16),
        Text(
          message,
          textAlign: TextAlign.center,
          style: const TextStyle(color: muted),
        ),
        const SizedBox(height: 16),
        AppButton(label: 'Retry', onPressed: onRetry, isTV: isTV),
      ],
    ),
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
Future<String?> chooseValue(
  BuildContext context,
  String title,
  List<String> values, {
  String? selected,
}) => showDialog<String>(
  context: context,
  builder: (context) => AlertDialog(
    backgroundColor: panel,
    title: Text(title),
    content: SizedBox(
      width: 460,
      height: (values.length * 56.0).clamp(80, 420),
      child: ListView(
        children: values
            .map(
              (value) => ListTile(
                autofocus:
                    value == selected ||
                    selected == null && value == values.first,
                focusColor: shu.withValues(alpha: .3),
                title: Text(value),
                trailing: value == selected
                    ? const Icon(Icons.check, color: shu)
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
        child: const Text('Back'),
      ),
    ],
  ),
);
