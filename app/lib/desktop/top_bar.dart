import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../ui/components.dart';
import 'header_island.dart';
import 'window_frame.dart';

/// The top bar of desktop and TV: the logo and the end controls on islands
/// grown out of the top corners and the [tabs] on an island hanging from the
/// middle. On a computer the end island holds the window buttons, and macOS
/// keeps its own buttons top left, so the logo moves to the right there. A TV
/// passes its clock as [trailing] and an [inset] that keeps every control
/// inside the screen's safe area.
class DesktopTopBar extends StatelessWidget {
  const DesktopTopBar({
    super.key,
    required this.brand,
    required this.tabs,
    this.leading,
    this.trailing,
    this.inset = EdgeInsets.zero,
  });

  final Widget brand, tabs;

  /// Shown before the logo, such as a back button on pushed pages.
  final Widget? leading;

  /// Replaces the window buttons, such as the clock on a TV.
  final Widget? trailing;

  /// Extra room at the top and sides, inside the islands.
  final EdgeInsets inset;

  static const height = 84.0;
  static const islandHeight = 66.0;

  /// The full height for this [inset] and the reader's text size.
  static double extent(BuildContext context, {double top = 0}) =>
      top + height - islandHeight + islandExtent(context);

  /// Island height, taller when large text needs the room.
  static double islandExtent(BuildContext context) => math.max(
    islandHeight,
    MediaQuery.textScalerOf(context).scale(20) * 1.4 + 28,
  );

  @override
  Widget build(BuildContext context) {
    final window = trailing == null;
    final mac = window && macWindowLayout;
    final flush = window && windowButtonStyle == WindowButtonStyle.windows;
    final islandTall = islandExtent(context);
    // A narrow window has no room for the name beside the centred tabs.
    final showBrand = MediaQuery.sizeOf(context).width >= 760;
    Widget island(
      IslandSide side,
      Widget child,
      EdgeInsets padding, {
      AlignmentGeometry align = Alignment.center,
    }) => HeaderIsland(
      side: side,
      padding: padding.copyWith(top: padding.top + inset.top),
      child: SizedBox(
        height: islandTall,
        child: Align(alignment: align, widthFactor: 1, child: child),
      ),
    );
    final start = Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (mac) SizedBox(width: windowButtonsLeading),
        if (leading != null) ...[leading!, const SizedBox(width: 10)],
        if (!mac && showBrand) brand,
      ],
    );
    return SizedBox(
      height: extent(context, top: inset.top),
      child: Stack(
        clipBehavior: Clip.none,
        children: [
          Positioned(
            top: 0,
            left: 0,
            child: island(
              IslandSide.left,
              start,
              EdgeInsets.fromLTRB(
                mac ? 0 : (leading != null ? 24 : 40) + inset.left,
                0,
                mac ? 8 : 28,
                0,
              ),
            ),
          ),
          Positioned(
            top: 0,
            left: 0,
            right: 0,
            child: Center(
              child: island(
                IslandSide.center,
                tabs,
                const EdgeInsets.fromLTRB(22, 6, 22, 0),
              ),
            ),
          ),
          Positioned(
            top: 0,
            right: 0,
            child: island(
              IslandSide.right,
              trailing ??
                  (mac
                      ? (showBrand ? brand : const SizedBox.shrink())
                      : const WindowButtons()),
              flush
                  ? const EdgeInsets.only(left: 18)
                  : EdgeInsets.fromLTRB(26, 0, 24 + inset.right, 0),
              align: flush ? Alignment.topCenter : Alignment.center,
            ),
          ),
        ],
      ),
    );
  }
}

/// A plain top bar tab for pages pushed over the main tabs: the label, an
/// underline when it is the open tab, brighter on hover.
class DesktopTab extends StatefulWidget {
  const DesktopTab({
    super.key,
    required this.label,
    required this.selected,
    required this.onPressed,
  });
  final String label;
  final bool selected;
  final VoidCallback onPressed;
  @override
  State<DesktopTab> createState() => _DesktopTabState();
}

class _DesktopTabState extends State<DesktopTab> {
  bool hovered = false;
  @override
  Widget build(BuildContext context) => FocusBox(
    isTV: true,
    radius: 16,
    frameGap: 5,
    focusFill: Colors.transparent,
    hoverColor: Colors.transparent,
    onHover: (value) => setState(() => hovered = value),
    onPressed: widget.onPressed,
    child: Container(
      constraints: const BoxConstraints(minHeight: 48),
      padding: const EdgeInsets.symmetric(horizontal: 6),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(
            widget.label,
            style: uiStyle(
              context,
              weight: widget.selected ? FontWeight.w600 : FontWeight.w500,
              color: widget.selected || hovered ? fgBright : muted,
            ),
          ),
          const SizedBox(height: 4),
          Container(
            width: 34,
            height: 3,
            decoration: BoxDecoration(
              color: widget.selected ? shu : Colors.transparent,
              borderRadius: BorderRadius.circular(2),
            ),
          ),
        ],
      ),
    ),
  );
}
