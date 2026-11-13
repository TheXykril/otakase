import 'dart:async';
import 'dart:math' as math;
import 'dart:ui';

import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';

import '../desktop/desktop_core.dart';
import '../desktop/top_bar.dart' show DesktopTopBar;

import 'package:flutter/services.dart';
import 'package:lucide_icons_flutter/lucide_icons.dart';

import '../data/models.dart';
import 'tokens.dart';

export 'package:lucide_icons_flutter/lucide_icons.dart';

export 'tokens.dart';

const mono = TextStyle(
  fontFamily: 'JetBrainsMono',
  fontFamilyFallback: ['NotoSansJP'],
  fontSize: 15,
  letterSpacing: .2,
  color: muted,
  height: 1.5,
);
const prose = TextStyle(
  fontFamily: 'Inter',
  fontFamilyFallback: ['NotoSansJP'],
  fontSize: 16,
  height: 1.4,
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

// Share the device typography with routes, sheets and dialogs.
class AppLayout extends InheritedWidget {
  const AppLayout({
    super.key,
    required this.isTV,
    this.minText,
    required super.child,
  });
  final bool isTV;

  /// Overrides the screen's smallest text, for a window outside the scaled
  /// canvas such as the desktop launcher.
  final double? minText;
  static bool tvOf(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<AppLayout>()?.isTV ?? false;

  /// Which column of [KageType] this context reads from. Desktop has its
  /// own: it is wide like a TV but read from arm's length.
  static TypeScreen screenOf(BuildContext context) => !tvOf(context)
      ? TypeScreen.phone
      : isDesktop
      ? TypeScreen.desktop
      : TypeScreen.tv;

  static double minTextOf(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<AppLayout>()?.minText ??
      KageType.floor[screenOf(context)]!;
  @override
  bool updateShouldNotify(AppLayout old) =>
      old.isTV != isTV || old.minText != minText;
}

/// A named text size for this screen; see [KageType].
TextStyle roleStyle(
  BuildContext context,
  TextRole role, {
  Color color = fg,
  FontWeight? weight,
}) {
  final size = KageType.size(role, AppLayout.screenOf(context));
  return switch (role) {
    TextRole.display || TextRole.title => display.copyWith(
      fontSize: size,
      color: color == fg ? fgBright : color,
    ),
    TextRole.kicker => mono.copyWith(
      fontSize: size,
      color: color,
      letterSpacing: size * .08,
      fontWeight: weight ?? FontWeight.w500,
    ),
    _ => uiStyle(
      context,
      size: size,
      color: role == TextRole.meta && color == fg ? muted : color,
      weight:
          weight ??
          (role == TextRole.heading || role == TextRole.label
              ? FontWeight.w600
              : FontWeight.w400),
    ),
  };
}

// Android TVs report different densities for the same output resolution.
// A 1600 x 900 canvas gives every 16:9 TV the same compact composition.
// Scale the whole Navigator so routes, focus frames and hit testing agree.
/// Whether the desktop viewer is moving around with the keyboard (arrow keys
/// or Tab) rather than the mouse.
abstract final class DesktopInput {
  static final keyboardMode = ValueNotifier(false);
  static bool get keyboard => keyboardMode.value;
  static bool _pointerListening = false;
  static void key(KeyEvent event) {
    if (event is KeyDownEvent &&
        {
          LogicalKeyboardKey.arrowUp,
          LogicalKeyboardKey.arrowDown,
          LogicalKeyboardKey.arrowLeft,
          LogicalKeyboardKey.arrowRight,
          LogicalKeyboardKey.tab,
        }.contains(event.logicalKey)) {
      keyboardMode.value = true;
    }
  }

  static bool _key(KeyEvent event) {
    key(event);
    return false;
  }

  /// Where the mouse last went down, in window coordinates. A desktop
  /// picker opens its menu here.
  static Offset? lastPointerDown;

  static void pointer(PointerEvent event) {
    if (event is PointerDownEvent) lastPointerDown = event.position;
    if (event is PointerDownEvent ||
        event is PointerScrollEvent ||
        event is PointerHoverEvent ||
        event is PointerMoveEvent) {
      keyboardMode.value = false;
    }
  }

  static void listen() {
    // Reattach safely after an engine/test input reset; never register twice.
    HardwareKeyboard.instance.removeHandler(_key);
    HardwareKeyboard.instance.addHandler(_key);
    if (!_pointerListening) {
      GestureBinding.instance.pointerRouter.addGlobalRoute(pointer);
      _pointerListening = true;
    }
  }
}

class TVViewport extends StatefulWidget {
  const TVViewport({super.key, required this.child});
  final Widget child;
  static final traversal = RemoteTraversalPolicy();
  @override
  State<TVViewport> createState() => _TVViewportState();
}

class _TVViewportState extends State<TVViewport> {
  bool checking = false;

  @override
  void initState() {
    super.initState();
    FocusManager.instance.addListener(focusChanged);
    if (isDesktop) DesktopInput.listen();
  }

  @override
  void dispose() {
    FocusManager.instance.removeListener(focusChanged);
    super.dispose();
  }

  // A remote has no pointer to pick a control with. When focus ends up
  // parked on a scope, or behind the page on top (a page that opens with
  // nothing autofocused, or a focused button that disappears), move it to a
  // control on the top page once autofocus and the page's own requests have
  // had their turn.
  void focusChanged() {
    if (checking || strandedIn() == null) return;
    checking = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      scheduleMicrotask(() {
        checking = false;
        if (!mounted) return;
        final scope = strandedIn();
        if (scope != null) landFocus(scope);
      });
    });
    WidgetsBinding.instance.ensureVisualUpdate();
  }

  /// The scope focus should move into, or null when focus is on a control of
  /// the top page.
  static FocusScopeNode? strandedIn() {
    final primary = FocusManager.instance.primaryFocus;
    final context = primary?.context;
    if (primary == null || context == null || !context.mounted) return null;
    final navigator = Navigator.maybeOf(context, rootNavigator: true);
    if (navigator == null) return null;
    Route<dynamic>? top;
    navigator.popUntil((route) {
      top = route;
      return true;
    });
    final page = top is ModalRoute ? (top as ModalRoute).subtreeContext : null;
    if (page == null || !page.mounted) return null;
    final scope = FocusScope.of(page);
    final inside = primary == scope || primary.ancestors.contains(scope);
    if (inside && primary is! FocusScopeNode) return null;
    if (inside && primary is FocusScopeNode && hasControl(primary)) {
      return primary;
    }
    return hasControl(scope) ? scope : null;
  }

  static Iterable<FocusNode> controls(FocusScopeNode scope) =>
      scope.traversalDescendants.where(
        (node) =>
            node is! FocusScopeNode &&
            node.context != null &&
            node.canRequestFocus &&
            !node.skipTraversal &&
            !node.rect.isEmpty,
      );

  static bool hasControl(FocusScopeNode scope) => controls(scope).isNotEmpty;

  /// Focuses the control nearest the top left of the visible page.
  static void landFocus(FocusScopeNode scope) {
    final remembered = scope.focusedChild;
    if (remembered != null &&
        remembered is! FocusScopeNode &&
        remembered.canRequestFocus &&
        remembered.context != null) {
      remembered.requestFocus();
      return;
    }
    final all = controls(scope).toList();
    final visible = all.where((node) => node.rect.bottom > 0).toList();
    final pool = visible.isEmpty ? all : visible;
    pool.sort((a, b) {
      final row = a.rect.top.round().compareTo(b.rect.top.round());
      return row != 0 ? row : a.rect.left.compareTo(b.rect.left);
    });
    pool.first.requestFocus();
  }

  @override
  Widget build(BuildContext context) => FocusTraversalGroup(
    policy: TVViewport.traversal,
    child: LayoutBuilder(
      builder: (context, constraints) {
        // Desktop windows are closer to the screen than a TV and can be any
        // size, so they use a larger canvas and never shrink text below
        // a readable size.
        // The floor of .8 keeps desktop text (17 on the canvas) at least
        // about 14 px on screen in a small window.
        final scale = isDesktop
            ? math.max(
                .8,
                math.min(
                  constraints.maxWidth / 1920,
                  constraints.maxHeight / 1080,
                ),
              )
            : math.min(
                constraints.maxWidth / 1600,
                constraints.maxHeight / 900,
              );
        if (!scale.isFinite || scale <= 0) return widget.child;
        final size = Size(
          constraints.maxWidth / scale,
          constraints.maxHeight / scale,
        );
        final media = MediaQuery.of(context);
        return FittedBox(
          alignment: Alignment.topLeft,
          fit: BoxFit.contain,
          child: SizedBox(
            width: size.width,
            height: size.height,
            child: MediaQuery(
              data: media.copyWith(
                size: size,
                devicePixelRatio: media.devicePixelRatio * scale,
                padding: media.padding / scale,
                viewPadding: media.viewPadding / scale,
                viewInsets: media.viewInsets / scale,
                systemGestureInsets: media.systemGestureInsets / scale,
              ),
              child: widget.child,
            ),
          ),
        );
      },
    ),
  );
}

TextStyle uiStyle(
  BuildContext context, {
  double size = 16,
  Color color = fg,
  FontWeight weight = FontWeight.w400,
  double height = 1.4,
}) => TextStyle(
  fontFamily: 'Inter',
  fontFamilyFallback: const ['NotoSansJP'],
  fontSize: math.max(size, AppLayout.minTextOf(context)),
  fontWeight: weight,
  height: height,
  color: color,
);

const phoneBarHeight = 72.0, phoneBarMargin = 7.0;

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
    this.radius = KageRadius.md,
    this.frameGap = 5,
    this.scale = 1.00,
    this.onFocused,
    this.outsideFrame = false,
    this.showFocusBorder = true,
    this.focusFill = Colors.transparent,
    this.hoverColor,
    this.onHover,
    this.onFocusChanged,
    this.onLongPress,
    this.lift = false,
  });
  final Widget child;
  final bool outsideFrame, showFocusBorder;

  /// Cover art: on a computer it rises a little, with a deeper shadow and a
  /// touch of light, while the mouse is over it.
  final bool lift;
  final Color focusFill;
  final Color? hoverColor;
  final ValueChanged<bool>? onHover;
  final ValueChanged<bool>? onFocusChanged;
  final VoidCallback? onPressed, onFocused, onLongPress;
  final bool isTV, autofocus;
  final FocusNode? focusNode;
  final EdgeInsets padding;
  final double radius, scale, frameGap;
  @override
  State<FocusBox> createState() => _FocusBoxState();
}

class _FocusBoxState extends State<FocusBox> {
  bool focused = false;
  bool hovered = false;
  bool revealScheduled = false;
  void modeChanged(FocusHighlightMode _) {
    if (mounted) setState(() {});
  }

  @override
  void initState() {
    super.initState();
    FocusManager.instance.addHighlightModeListener(modeChanged);
    if (isDesktop) DesktopInput.listen();
    DesktopInput.keyboardMode.addListener(inputChanged);
  }

