import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:media_kit/media_kit.dart';
import 'package:path_provider/path_provider.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'core.dart';
import 'data/account.dart';
import 'data/account_center.dart';
import 'data/auth.dart';
import 'data/discord.dart' show appVersion;
import 'data/tv_remote.dart' show localDeviceName;
import 'data/profiles.dart';
import 'desktop/desktop_core.dart';
import 'desktop/launcher.dart';
import 'desktop/launcher_playback.dart';
import 'desktop/launcher_toggle.dart';
import 'desktop/tiling.dart';
import 'desktop/window_frame.dart';
import 'host.dart';
import 'services.dart';
import 'ui/rate_prompt.dart';
import 'ui/app_keys.dart';
import 'ui/app_shell.dart';
import 'ui/components.dart';
import 'ui/seal.dart';
import 'ui/theme.dart';
import 'ui/profiles_page.dart';

Future<void> main([List<String> args = const []]) async {
  final binding = WidgetsFlutterBinding.ensureInitialized();
  askContentOnFirstRun = true;
  final reduceMotion =
      binding.platformDispatcher.accessibilityFeatures.disableAnimations;
  final wayland = launcherFadesContent;
  MediaKit.ensureInitialized();
  LauncherToggle? toggle;
  LauncherPlayback? playback;
  String? workspace;
  if (isDesktop) {
    final playerArgument = args.indexOf(LauncherPlayback.argument);
    if (playerArgument >= 0) {
      if (playerArgument + 1 >= args.length) exit(1);
      try {
        playback = await LauncherPlayback.read(args[playerArgument + 1]);
      } on Exception {
        exit(1);
      } on StateError {
        exit(1);
      }
    }
    readDesktopArguments(args);
    if (desktopLauncher.value) {
      try {
        toggle = await LauncherToggle.open(
          await getApplicationSupportDirectory(),
        );
        if (toggle == null) exit(0);
      } on FileSystemException {
        // An unavailable settings directory should not prevent opening.
      }
      workspace = await focusedTilerWorkspace();
      await prepareLauncherWindow(fadeContent: wayland);
    } else {
      await setUpDesktopWindow(
        size: playback == null || playback.opensFullApp
            ? fullAppSize
            : launcherPlayerSize,
      );
      await showDesktopWindow();
    }
    await watchDesktopQuit();
  }
  final core = CoreClient();
  final androidTV = !isDesktop && await core.isTV();
  Host.tv = androidTV;
  final preferences = await SharedPreferences.getInstance();
  final tokens = platformTokenStore();
  final profiles = ProfileManager(
    preferences: preferences,
    tokens: tokens,
    core: core,
    // Desktop starts from the wide TV layout (shelves, top navigation) and
    // adds mouse and window controls on top.
    isTV: isDesktop || androidTV,
  );
  // Otakase accounts start first, so a sign-in link that opened the app
  // finds them ready.
  try {
    final deviceName = await localDeviceName().catchError((Object _) => null);
    profiles.accounts = AccountCenter(
      profiles: profiles,
      preferences: preferences,
      tokens: tokens,
      device: DeviceInfo(
        name: deviceName == null || deviceName.trim().isEmpty
            ? (androidTV ? 'Android TV' : 'Otakase')
            : deviceName.trim(),
        os: AccountManager.osName(),
        isTV: androidTV,
        version: appVersion,
      ),
    );
    await profiles.accounts!.initialize();
  } catch (_) {
    /* Accounts are optional: the app works signed out. */
  }
  try {
    await profiles.initialize(playbackProfile: playback?.profileId);
  } catch (e) {
    profiles.error = e.toString();
  }
  if (!isDesktop) {
    await SystemChrome.setEnabledSystemUIMode(
      SystemUiMode.manual,
      overlays: [SystemUiOverlay.bottom],
    );
  }
  final app = ProfileHost(
    manager: profiles,
    playback: playback?.opensFullApp == true ? null : playback,
  );
  final ready = isDesktop && desktopLauncher.value && wayland
      ? Completer<void>()
      : null;
  runApp(
    ready == null
        ? app
        : LauncherStartup(
            ready: ready.future,
            reduceMotion: reduceMotion,
            child: app,
          ),
  );
  if (isDesktop && desktopLauncher.value) {
    await binding.waitUntilFirstFrameRasterized;
    await revealLauncherWindow(
      reduceMotion: reduceMotion,
      fadeContent: ready != null,
      workspace: workspace,
    );
    ready?.complete();
    if (toggle != null) {
      final owner = toggle;
      desktopLauncher.addListener(() {
        if (!desktopLauncher.value) unawaited(owner.close());
      });
      owner.watch(() async {
        if (!desktopLauncher.value) return;
        if (desktopCloseHook != null) {
          await quitDesktopApp();
        } else {
          await dismissLauncherWindow(
            reduceMotion: reduceMotion,
            fadeContent: wayland,
          );
          await profiles.close().timeout(
            const Duration(seconds: 5),
            onTimeout: () {},
          );
          await closeDesktopWindow();
        }
      });
    }
  }
}

class ProfileHost extends StatefulWidget {
  const ProfileHost({super.key, required this.manager, this.playback});
  final ProfileManager manager;
  final LauncherPlayback? playback;
  @override
  State<ProfileHost> createState() => _ProfileHostState();
}

