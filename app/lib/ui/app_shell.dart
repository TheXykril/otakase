import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../core.dart';
import '../data/models.dart';
import '../player.dart';
import '../services.dart';
import 'catalog_pages.dart';
import 'components.dart';
import 'device_status.dart';
import 'home_page.dart';
import 'more_page.dart';
import 'seal.dart';
import 'show_page.dart';
import 'community_page.dart';
import 'movie_page.dart';

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
  final navFocus = List.generate(
    3,
    (i) => FocusNode(
      debugLabel: [
        'Home navigation',
        'Search navigation',
        'More navigation',
      ][i],
    ),
  );
  final scopes = List.generate(3, (i) => FocusScopeNode(debugLabel: 'Root $i'));
  @override
  void initState() {
    super.initState();
    widget.services.selectRoot = choose;
    WidgetsBinding.instance.addPostFrameCallback((_) async {
      await widget.services.discord.initialize();
      if (mounted) await birthdayCapsule(context, widget.services, widget.isTV);
    });
  }

  @override
  void dispose() {
    widget.services.selectRoot = null;
    for (final n in navFocus) {
      n.dispose();
    }
    for (final s in scopes) {
      s.dispose();
    }
    super.dispose();
  }

  void choose(int i) {
    setState(() => selected = i);
    widget.services.root = i;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) focusContent(i);
    });
  }

  void focusContent(int index) {
    final scope = scopes[index];
    final remembered = scope.focusedChild;
    scope.requestFocus();
    if (remembered == null) scope.nextFocus();
  }

  Future<void> open(Anime a, {Object? heroTag}) => Navigator.push(
    context,
    OtakaseRoute<void>(
      builder: (_) => ShowPage(
        services: widget.services,
        anime: a,
        isTV: widget.isTV,
        heroTag: heroTag,
      ),
    ),
  );
  void resume(WatchEntry h) => Navigator.push(
    context,
    OtakaseRoute<void>(
      builder: (_) => PlayerPage(
        services: widget.services,
        anime: h.anime,
        episode: h.episode,
        isTV: widget.isTV,
        mode: widget.services.store.text('SubOrDub'),
      ),
    ),
  );
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
    if (selected != 0) {
      choose(0);
      return;
    }
    final exit = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('Exit Otakase?'),
        actions: [
          TextButton(
            autofocus: true,
            onPressed: () => Navigator.pop(c, false),
            child: const Text('Stay'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(c, true),
            child: const Text('Exit'),
          ),
        ],
      ),
    );
    if (exit == true && mounted) {
      final milestone = await widget.services.heritage.milestoneOnExit();
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
      await widget.services.discord.stopWatching();
      await SystemNavigator.pop();
    }
  }

  Widget nav(int index) {
    final label = ['Home', 'Search', 'More'][index];
    final active = index == selected;
    return Focus(
      onKeyEvent: (_, event) {
        if (widget.isTV &&
            event is KeyDownEvent &&
            event.logicalKey == LogicalKeyboardKey.arrowDown) {
          scopes[selected].requestFocus();
          return KeyEventResult.handled;
        }
        return KeyEventResult.ignored;
      },
      child: FocusBox(
        key: ValueKey('nav-$label'),
        focusNode: navFocus[index],
        isTV: widget.isTV,
        radius: widget.isTV ? 8 : 16,
        frameGap: widget.isTV ? 5 : 0,
        onFocusChanged: (value) => setState(
          () => focusedNav = value
              ? index
              : focusedNav == index
              ? null
              : focusedNav,
        ),
        onPressed: () => choose(index),
        child: Container(
          constraints: BoxConstraints(minHeight: widget.isTV ? 48 : 50),
          padding: EdgeInsets.symmetric(horizontal: widget.isTV ? 6 : 4),
          child: widget.isTV
              ? Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(
                      label,
                      style: uiStyle(
                        context,
                        weight: active ? FontWeight.w600 : FontWeight.w500,
                        color:
                            focusedNav == index &&
                                FocusManager.instance.highlightMode ==
                                    FocusHighlightMode.traditional
                            ? fgBright
                            : active
                            ? fg
                            : muted,
                      ),
                    ),
                    const SizedBox(height: 7),
                    Container(
                      width: 36,
                      height: 3,
                      color: active ? shu : Colors.transparent,
                    ),
                  ],
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
                            [
                              LucideIcons.house,
                              LucideIcons.search,
                              LucideIcons.ellipsis,
                            ][index],
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

  Widget header() => widget.isTV
      ? Padding(
          padding: EdgeInsets.fromLTRB(
            math.max(48, MediaQuery.paddingOf(context).left),
            math.max(27, MediaQuery.paddingOf(context).top),
            48,
            12,
          ),
          child: Row(
            children: [
              const Brand(),
              const SizedBox(width: 28),
              for (var i = 0; i < 3; i++) ...[
                nav(i),
                if (i < 2) const SizedBox(width: 14),
              ],
              const Spacer(),
              const DeviceStatus(isTV: true),
            ],
          ),
        )
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
          children: [for (var i = 0; i < 3; i++) Expanded(child: nav(i))],
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
    ]),
    builder: (context, _) {
      final keyboard = MediaQuery.viewInsetsOf(context).bottom > 0;
      final pages = <Widget>[
        HomePage(
          services: widget.services,
          isTV: widget.isTV,
          onOpen: open,
          onResume: resume,
          onSearch: () => choose(1),
          onAccounts: () => openAccounts(context, widget.services, widget.isTV),
        ),
        SearchPage(
          services: widget.services,
          isTV: widget.isTV,
          onOpen: open,
          onVoice: voice,
        ),
        MorePage(services: widget.services, isTV: widget.isTV, onOpen: open),
      ];
      return PopScope(
        canPop: false,
        onPopInvokedWithResult: (popped, result) {
          if (!popped) unawaited(back());
        },
        child: Scaffold(
          body: Stack(
            children: [
              Column(
                children: [
                  header(),
                  Expanded(
                    child: PageInsets(
                      bottom: widget.isTV
                          ? math.max(27, MediaQuery.paddingOf(context).bottom)
                          : keyboard
                          ? 16
                          : (72 +
                                    (MediaQuery.textScalerOf(context).scale(1) -
                                                1)
                                            .clamp(0, 2) *
                                        64) +
                                7 +
                                math.max(
                                  MediaQuery.viewPaddingOf(context).bottom,
                                  MediaQuery.paddingOf(context).bottom,
                                ),
                      child: PosterNavigation(
                        onOpen: (a, tag) => open(a, heroTag: tag),
                        child: IndexedStack(
                          index: selected,
                          children: [
                            for (var i = 0; i < pages.length; i++)
                              ExcludeFocus(
                                excluding: i != selected,
                                child: FocusScope(
                                  node: scopes[i],
                                  child: FocusTraversalGroup(child: pages[i]),
                                ),
                              ),
                          ],
                        ),
                      ),
                    ),
                  ),
                ],
              ),
              if (!widget.isTV && !keyboard) phoneNav(),
            ],
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