  @override
  void dispose() {
    FocusManager.instance.removeHighlightModeListener(modeChanged);
    DesktopInput.keyboardMode.removeListener(inputChanged);
    super.dispose();
  }

  void inputChanged() {
    if (mounted && isDesktop) setState(() {});
  }

  @override
  void didUpdateWidget(FocusBox old) {
    super.didUpdateWidget(old);
    if (focused && !isDesktop) scheduleReveal();
  }

  void scheduleReveal() {
    if (revealScheduled) return;
    revealScheduled = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      revealScheduled = false;
      unawaited(reveal());
    });
  }

  Future<void> reveal() async {
    if (!mounted || !focused || Scrollable.maybeOf(context) == null) return;
    // With a mouse the page stays where the viewer scrolled it; only arrow
    // keys and Tab scroll the focused control into view.
    if (isDesktop && !DesktopInput.keyboard) return;
    final row = context.findAncestorStateOfType<_RemoteFocusRowState>();
    final item = context.getInheritedWidgetOfExactType<RemoteFocusItem>();
    if (row != null && item != null) {
      if (!await row.reveal(item.index) || !mounted || !focused) return;
    }
    await revealFocus(context, stillFocused: () => mounted && focused);
  }

  @override
  Widget build(BuildContext context) {
    final visible =
        focused &&
        (isDesktop
            ? DesktopInput.keyboard
            : FocusManager.instance.highlightMode ==
                  FocusHighlightMode.traditional);
    final interaction = Material(
      color: Colors.transparent,
      child: InkWell(
        autofocus: widget.autofocus,
        focusNode: widget.focusNode,
        onTap: widget.onPressed,
        onLongPress: widget.onLongPress,
        focusColor: Colors.transparent,
        hoverColor: widget.hoverColor ?? panelRaised,
        onHover: (value) {
          if (widget.lift && isDesktop && hovered != value) {
            setState(() => hovered = value);
          }
          widget.onHover?.call(value);
        },
        splashColor: accentDeep.withValues(alpha: .2),
        borderRadius: BorderRadius.circular(widget.radius),
        onFocusChange: (value) {
          setState(() => focused = value);
          widget.onFocusChanged?.call(value);
          if (value) {
            widget.onFocused?.call();
            scheduleReveal();
          }
        },
        child: Padding(padding: widget.padding, child: widget.child),
      ),
    );
    if (widget.outsideFrame) {
      final framed = CustomPaint(
        foregroundPainter: visible
            ? _SplitFrame(widget.radius, widget.isTV)
            : null,
        child: interaction,
      );
      if (!widget.lift || !isDesktop) return framed;
      final motion = MediaQuery.disableAnimationsOf(context)
          ? Duration.zero
          : const Duration(milliseconds: 160);
      return AnimatedScale(
        duration: motion,
        curve: Curves.easeOut,
        scale: hovered ? 1.04 : 1,
        child: AnimatedContainer(
          duration: motion,
          curve: Curves.easeOut,
          foregroundDecoration: BoxDecoration(
            color: Colors.white.withValues(alpha: hovered ? .06 : 0),
            borderRadius: BorderRadius.circular(widget.radius),
          ),
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(widget.radius),
            boxShadow: [
              BoxShadow(
                color: Colors.black.withValues(alpha: hovered ? .55 : 0),
                blurRadius: 22,
                offset: const Offset(0, 10),
              ),
            ],
          ),
          child: framed,
        ),
      );
    }
    return AnimatedContainer(
      duration: MediaQuery.disableAnimationsOf(context)
          ? Duration.zero
          : const Duration(milliseconds: 180),
      curve: Curves.easeOut,
      padding: EdgeInsets.all(widget.frameGap),
      // Controls show one focus ring: 2 px of bright text colour just
      // outside the control, following its corner. Artwork uses the split
      // frame above instead.
      decoration: BoxDecoration(
        color: visible ? widget.focusFill : Colors.transparent,
        borderRadius: BorderRadius.circular(widget.radius + widget.frameGap),
        border: Border.all(
          color: visible && widget.showFocusBorder
              ? fgBright
              : Colors.transparent,
          width: 2,
        ),
      ),
      child: interaction,
    );
  }
}

/// Height of anything painted over the top of the page's scroll view, such as
/// the TV header that floats above Home.
class ScrollClearance extends InheritedWidget {
  const ScrollClearance({super.key, required this.top, required super.child});
  final double top;
  static double topOf(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<ScrollClearance>()?.top ?? 0;
  @override
  bool updateShouldNotify(ScrollClearance old) => old.top != top;
}

/// Marks the whole of a card, label included, as what a focused control
/// inside it scrolls into view.
class RevealArea extends StatelessWidget {
  const RevealArea({super.key, required this.child});
  final Widget child;
  @override
  Widget build(BuildContext context) => child;
}

BuildContext _revealArea(BuildContext context) {
  BuildContext? area;
  context.visitAncestorElements((element) {
    if (element.widget is Scrollable) return false;
    if (element.widget is RevealArea) {
      area = element;
      return false;
    }
    return true;
  });
  return area ?? context;
}

ScrollableState? _verticalScrollable(BuildContext context) {
  var scrollable = Scrollable.maybeOf(context);
  while (scrollable != null && scrollable.axisDirection != AxisDirection.down) {
    scrollable = Scrollable.maybeOf(scrollable.context);
  }
  return scrollable;
}

/// Scrolls a focused control into view. A control that fits on the page's
/// first screen brings the page back to its top, so headings above it show
/// again, and nothing ends up under [ScrollClearance].
Future<void> revealFocus(
  BuildContext focused, {
  bool Function()? stillFocused,
}) async {
  final context = _revealArea(focused);
  bool live() => stillFocused?.call() ?? true;
  final duration = MediaQuery.disableAnimationsOf(context)
      ? Duration.zero
      : const Duration(milliseconds: 180);
  final page = _verticalScrollable(context);
  final box = context.findRenderObject();
  final viewport = page?.context.findRenderObject();
  if (AppLayout.tvOf(context) &&
      page != null &&
      box is RenderBox &&
      box.attached &&
      viewport is RenderBox &&
      page.position.hasContentDimensions) {
    final position = page.position;
    final top = box.localToGlobal(Offset.zero, ancestor: viewport).dy;
    final bottom = top + box.size.height;
    final clearance = ScrollClearance.topOf(context);
    final double target =
        bottom + position.pixels - position.minScrollExtent <=
            position.viewportDimension
        ? position.minScrollExtent
        : top < clearance
        ? math.max(position.minScrollExtent, position.pixels + top - clearance)
        : position.pixels;
    if ((target - position.pixels).abs() > .5) {
      if (duration == Duration.zero) {
        position.jumpTo(target);
      } else {
        await position.animateTo(
          target,
          duration: duration,
          curve: Curves.ease,
        );
        // Another reveal took over the scroll.
        if ((position.pixels - target).abs() > .5) return;
      }
    }
  }
  if (!context.mounted || !live()) return;
  // Keep visible controls still. Only scroll the edge outside the viewport.
  await Scrollable.ensureVisible(
    context,
    duration: duration,
    alignmentPolicy: ScrollPositionAlignmentPolicy.keepVisibleAtEnd,
  );
  if (!context.mounted || !live()) return;
  await Scrollable.ensureVisible(
    context,
    duration: duration,
    alignmentPolicy: ScrollPositionAlignmentPolicy.keepVisibleAtStart,
  );
}

/// Remote traversal for TV. Up and down weigh distance the way Android's own
/// focus search does: a control in line with the current one wins only when
/// it is not further away than the whole of a closer one, so a narrow button
/// right above is never skipped for a wider one further up.
class RemoteTraversalPolicy extends ReadingOrderTraversalPolicy {
  RemoteTraversalPolicy()
    : super(
        requestFocusCallback:
            (node, {alignment, alignmentPolicy, curve, duration}) {
              node.requestFocus();
              WidgetsBinding.instance.addPostFrameCallback((_) {
                final context = node.context;
                // A FocusBox reveals itself when it gains focus.
                if (context != null &&
                    node.hasPrimaryFocus &&
                    context.findAncestorStateOfType<_FocusBoxState>() == null) {
                  unawaited(
                    revealFocus(context, stillFocused: () => node.hasFocus),
                  );
                }
              });
            },
      );

  @override
  bool inDirection(FocusNode currentNode, TraversalDirection direction) {
    final up = direction == TraversalDirection.up;
    final scope = currentNode.nearestScope;
    if ((!up && direction != TraversalDirection.down) || scope == null) {
      return super.inDirection(currentNode, direction);
    }
    final from = currentNode.rect;
    double gap(Rect rect) =>
        up ? from.top - rect.bottom : rect.top - from.bottom;
    final candidates = scope.traversalDescendants
        .where(
          (node) =>
              node != currentNode &&
              !node.rect.isEmpty &&
              gap(node.rect) > -1 &&
              !currentNode.ancestors.contains(node) &&
              !node.ancestors.contains(currentNode),
        )
        .toList();
    if (candidates.isEmpty) return super.inDirection(currentNode, direction);
    double far(Rect rect) =>
        up ? from.top - rect.top : rect.bottom - from.bottom;
    bool inLine(Rect rect) => rect.right > from.left && rect.left < from.right;
    double score(Rect rect) {
      final major = math.max(0.0, gap(rect));
      // Centres line up a grid. From a much wider control, such as a text
      // field, the first of the narrower ones below it lines up by its left.
      final minor = from.width > rect.width * 1.5
          ? (rect.left - from.left).abs()
          : (rect.center.dx - from.center.dx).abs();
      return 13 * major * major + minor * minor;
    }

    bool beats(Rect a, Rect b) {
      if (inLine(a) && !inLine(b) && gap(a) < far(b)) return true;
      if (inLine(b) && !inLine(a) && gap(b) < far(a)) return false;
      return score(a) < score(b);
    }

    final target = candidates.reduce((a, b) => beats(b.rect, a.rect) ? b : a);
    requestFocusCallback(target);
    return true;
  }
}

// Horizontal shelves clamp at their ends instead of reaching another shelf.
// Vertical movement still follows the surrounding page's traversal policy.
class RemoteFocusRow extends StatefulWidget {
  const RemoteFocusRow({
    super.key,
    required this.isTV,
    required this.child,
    required this.controller,
    required this.itemCount,
    required this.itemWidth,
    required this.spacing,
    required this.padding,
    this.onUp,
  });
  final bool isTV;
  final Widget child;
  final ScrollController controller;
  final int itemCount;
  final double itemWidth, spacing, padding;
  final VoidCallback? onUp;
  @override
  State<RemoteFocusRow> createState() => _RemoteFocusRowState();
}

class _RemoteFocusRowState extends State<RemoteFocusRow> {
  final region = FocusNode(canRequestFocus: false, debugLabel: 'Shelf');
  int? pendingIndex;
  int movement = 0;
  @override
  void dispose() {
    region.dispose();
    super.dispose();
  }