class _ProfileHostState extends State<ProfileHost> {
  @override
  void dispose() {
    unawaited(widget.manager.close());
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: Listenable.merge([widget.manager, desktopLauncher]),
    builder: (context, _) {
      final services = widget.manager.session;
      if (services != null) {
        return Directionality(
          textDirection: TextDirection.ltr,
          child: Stack(
            children: [
              OtakaseApp(
                key: ValueKey(
                  'session-${widget.manager.activeId}-${widget.manager.generation}-${desktopLauncher.value}',
                ),
                services: services,
                isTV: widget.manager.isTV,
                playback: widget.playback,
              ),
              if (widget.manager.busy)
                const Positioned.fill(
                  child: ColoredBox(
                    color: bg,
                    child: Center(child: CircularProgressIndicator(color: shu)),
                  ),
                ),
            ],
          ),
        );
      }
      return AppLayout(
        isTV: widget.manager.isTV,
        child: MaterialApp(
          shortcuts: appShortcuts,
          debugShowCheckedModeBanner: false,
          builder: (context, child) => desktopLauncher.value
              ? child!
              : DesktopFrame(
                  child: widget.manager.isTV
                      ? TVViewport(child: child!)
                      : CutoutInset(child: child!),
                ),
          theme: buildOtakaseTheme(isTV: widget.manager.isTV),
          home: widget.manager.profiles.isEmpty || !widget.manager.initialized
              ? Scaffold(
                  body: SafeArea(
                    child: Center(
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          Text(widget.manager.error ?? 'Loading profiles'),
                          TextButton(
                            onPressed: () async {
                              try {
                                await widget.manager.initialize(
                                  playbackProfile: widget.playback?.profileId,
                                );
                              } catch (e) {
                                widget.manager.error = e.toString();
                                if (mounted) setState(() {});
                              }
                            },
                            child: const Text('Retry'),
                          ),
                        ],
                      ),
                    ),
                  ),
                )
              : ProfilesChooser(manager: widget.manager),
        ),
      );
    },
  );
}

class OtakaseApp extends StatefulWidget {
  const OtakaseApp({
    super.key,
    required this.services,
    required this.isTV,
    this.splash = false,
    this.playback,
  });
  final AppServices services;
  final LauncherPlayback? playback;
  final bool isTV, splash;
  @override
  State<OtakaseApp> createState() => _OtakaseAppState();
}

class _OtakaseAppState extends State<OtakaseApp> {
  late final ratingPrompt = RatingPrompt(widget.services, isTV: widget.isTV);
  bool _openingFullApp = false;

  Future<void> openFullApp() async {
    if (_openingFullApp) return;
    _openingFullApp = true;
    await resizeDesktopWindow(launcher: false);
    desktopLauncher.value = false;
  }

  void modeChanged(FocusHighlightMode _) {
    if (mounted) setState(() {});
  }

  void desktopInputChanged() {
    if (mounted && isDesktop) setState(() {});
  }

  @override
  void initState() {
    super.initState();
    FocusManager.instance.addHighlightModeListener(modeChanged);
    DesktopInput.keyboardMode.addListener(desktopInputChanged);
  }

  @override
  void dispose() {
    FocusManager.instance.removeHighlightModeListener(modeChanged);
    DesktopInput.keyboardMode.removeListener(desktopInputChanged);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final traditional = isDesktop
        ? DesktopInput.keyboard
        : FocusManager.instance.highlightMode == FocusHighlightMode.traditional;
    return AppLayout(
      isTV: widget.isTV,
      child: MaterialApp(
        title: 'Otakase',
        shortcuts: appShortcuts,
        navigatorObservers: [ratingPrompt],
        debugShowCheckedModeBanner: false,
        builder: (context, child) => desktopLauncher.value
            ? child!
            : DesktopFrame(
                child: widget.isTV
                    ? TVViewport(child: child!)
                    : CutoutInset(child: child!),
              ),
        theme: buildOtakaseTheme(isTV: widget.isTV, traditional: traditional),
        home: widget.playback != null
            ? LauncherPlayerWindow(
                request: widget.playback!,
                services: widget.services,
              )
            : desktopLauncher.value
            ? LauncherPage(services: widget.services, onOpenApp: openFullApp)
            : widget.splash
            ? SplashGate(
                child: AppShell(services: widget.services, isTV: widget.isTV),
              )
            : AppShell(services: widget.services, isTV: widget.isTV),
      ),
    );
  }
}

class SplashGate extends StatefulWidget {
  const SplashGate({super.key, required this.child});
  final Widget child;
  @override
  State<SplashGate> createState() => _SplashGateState();
}

class _SplashGateState extends State<SplashGate> {
  Timer? timer;
  bool done = false, visible = false;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) setState(() => visible = true);
    });
    timer = Timer(Duration.zero, () {
      if (mounted) setState(() => done = true);
    });
  }

  @override
  void dispose() {
    timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AnimatedSwitcher(
    duration: const Duration(milliseconds: 300),
    child: done
        ? widget.child
        : Scaffold(
            key: const ValueKey('splash'),
            body: Center(
              child: AnimatedOpacity(
                opacity: visible ? 1 : 0,
                duration: const Duration(milliseconds: 300),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    OtakaseSeal(size: AppLayout.tvOf(context) ? 96 : 64),
                    const SizedBox(height: 32),
                    Text('お任せ', style: uiStyle(context, size: 20, color: fg)),
                    const SizedBox(height: 12),
                    Text(
                      'Leave it to us.',
                      style: uiStyle(context, color: muted),
                    ),
                  ],
                ),
              ),
            ),
          ),
  );
}
