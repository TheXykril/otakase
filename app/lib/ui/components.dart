import 'dart:async';
import 'dart:math' as math;
import 'dart:ui';

import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';

import '../desktop/desktop_core.dart';

import 'package:flutter/services.dart';
import 'package:lucide_icons_flutter/lucide_icons.dart';

import '../data/models.dart';

export 'package:lucide_icons_flutter/lucide_icons.dart';

const bg = Color(0xff0e0d0c),
    bgDeep = Color(0xff080707),
    panel = Color(0xff191614),
    line = Color(0xff3c332d),
    fg = Color(0xffece4d6),
    fgBright = Color(0xfffff8ec),
    muted = Color(0xffb9b0a4),
    panelRaised = Color(0xff211b18),
    selectedPanel = Color(0xff2b221e),
    lineStrong = Color(0xff655046),
    accentText = Color(0xfff28b74),
    disabled = Color(0xff8a8276),
    shu = Color(0xffd2492f),
    accentDeep = Color(0xffa83a22),
    ok = Color(0xff9bb67a),
    warn = Color(0xffe3b45a),
    info = Color(0xff7fa2c4);
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
  const AppLayout({super.key, required this.isTV, required super.child});
  final bool isTV;
  static bool tvOf(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<AppLayout>()?.isTV ?? false;
  @override
  bool updateShouldNotify(AppLayout old) => old.isTV != isTV;
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

  static void pointer(PointerEvent event) {
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
        final scale = isDesktop
            ? math.max(
                .7,
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
  fontSize: math.max(size, AppLayout.tvOf(context) ? 20 : 15),
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
    this.radius = 12,
    this.frameGap = 5,
    this.scale = 1.00,
    this.onFocused,
    this.outsideFrame = false,
    this.showFocusBorder = true,
    this.focusFill = bg,
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
        hoverColor: widget.hoverColor ?? line.withValues(alpha: .4),
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
      decoration: BoxDecoration(
        color: visible ? widget.focusFill : Colors.transparent,
        borderRadius: BorderRadius.circular(widget.radius + 7),
        border: Border.all(
          color: visible && widget.showFocusBorder ? shu : Colors.transparent,
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
    this.disabledReason,
  });
  final String label;
  final VoidCallback? onPressed;
  final IconData? icon;
  final bool filled, isTV, autofocus;
  final FocusNode? focusNode;
  final String? disabledReason;
  @override
  Widget build(BuildContext context) {
    final button = FocusBox(
      isTV: isTV,
      autofocus: autofocus,
      focusNode: focusNode,
      radius: 999,
      onPressed: onPressed,
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 250),
        curve: Curves.easeOut,
        padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
        decoration: BoxDecoration(
          color: onPressed == null
              ? line
              : filled
              ? shu
              : fgBright.withValues(alpha: .12),
          borderRadius: BorderRadius.circular(999),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (icon != null) ...[
              Icon(
                icon,
                size: isTV ? 24 : 20,
                color: onPressed == null
                    ? muted
                    : filled
                    ? bgDeep
                    : fgBright,
              ),
              const SizedBox(width: 8),
            ],
            Flexible(
              child: Text(
                label,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: uiStyle(
                  context,
                  weight: FontWeight.w600,
                  color: onPressed == null
                      ? muted
                      : filled
                      ? bgDeep
                      : fgBright,
                ),
              ),
            ),
          ],
        ),
      ),
    );
    if (onPressed != null || disabledReason == null) return button;
    return Tooltip(
      message: disabledReason!,
      child: Semantics(enabled: false, hint: disabledReason, child: button),
    );
  }
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
    radius: 999,
    scale: 1.02,
    child: AnimatedContainer(
      duration: const Duration(milliseconds: 250),
      curve: Curves.easeOut,
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
      decoration: BoxDecoration(
        color: selected
            ? (isTV ? fg : shu.withValues(alpha: .22))
            : fgBright.withValues(alpha: .08),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        label,
        style: uiStyle(
          context,
          color: selected ? (isTV ? bg : fgBright) : muted,
          weight: selected ? FontWeight.w600 : FontWeight.w500,
        ),
      ),
    ),
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
        borderRadius: BorderRadius.circular(12),
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
              borderRadius: BorderRadius.circular(8),
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
  });
  final Anime anime;
  final VoidCallback onOpen;
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
              FocusBox(
                isTV: widget.isTV,
                autofocus: widget.autofocus,
                outsideFrame: true,
                lift: true,
                radius: widget.cropped ? 14 : 10,
                onPressed: open,
                onFocused: widget.onFocused,
                onFocusChanged: (value) => setState(() => focused = value),
                child: Hero(
                  tag: 'poster-${widget.key ?? a.key}',
                  child: ClipRRect(
                    borderRadius: BorderRadius.circular(
                      widget.cropped ? 14 : 10,
                    ),
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
  });
  final String title;
  final List<Anime> items;
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

class MetaLine extends StatelessWidget {
  const MetaLine(this.anime, {super.key});
  final Anime anime;
  @override
  Widget build(BuildContext context) => Wrap(
    spacing: 6,
    runSpacing: 6,
    children:
        [
              if (anime.score > 0) '★ ${anime.score.toStringAsFixed(1)}',
              anime.formatLabel,
              if (anime.year > 0) '${anime.year}',
              if (anime.duration > 0) '${anime.duration} min',
              if (anime.episodes > 0) '${anime.episodes} ep',
            ]
            .where((s) => s.isNotEmpty)
            .map(
              (label) => Container(
                padding: const EdgeInsets.symmetric(
                  horizontal: 10,
                  vertical: 5,
                ),
                decoration: BoxDecoration(
                  color: bgDeep.withValues(alpha: .65),
                  borderRadius: BorderRadius.circular(999),
                ),
                child: Text(
                  label,
                  style: uiStyle(context, size: 15, color: fg),
                ),
              ),
            )
            .toList(),
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
    title: Text(
      title,
      style: uiStyle(context, size: 22, weight: FontWeight.w600),
    ),
    content: SizedBox(
      width: 460,
      height: (values.length * 56.0).clamp(80, 420),
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: values
              .map(
                (value) => ChoiceRow(
                  autofocus:
                      value ==
                      (values.contains(selected) ? selected : values.first),
                  title: Text(value, style: uiStyle(context)),
                  trailing: value == selected
                      ? const Icon(LucideIcons.check, color: shu)
                      : null,
                  onTap: () => Navigator.pop(context, value),
                ),
              )
              .toList(),
        ),
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
  Widget build(BuildContext context) => Semantics(
    label: label,
    button: true,
    child: Tooltip(
      message: label,
      child: FocusBox(
        onPressed: onPressed,
        isTV: AppLayout.tvOf(context),
        focusNode: focusNode,
        radius: 999,
        child: Container(
          width: primary ? 64 : 56,
          height: primary ? 64 : 56,
          decoration: BoxDecoration(
            shape: BoxShape.circle,
            color: primary ? shu : panel,
          ),
          child: Center(
            child: icon != null
                ? Icon(
                    icon,
                    color: primary ? bgDeep : fgBright,
                    size: primary ? 30 : 24,
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
