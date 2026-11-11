import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../core.dart';
import '../data/models.dart';
import '../data/movies.dart';
import '../services.dart';
import 'catalog_pages.dart';
import 'components.dart';
import 'device_status.dart';
import 'home_page.dart';
import 'more_page.dart';
import 'seal.dart';
import 'show_page.dart';
import 'community_page.dart';
import 'community_extras_page.dart';
import '../data/community_extras.dart';
import 'movie_page.dart';
import 'movies_pages.dart';
import 'remote_now_playing.dart';
import 'tv_remote_page.dart';
import '../data/tv_remote.dart';
import '../desktop/desktop_core.dart';
import '../desktop/top_bar.dart';
import '../desktop/window_frame.dart';
import '../desktop/external_mpv.dart';
import '../desktop/launcher.dart';
import '../desktop/system_install.dart';
import '../host.dart';

/// What the active profile shows; the shell sets it as it builds.
ContentMode shellContent = ContentMode.anime;

/// The top-level pages, the same for every content setting: films and series
/// share Home and Search with anime, and Schedule lives in More.
List<String> get rootTabs => const ['Home', 'Search', 'More'];

const _tabIcons = {
  'Home': LucideIcons.house,
  'Search': LucideIcons.search,
  'More': LucideIcons.ellipsis,
};

/// Most tabs any content setting gives.
const _maxTabs = 3;

class AppShell extends StatefulWidget {
  const AppShell({super.key, required this.services, required this.isTV});
  final AppServices services;
  final bool isTV;
  @override
  State<AppShell> createState() => _AppShellState();
}

class _AppShellState extends State<AppShell> {
  int selected = 0;
  int? focusedNav;