  bool scrollTo(int index) {
    if (!widget.controller.hasClients) return false;
    final position = widget.controller.position;
    final start = widget.padding + index * (widget.itemWidth + widget.spacing);
    final end = start + widget.itemWidth;
    final target = start < position.pixels + 7
        ? start - 7
        : end > position.pixels + position.viewportDimension - 7
        ? end - position.viewportDimension + 7
        : position.pixels;
    final offset = target.clamp(
      position.minScrollExtent,
      position.maxScrollExtent,
    );
    if ((offset - position.pixels).abs() < .5) return false;
    widget.controller.jumpTo(offset);
    return true;
  }

  Future<bool> reveal(int index) async {
    if (pendingIndex != null && pendingIndex != index) return false;
    if (scrollTo(index)) await WidgetsBinding.instance.endOfFrame;
    return mounted;
  }

  FocusNode? itemFocus(int index) => region.traversalDescendants
      .where(
        (focus) =>
            focus.context
                ?.getInheritedWidgetOfExactType<RemoteFocusItem>()
                ?.index ==
            index,
      )
      .firstOrNull;

  KeyEventResult key(FocusNode node, KeyEvent event) {
    if (!widget.isTV || (event is! KeyDownEvent && event is! KeyRepeatEvent)) {
      return KeyEventResult.ignored;
    }
    if (event.logicalKey == LogicalKeyboardKey.arrowUp && widget.onUp != null) {
      widget.onUp!();
      return KeyEventResult.handled;
    }
    final step = switch (event.logicalKey) {
      LogicalKeyboardKey.arrowLeft => -1,
      LogicalKeyboardKey.arrowRight => 1,
      _ => 0,
    };
    if (step == 0) return KeyEventResult.ignored;
    final current = FocusManager.instance.primaryFocus;
    if (current == null) return KeyEventResult.ignored;
    final item = current.context
        ?.getInheritedWidgetOfExactType<RemoteFocusItem>();
    if (item == null || widget.itemCount == 0) return KeyEventResult.ignored;
    final index = ((pendingIndex ?? item.index) + step).clamp(
      0,
      widget.itemCount - 1,
    );
    pendingIndex = index;
    final request = ++movement;
    FocusTraversalGroup.maybeOf(current.context!)
        ?.invalidateScopeData(current.nearestScope!);
    scrollTo(index);
    itemFocus(index)?.requestFocus();
    // A held arrow can reach a title beyond the list's current lazy children.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || request != movement) return;
      if (region.hasFocus) itemFocus(index)?.requestFocus();
      pendingIndex = null;
    });
    return KeyEventResult.handled;
  }

  @override
  Widget build(BuildContext context) => Focus(
    focusNode: region,
    canRequestFocus: false,
    onKeyEvent: key,
    child: widget.child,
  );
}

class RemoteFocusItem extends InheritedWidget {
  const RemoteFocusItem({super.key, required this.index, required super.child});
  final int index;
  @override
  bool updateShouldNotify(RemoteFocusItem old) => old.index != index;
}

// Single-line text editing consumes vertical arrows before normal traversal.
class RemoteTextInput extends StatefulWidget {
  const RemoteTextInput({
    super.key,
    required this.isTV,
    required this.builder,
    this.onUp,
    this.onActivate,
    this.onFocusChanged,
    this.multiline = false,
    this.focusNode,
  });
  final bool isTV, multiline;
  final Widget Function(FocusNode) builder;

  /// Lets the owner move focus into the field. Its key handler is replaced.
  final FocusNode? focusNode;
  final VoidCallback? onUp;
  final ValueChanged<FocusNode>? onActivate;
  final ValueChanged<bool>? onFocusChanged;
  @override
  State<RemoteTextInput> createState() => _RemoteTextInputState();
}

class _RemoteTextInputState extends State<RemoteTextInput> {
  late final focus = (widget.focusNode ?? FocusNode(debugLabel: 'Text input'))
    ..onKeyEvent = key;
  @override
  void initState() {
    super.initState();
    focus.addListener(focusChanged);
  }

  void focusChanged() => widget.onFocusChanged?.call(focus.hasFocus);

  KeyEventResult key(FocusNode node, KeyEvent event) {
    if (!widget.isTV ||
        (event is! KeyDownEvent && event is! KeyRepeatEvent) ||
        widget.multiline && View.of(context).viewInsets.bottom > 0) {
      return KeyEventResult.ignored;
    }
    if (widget.onActivate case final activate?) {
      if (event.logicalKey == LogicalKeyboardKey.select ||
          event.logicalKey == LogicalKeyboardKey.enter ||
          event.logicalKey == LogicalKeyboardKey.numpadEnter) {
        if (event is KeyDownEvent) activate(node);
        return KeyEventResult.handled;
      }
    }
    // Move by position, so a button beside the field is not taken for the
    // control below or above it.
    if (event.logicalKey == LogicalKeyboardKey.arrowDown) {
      if (!node.focusInDirection(TraversalDirection.down)) node.nextFocus();
      return KeyEventResult.handled;
    }
    // Past the end of the text, Right reaches a button beside the field.
    if (event.logicalKey == LogicalKeyboardKey.arrowRight) {
      final value = node.context
          ?.findAncestorStateOfType<EditableTextState>()
          ?.textEditingValue;
      if (value != null &&
          value.selection.isCollapsed &&
          value.selection.baseOffset >= value.text.length &&
          node.focusInDirection(TraversalDirection.right)) {
        return KeyEventResult.handled;
      }
      return KeyEventResult.ignored;
    }
    if (event.logicalKey == LogicalKeyboardKey.arrowUp) {
      if (widget.onUp case final up?) {
        up();
      } else if (!node.focusInDirection(TraversalDirection.up)) {
        node.previousFocus();
      }
      return KeyEventResult.handled;
    }
    return KeyEventResult.ignored;
  }

  @override
  void dispose() {
    focus.removeListener(focusChanged);
    if (widget.focusNode == null) focus.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => widget.builder(focus);
}

/// How much a button asks for attention. One [primary] per screen.
enum ButtonVariant {
  /// Shu fill: Resume, Play, Sign in, Apply.
  primary,

  /// Raised panel with a hairline: Add to list, Download, Play on.
  secondary,

  /// Red text only: dialog actions, More, Reset.
  text,

  /// Like [secondary], in the danger colour: Sign out, Delete.
  danger,
}

enum ButtonSize { md, sm }

class AppButton extends StatelessWidget {
  const AppButton({
    super.key,
    required this.label,
    required this.onPressed,
    this.icon,
    this.filled = false,
    this.variant,
    this.size = ButtonSize.md,
    this.isTV = false,
    this.autofocus = false,
    this.focusNode,
    this.disabledReason,
    this.iconOnly = false,
    this.trailingIcon,
  });
  final String label;
  final VoidCallback? onPressed;
  final IconData? icon;

  /// A square button that shows only [icon]; [label] becomes its tooltip and
  /// spoken name.
  final bool iconOnly;

  /// A small icon after the label, such as a chevron on a button that opens
  /// a picker.
  final IconData? trailingIcon;

  /// Shorthand for [ButtonVariant.primary] when [variant] is not given.
  final bool filled;
  final ButtonVariant? variant;
  final ButtonSize size;
  final bool isTV, autofocus;
  final FocusNode? focusNode;
  final String? disabledReason;

  ButtonVariant get kind =>
      variant ?? (filled ? ButtonVariant.primary : ButtonVariant.secondary);

  /// Button height for [size] on this device.
  static double heightOf(ButtonSize size, bool isTV) => switch (size) {
    ButtonSize.md => isTV ? 56 : 44,
    ButtonSize.sm => isTV ? 48 : 36,
  };

  @override
  Widget build(BuildContext context) {
    final off = onPressed == null;
    final (fill, border, ink) = off
        ? (line, Colors.transparent, muted)
        : switch (kind) {
            ButtonVariant.primary => (shu, Colors.transparent, bgDeep),
            ButtonVariant.secondary => (panelRaised, line, fg),
            ButtonVariant.text => (
              Colors.transparent,
              Colors.transparent,
              accentText,
            ),
            ButtonVariant.danger => (panelRaised, line, danger),
          };
    final pad = kind == ButtonVariant.text
        ? (isTV ? 12.0 : 8.0)
        : size == ButtonSize.sm
        ? (isTV ? 16.0 : 12.0)
        : (isTV ? 20.0 : 16.0);
    final button = FocusBox(
      isTV: isTV,
      autofocus: autofocus,
      focusNode: focusNode,
      radius: KageRadius.button,
      onPressed: onPressed,
      child: AnimatedContainer(
        duration: KageMotion.base,
        curve: KageMotion.enter,
        constraints: BoxConstraints(
          minHeight: heightOf(size, isTV),
          minWidth: iconOnly ? heightOf(size, isTV) : 0,
        ),
        padding: iconOnly
            ? EdgeInsets.zero
            : EdgeInsets.symmetric(horizontal: pad, vertical: 6),
        decoration: BoxDecoration(
          color: fill,
          borderRadius: BorderRadius.circular(KageRadius.button),
          border: Border.all(color: border),
        ),
        child: iconOnly && icon != null
            ? Center(
                child: Icon(icon, size: isTV ? 24 : 20, color: ink),
              )
            // A stretched button keeps its label in the middle; a loose
            // one stays as wide as its label.
            : Align(
                widthFactor: 1,
                heightFactor: 1,
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    if (icon != null) ...[
                      Icon(icon, size: isTV ? 24 : 20, color: ink),
                      const SizedBox(width: 8),
                    ],
                    Flexible(
                      child: Text(
                        label,
                        maxLines: size == ButtonSize.sm ? 1 : 2,
                        overflow: TextOverflow.ellipsis,
                        style: uiStyle(
                          context,
                          weight: FontWeight.w600,
                          color: ink,
                        ),
                      ),
                    ),
                    if (trailingIcon != null) ...[
                      const SizedBox(width: 6),
                      Icon(trailingIcon, size: isTV ? 20 : 16, color: muted),
                    ],
                  ],
                ),
              ),
      ),
    );
    if (iconOnly) {
      final labelled = Tooltip(
        message: disabledReason ?? label,
        child: Semantics(
          label: label,
          button: true,
          enabled: onPressed != null,
          excludeSemantics: true,
          child: button,
        ),
      );
      return labelled;
    }
    if (onPressed != null || disabledReason == null) return button;
    return Tooltip(
      message: disabledReason!,
      child: Semantics(enabled: false, hint: disabledReason, child: button),
    );
  }
}

/// Pick one of a few (2 to about 7): one plate, the chosen segment on a
/// darker tile. Replaces rows of separate pills.
/// A number with − and + on either side. Holding a button repeats it,
/// faster the longer it is held; tapping the number types a value.
class AppStepper extends StatefulWidget {
  const AppStepper({
    super.key,
    required this.label,
    required this.value,
    required this.text,
    required this.onChanged,
    this.min = 0,
    this.max,
    this.isTV = false,
    this.enabled = true,
  });

  /// What the number counts, for tooltips and the type-in dialog.
  final String label;
  final int value, min;
  final int? max;

