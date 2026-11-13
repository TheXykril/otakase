import 'package:flutter/material.dart';

import '../ui/tokens.dart';

/// Where a top bar island sits: grown out of the left or right window
/// corner, or hanging from the middle of the top edge.
enum IslandSide { left, center, right }

/// A soft dark shape behind a group of top bar controls, like a phone camera
/// island. Its top melts into the window edge with curved flares and its
/// edges fade into the art behind it, so no hard line shows.
class HeaderIsland extends StatelessWidget {
  const HeaderIsland({
    super.key,
    required this.side,
    required this.child,
    this.padding = EdgeInsets.zero,
  });

  final IslandSide side;
  final Widget child;
  final EdgeInsets padding;

  @override
  Widget build(BuildContext context) => CustomPaint(
    painter: IslandPainter(side),
    child: Padding(padding: padding, child: child),
  );
}

class IslandPainter extends CustomPainter {
  const IslandPainter(this.side);
  final IslandSide side;

  static const color = islandFill;
  static const flare = 18.0;
  static const radius = 26.0;

  /// The island outline for a box of [size]. Flares reach [flare] past the
  /// open sides, and window-edge sides run well off screen so the blur never
  /// shows an edge there.
  static Path shape(IslandSide side, Size size) {
    final w = size.width, h = size.height, f = flare, r = radius;
    const off = 80.0;
    final p = Path();
    switch (side) {
      case IslandSide.center:
        p
          ..moveTo(-f, -off)
          ..lineTo(w + f, -off)
          ..lineTo(w + f, 0)
          ..cubicTo(w + f * .55, 0, w, f * .45, w, f)
          ..lineTo(w, h - r)
          ..quadraticBezierTo(w, h, w - r, h)
          ..lineTo(r, h)
          ..quadraticBezierTo(0, h, 0, h - r)
          ..lineTo(0, f)
          ..cubicTo(0, f * .45, -f * .55, 0, -f, 0)
          ..close();
      case IslandSide.left:
        p
          ..moveTo(-off, -off)
          ..lineTo(w + f, -off)
          ..lineTo(w + f, 0)
          ..cubicTo(w + f * .55, 0, w, f * .45, w, f)
          ..lineTo(w, h - r)
          ..quadraticBezierTo(w, h, w - r, h)
          ..lineTo(-off, h)
          ..close();
      case IslandSide.right:
        p
          ..moveTo(w + off, -off)
          ..lineTo(-f, -off)
          ..lineTo(-f, 0)
          ..cubicTo(-f * .55, 0, 0, f * .45, 0, f)
          ..lineTo(0, h - r)
          ..quadraticBezierTo(0, h, r, h)
          ..lineTo(w + off, h)
          ..close();
    }
    return p;
  }

  @override
  void paint(Canvas canvas, Size size) {
    final path = shape(side, size);
    // A wide faded shadow drawn twice for depth, then the island itself with
    // a short soft edge on top.
    final halo = Paint()
      ..color = color.withValues(alpha: .92)
      ..maskFilter = const MaskFilter.blur(BlurStyle.normal, 12);
    canvas
      ..drawPath(path, halo)
      ..drawPath(path, halo)
      ..drawPath(
        path,
        Paint()
          ..color = color.withValues(alpha: .9)
          ..maskFilter = const MaskFilter.blur(BlurStyle.normal, 3),
      );
  }

  @override
  bool shouldRepaint(IslandPainter old) => old.side != side;
}
