import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

/// Keys for the whole app. Android, Windows and Linux use Flutter's own
/// defaults; the web build (the website's demo) gets the same ones, since
/// Flutter's web defaults make the arrows scroll instead of moving focus the
/// way a TV remote does, and Enter only presses buttons.
Map<ShortcutActivator, Intent>? get appShortcuts => kIsWeb ? _remote : null;

const _remote = <ShortcutActivator, Intent>{
  SingleActivator(LogicalKeyboardKey.enter): ActivateIntent(),
  SingleActivator(LogicalKeyboardKey.numpadEnter): ActivateIntent(),
  SingleActivator(LogicalKeyboardKey.space): ActivateIntent(),
  SingleActivator(LogicalKeyboardKey.gameButtonA): ActivateIntent(),
  SingleActivator(LogicalKeyboardKey.select): ActivateIntent(),
  SingleActivator(LogicalKeyboardKey.escape): DismissIntent(),
  SingleActivator(LogicalKeyboardKey.tab): NextFocusIntent(),
  SingleActivator(LogicalKeyboardKey.tab, shift: true): PreviousFocusIntent(),
  SingleActivator(LogicalKeyboardKey.arrowLeft): DirectionalFocusIntent(
    TraversalDirection.left,
  ),
  SingleActivator(LogicalKeyboardKey.arrowRight): DirectionalFocusIntent(
    TraversalDirection.right,
  ),
  SingleActivator(LogicalKeyboardKey.arrowDown): DirectionalFocusIntent(
    TraversalDirection.down,
  ),
  SingleActivator(LogicalKeyboardKey.arrowUp): DirectionalFocusIntent(
    TraversalDirection.up,
  ),
  SingleActivator(LogicalKeyboardKey.arrowUp, control: true): ScrollIntent(
    direction: AxisDirection.up,
  ),
  SingleActivator(LogicalKeyboardKey.arrowDown, control: true): ScrollIntent(
    direction: AxisDirection.down,
  ),
  SingleActivator(LogicalKeyboardKey.pageUp): ScrollIntent(
    direction: AxisDirection.up,
    type: ScrollIncrementType.page,
  ),
  SingleActivator(LogicalKeyboardKey.pageDown): ScrollIntent(
    direction: AxisDirection.down,
    type: ScrollIncrementType.page,
  ),
};