  /// The tab under the mouse on a computer; it brightens but stays closed.
  int? hoveredNav;
  bool rootIsCurrent = true;
  StreamSubscription<String>? remoteEvents;
  final navFocus = List.generate(
    _maxTabs,
    (i) => FocusNode(debugLabel: 'Tab $i navigation'),
  );
  final scopes = List.generate(
    _maxTabs,
    (i) => FocusScopeNode(debugLabel: 'Root $i'),
  );
  List<String> shownTabs = const [];
  final searchField = FocusNode(debugLabel: 'Search field');
  void modeChanged(FocusHighlightMode _) {
    if (mounted) setState(() {});
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final current = ModalRoute.isCurrentOf(context) ?? true;
    if (widget.isTV && current && !rootIsCurrent) {
      // The route can request focus before its children have been re-enabled.
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && !navFocus.any((node) => node.hasFocus)) {
          focusContent(selected);
        }
      });
    }
    rootIsCurrent = current;
  }

  @override
  void initState() {
    super.initState();
    FocusManager.instance.addHighlightModeListener(modeChanged);
    widget.services.selectRoot = choose;
    desktopCloseHook = closeWindow;
    widget.services.auth.remoteLink = remoteLink;
    remoteEvents = widget.services.tvRemote.events.listen((text) {
      if (mounted) communityMessage(context, text);
    });
    unawaited(startRemote());
    widget.services.tvRemote.addListener(remoteAsk);
    // Trakt sits with the other trackers in Accounts and on More.
    unawaited(widget.services.trakt.load());
    if ((Host.android || isDesktop) &&
        widget.services.store.flag('CheckUpdates')) {
      unawaited(widget.services.updates.check());
    }
    HardwareKeyboard.instance.addHandler(konamiKey);
    widget.services.party.open = partyOpen;
    WidgetsBinding.instance.addPostFrameCallback((_) async {
      await widget.services.discord.initialize();
      unawaited(widget.services.extras.refreshClub());
      if (mounted) await birthdayCapsule(context, widget.services, widget.isTV);
      if (mounted && isDesktop && !desktopLauncher.value) {
        await offerSystemInstall(context, widget.services.store.preferences);
      }
    });
  }

  final konami = KonamiTracker();

  /// ↑↑↓↓←→←→BA anywhere outside the player opens the cheat code hunt.
  bool konamiKey(KeyEvent event) {
    if (event is! KeyDownEvent || !mounted) return false;
    final name = switch (event.logicalKey) {
      LogicalKeyboardKey.arrowUp => 'up',
      LogicalKeyboardKey.arrowDown => 'down',
      LogicalKeyboardKey.arrowLeft => 'left',
      LogicalKeyboardKey.arrowRight => 'right',
      LogicalKeyboardKey.keyB => 'b',
      LogicalKeyboardKey.keyA => 'a',
      LogicalKeyboardKey.select || LogicalKeyboardKey.enter => 'select',
      _ => 'other',
    };
    if (konami.press(name) && widget.services.playerStatus == null) {
      unawaited(openCheat(context, widget.services, widget.isTV));
    }
    return false;
  }

  /// A watch party member opens the host's show and episode.
  Future<void> partyOpen(int anilistId, int episode) async {
    if (!mounted) return;
    var anime = Anime(id: anilistId, title: 'Watch party');
    try {
      anime = await widget.services.catalog.detail(
        anime,
        adult: widget.services.store.adultAllowed,
      );
    } catch (_) {
      // The player looks the show up by its AniList id anyway.
    }
    if (!mounted) return;
    unawaited(
      Navigator.push(
        context,
        OtakaseRoute<void>(
          builder: (_) => playerFor(
            services: widget.services,
            anime: anime,
            episode: episode,
            isTV: widget.isTV,
            mode: widget.services.store.text('SubOrDub'),
          ),
        ),
      ),
    );
  }

  @override
  void dispose() {
    HardwareKeyboard.instance.removeHandler(konamiKey);
    if (widget.services.party.open == partyOpen) {
      widget.services.party.open = null;
    }
    FocusManager.instance.removeHighlightModeListener(modeChanged);
    widget.services.auth.remoteLink = null;
    unawaited(remoteEvents?.cancel() ?? Future<void>.value());
    widget.services.selectRoot = null;
    if (desktopCloseHook == closeWindow) desktopCloseHook = null;
    for (final n in navFocus) {
      n.dispose();
    }
    for (final s in scopes) {
      s.dispose();
    }
    searchField.dispose();
    if (!widget.services.tvRemote.closed) {
      widget.services.tvRemote.removeListener(remoteAsk);
    }
    super.dispose();
  }

  String? askShown;

  /// A phone asked to control this TV or computer: ask whoever is
  /// watching, over any page or the player.
  void remoteAsk() {
    final ask = widget.services.tvRemote.ask;
    if (!mounted || ask == null || ask.id == askShown) return;
    askShown = ask.id;
    unawaited(
      showDialog<void>(
        context: context,
        useRootNavigator: true,
        builder: (_) =>
            RemoteAskDialog(remote: widget.services.tvRemote, ask: ask),
      ),
    );
  }

  Future<void> startRemote() async {
    await widget.services.tvRemote.initialize();
    if (widget.services.closed) return;
    // Desktops only listen once a phone is paired, so a first launch never
    // opens a firewall prompt; the Phone remote page starts it on demand.
    if (widget.isTV &&
        (Host.android || isDesktop && widget.services.tvRemote.hasPhones)) {
      await widget.services.tvRemote.startTV();
    }
    try {
      final initial = await const MethodChannel('otakase/device')
          .invokeMethod<String>('initialRemote');
      if (initial != null && mounted) await remoteLink(Uri.parse(initial));
    } on MissingPluginException {
      /* No Android host is attached. */
    } on PlatformException {
      /* Pairing is also available through More. */
    }
  }

  Future<void> remoteLink(Uri uri) async {
    if (!mounted || widget.isTV) return;
    try {
      await const MethodChannel('otakase/device')
          .invokeMethod<String>('initialRemote');
      final code = RemotePairCode.fromUri(uri);
      if (mounted) {
        await Navigator.push(
          context,
          OtakaseRoute<void>(
            builder: (_) => TVRemotePage(
              services: widget.services,
              isTV: false,
              pairCode: code,
            ),
          ),
        );
      }
    } catch (e) {
      if (mounted) communityMessage(context, e.toString());
    }
  }

  void choose(int i, {bool focusNavigation = false}) {
    setState(() => selected = i);
    widget.services.root = i;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted ||
          selected != i ||
          ModalRoute.isCurrentOf(context) == false) {
        return;
      }
      if (widget.isTV && focusNavigation) {
        navFocus[i].requestFocus();
      } else {
        focusContent(i);
      }
    });
  }

  void focusContent(int index) {
    if (ModalRoute.isCurrentOf(context) == false || selected != index) return;
    // On desktop, Search starts in the field so typing searches at once.
    if (isDesktop && index == 1) {
      searchField.requestFocus();
      return;
    }
    final scope = scopes[index];
    final remembered = scope.focusedChild;
    scope.requestFocus();
    if (remembered == null) scope.nextFocus();
  }

  KeyEventResult contentKey(FocusNode node, KeyEvent event) {
    if (!widget.isTV ||
        ModalRoute.isCurrentOf(context) == false ||
        (event is! KeyDownEvent && event is! KeyRepeatEvent) ||
        event.logicalKey != LogicalKeyboardKey.arrowUp) {
      return KeyEventResult.ignored;
    }
    final current = FocusManager.instance.primaryFocus;
    if (current == null || !scopes[selected].hasFocus) {
      return KeyEventResult.ignored;
    }
    if (!current.focusInDirection(TraversalDirection.up)) {
      navFocus[selected].requestFocus();
    }
    return KeyEventResult.handled;
  }

  Future<void> open(Anime a, {Object? heroTag}) {
    // Films and series on Home and Search come as cards; they open their
    // own page.
    if (titleCardsOf(widget.services).title(a) case final title?) {
      return openTitle(context, widget.services, title, widget.isTV);
    }
    return Navigator.push(
      context,
      OtakaseRoute<void>(
        builder: (_) => isOtakaseMovie(a)
            ? OtakaseMovie(services: widget.services, isTV: widget.isTV)
            : ShowPage(
                services: widget.services,
                anime: a,
                isTV: widget.isTV,
                heroTag: heroTag,
              ),
      ),
    );
  }

  void resume(WatchEntry h) {
    if (titleCardsOf(widget.services).title(h.anime) case final title?) {
      unawaited(resumeTitle(context, widget.services, title, widget.isTV));
      return;
    }
    unawaited(
      Navigator.push(
        context,
        OtakaseRoute<void>(
          builder: (_) => playerFor(
            services: widget.services,
            anime: h.anime,
            episode: h.episode,
            isTV: widget.isTV,
            mode: widget.services.store.text('SubOrDub'),
          ),
        ),
      ),
    );
  }

  Future<String?> voice() async {
    try {
      return await CoreClient.channel.invokeMethod<String>('voiceSearch');
    } on PlatformException {
      return null;
    } on MissingPluginException {
      return null;
    }
  }

  Future<void> back() async {
    if (MediaQuery.viewInsetsOf(context).bottom > 0) {
      FocusManager.instance.primaryFocus?.unfocus();
      return;
    }
    if (widget.isTV && scopes[selected].hasFocus) {
      navFocus[selected].requestFocus();
      return;
    }
    if (selected != 0) {
      choose(0, focusNavigation: widget.isTV);
      return;
    }
    // Esc on Home leaves the window open; the close button closes it.
    if (isDesktop) return;
    final exit = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('Exit Otakase?'),
        actions: [
          TextButton(
            autofocus: !widget.isTV,
            onPressed: () => Navigator.pop(c, false),
            child: const Text('Stay'),
          ),
          TextButton(
            autofocus: widget.isTV,
            onPressed: () => Navigator.pop(c, true),
            child: const Text('Exit'),
          ),
        ],
      ),
    );
    if (exit == true && mounted) {
      final milestone = otakaseMovieEnabled
          ? await widget.services.heritage.milestoneOnExit()
          : 0;
      if (!mounted) return;
      if (milestone > 0) {
        await Navigator.push(
          context,
          OtakaseRoute<void>(
            builder: (_) => OtakaseMovie(
              services: widget.services,
              isTV: widget.isTV,
              milestone: milestone,
            ),
          ),
        );
      }
      if (!mounted) return;
      final greeting = await widget.services.discord.exitGreeting();
      if (!mounted) return;
      if (!widget.isTV) {
        await showDialog<void>(
          context: context,
          builder: (c) => AlertDialog(
            content: Text(greeting),
            actions: [
              TextButton(
                autofocus: true,
                onPressed: () => Navigator.pop(c),
                child: const Text('Close'),
              ),
            ],
          ),
        );
      }
      await widget.services.discord.stopWatching();
      await SystemNavigator.pop();
    }
  }

  /// The desktop close button: no "Exit Otakase?" question, the click is
  /// the answer.
  /// Saves playback, signs out of Discord presence and closes the window.
  Future<void> closeWindow() async {
    await hideDesktopWindow();
    await widget.services.close().timeout(
      const Duration(seconds: 5),
      onTimeout: () {},
    );
    await closeDesktopWindow();
  }

  Widget nav(int index) {
    final label = rootTabs[index];
    final active = index == selected;
    return Focus(
      onKeyEvent: (_, event) {
        if (widget.isTV &&
            (event is KeyDownEvent || event is KeyRepeatEvent) &&
            (event.logicalKey == LogicalKeyboardKey.arrowLeft ||
                event.logicalKey == LogicalKeyboardKey.arrowRight)) {
          final step = event.logicalKey == LogicalKeyboardKey.arrowLeft
              ? -1
              : 1;
          navFocus[(index + step).clamp(0, rootTabs.length - 1)].requestFocus();
          return KeyEventResult.handled;
        }
        if (widget.isTV &&
            (event is KeyDownEvent || event is KeyRepeatEvent) &&
            event.logicalKey == LogicalKeyboardKey.arrowDown) {
          if (index != selected) {
            choose(index);
          } else {
            focusContent(index);
          }
          return KeyEventResult.handled;
        }
        return KeyEventResult.ignored;
      },
      child: FocusBox(
        key: ValueKey('nav-$label'),
        focusNode: navFocus[index],
        isTV: widget.isTV,
        radius: 16,
        frameGap: widget.isTV ? 5 : 0,
        showFocusBorder: !widget.isTV,
        focusFill: widget.isTV ? Colors.transparent : bg,
        hoverColor: widget.isTV ? Colors.transparent : null,
        // A TV remote moves between tabs by focus, so focus opens the tab.
        // A computer has a mouse: hovering only lights the tab up and a
        // click opens it.
        onHover: widget.isTV && !isDesktop
            ? (hovered) {
                if (hovered) navFocus[index].requestFocus();
              }
            : (hovered) => setState(
                () => hoveredNav = hovered
                    ? index
                    : hoveredNav == index
                    ? null
                    : hoveredNav,
              ),
        onFocusChanged: (value) => setState(() {
          focusedNav = value
              ? index
              : focusedNav == index
              ? null
              : focusedNav;
          if (widget.isTV && !isDesktop && value) {
            selected = index;
            widget.services.root = index;
          }
        }),
        onPressed: () => choose(index),
        child: Container(
          constraints: BoxConstraints(minHeight: widget.isTV ? 48 : 50),
          padding: EdgeInsets.symmetric(horizontal: widget.isTV ? 6 : 4),
          child: widget.isTV
              ? Builder(
                  builder: (context) {
                    final remote =
                        FocusManager.instance.highlightMode ==
                        FocusHighlightMode.traditional;
                    final focused = remote && focusedNav == index;
                    // While the header has focus, the focused tab grows and
                    // glows and the others fade, without a frame or pill.
                    final faded = remote && focusedNav != null && !focused;
                    final motion = MediaQuery.disableAnimationsOf(context)
                        ? Duration.zero
                        : const Duration(milliseconds: 180);
                    return Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        AnimatedOpacity(
                          duration: motion,
                          opacity: faded ? .4 : 1,
                          child: AnimatedScale(
                            duration: motion,
                            curve: Curves.easeOut,
                            scale: focused ? 1.15 : 1,
                            child: Text(
                              label,
                              style:
                                  uiStyle(
                                    context,
                                    weight: focused
                                        ? FontWeight.w700
                                        : active
                                        ? FontWeight.w600
                                        : FontWeight.w500,
                                    color: focused || hoveredNav == index
                                        ? fgBright
                                        : active
                                        ? fg
                                        : muted,
                                  ).copyWith(
                                    shadows: focused
                                        ? [
                                            Shadow(
                                              color: shu.withValues(alpha: .8),
                                              blurRadius: 10,
                                            ),
                                            Shadow(
                                              color: shu.withValues(alpha: .5),
                                              blurRadius: 28,
                                            ),
                                          ]
                                        : null,
                                  ),
                            ),
                          ),
                        ),
                        const SizedBox(height: 7),
                        Container(
                          width: 36,
                          height: 3,
                          color: active ? shu : Colors.transparent,
                        ),
                      ],
                    );
                  },
                )
              : Stack(
                  alignment: Alignment.topCenter,
                  children: [
                    if (active) Container(width: 33, height: 3, color: shu),
                    Padding(
                      padding: const EdgeInsets.only(top: 7),
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          Icon(
                            _tabIcons[label] ?? LucideIcons.circle,
                            size: 22,
                            color: active ? accentText : muted,
                          ),
                          Text(
                            label,
                            style: uiStyle(
                              context,
                              size: 15,
                              color:
                                  focusedNav == index &&
                                      FocusManager.instance.highlightMode ==
                                          FocusHighlightMode.traditional
                                  ? fgBright
                                  : active
                                  ? fg
                                  : muted,
                              weight: active
                                  ? FontWeight.w600
                                  : FontWeight.w500,
                            ),
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
        ),
      ),
    );
  }

  Widget get navTabs => Row(
    mainAxisSize: MainAxisSize.min,
    children: [
      for (var i = 0; i < rootTabs.length; i++) ...[
        nav(i),
        if (i < rootTabs.length - 1) const SizedBox(width: 14),
      ],
    ],
  );

  /// Desktop and TV share one top bar: tabs on an island in the middle, the
  /// logo on the left corner island. A computer has its window buttons on
  /// the right one (macOS swaps them with the logo); a TV its clock.
  Widget header() => isDesktop
      ? DesktopTopBar(brand: const Brand(), tabs: navTabs)
      : widget.isTV
      ? TVTopBar(tabs: navTabs)
      : Padding(
          padding: EdgeInsets.fromLTRB(
            20,
            math.max(MediaQuery.viewPaddingOf(context).top, 25),
            20,
            12,
          ),
          child: ConstrainedBox(
            constraints: const BoxConstraints(minHeight: 38),
            child: SizedBox(
              width: double.infinity,
              child: Wrap(
                alignment: WrapAlignment.spaceBetween,
                crossAxisAlignment: WrapCrossAlignment.center,
                runSpacing: 8,
                children: const [Brand(), DeviceStatus()],
              ),
            ),
          ),
        );

  /// While this phone controls a TV that plays something, a bar above the
  /// tabs shows it and opens the full remote.
  Widget remoteBar() => ValueListenableBuilder(
    valueListenable: widget.services.remoteLink,
    builder: (context, link, _) => link == null
        ? const SizedBox.shrink()
        : ListenableBuilder(
            listenable: link,
            builder: (context, _) => link.status['has_player'] != true
                ? const SizedBox.shrink()
                : Positioned(
                    left: 12,
                    right: 12,
                    bottom:
                        math.max(
                          MediaQuery.viewPaddingOf(context).bottom,
                          MediaQuery.paddingOf(context).bottom,
                        ) +
                        7 +
                        72 +
                        10,
                    child: RemoteMiniBar(services: widget.services, link: link),
                  ),
          ),
  );

  Widget phoneNav() => Positioned(
    left: 20,
    right: 20,
    bottom:
        math.max(
          MediaQuery.viewPaddingOf(context).bottom,
          MediaQuery.paddingOf(context).bottom,
        ) +
        7,
    child: Center(
      child: Container(
        key: const Key('phone-bottom-bar'),
        width: math.min(292, MediaQuery.sizeOf(context).width - 40),
        constraints: const BoxConstraints(minHeight: 72),
        padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 7),
        decoration: BoxDecoration(
          color: panel.withValues(alpha: .98),
          borderRadius: BorderRadius.circular(24),
          border: Border.all(color: line),
          boxShadow: const [
            BoxShadow(
              color: Color(0x87000000),
              offset: Offset(0, 12),
              blurRadius: 35,
            ),
          ],
        ),
        child: Row(
          children: [
            for (var i = 0; i < rootTabs.length; i++) Expanded(child: nav(i)),
          ],
        ),
      ),
    ),
  );
  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: Listenable.merge([
      widget.services.store,
      widget.services.auth,
      widget.services.tracking,
      widget.services.discord,
      widget.services.trakt,
      widget.services.remoteLink,
    ]),
    builder: (context, _) {
      final keyboard = MediaQuery.viewInsetsOf(context).bottom > 0;
      shellContent = contentModeOf(widget.services.store);
      final tabs = rootTabs;
      if (!listEquals(tabs, shownTabs)) {
        // The content setting changed: stay on the same tab where it still
        // exists, so changing it from More stays in More.
        final was = shownTabs.isEmpty ? 'Home' : shownTabs[selected];
        selected = math.max(0, tabs.indexOf(was));
        widget.services.root = selected;
        shownTabs = tabs;
      }
      final moviesOnly = shellContent == ContentMode.movies;
      final pages = <Widget>[
        if (moviesOnly)
          MoviesHome(
            services: widget.services,
            isTV: widget.isTV,
            searchButton: false,
          )
        else
          HomePage(
            services: widget.services,
            isTV: widget.isTV,
            active: selected == 0 && rootIsCurrent,
            topInset: widget.isTV ? tvHeaderExtent(context) : 0,
            onOpen: open,
            onResume: resume,
            onSearch: () => choose(1),
            onAccounts: () =>
                openAccounts(context, widget.services, widget.isTV),
          ),
        if (moviesOnly)
          MovieSearchPage(
            services: widget.services,
            isTV: widget.isTV,
            fieldFocus: isDesktop ? searchField : null,
          )
        else
          SearchPage(
            services: widget.services,
            isTV: widget.isTV,
            onOpen: open,
            onVoice: voice,
            fieldFocus: searchField,
            onNavigateUp: () => navFocus[1].requestFocus(),
            titles: shellContent == ContentMode.both,
          ),
        MorePage(services: widget.services, isTV: widget.isTV, onOpen: open),
      ];
      return ExcludeFocus(
        excluding: widget.isTV && !rootIsCurrent,
        child: PopScope(
          canPop: false,
          onPopInvokedWithResult: (popped, result) {
            if (!popped) unawaited(back());
          },
          child: Scaffold(
            body: Stack(
              children: [
                Column(
                  children: [
                    if (!widget.isTV) header(),
                    Expanded(
                      child: PageInsets(
                        bottom: widget.isTV
                            ? math.max(27, MediaQuery.paddingOf(context).bottom)
                            : keyboard
                            ? 16
                            : (72 +
                                      (MediaQuery.textScalerOf(context)
                                                      .scale(1) -
                                                  1)
                                              .clamp(0, 2) *
                                          64) +
                                  7 +
                                  // Room for the bar of a TV this phone
                                  // controls.
                                  (widget.services.remoteLink.value == null
                                      ? 0
                                      : 76) +
                                  math.max(
                                    MediaQuery.viewPaddingOf(context).bottom,
                                    MediaQuery.paddingOf(context).bottom,
                                  ),
                        child: PosterNavigation(
                          onOpen: (a, tag) => open(a, heroTag: tag),
                          child: Focus(
                            canRequestFocus: false,
                            onKeyEvent: contentKey,
                            child: IndexedStack(
                              index: selected,
                              children: [
                                for (var i = 0; i < pages.length; i++)
                                  ExcludeFocus(
                                    excluding: i != selected,
                                    child: FocusScope(
                                      node: scopes[i],
                                      child: FocusTraversalGroup(
                                        policy: widget.isTV
                                            ? TVViewport.traversal
                                            : null,
                                        child: !widget.isTV
                                            ? pages[i]
                                            : i == 0
                                            ? ScrollClearance(
                                                top: tvHeaderExtent(context),
                                                child: pages[i],
                                              )
                                            : Padding(
                                                padding: EdgeInsets.only(
                                                  top: tvHeaderExtent(context),
                                                ),
                                                child: pages[i],
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
                  ],
                ),
                if (widget.isTV)
                  Positioned(
                    top: 0,
                    left: 0,
                    right: 0,
                    child: SizedBox(
                      height: tvHeaderExtent(context),
                      child: DecoratedBox(
                        key: const Key('tv-header-scrim'),
                        // The islands keep the bar readable over art and
                        // scrolled pages, on desktop and TV alike.
                        decoration: const BoxDecoration(),
                        child: WindowDragArea(child: header()),
                      ),
                    ),
                  ),
                if (!widget.isTV && !keyboard) remoteBar(),
                if (!widget.isTV && !keyboard) phoneNav(),
              ],
            ),
          ),
        ),
      );
    },
  );
}

class Brand extends StatelessWidget {
  const Brand({super.key});
  @override
  Widget build(BuildContext context) => Row(
    mainAxisSize: MainAxisSize.min,
    children: [
      OtakaseSeal(size: AppLayout.tvOf(context) ? 34 : 29),
      const SizedBox(width: 7),
      Text(
        'OTAKASE',
        style: display.copyWith(
          fontSize: AppLayout.tvOf(context) ? 22 : 19,
          letterSpacing: -.57,
        ),
      ),
    ],
  );
}

/// The TV top bar: the desktop island bar with the clock in place of the
/// window buttons, kept inside the screen's safe area.
class TVTopBar extends StatelessWidget {
  const TVTopBar({super.key, required this.tabs, this.leading});
  final Widget tabs;

  /// Shown before the logo, such as the back button on a show.
  final Widget? leading;
  @override
  Widget build(BuildContext context) {
    final safe = MediaQuery.paddingOf(context);
    return DesktopTopBar(
      brand: const Brand(),
      tabs: tabs,
      leading: leading,
      trailing: const DeviceStatus(isTV: true),
      inset: EdgeInsets.fromLTRB(
        math.max(8, safe.left - 40),
        math.max(18, safe.top - 9),
        math.max(24, safe.right - 24),
        0,
      ),
    );
  }
}