  /// How the value reads, like "3 / 12" or "Unscored".
  final String text;
  final ValueChanged<int> onChanged;
  final bool isTV, enabled;

  @override
  State<AppStepper> createState() => _AppStepperState();
}

class _AppStepperState extends State<AppStepper> {
  Timer? repeat;
  int held = 0;

  int clamp(int v) {
    final top = widget.max;
    return math.max(widget.min, top == null ? v : math.min(top, v));
  }

  bool can(int delta) =>
      widget.enabled && clamp(widget.value + delta) != widget.value;

  void step(int delta) {
    if (can(delta)) widget.onChanged(clamp(widget.value + delta));
  }

  /// Starts repeating after a short hold. Raw pointer events, so the
  /// button's own tap and tooltip gestures still work.
  void hold(int delta) {
    held = 0;
    repeat?.cancel();
    repeat = Timer(const Duration(milliseconds: 450), () {
      repeat = Timer.periodic(const Duration(milliseconds: 90), (_) {
        held++;
        // One at a time for a second, then five, then ten.
        step(
          delta *
              (held > 30
                  ? 10
                  : held > 11
                  ? 5
                  : 1),
        );
        if (!can(delta)) stop();
      });
    });
  }

  void stop() {
    repeat?.cancel();
    repeat = null;
  }

  @override
  void didUpdateWidget(AppStepper old) {
    super.didUpdateWidget(old);
    if (!widget.enabled) stop();
  }

  @override
  void dispose() {
    stop();
    super.dispose();
  }

  Future<void> type() async {
    final controller = TextEditingController(text: '${widget.value}');
    final top = widget.max;
    final result = await showDialog<int>(
      context: context,
      builder: (context) {
        void done() =>
            Navigator.pop(context, int.tryParse(controller.text.trim()));
        return AlertDialog(
          title: Text(widget.label),
          content: RemoteTextInput(
            isTV: widget.isTV,
            builder: (focus) => TextField(
              key: const Key('stepper-input'),
              focusNode: focus,
              autofocus: true,
              controller: controller,
              keyboardType: TextInputType.number,
              inputFormatters: [FilteringTextInputFormatter.digitsOnly],
              decoration: InputDecoration(
                hintText: top == null
                    ? 'From ${widget.min}'
                    : 'From ${widget.min} to $top',
              ),
              onSubmitted: (_) => done(),
            ),
          ),
          actions: [
            AppButton(
              label: 'Cancel',
              variant: ButtonVariant.text,
              isTV: widget.isTV,
              onPressed: () => Navigator.pop(context),
            ),
            AppButton(
              label: 'Set',
              variant: ButtonVariant.primary,
              isTV: widget.isTV,
              onPressed: done,
            ),
          ],
        );
      },
    );
    // The dialog can still paint the field for a moment after it closes.
    await Future<void>.delayed(const Duration(milliseconds: 250));
    controller.dispose();
    if (result != null && mounted) widget.onChanged(clamp(result));
  }

  Widget button(int delta) {
    final name =
        '${delta < 0 ? 'Decrease' : 'Increase'} '
        '${widget.label.toLowerCase()}';
    final live = can(delta);
    return Listener(
      onPointerDown: live ? (_) => hold(delta) : null,
      onPointerUp: (_) => stop(),
      onPointerCancel: (_) => stop(),
      child: AppButton(
        label: name,
        icon: delta < 0 ? LucideIcons.minus : LucideIcons.plus,
        iconOnly: true,
        isTV: widget.isTV,
        onPressed: live ? () => step(delta) : null,
      ),
    );
  }

  @override
  Widget build(BuildContext context) => Row(
    children: [
      button(-1),
      const SizedBox(width: KageSpace.s2),
      Expanded(
        child: FocusBox(
          key: ValueKey('stepper-${widget.label}'),
          isTV: widget.isTV,
          radius: KageRadius.input,
          onPressed: widget.enabled ? type : null,
          child: Tooltip(
            message: 'Type a number',
            child: Container(
              height: AppButton.heightOf(ButtonSize.md, widget.isTV),
              alignment: Alignment.center,
              decoration: BoxDecoration(
                color: fgBright.withValues(alpha: .04),
                border: Border.all(color: line),
                borderRadius: BorderRadius.circular(KageRadius.input),
              ),
              child: Text(
                widget.text,
                style: uiStyle(context, weight: FontWeight.w600),
              ),
            ),
          ),
        ),
      ),
      const SizedBox(width: KageSpace.s2),
      button(1),
    ],
  );
}

class AppSegmented<T> extends StatelessWidget {
  const AppSegmented({
    super.key,
    required this.options,
    required this.selected,
    required this.onChanged,
    this.isTV = false,
    this.keyOf,
    this.expand = false,
    this.columns,
  });
  final List<(T, String)> options;

  /// Lays the segments out in rows of this many, each row full width, for
  /// choices too many for one line.
  final int? columns;
  final T selected;
  final ValueChanged<T> onChanged;
  final bool isTV;

  /// Key for each segment, for tests and focus.
  final Key Function(T value)? keyOf;

  /// Segments share the full width instead of hugging their labels.
  final bool expand;

  @override
  Widget build(BuildContext context) {
    Widget segment(T value, String label) {
      final child = AppSegment(
        key: keyOf?.call(value),
        label: label,
        selected: value == selected,
        isTV: isTV,
        onPressed: () => onChanged(value),
      );
      return expand ? Expanded(child: child) : child;
    }

    return Container(
      padding: const EdgeInsets.all(2),
      decoration: BoxDecoration(
        color: panel,
        border: Border.all(color: line),
        borderRadius: BorderRadius.circular(KageRadius.input),
      ),
      child: switch (columns) {
        final n? => Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            for (var i = 0; i < options.length; i += n)
              Row(
                children: [
                  for (var j = i; j < i + n; j++)
                    if (j < options.length)
                      Expanded(
                        child: AppSegment(
                          key: keyOf?.call(options[j].$1),
                          label: options[j].$2,
                          selected: options[j].$1 == selected,
                          isTV: isTV,
                          onPressed: () => onChanged(options[j].$1),
                        ),
                      )
                    else
                      const Spacer(),
                ],
              ),
          ],
        ),
        null => Row(
          mainAxisSize: expand ? MainAxisSize.max : MainAxisSize.min,
          children: [for (final (v, l) in options) segment(v, l)],
        ),
      },
    );
  }
}

/// One segment of [AppSegmented].
class AppSegment extends StatelessWidget {
  const AppSegment({
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
  Widget build(BuildContext context) => Semantics(
    selected: selected,
    button: true,
    child: FocusBox(
      isTV: isTV,
      radius: KageRadius.segment,
      scale: 1,
      onPressed: onPressed,
      child: AnimatedContainer(
        duration: KageMotion.fast,
        height: (isTV ? 52 : 40) - 6,
        alignment: Alignment.center,
        padding: EdgeInsets.symmetric(horizontal: isTV ? 20 : 14),
        decoration: BoxDecoration(
          color: selected ? selectedPanel : Colors.transparent,
          borderRadius: BorderRadius.circular(KageRadius.segment),
        ),
        child: Text(
          label,
          maxLines: 1,
          style: uiStyle(
            context,
            color: selected ? fgBright : muted,
            weight: selected ? FontWeight.w600 : FontWeight.w500,
          ),
        ),
      ),
    ),
  );
}

/// Pick many: a removable or toggled token, such as a genre. Chosen chips
/// get a darker tile, a Shu outline and a check.
class AppChip extends StatelessWidget {
  const AppChip({
    super.key,
    required this.label,
    required this.selected,
    required this.onPressed,
    this.isTV = false,
    this.autofocus = false,
  });
  final String label;
  final bool selected, isTV, autofocus;
  final VoidCallback onPressed;
  @override
  Widget build(BuildContext context) => Semantics(
    selected: selected,
    child: FocusBox(
      isTV: isTV,
      autofocus: autofocus,
      onPressed: onPressed,
      radius: KageRadius.chip,
      outsideFrame: true,
      child: AnimatedContainer(
        duration: KageMotion.fast,
        constraints: BoxConstraints(minHeight: isTV ? 48 : 36),
        padding: EdgeInsets.symmetric(horizontal: isTV ? 16 : 12),
        decoration: BoxDecoration(
          color: selected ? selectedPanel : panelRaised,
          borderRadius: BorderRadius.circular(KageRadius.chip),
          border: Border.all(color: selected ? shu : line),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (selected) ...[
              Icon(LucideIcons.check, size: isTV ? 18 : 14, color: fgBright),
              const SizedBox(width: 6),
            ],
            Text(
              label,
              style: uiStyle(
                context,
                size: isTV ? 17 : 14,
                color: selected ? fgBright : fg,
                weight: selected ? FontWeight.w600 : FontWeight.w400,
              ),
            ),
          ],
        ),
      ),
    ),
  );
}

/// Facts about a title as one line of text ("★ 7.6 · TV · 12 ep · 2026"),
/// not a row of chips.
class MetaText extends StatelessWidget {
  const MetaText(
    this.parts, {
    super.key,
    this.size = 16,
    this.color = muted,
    this.maxLines = 2,
  });
  final List<String> parts;
  final double size;
  final Color color;
  final int maxLines;
  @override
  Widget build(BuildContext context) {
    final text = parts.where((p) => p.trim().isNotEmpty).join(' · ');
    if (text.isEmpty) return const SizedBox.shrink();
    return Text(
      text,
      maxLines: maxLines,
      overflow: TextOverflow.ellipsis,
      style: uiStyle(context, size: size, color: color),
    );
  }
}

/// The one back button: Lucide arrow, the platform's tooltip.
class AppBackButton extends StatelessWidget {
  const AppBackButton({
    super.key,
    this.focusNode,
    this.autofocus = false,
    this.color,
    this.onPressed,
  });
  final FocusNode? focusNode;
  final bool autofocus;
  final Color? color;
  final VoidCallback? onPressed;
  @override
  Widget build(BuildContext context) => IconButton(
    focusNode: focusNode,
    autofocus: autofocus,
    tooltip: MaterialLocalizations.of(context).backButtonTooltip,
    icon: Icon(LucideIcons.arrowLeft, color: color),
    onPressed: onPressed ?? () => Navigator.maybePop(context),
  );
}

class PageHeading extends StatelessWidget {
  const PageHeading(this.title, this.kanji, {super.key});
  final String title, kanji;
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 8),
    child: Row(
      children: [
        Expanded(
          child: Text(
            title,
            style: uiStyle(
              context,
              size: AppLayout.tvOf(context) ? 32 : 29,
              weight: FontWeight.w700,
              height: 1.2,
            ),
          ),
        ),
        ExcludeSemantics(
          child: Text(
            kanji,
            style: TextStyle(
              fontFamily: 'NotoSansJP',
              fontWeight: FontWeight.w500,
              fontSize: AppLayout.tvOf(context) ? 25 : 34,
              color: accentText,
            ),
          ),
        ),
      ],
    ),
  );
}

