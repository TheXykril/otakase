import 'dart:async';
import 'dart:io';
import 'dart:ui' show AppExitResponse;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:window_manager/window_manager.dart';

import '../ui/components.dart';
import 'desktop_core.dart';
import '../host.dart';

const fullAppSize = Size(1440, 900);
const fullAppMinimumSize = Size(960, 600);

/// Prepares the window before the runner renders its first frame.
Future<void> setUpDesktopWindow({Size size = fullAppSize}) async {
  await windowManager.ensureInitialized();
  await windowManager.waitUntilReadyToShow(
    WindowOptions(
      title: 'Otakase',
      size: size,
      minimumSize: fullAppMinimumSize,
      center: true,
      backgroundColor: bg,
      titleBarStyle: TitleBarStyle.hidden,
    ),
  );
}

Future<void> showDesktopWindow() async {
  await windowManager.show();
  await windowManager.focus();
}

/// Space the macOS traffic lights take at the left of the top bar.
/// Set by tests to draw the macOS layout on another OS.
@visibleForTesting
bool? debugMacLayout;

/// True when the window uses the macOS layout: Apple's own buttons on the
/// left, nothing drawn by the app for them.
bool get macWindowLayout => debugMacLayout ?? Host.macOS;

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
    (Host.linux && isTilingWindowManager(Host.environment));

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
        : windowButtonStyleFor(windows: Host.windows, env: Host.environment));

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
    if (Host.linux) return;
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
    if (!Host.linux) setState(() => maximized = true);
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
              ? (danger ? WindowColors.windowsClose : WindowColors.hover)
              : Colors.transparent,
          child: Icon(widget.icon, size: 15, color: fgBright),
        );
      case WindowButtonStyle.gnome:
        return Container(
          width: 24,
          height: 24,
          decoration: BoxDecoration(
            shape: BoxShape.circle,
            color: hovered ? WindowColors.pressed : WindowColors.hover,
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
                ? (hovered ? WindowColors.kdeClose : shadeClear)
                : (hovered ? WindowColors.pressed : shadeClear),
          ),
          child: Icon(widget.icon, size: 13, color: WindowColors.kdeIcon),
        );
      case WindowButtonStyle.tiling:
        return Container(
          width: 46,
          height: 34,
          decoration: BoxDecoration(
            color: hovered ? (danger ? shu : panelRaised) : Colors.transparent,
            borderRadius: BorderRadius.circular(KageRadius.sm),
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
  await hideDesktopWindow();
  exit(0);
}

/// Hides the window so a close feels instant while playback is saved.
Future<void> hideDesktopWindow() async {
  try {
    await windowManager.hide().timeout(const Duration(seconds: 1));
  } catch (_) {
    // Quitting matters more than hiding first.
  }
}

Future<void>? _quitting;

/// Saves and stops everything through [desktopCloseHook], then closes.
/// Every way of quitting lands here: the close button, the window manager
/// (Super+W, Alt+F4, the task bar), Cmd+Q and termination signals. A second
/// request while the first one saves joins it. If saving hangs or fails the
/// app still exits.
Future<void> quitDesktopApp() => _quitting ??= _quit();

Future<void> _quit() async {
  try {
    await (desktopCloseHook ?? closeDesktopWindow)().timeout(
      const Duration(seconds: 10),
    );
  } catch (_) {
    await closeDesktopWindow();
  }
  // The hook returned without closing (another quit was already under way):
  // let a later request try again.
  _quitting = null;
}

/// Routes window close requests and termination signals through
/// [quitDesktopApp], so playback is saved and the player stopped before the
/// process ends. Without this the window manager destroys the window while
/// the video still plays and the app crashes on the way out.
Future<void> watchDesktopQuit() async {
  if (!isDesktop || _quitWatcher != null) return;
  final watcher = _quitWatcher = _DesktopQuitWatcher();
  windowManager.addListener(watcher);
  await windowManager.setPreventClose(true);
  watcher.lifecycle = AppLifecycleListener(
    onExitRequested: () async {
      unawaited(quitDesktopApp());
      return AppExitResponse.cancel;
    },
  );
  final signals = [
    ProcessSignal.sigint,
    if (!Host.windows) ...[ProcessSignal.sigterm, ProcessSignal.sighup],
  ];
  for (final signal in signals) {
    try {
      signal.watch().listen((_) => unawaited(quitDesktopApp()));
    } on SignalException {
      // Not every signal can be watched on every system.
    }
  }
}

_DesktopQuitWatcher? _quitWatcher;

class _DesktopQuitWatcher with WindowListener {
  AppLifecycleListener? lifecycle;

  @override
  void onWindowClose() => unawaited(quitDesktopApp());
}

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
