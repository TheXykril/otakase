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
/// Set by tests to draw the macOS layout on another OS.
@visibleForTesting
bool? debugMacLayout;

/// True when the window uses the macOS layout: Apple's own buttons on the
/// left, nothing drawn by the app for them.
bool get macWindowLayout => debugMacLayout ?? Platform.isMacOS;

double get windowButtonsLeading => isDesktop && macWindowLayout ? 78 : 0;

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
  if (tilingWindowManager) return;
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

/// Tiling window managers place every window themselves and have no
/// minimised state, so minimise and maximise do nothing there. Spotted from
/// the environment the compositor sets for its apps.
bool isTilingWindowManager(Map<String, String> env) {
  const sockets = [
    'HYPRLAND_INSTANCE_SIGNATURE',
    'SWAYSOCK',
    'I3SOCK',
    'NIRI_SOCKET',
  ];
  if (sockets.any(env.containsKey)) return true;
  const tiling = {
    'hyprland',
    'sway',
    'i3',
    'niri',
    'river',
    'bspwm',
    'dwm',
    'qtile',
    'xmonad',
    'herbstluftwm',
    'awesome',
    'leftwm',
    'spectrwm',
  };
  final desktops = [
    ...(env['XDG_CURRENT_DESKTOP'] ?? '').split(':'),
    env['XDG_SESSION_DESKTOP'] ?? '',
    env['DESKTOP_SESSION'] ?? '',
  ].map((d) => d.trim().toLowerCase());
  return desktops.any(tiling.contains);
}

/// Set by tests in place of reading the real environment.
@visibleForTesting
bool? debugTilingOverride;

bool get tilingWindowManager =>
    debugTilingOverride ??
    (Platform.isLinux && isTilingWindowManager(Platform.environment));

/// How the window buttons are drawn: like the system the app runs on.
enum WindowButtonStyle {
  /// Windows 10/11: square, full height, close turns red.
  windows,

  /// GNOME and most other Linux desktops: small round grey buttons.
  gnome,

  /// KDE Plasma (Breeze): arrows for minimise and maximise, red close circle.
  kde,

  /// Tiling window managers: only Otakase's own close button.
  tiling,
}

/// Picks the button style from the OS and, on Linux, the desktop.
WindowButtonStyle windowButtonStyleFor({
  required bool windows,
  required Map<String, String> env,
}) {
  if (windows) return WindowButtonStyle.windows;
  if (isTilingWindowManager(env)) return WindowButtonStyle.tiling;
  final desktop = [
    env['XDG_CURRENT_DESKTOP'] ?? '',
    env['XDG_SESSION_DESKTOP'] ?? '',
    env['DESKTOP_SESSION'] ?? '',
  ].join(':').toLowerCase();
  if (desktop.contains('kde') || desktop.contains('plasma')) {
    return WindowButtonStyle.kde;
  }
  return WindowButtonStyle.gnome;
}

/// Set by tests in place of reading the real system.
@visibleForTesting
WindowButtonStyle? debugWindowButtonStyle;

WindowButtonStyle get windowButtonStyle =>
    debugWindowButtonStyle ??
    (debugTilingOverride == true
        ? WindowButtonStyle.tiling
        : windowButtonStyleFor(
            windows: Platform.isWindows,
            env: Platform.environment,
          ));

/// Minimise, maximise and close for Windows and Linux, drawn like the
/// system's own buttons. Tiling window managers only get close. macOS uses its own
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
    if (!isDesktop || macWindowLayout) return;
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
    if (isDesktop && !macWindowLayout) windowManager.removeListener(this);
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
    if (!isDesktop || macWindowLayout) return const SizedBox.shrink();
    final style = windowButtonStyle;
    final tiling = style == WindowButtonStyle.tiling || tilingWindowManager;
    final kde = style == WindowButtonStyle.kde;
    return Row(
      mainAxisSize: MainAxisSize.min,
      spacing: switch (style) {
        WindowButtonStyle.gnome => 10,
        WindowButtonStyle.kde => 8,
        _ => 0,
      },
      children: [
        if (!tiling)
          _WindowButton(
            key: const Key('window-minimize'),
            style: style,
            icon: kde ? LucideIcons.chevronDown : LucideIcons.minus,
            tooltip: 'Minimize',
            onPressed: () => unawaited(windowManager.minimize()),
          ),
        if (!tiling)
          _WindowButton(
            key: const Key('window-maximize'),
            style: style,
            icon: kde
                ? (maximized
                      ? LucideIcons.chevronsDownUp
                      : LucideIcons.chevronUp)
                : maximized
                ? LucideIcons.copy
                : LucideIcons.square,
            tooltip: maximized ? 'Restore' : 'Maximize',
            onPressed: () => unawaited(toggleMaximized()),
          ),
        _WindowButton(
          key: const Key('window-close'),
          style: tiling ? WindowButtonStyle.tiling : style,
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
    required this.style,
    required this.icon,
    required this.tooltip,
    required this.onPressed,
    this.danger = false,
  });
  final WindowButtonStyle style;
  final IconData icon;
  final String tooltip;
  final VoidCallback onPressed;
  final bool danger;
  @override
  State<_WindowButton> createState() => _WindowButtonState();
}

class _WindowButtonState extends State<_WindowButton> {
  bool hovered = false;

  Widget face() {
    final danger = widget.danger;
    switch (widget.style) {
      case WindowButtonStyle.windows:
        return Container(
          width: 46,
          height: 36,
          color: hovered
              ? (danger ? const Color(0xffc42b1c) : const Color(0x1affffff))
              : Colors.transparent,
          child: Icon(widget.icon, size: 15, color: fgBright),
        );
      case WindowButtonStyle.gnome:
        return Container(
          width: 24,
          height: 24,
          decoration: BoxDecoration(
            shape: BoxShape.circle,
            color: hovered ? const Color(0x33ffffff) : const Color(0x1affffff),
          ),
          child: Icon(widget.icon, size: 13, color: fgBright),
        );
      case WindowButtonStyle.kde:
        return Container(
          width: 20,
          height: 20,
          decoration: BoxDecoration(
            shape: BoxShape.circle,
            color: danger
                ? (hovered ? const Color(0xffda4453) : const Color(0x00000000))
                : (hovered ? const Color(0x33ffffff) : const Color(0x00000000)),
          ),
          child: Icon(widget.icon, size: 13, color: const Color(0xffeff0f1)),
        );
      case WindowButtonStyle.tiling:
        return Container(
          width: 46,
          height: 34,
          decoration: BoxDecoration(
            color: hovered ? (danger ? shu : panelRaised) : Colors.transparent,
            borderRadius: BorderRadius.circular(8),
          ),
          child: Icon(widget.icon, size: 16, color: hovered ? fgBright : muted),
        );
    }
  }

  @override
  Widget build(BuildContext context) => Tooltip(
    message: widget.tooltip,
    child: MouseRegion(
      onEnter: (_) => setState(() => hovered = true),
      onExit: (_) => setState(() => hovered = false),
      child: GestureDetector(onTap: widget.onPressed, child: face()),
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