class SectionLabel extends StatelessWidget {
  const SectionLabel(this.title, {super.key});
  final String title;
  @override
  Widget build(BuildContext context) => Text(
    title,
    style: uiStyle(
      context,
      size: AppLayout.tvOf(context) ? 26 : 21,
      weight: FontWeight.w600,
    ),
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
  Widget build(BuildContext context) => Center(
    heightFactor: 1,
    child: ConstrainedBox(
      constraints: const BoxConstraints(maxWidth: 560),
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 32, horizontal: 24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(
              error ? '迷' : '空',
              style: TextStyle(
                fontFamily: 'NotoSansJP',
                fontSize: AppLayout.tvOf(context) ? 25 : 24,
                fontWeight: FontWeight.w700,
                height: 1.2,
                color: accentText,
              ),
            ),
            const SizedBox(height: 16),
            Text(
              message,
              textAlign: TextAlign.center,
              style: uiStyle(context, color: muted),
            ),
            const SizedBox(height: 16),
            if (action != null)
              action!
            else
              Text('Leave it to us.', style: uiStyle(context, color: muted)),
          ],
        ),
      ),
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
        borderRadius: BorderRadius.circular(KageRadius.md),
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

/// Stand-in rows while a list loads: shimmering blocks the height of a
/// row, so the page does not jump when the rows arrive.
class SkeletonList extends StatelessWidget {
  const SkeletonList({super.key, this.rows = 3, this.height = 56});
  final int rows;
  final double height;
  @override
  Widget build(BuildContext context) => SkeletonShimmer(
    child: Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        for (var i = 0; i < rows; i++)
          Padding(
            padding: const EdgeInsets.symmetric(vertical: KageSpace.s1),
            child: SkeletonBlock(height: height - KageSpace.s2),
          ),
      ],
    ),
  );
}

