import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:window_manager/window_manager.dart';

import '../ui/components.dart';
import 'desktop_core.dart';

/// Opens the desktop window without a system title bar. The app's own top
/// bar moves the window and holds the window buttons. macOS keeps its
/// traffic-light buttons, drawn over the top-left corner of the app.
Future<void> setUpDesktopWindow({bool launcher = false}) async {
  await windowManager.ensureInitialized();
  final options = WindowOptions(
    title: launcher ? 'Otakase Launcher' : 'Otakase',
    size: launcher ? const Size(680, 560) : const Size(1440, 900),
    minimumSize: launcher ? const Size(480, 360) : const Size(960, 600),
    center: true,
    alwaysOnTop: launcher,
    backgroundColor: bg,
    titleBarStyle: TitleBarStyle.hidden,
    windowButtonVisibility: !launcher,
  );
  await windowManager.waitUntilReadyToShow(options, () async {
    await windowManager.show();
    await windowManager.focus();
  });
}

/// Space the macOS traffic lights take at the left of the top bar.
double get windowButtonsLeading => isDesktop && Platform.isMacOS ? 78 : 0;

/// Lets the user drag the window by the empty parts of [child] and maximise
/// it with a double click there. Buttons in [child] still get every click
/// straight away. Does nothing outside desktop builds.
class WindowDragArea extends StatelessWidget {
  const WindowDragArea({super.key, required this.child});
  final Widget child;
  @override
  Widget build(BuildContext context) {
    if (!isDesktop) return child;
    return Stack(
      children: [
        Positioned.fill(
          child: GestureDetector(
            key: const Key('window-drag-area'),
            behavior: HitTestBehavior.opaque,
            onPanStart: (_) => unawaited(windowManager.startDragging()),
            onDoubleTap: () => unawaited(toggleMaximized()),
          ),
        ),
        child,
      ],
    );
  }
}

Future<void> toggleMaximized() async {
  if (await windowManager.isMaximized()) {
    await windowManager.unmaximize();
  } else {
    await windowManager.maximize();
  }
}

/// Turns full screen on or off and returns the new state.
Future<bool> setDesktopFullScreen(bool on) async {
  await windowManager.setFullScreen(on);
  return on;
}

/// Minimise, maximise and close for Windows and Linux. macOS uses its own
/// buttons, so this is empty there.
class WindowButtons extends StatefulWidget {
  const WindowButtons({super.key, this.onClose});

  /// Runs instead of closing straight away, for example to stop Discord
  /// presence first. It must close the window itself.
  final Future<void> Function()? onClose;
  @override
  State<WindowButtons> createState() => _WindowButtonsState();
}

class _WindowButtonsState extends State<WindowButtons> with WindowListener {
  bool maximized = false;

  @override
  void initState() {
    super.initState();
    if (!isDesktop || Platform.isMacOS) return;
    windowManager.addListener(this);
    // Wayland compositors report tiled windows as maximised, so Linux always
    // shows the plain maximise icon.
    if (Platform.isLinux) return;
    unawaited(
      windowManager.isMaximized().then((value) {
        if (mounted) setState(() => maximized = value);
      }),
    );
  }

  @override
  void dispose() {
    if (isDesktop && !Platform.isMacOS) windowManager.removeListener(this);
    super.dispose();
  }

  @override
  void onWindowMaximize() {
    if (!Platform.isLinux) setState(() => maximized = true);
  }

  @override
  void onWindowUnmaximize() => setState(() => maximized = false);

  @override
  Widget build(BuildContext context) {
    if (!isDesktop || Platform.isMacOS) return const SizedBox.shrink();
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        _WindowButton(
          key: const Key('window-minimize'),
          icon: LucideIcons.minus,
          tooltip: 'Minimize',
          onPressed: () => unawaited(windowManager.minimize()),
        ),
        _WindowButton(
          key: const Key('window-maximize'),
          icon: maximized ? LucideIcons.copy : LucideIcons.square,
          tooltip: maximized ? 'Restore' : 'Maximize',
          onPressed: () => unawaited(toggleMaximized()),
        ),
        _WindowButton(
          key: const Key('window-close'),
          icon: LucideIcons.x,
          tooltip: 'Close',
          danger: true,
          onPressed: () => unawaited(
            widget.onClose != null ? widget.onClose!() : quitDesktopApp(),
          ),
        ),
      ],
    );
  }
}

class _WindowButton extends StatefulWidget {
  const _WindowButton({
    super.key,
    required this.icon,
    required this.tooltip,
    required this.onPressed,
    this.danger = false,
  });
  final IconData icon;
  final String tooltip;
  final VoidCallback onPressed;
  final bool danger;
  @override
  State<_WindowButton> createState() => _WindowButtonState();
}

class _WindowButtonState extends State<_WindowButton> {
  bool hovered = false;
  @override
  Widget build(BuildContext context) => Tooltip(
    message: widget.tooltip,
    child: MouseRegion(
      onEnter: (_) => setState(() => hovered = true),
      onExit: (_) => setState(() => hovered = false),
      child: GestureDetector(
        onTap: widget.onPressed,
        child: Container(
          width: 46,
          height: 34,
          decoration: BoxDecoration(
            color: hovered
                ? (widget.danger ? shu : panelRaised)
                : Colors.transparent,
            borderRadius: BorderRadius.circular(8),
          ),
          child: Icon(widget.icon, size: 16, color: hovered ? fgBright : muted),
        ),
      ),
    ),
  );
}

/// Runs when a window close button is pressed, set by the app shell so it
/// can stop Discord presence first. It must close the window itself.
Future<void> Function()? desktopCloseHook;

/// Closes the window and ends the app. The process exits right away instead
/// of tearing the Flutter view down: on Linux that teardown can crash while
/// the player's video texture is still alive. Callers save their state first
/// (see [quitDesktopApp]).
Future<void> closeDesktopWindow() async {
  await windowManager.hide();
  exit(0);
}

/// Saves and stops everything through [desktopCloseHook], then closes.
Future<void> quitDesktopApp() async =>
    await (desktopCloseHook ?? closeDesktopWindow)();

/// Wraps every desktop route: Esc goes back one page, and a thin strip
/// along the top edge moves the window on pages whose header is not a
/// drag area.
class DesktopFrame extends StatelessWidget {
  const DesktopFrame({super.key, required this.child});
  final Widget child;

  static void back() {
    final context = FocusManager.instance.primaryFocus?.context;
    if (context != null) unawaited(Navigator.maybeOf(context)?.maybePop());
  }

  @override
  Widget build(BuildContext context) {
    if (!isDesktop) return child;
    return CallbackShortcuts(
      bindings: {const SingleActivator(LogicalKeyboardKey.escape): back},
      child: Stack(
        children: [
          child,
          Positioned(
            top: 0,
            left: windowButtonsLeading,
            right: 160,
            height: 10,
            child: GestureDetector(
              key: const Key('window-edge-drag'),
              behavior: HitTestBehavior.opaque,
              onPanStart: (_) => unawaited(windowManager.startDragging()),
              onDoubleTap: () => unawaited(toggleMaximized()),
            ),
          ),
        ],
      ),
    );
  }
}