/// The animated thread: a thin Shu line that runs while an action works.
/// Content that is loading shows a skeleton instead.
class WorkingLine extends StatelessWidget {
  const WorkingLine({super.key, this.label = 'Working'});
  final String label;
  @override
  Widget build(BuildContext context) => Semantics(
    label: label,
    child: ClipRRect(
      borderRadius: const BorderRadius.all(Radius.circular(KageRadius.mark)),
      child: const LinearProgressIndicator(
        minHeight: 2,
        color: shu,
        backgroundColor: line,
      ),
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
        Row(
          children: [
            Image.asset('assets/mark.png', width: 32, height: 32),
            const SizedBox(width: 16),
            const SkeletonBlock(width: 160),
          ],
        ),
        const SizedBox(height: 24),
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
    this.alignment = Alignment.center,
  });
  final String url;
  final double width, height;
  final bool banner;
  final BoxFit fit;
  final Alignment alignment;
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
          alignment: alignment,
        ),
      );
    }
    return RepaintBoundary(
      child: CachedNetworkImage(
        imageUrl: url,
        width: width,
        height: height,
        fit: fit,
        alignment: alignment,
        memCacheWidth: pixels,
        maxWidthDiskCache: pixels,
        fadeInDuration: const Duration(milliseconds: 180),
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

// Keep the overlay clearance equal to the TV header's measured type and frame.
double tvHeaderExtent(BuildContext context) =>
    math.max(27, MediaQuery.paddingOf(context).top) +
    math.max(48, MediaQuery.textScalerOf(context).scale(20) * 1.4 + 10) +
    14 +
    12;

class TVHomeHero extends StatelessWidget {
  const TVHomeHero({super.key, this.anime, required this.child});
  final Anime? anime;
  final Widget child;
  @override
  Widget build(BuildContext context) => Stack(
    children: [
      if (anime != null)
        Positioned.fill(
          child: IgnorePointer(
            child: LayoutBuilder(
              builder: (context, box) => Stack(
                fit: StackFit.expand,
                children: [
                  HeroArt(anime!, height: box.maxHeight),
                  const DecoratedBox(
                    key: Key('home-top-vignette'),
                    decoration: BoxDecoration(
                      gradient: LinearGradient(
                        begin: Alignment.topCenter,
                        end: Alignment.bottomCenter,
                        colors: [
                          Color(0xd90e0d0c),
                          Color(0x400e0d0c),
                          Color(0x000e0d0c),
                        ],
                        stops: [0, .38, .7],
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      child,
    ],
  );
}

/// Art for an episode with no still of its own and a show with no banner:
/// the cover as a soft colour wash with the episode number on it, so a row
/// of episodes doesn't repeat one cropped poster.
class EpisodeArtFallback extends StatelessWidget {
  const EpisodeArtFallback(
    this.cover,
    this.episode, {
    super.key,
    required this.width,
    required this.height,
  });
  final String cover;
  final int episode;
  final double width, height;
  @override
  Widget build(BuildContext context) => ClipRect(
    child: Stack(
      fit: StackFit.expand,
      children: [
        const ColoredBox(color: panelRaised),
        ImageFiltered(
          imageFilter: ImageFilter.blur(sigmaX: 14, sigmaY: 14),
          child: Opacity(
            opacity: .55,
            child: Transform.scale(
              scale: 1.3,
              child: CoverImage(cover, width: width, height: height),
            ),
          ),
        ),
        Center(
          child: Text(
            episode.toString().padLeft(2, '0'),
            style: display.copyWith(
              fontSize: height * .42,
              height: 1,
              color: fgBright.withValues(alpha: .92),
              shadows: const [Shadow(color: Color(0x99000000), blurRadius: 12)],
            ),
          ),
        ),
      ],
    ),
  );
}

/// A show without a banner in a wide slot: the cover as a colour wash with
/// the sharp cover set into it, instead of a cropped, enlarged poster.
class WideCoverArt extends StatelessWidget {
  const WideCoverArt(
    this.cover, {
    super.key,
    required this.width,
    required this.height,
  });
  final String cover;
  final double width, height;
  @override
  Widget build(BuildContext context) {
    final inset = height * .1;
    final posterHeight = height - inset * 2;
    return ClipRect(
      child: Stack(
        fit: StackFit.expand,
        children: [
          const ColoredBox(color: panelRaised),
          ImageFiltered(
            imageFilter: ImageFilter.blur(sigmaX: 18, sigmaY: 18),
            child: Opacity(
              opacity: .55,
              child: Transform.scale(
                scale: 1.3,
                child: CoverImage(cover, width: width, height: height),
              ),
            ),
          ),
          Positioned(
            left: inset * 1.4,
            top: inset,
            child: ClipRRect(
              borderRadius: BorderRadius.circular(KageRadius.sm),
              child: CoverImage(
                cover,
                width: posterHeight * 2 / 3,
                height: posterHeight,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class HeroArt extends StatelessWidget {
  const HeroArt(
    this.anime, {
    super.key,
    required this.height,
    this.posterPadding = EdgeInsets.zero,
    this.poster = true,
  });
  final Anime anime;
  final double height;

  /// Space kept around the poster shown when there is no banner, so it stays
  /// clear of anything drawn over the art.
  final EdgeInsets posterPadding;

  /// Whether a wide screen without a banner shows the cover as a poster.
  /// Off where the page draws its own poster.
  final bool poster;

  @override
  Widget build(BuildContext context) {
    final width = MediaQuery.sizeOf(context).width;
    final cover = anime.coverLarge.isNotEmpty ? anime.coverLarge : anime.cover;
    // A phone is about as wide as the cover: it fills the header, cropped
    // to the upper part where faces usually are. Wider screens would blow
    // the cover up past its resolution, so it becomes a soft colour wash
    // with the cover itself shown as a framed poster.
    final narrow = width < 700;
    final framed = poster && !narrow;
    final posterHeight = posterPadding == EdgeInsets.zero
        ? height * .74
        : height - posterPadding.vertical;
    return ClipRect(
      child: Stack(
        fit: StackFit.expand,
        children: [
          const ColoredBox(color: bgDeep),
          if (anime.banner.isNotEmpty)
            CoverImage(anime.banner, width: width, height: height, banner: true)
          else if (narrow)
            CoverImage(
              key: const Key('hero-fallback-cover'),
              cover,
              width: width,
              height: height,
              banner: true,
              alignment: const Alignment(0, -.45),
            )
          else ...[
            ImageFiltered(
              key: const Key('hero-fallback-backdrop'),
              imageFilter: ImageFilter.blur(sigmaX: 40, sigmaY: 40),
              child: Opacity(
                opacity: .5,
                child: Transform.scale(
                  scale: 1.2,
                  child: CoverImage(
                    cover,
                    width: width,
                    height: height,
                    alignment: const Alignment(0, -.3),
                  ),
                ),
              ),
            ),
            if (framed)
              Padding(
                padding: posterPadding == EdgeInsets.zero
                    ? EdgeInsets.fromLTRB(0, height * .13, width * .08, 0)
                    : posterPadding,
                child: Align(
                  alignment: posterPadding == EdgeInsets.zero
                      ? Alignment.topRight
                      : Alignment.centerRight,
                  child: Container(
                    width: posterHeight * 2 / 3,
                    height: posterHeight,
                    decoration: BoxDecoration(
                      borderRadius: BorderRadius.circular(KageRadius.poster),
                      boxShadow: const [
                        BoxShadow(
                          color: Color(0x99000000),
                          blurRadius: 30,
                          offset: Offset(0, 14),
                        ),
                      ],
                    ),
                    child: ClipRRect(
                      borderRadius: BorderRadius.circular(KageRadius.poster),
                      child: CoverImage(
                        key: const Key('hero-fallback-poster'),
                        cover,
                        width: posterHeight * 2 / 3,
                        height: posterHeight,
                      ),
                    ),
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

/// Opens a sub-page (settings, accounts, details) the one way each screen
/// expects: a full page on a phone and a panel from the right edge on a TV
/// or computer, with the page still in view beside it.
Future<T?> openSubPage<T>(
  BuildContext context,
  Widget page, {
  required bool isTV,
  String label = 'Close',
}) {
  if (!isTV) {
    return Navigator.push<T>(context, OtakaseRoute<T>(builder: (_) => page));
  }
  return showGeneralDialog<T>(
    context: context,
    barrierDismissible: true,
    barrierLabel: label,
    barrierColor: bgDeep.withValues(alpha: .6),
    transitionDuration: MediaQuery.disableAnimationsOf(context)
        ? Duration.zero
        : KageMotion.slow,
    // On a computer the panel starts under the top bar, so the window
    // buttons stay where they always are.
    pageBuilder: (context, _, _) => Padding(
      padding: EdgeInsets.only(
        top: isDesktop ? DesktopTopBar.extent(context) : 0,
      ),
      child: Align(
        alignment: Alignment.centerRight,
        child: Container(
          width: (MediaQuery.sizeOf(context).width * .35).clamp(432, 560),
          clipBehavior: Clip.antiAlias,
          decoration: const BoxDecoration(
            color: bg,
            borderRadius: BorderRadius.horizontal(
              left: Radius.circular(KageRadius.sheet),
            ),
            border: Border.fromBorderSide(BorderSide(color: line)),
          ),
          // One surface from the top bar down, whatever the page sets.
          child: Theme(
            data: Theme.of(context).copyWith(
              scaffoldBackgroundColor: panel,
              canvasColor: panel,
              appBarTheme: Theme.of(context).appBarTheme
                  .copyWith(backgroundColor: panel),
            ),
            child: SubPanelScope(child: page),
          ),
        ),
      ),
    ),
    transitionBuilder: (context, animation, _, child) {
      final curved = CurvedAnimation(
        parent: animation,
        curve: KageMotion.enter,
        reverseCurve: KageMotion.exit,
      );
      return FadeTransition(
        opacity: curved,
        child: SlideTransition(
          position: Tween(
            begin: const Offset(.12, 0),
            end: Offset.zero,
          ).animate(curved),
          child: child,
        ),
      );
    },
  );
}

/// Sets a note's place: on a phone it floats just above the bottom nav bar
/// (or the screen edge on pages without one); TVs and computers get the
/// compact note from the theme.
SnackBar placeNote(BuildContext context, SnackBar note) {
  if (AppLayout.tvOf(context) || note.margin != null) return note;
  return SnackBar(
    key: note.key,
    content: note.content,
    action: note.action,
    duration: note.duration,
    showCloseIcon: note.showCloseIcon,
    margin: EdgeInsets.fromLTRB(
      KageSpace.s3,
      0,
      KageSpace.s3,
      PageInsets.bottomOf(context) + KageSpace.s2,
    ),
  );
}

/// Marks a page shown in a sub-page panel, where the top bar end stays empty
/// on a computer: the window buttons belong to the window, not the panel.
class SubPanelScope extends InheritedWidget {
  const SubPanelScope({super.key, required super.child});
  static bool of(BuildContext context) =>
      context.getInheritedWidgetOfExactType<SubPanelScope>() != null;
  @override
  bool updateShouldNotify(SubPanelScope old) => false;
}

/// A show's synopsis: a few lines, with More to read the rest.
class Synopsis extends StatefulWidget {
  const Synopsis(this.text, {super.key, this.lines = 3, this.isTV = false});
  final String text;
  final int lines;
  final bool isTV;
  @override
  State<Synopsis> createState() => _SynopsisState();
}

class _SynopsisState extends State<Synopsis> {
  bool open = false;
  @override
  Widget build(BuildContext context) {
    if (widget.text.isEmpty) return const SizedBox.shrink();
    final style = uiStyle(context, size: 15, height: 1.5);
    return LayoutBuilder(
      builder: (context, constraints) {
        final painter = TextPainter(
          text: TextSpan(text: widget.text, style: style),
          maxLines: widget.lines,
          textDirection: Directionality.of(context),
          textScaler: MediaQuery.textScalerOf(context),
        )..layout(maxWidth: constraints.maxWidth);
        final long = painter.didExceedMaxLines;
        painter.dispose();
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            AnimatedSize(
              duration: MediaQuery.disableAnimationsOf(context)
                  ? Duration.zero
                  : KageMotion.base,
              curve: KageMotion.enter,
              alignment: Alignment.topLeft,
              child: Text(
                widget.text,
                key: const Key('synopsis'),
                maxLines: open ? null : widget.lines,
                overflow: open ? null : TextOverflow.ellipsis,
                style: style,
              ),
            ),
            if (long)
              FocusBox(
                key: const Key('synopsis-more'),
                isTV: widget.isTV,
                radius: KageRadius.xs,
                frameGap: 2,
                padding: const EdgeInsets.symmetric(vertical: KageSpace.s2),
                onPressed: () => setState(() => open = !open),
                child: Text(
                  open ? 'Less' : 'More',
                  style: uiStyle(
                    context,
                    size: 15,
                    weight: FontWeight.w600,
                    color: accentText,
                  ),
                ),
              ),
          ],
        );
      },
    );
  }
}

/// The watched mark on episode art: a small 済 stamp in Shu, the same on
/// every screen.
class WatchedStamp extends StatelessWidget {
  const WatchedStamp({super.key, this.size = 26});
  final double size;
  @override
  Widget build(BuildContext context) => Semantics(
    label: 'Watched',
    excludeSemantics: true,
    child: Container(
      width: size,
      height: size,
      alignment: Alignment.center,
      decoration: BoxDecoration(
        color: bgDeep.withValues(alpha: .85),
        borderRadius: const BorderRadius.all(Radius.circular(KageRadius.xs)),
        border: Border.all(color: shu, width: 1.5),
      ),
      child: Text(
        '済',
        style: TextStyle(
          fontFamily: 'NotoSansJP',
          fontSize: size * .58,
          height: 1,
          color: accentText,
          fontWeight: FontWeight.w700,
        ),
      ),
    ),
  );
}

class OtakaseRoute<T> extends PageRouteBuilder<T> {
  OtakaseRoute({required WidgetBuilder builder})
    : super(
        pageBuilder: (context, a, b) => builder(context),
        transitionDuration: const Duration(milliseconds: 300),
        reverseTransitionDuration: const Duration(milliseconds: 250),
        transitionsBuilder: (context, a, b, child) => FadeTransition(
          opacity: CurvedAnimation(parent: a, curve: Curves.easeOut),
          child: child,
        ),
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

class PosterCard extends StatefulWidget {
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
    this.landscape = false,
    this.cropped = false,
    this.onMenu,
  });
  final Anime anime;
  final VoidCallback onOpen;

  /// More for this card: a long press, a right click, or the remote's
  /// Info or Menu key. Null when the card has nothing more.
  final VoidCallback? onMenu;
  final bool isTV, autofocus, landscape, cropped;
  final double width;
  final double? progress;
  final String? caption;
  final VoidCallback? onFocused;
  @override
  State<PosterCard> createState() => _PosterCardState();
}

class _PosterCardState extends State<PosterCard> {
  bool focused = false;
  void open() => PosterNavigation.open(
    context,
    widget.anime,
    'poster-${widget.key ?? widget.anime.key}',
    widget.onOpen,
  );
  @override
  Widget build(BuildContext context) {
    final a = widget.anime;
    return RevealArea(
      child: SizedBox(
        width: widget.width,
        child: Semantics(
          label: a.title,
          button: true,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: [
              CardMenu(
                onMenu: widget.onMenu,
                child: FocusBox(
                  isTV: widget.isTV,
                  autofocus: widget.autofocus,
                  outsideFrame: true,
                  lift: true,
                  radius: KageRadius.poster,
                  onPressed: open,
                  onLongPress: widget.onMenu,
                  onFocused: widget.onFocused,
                  onFocusChanged: (value) => setState(() => focused = value),
                  child: Hero(
                    tag: 'poster-${widget.key ?? a.key}',
                    child: ClipRRect(
                      borderRadius: BorderRadius.circular(KageRadius.poster),
                      child: AspectRatio(
                        aspectRatio: widget.landscape
                            ? 16 / 9
                            : widget.cropped
                            ? (widget.isTV ? 143 / 194 : 132 / 184)
                            : 2 / 3,
                        child: Stack(
                          fit: StackFit.expand,
                          children: [
                            if (widget.landscape && a.banner.isEmpty)
                              WideCoverArt(
                                a.cover,
                                width: widget.width,
                                height: widget.width * 9 / 16,
                              )
                            else
                              CoverImage(
                                widget.landscape ? a.banner : a.cover,
                                width: widget.width,
                                height: widget.width * 1.5,
                              ),
                            if (widget.progress != null && widget.progress! > 0)
                              Positioned(
                                left: 0,
                                right: 0,
                                bottom: 0,
                                child: LinearProgressIndicator(
                                  value: widget.progress!.clamp(0, 1),
                                  minHeight: 3,
                                  color: shu,
                                  backgroundColor: line,
                                ),
                              ),
                          ],
                        ),
                      ),
                    ),
                  ),
                ),
              ),
              const SizedBox(height: 7),
              GestureDetector(
                onTap: open,
                child: Text(
                  a.title,
                  maxLines: widget.isTV ? 1 : 2,
                  overflow: TextOverflow.ellipsis,
                  style: uiStyle(
                    context,
                    size: 16,
                    weight: FontWeight.w500,
                    color: focused ? fgBright : fg,
                    height: 1.3,
                  ),
                ),
              ),
              const SizedBox(height: 5),
              Text(
                widget.caption ??
                    '${a.formatLabel}${a.year > 0 ? ' · ${a.year}' : ''}',
                maxLines: widget.isTV ? 1 : 2,
                overflow: TextOverflow.ellipsis,
                style: uiStyle(context, size: 15, color: muted),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _SplitFrame extends CustomPainter {
  _SplitFrame(this.radius, this.tv);
  final double radius;
  final bool tv;
  @override
  void paint(Canvas canvas, Size size) {
    final rect = Offset.zero & size;
    RRect shape(Rect r) =>
        RRect.fromRectAndRadius(r, Radius.circular(radius + 5));
    if (tv) {
      canvas.drawRRect(
        shape(rect.inflate(6).shift(const Offset(4, 3))),
        Paint()
          ..color = shu.withValues(alpha: .145)
          ..style = PaintingStyle.stroke
          ..strokeWidth = 2,
      );
    }
    canvas.drawRRect(
      shape(rect.inflate(3)),
      Paint()
        ..color = bg
        ..style = PaintingStyle.stroke
        ..strokeWidth = 8,
    );
    canvas.drawRRect(
      shape(rect.inflate(6)),
      Paint()
        ..color = shu
        ..style = PaintingStyle.stroke
        ..strokeWidth = 2,
    );
  }

  @override
  bool shouldRepaint(_SplitFrame old) => old.radius != radius || old.tv != tv;
}

/// The card width that fills a row of [available] width exactly, with
/// [padding] at both ends and [gap] between cards no narrower than
/// [minWidth], so a desktop shelf never ends in empty space.
double fittedCardWidth(
  double available,
  double minWidth, {
  required double padding,
  required double gap,
}) {
  final room = available - padding * 2;
  final count = math.max(1, ((room + gap) / (minWidth + gap)).floor());
  return (room - gap * (count - 1)) / count;
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
    this.landscape = false,
    this.onMenu,
  });
  final String title;
  final List<Anime> items;

  /// More for one card, such as removing it from Continue watching.
  final void Function(Anime)? onMenu;
  final bool isTV, landscape;
  final void Function(Anime) onOpen;
  final double? Function(Anime)? progress;
  final String? Function(Anime)? captions;
  final Widget? trailing;
  @override
  State<PosterRow> createState() => _PosterRowState();
}

class _PosterRowState extends State<PosterRow> {
  final scroll = ScrollController();
  @override
  void dispose() {
    scroll.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (widget.items.isEmpty) return const SizedBox.shrink();
    if (isDesktop) {
      // Desktop rows size the cards so they fill the window edge to edge.
      return LayoutBuilder(
        builder: (context, box) => row(
          context,
          fittedCardWidth(
            box.maxWidth,
            widget.landscape ? 380 : 200,
            padding: widget.isTV ? 41 : 13,
            gap: widget.isTV ? 26 : 12,
          ),
        ),
      );
    }
    return row(
      context,
      widget.landscape
          ? (widget.isTV ? 340.0 : 264.0)
          : (widget.isTV ? 143.0 : 132.0),
    );
  }

  Widget row(BuildContext context, double width) {
    final artHeight = widget.landscape
        ? width * 9 / 16
        : isDesktop
        ? width * 194 / 143
        : (widget.isTV ? 194.0 : 184.0);
    return Padding(
      key: ValueKey('section-${widget.title}'),
      padding: const EdgeInsets.only(top: 28, bottom: 4),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: EdgeInsets.symmetric(horizontal: widget.isTV ? 48 : 20),
            child: Row(
              children: [
                Expanded(child: SectionLabel(widget.title)),
                ?widget.trailing,
              ],
            ),
          ),
          const SizedBox(height: 14),
          SizedBox(
            height:
                artHeight +
                (widget.isTV ? 108 : 120) *
                    MediaQuery.textScalerOf(context).scale(1),
            child: RemoteFocusRow(
              isTV: widget.isTV,
              controller: scroll,
              itemCount: widget.items.length,
              itemWidth: width,
              spacing: widget.isTV ? 26 : 12,
              padding: widget.isTV ? 41 : 13,
              child: ListView.separated(
                controller: scroll,
                key: PageStorageKey('row-${widget.title}'),
                scrollDirection: Axis.horizontal,
                padding: EdgeInsets.symmetric(
                  horizontal: widget.isTV ? 41 : 13,
                  vertical: 8,
                ),
                clipBehavior: Clip.none,
                itemCount: widget.items.length,
                findItemIndexCallback: (key) {
                  final index = widget.items.indexWhere(
                    (anime) => ValueKey('shelf-${anime.key}') == key,
                  );
                  return index < 0 ? null : index;
                },
                separatorBuilder: (_, i) =>
                    SizedBox(width: widget.isTV ? 26 : 12),
                itemBuilder: (_, i) => RemoteFocusItem(
                  key: ValueKey('shelf-${widget.items[i].key}'),
                  index: i,
                  child: PosterCard(
                    key: ValueKey('${widget.title}-${widget.items[i].key}'),
                    anime: widget.items[i],
                    onOpen: () => widget.onOpen(widget.items[i]),
                    isTV: widget.isTV,
                    width: width,
                    landscape: widget.landscape,
                    cropped: !widget.landscape,
                    progress: widget.progress?.call(widget.items[i]),
                    caption: widget.captions?.call(widget.items[i]),
                    onMenu: widget.onMenu == null
                        ? null
                        : () => widget.onMenu!(widget.items[i]),
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// Opens [onMenu] from a right click, or the remote's Info or Menu key while
/// the card inside has focus.
class CardMenu extends StatelessWidget {
  const CardMenu({super.key, required this.onMenu, required this.child});
  final VoidCallback? onMenu;
  final Widget child;
  @override
  Widget build(BuildContext context) {
    final menu = onMenu;
    if (menu == null) return child;
    return Focus(
      skipTraversal: true,
      canRequestFocus: false,
      onKeyEvent: (_, event) {
        if (event.logicalKey == LogicalKeyboardKey.info ||
            event.logicalKey == LogicalKeyboardKey.contextMenu) {
          if (event is KeyDownEvent) menu();
          return KeyEventResult.handled;
        }
        return KeyEventResult.ignored;
      },
      child: GestureDetector(onSecondaryTap: menu, child: child),
    );
  }
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

/// The one list row: Settings, More, Accounts, pickers. An optional icon,
/// a title with an optional line under it, then a value, a switch, a custom
/// trailing widget or a chevron. A chosen row gets the darker panel and a
/// 3 px Shu bar on its leading edge.
class AppRow extends StatelessWidget {
  const AppRow({
    super.key,
    required this.title,
    this.subtitle,
    this.icon,
    this.value,
    this.trailing,
    this.onTap,
    this.toggle,
    this.selected = false,
    this.danger = false,
    this.external = false,
    this.divider = false,
    this.enabled = true,
    this.isTV = false,
    this.autofocus = false,
    this.focusNode,
    this.leading,
  });
  final String title;
  final String? subtitle, value;
  final IconData? icon;

  /// Replaces [icon], for avatars and seals.
  final Widget? leading;
  final Widget? trailing;
  final VoidCallback? onTap;

  /// Shows a switch in this state; tapping the row flips it.
  final bool? toggle;
  final bool selected, danger, external, divider, enabled, isTV, autofocus;
  final FocusNode? focusNode;

  @override
  Widget build(BuildContext context) {
    final live = enabled && (onTap != null || toggle != null);
    // A TV sits across the room; the computer shares TV layouts but is read
    // up close, so it keeps rows compact.
    final tall = isTV && !isDesktop;
    final ink = !enabled
        ? muted
        : danger
        ? KageColors.danger
        : selected
        ? fgBright
        : fg;
    return Semantics(
      selected: selected,
      toggled: toggle,
      child: FocusBox(
        isTV: isTV,
        autofocus: autofocus,
        focusNode: focusNode,
        radius: KageRadius.md,
        onPressed: enabled ? onTap : null,
        child: Container(
          constraints: BoxConstraints(minHeight: tall ? 72 : (isTV ? 44 : 56)),
          padding: EdgeInsets.symmetric(
            horizontal: tall ? 20 : 16,
            vertical: tall ? 12 : 10,
          ),
          decoration: BoxDecoration(
            color: selected ? selectedPanel : Colors.transparent,
            borderRadius: divider ? null : BorderRadius.circular(KageRadius.md),
            border: selected
                ? const Border(left: BorderSide(color: shu, width: 3))
                : divider
                ? const Border(bottom: BorderSide(color: hairline))
                : null,
          ),
          child: Row(
            children: [
              if (leading != null) ...[
                leading!,
                SizedBox(width: tall ? 16 : 14),
              ] else if (icon != null) ...[
                Icon(
                  icon,
                  size: tall ? 24 : 20,
                  color: danger && enabled
                      ? KageColors.danger
                      : selected
                      ? fgBright
                      : muted,
                ),
                SizedBox(width: tall ? 16 : 14),
              ],
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(
                      title,
                      style: uiStyle(
                        context,
                        color: ink,
                        weight: selected ? FontWeight.w600 : FontWeight.w500,
                      ),
                    ),
                    if (subtitle != null && subtitle!.isNotEmpty) ...[
                      const SizedBox(height: 2),
                      Text(
                        subtitle!,
                        style: uiStyle(context, size: 15, color: muted),
                      ),
                    ],
                  ],
                ),
              ),
              if (value != null && value!.isNotEmpty) ...[
                const SizedBox(width: 12),
                ConstrainedBox(
                  constraints: BoxConstraints(maxWidth: isTV ? 320 : 140),
                  child: Text(
                    value!,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    textAlign: TextAlign.end,
                    style: uiStyle(context, size: 15, color: muted),
                  ),
                ),
              ],
              if (toggle != null) ...[
                const SizedBox(width: 12),
                ExcludeFocus(
                  child: IgnorePointer(
                    child: Switch(
                      value: toggle!,
                      activeThumbColor: fgBright,
                      onChanged: enabled ? (_) {} : null,
                    ),
                  ),
                ),
              ] else if (trailing != null) ...[
                const SizedBox(width: 12),
                trailing!,
              ] else if (live) ...[
                const SizedBox(width: 8),
                Icon(
                  external
                      ? LucideIcons.arrowUpRight
                      : LucideIcons.chevronRight,
                  size: 18,
                  color: muted,
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

/// A short mono heading over a group of rows, with a hairline to the edge.
class GroupHeading extends StatelessWidget {
  const GroupHeading(this.title, {super.key, this.padding});
  final String title;
  final EdgeInsets? padding;
  @override
  Widget build(BuildContext context) => Padding(
    padding: padding ?? const EdgeInsets.fromLTRB(16, 24, 16, 8),
    child: Semantics(
      header: true,
      child: Row(
        children: [
          Text(
            title.toUpperCase(),
            style: mono.copyWith(
              fontSize: AppLayout.tvOf(context) ? 18 : 13,
              letterSpacing: 1.2,
              color: fgBright,
            ),
          ),
          const SizedBox(width: 10),
          const Expanded(child: Divider(height: 1, color: line)),
        ],
      ),
    ),
  );
}

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

/// Lets the viewer pick one of [values]. The same call looks right on each
/// screen: a bottom sheet on a phone, a side panel on a TV and a menu at the
/// pointer on a computer. A computer question that carries a [note] stays a
/// dialog, because a menu has no room to explain.
Future<String?> chooseValue(
  BuildContext context,
  String title,
  List<String> values, {
  String? selected,
  String? note,
}) {
  final tv = AppLayout.tvOf(context);
  if (isDesktop) {
    final anchor = note == null ? _pickerAnchor(context) : null;
    if (anchor != null) {
      return _chooseMenu(context, anchor, values, selected);
    }
    return _chooseDialog(context, title, values, selected, note);
  }
  if (tv) return _choosePanel(context, title, values, selected, note);
  return _chooseSheet(context, title, values, selected, note);
}

List<Widget> _pickerRows(
  BuildContext context,
  List<String> values,
  String? selected, {
  required bool tv,
}) => [
  for (final value in values)
    _PickerOption(
      label: value,
      selected: value == selected,
      tv: tv,
      autofocus: value == (values.contains(selected) ? selected : values.first),
      onTap: () => Navigator.pop(context, value),
    ),
];

/// One choice in a picker: a compact line, the current one on a raised tile
/// with a Shu check.
class _PickerOption extends StatelessWidget {
  const _PickerOption({
    required this.label,
    required this.selected,
    required this.tv,
    required this.autofocus,
    required this.onTap,
  });
  final String label;
  final bool selected, tv, autofocus;
  final VoidCallback onTap;
  @override
  Widget build(BuildContext context) => Semantics(
    selected: selected,
    button: true,
    child: FocusBox(
      autofocus: autofocus,
      isTV: tv,
      radius: KageRadius.button,
      frameGap: 2,
      onPressed: onTap,
      child: Container(
        constraints: BoxConstraints(minHeight: tv ? 52 : 48),
        padding: const EdgeInsets.symmetric(horizontal: KageSpace.s3),
        decoration: BoxDecoration(
          color: selected ? selectedPanel : Colors.transparent,
          borderRadius: const BorderRadius.all(
            Radius.circular(KageRadius.button),
          ),
        ),
        child: Row(
          children: [
            Expanded(
              child: Text(
                label,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: uiStyle(
                  context,
                  color: selected ? fgBright : fg,
                  weight: selected ? FontWeight.w600 : FontWeight.w400,
                ),
              ),
            ),
            if (selected)
              Icon(LucideIcons.check, size: tv ? 22 : 20, color: shu),
          ],
        ),
      ),
    ),
  );
}

/// Picker title as a mono kicker with a hairline rule, like the section
/// labels on the pages, and an optional note under it.
Widget _pickerHeading(BuildContext context, String title, String? note) =>
    Padding(
      padding: const EdgeInsets.fromLTRB(
        KageSpace.s3,
        0,
        KageSpace.s3,
        KageSpace.s2,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Flexible(
                child: Text(
                  title.toUpperCase(),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: mono.copyWith(
                    color: fgBright,
                    fontSize: AppLayout.tvOf(context) ? 16 : 13,
                    letterSpacing: 1.6,
                    height: 1.2,
                  ),
                ),
              ),
              const SizedBox(width: KageSpace.s3),
              const Expanded(child: Divider(color: hairline, height: 1)),
            ],
          ),
          if (note != null)
            Padding(
              padding: const EdgeInsets.only(top: KageSpace.s2),
              child: Text(note, style: uiStyle(context, color: muted)),
            ),
        ],
      ),
    );

/// The floating plate both pickers sit on: panel colour, hairline edge and
/// one soft shadow, like the phone nav bar.
Widget _pickerPlate({required Widget child, required EdgeInsets padding}) =>
    Container(
      padding: padding,
      decoration: const BoxDecoration(
        color: panel,
        borderRadius: BorderRadius.all(Radius.circular(KageRadius.sheet)),
        border: Border.fromBorderSide(BorderSide(color: line)),
        boxShadow: [
          BoxShadow(color: scrim, offset: Offset(0, 8), blurRadius: 24),
        ],
      ),
      child: child,
    );

Future<String?> _chooseSheet(
  BuildContext context,
  String title,
  List<String> values,
  String? selected,
  String? note,
) => showModalBottomSheet<String>(
  context: context,
  isScrollControlled: true,
  useSafeArea: true,
  backgroundColor: Colors.transparent,
  elevation: 0,
  constraints: BoxConstraints(
    maxHeight: MediaQuery.sizeOf(context).height * .7,
  ),
  builder: (context) => SafeArea(
    top: false,
    minimum: const EdgeInsets.only(bottom: KageSpace.s3),
    child: Padding(
      padding: const EdgeInsets.symmetric(horizontal: KageSpace.s3),
      child: _pickerPlate(
        padding: const EdgeInsets.fromLTRB(
          KageSpace.s2,
          KageSpace.s2,
          KageSpace.s2,
          KageSpace.s2,
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Center(
              child: Container(
                width: 36,
                height: 4,
                margin: const EdgeInsets.only(bottom: KageSpace.s3),
                decoration: const BoxDecoration(
                  color: lineStrong,
                  borderRadius: BorderRadius.all(
                    Radius.circular(KageRadius.full),
                  ),
                ),
              ),
            ),
            _pickerHeading(context, title, note),
            Flexible(
              child: SingleChildScrollView(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: _pickerRows(context, values, selected, tv: false),
                ),
              ),
            ),
          ],
        ),
      ),
    ),
  ),
);

Future<String?> _choosePanel(
  BuildContext context,
  String title,
  List<String> values,
  String? selected,
  String? note,
) => showGeneralDialog<String>(
  context: context,
  barrierDismissible: true,
  barrierLabel: title,
  barrierColor: const Color(0x66000000),
  transitionDuration: MediaQuery.disableAnimationsOf(context)
      ? Duration.zero
      : KageMotion.base,
  transitionBuilder: (context, animation, _, child) {
    final curved = CurvedAnimation(
      parent: animation,
      curve: KageMotion.enter,
      reverseCurve: KageMotion.exit,
    );
    return FadeTransition(
      opacity: curved,
      child: SlideTransition(
        position: Tween(
          begin: const Offset(.08, 0),
          end: Offset.zero,
        ).animate(curved),
        child: child,
      ),
    );
  },
  // A compact plate at the right edge, as tall as its list: the page stays
  // in view beside it.
  pageBuilder: (context, _, _) => SafeArea(
    child: Align(
      alignment: Alignment.centerRight,
      child: Padding(
        padding: const EdgeInsets.all(KageSpace.s6),
        child: ConstrainedBox(
          constraints: BoxConstraints(
            maxWidth: 400,
            maxHeight: MediaQuery.sizeOf(context).height * .8,
          ),
          child: Material(
            type: MaterialType.transparency,
            child: _pickerPlate(
              padding: const EdgeInsets.fromLTRB(
                KageSpace.s2,
                KageSpace.s4,
                KageSpace.s2,
                KageSpace.s2,
              ),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  _pickerHeading(context, title, note),
                  Flexible(
                    child: SingleChildScrollView(
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: _pickerRows(
                          context,
                          values,
                          selected,
                          tv: true,
                        ),
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    ),
  ),
);

/// The control that opened a desktop picker: the focused one with the
/// keyboard, else the spot the mouse went down. Null when neither is known.
Rect? _pickerAnchor(BuildContext context) {
  final overlay = Overlay.maybeOf(context)?.context.findRenderObject();
  if (overlay is! RenderBox) return null;
  if (DesktopInput.keyboard) {
    final box = FocusManager.instance.primaryFocus?.context?.findRenderObject();
    if (box is RenderBox && box.attached && box.hasSize) {
      final topLeft = overlay.globalToLocal(box.localToGlobal(Offset.zero));
      final bottomRight = overlay.globalToLocal(
        box.localToGlobal(box.size.bottomRight(Offset.zero)),
      );
      return Rect.fromPoints(topLeft, bottomRight);
    }
    return null;
  }
  final down = DesktopInput.lastPointerDown;
  if (down == null) return null;
  return overlay.globalToLocal(down) & Size.zero;
}

Future<String?> _chooseMenu(
  BuildContext context,
  Rect anchor,
  List<String> values,
  String? selected,
) {
  final overlay = Overlay.of(context).context.findRenderObject()! as RenderBox;
  return showMenu<String>(
    context: context,
    color: panel,
    position: RelativeRect.fromRect(anchor, Offset.zero & overlay.size),
    constraints: const BoxConstraints(minWidth: 220, maxWidth: 420),
    items: [
      for (final value in values)
        PopupMenuItem(
          value: value,
          child: Row(
            children: [
              SizedBox(
                width: 28,
                child: value == selected
                    ? const Icon(LucideIcons.check, size: 18, color: shu)
                    : null,
              ),
              Expanded(
                child: Text(
                  value,
                  style: uiStyle(
                    context,
                    color: value == selected ? fgBright : fg,
                    weight: value == selected
                        ? FontWeight.w600
                        : FontWeight.w400,
                  ),
                ),
              ),
            ],
          ),
        ),
    ],
  );
}

Future<String?> _chooseDialog(
  BuildContext context,
  String title,
  List<String> values,
  String? selected,
  String? note,
) => showDialog<String>(
  context: context,
  builder: (context) => AlertDialog(
    backgroundColor: panel,
    title: Text(
      title,
      style: uiStyle(context, size: 22, weight: FontWeight.w600),
    ),
    content: ConstrainedBox(
      // Grows with the list so a short one never needs scrolling.
      constraints: const BoxConstraints(
        minWidth: 460,
        maxWidth: 460,
        maxHeight: 420,
      ),
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            if (note != null)
              Padding(
                padding: const EdgeInsets.only(bottom: 12),
                child: Text(note, style: uiStyle(context, color: muted)),
              ),
            ..._pickerRows(
              context,
              values,
              selected,
              tv: AppLayout.tvOf(context),
            ),
          ],
        ),
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('Cancel'),
      ),
    ],
  ),
);

class TransportButton extends StatelessWidget {
  const TransportButton({
    super.key,
    required this.label,
    required this.onPressed,
    this.icon,
    this.text,
    this.primary = false,
    this.focusNode,
  });
  final String label;
  final VoidCallback onPressed;
  final IconData? icon;
  final String? text;
  final bool primary;
  final FocusNode? focusNode;
  @override
  Widget build(BuildContext context) {
    final tv = AppLayout.tvOf(context);
    return Semantics(
      label: label,
      button: true,
      child: Tooltip(
        message: label,
        child: FocusBox(
          onPressed: onPressed,
          isTV: tv,
          focusNode: focusNode,
          radius: KageRadius.full,
          // Phones get smaller circles so the picture stays in view.
          child: Container(
            width: primary ? (tv ? 64 : 52) : (tv ? 56 : 44),
            height: primary ? (tv ? 64 : 52) : (tv ? 56 : 44),
            decoration: BoxDecoration(
              shape: BoxShape.circle,
              color: primary ? shu : panel.withValues(alpha: .85),
            ),
            child: Center(
              child: icon != null
                  ? Icon(
                      icon,
                      color: primary ? bgDeep : fgBright,
                      size: primary ? (tv ? 30 : 24) : (tv ? 24 : 20),
                    )
                  : Text(
                      text!,
                      style: mono.copyWith(
                        color: fgBright,
                        fontSize: AppLayout.tvOf(context) ? 20 : 15,
                      ),
                    ),
            ),
          ),
        ),
      ),
    );
  }
}

/// The window draws under a phone's camera cutout (see MainActivity). In
/// landscape the cutout sits on the left or right edge, so menus and pages
/// keep their content beside it while the background still fills the
/// screen. Players call [enter] and [leave] so video runs edge to edge; their
/// controls stay in their own safe area.
class FullBleed {
  static final screens = ValueNotifier<int>(0);

  static void enter() => _change(1);
  static void leave() => _change(-1);

  // Players start and stop while the tree builds or is locked, so the count
  // moves after the frame.
  static void _change(int by) {
    WidgetsBinding.instance
      ..addPostFrameCallback((_) => screens.value += by)
      ..scheduleFrame();
  }
}

/// Keeps app content out of a side cutout unless a player is showing.
class CutoutInset extends StatelessWidget {
  const CutoutInset({super.key, required this.child});
  final Widget child;

  @override
  Widget build(BuildContext context) => ValueListenableBuilder<int>(
    valueListenable: FullBleed.screens,
    builder: (context, screens, _) => ColoredBox(
      color: bg,
      child: SafeArea(
        top: false,
        bottom: false,
        left: screens == 0,
        right: screens == 0,
        child: child,
      ),
    ),
  );
}

/// Part of a page that takes the remote's Back before the app does, like a
/// settings pane that hands focus back to its section in the sidebar.
class BackTo extends StatefulWidget {
  const BackTo({super.key, required this.onBack, required this.child});
  final VoidCallback onBack;
  final Widget child;

  static final _open = <_BackToState>{};

  /// Runs the Back of the innermost part holding focus. False when focus
  /// sits outside every such part.
  static bool handle() {
    for (final part in _open.toList().reversed) {
      if (part.node.hasFocus) {
        part.widget.onBack();
        return true;
      }
    }
    return false;
  }

  @override
  State<BackTo> createState() => _BackToState();
}

class _BackToState extends State<BackTo> {
  final node = FocusNode(debugLabel: 'Back to', skipTraversal: true);
  @override
  void initState() {
    super.initState();
    BackTo._open.add(this);
  }

  @override
  void dispose() {
    BackTo._open.remove(this);
    node.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) =>
      Focus(focusNode: node, canRequestFocus: false, child: widget.child);
}
