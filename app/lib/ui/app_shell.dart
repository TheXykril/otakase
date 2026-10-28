import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../core.dart';
import '../data/models.dart';
import '../data/store.dart';
import '../services.dart';
import 'catalog_pages.dart';
import 'components.dart';
import 'settings.dart';
import 'show_page.dart';

class AppShell extends StatefulWidget {
  const AppShell({super.key, required this.services, required this.isTV});
  final AppServices services;
  final bool isTV;
  @override
  State<AppShell> createState() => _AppShellState();
}

class _AppShellState extends State<AppShell> {
  final scaffold = GlobalKey<ScaffoldState>();
  final homeFocus = FocusNode(debugLabel: 'Home navigation');
  final bodyFocus = FocusNode(
        debugLabel: 'Catalog content',
        skipTraversal: true,
      ),
      topFocus = FocusNode(debugLabel: 'Top navigation', skipTraversal: true);
  int selected = 0;
  String voiceQuery = '';
  bool movies = false;
  Timer? clock;
  DateTime now = DateTime.now();
  AppStore get store => widget.services.store;
  @override
  void initState() {
    super.initState();
    clock = Timer.periodic(const Duration(minutes: 1), (_) {
      if (mounted) setState(() => now = DateTime.now());
    });
  }

  @override
  void dispose() {
    clock?.cancel();
    homeFocus.dispose();
    bodyFocus.dispose();
    topFocus.dispose();
    super.dispose();
  }

  void choose(int index) {
    setState(() {
      selected = index;
      if (index == 2) {
        movies = false;
        voiceQuery = '';
      }
    });
  }

  void open(Anime a, {Object? heroTag}) => Navigator.push(
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
  Future<void> settings() =>
      openSettings(context, widget.services, widget.isTV);
  Future<String?> voice() async {
    try {
      return await CoreClient.channel.invokeMethod<String>('voiceSearch');
    } on PlatformException {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('Voice search is unavailable. Use the search field.'),
          ),
        );
      }
      return null;
    } on MissingPluginException {
      return null;
    }
  }

  Future<void> voiceFromNav() async {
    final q = await voice();
    if (q != null && mounted) {
      setState(() {
        voiceQuery = q;
        selected = 2;
      });
    }
  }

  Future<void> back() async {
    if (scaffold.currentState?.isDrawerOpen == true) {
      Navigator.pop(context);
      return;
    }
    if (selected != 0) {
      setState(() => selected = 0);
      homeFocus.requestFocus();
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
            child: const Text('STAY'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(c, true),
            child: const Text('EXIT'),
          ),
        ],
      ),
    );
    if (exit == true) await SystemNavigator.pop();
  }

  Widget nav(String label, IconData icon, int index) => FocusBox(
    key: ValueKey('nav-$label'),
    isTV: widget.isTV,
    focusNode: index == 0 ? homeFocus : null,
    autofocus: index == 0,
    scale: 1.02,
    onPressed: () => choose(index),
    child: widget.isTV
        ? Container(
            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 10),
            decoration: BoxDecoration(
              border: Border(
                bottom: BorderSide(
                  color: selected == index ? shu : Colors.transparent,
                  width: 2,
                ),
              ),
            ),
            child: Text(
              label.toUpperCase(),
              style: mono.copyWith(
                fontSize: 11,
                color: selected == index ? fgBright : muted,
                letterSpacing: 1.7,
              ),
            ),
          )
        : SizedBox(
            height: 44,
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                Icon(icon, size: 21, color: selected == index ? shu : muted),
                const SizedBox(height: 6),
                Container(
                  width: 3,
                  height: 3,
                  decoration: BoxDecoration(
                    color: selected == index ? shu : Colors.transparent,
                    shape: BoxShape.circle,
                  ),
                ),
              ],
            ),
          ),
  );
  Widget tvNav() => Focus(
    focusNode: topFocus,
    child: Padding(
      padding: const EdgeInsets.fromLTRB(48, 27, 48, 16),
      child: Row(
        children: [
          FocusBox(
            isTV: true,
            onPressed: () => scaffold.currentState?.openDrawer(),
            child: const Brand(),
          ),
          const SizedBox(width: 24),
          IconButton(
            tooltip: 'Voice search',
            onPressed: voiceFromNav,
            icon: const Icon(LucideIcons.mic, size: 18),
          ),
          for (final e in [
            ('Search', LucideIcons.search, 2),
            ('Home', LucideIcons.house, 0),
            ('MyList', LucideIcons.bookmark, 1),
            ('Schedule', LucideIcons.calendarDays, 3),
          ])
            nav(e.$1, e.$2, e.$3),
          const Spacer(),
          IconButton(
            key: const Key('nav-Settings'),
            tooltip: 'Settings',
            onPressed: settings,
            icon: const Icon(LucideIcons.settings2, size: 19),
          ),
          const SizedBox(width: 16),
          Text(clockTime(now), style: mono.copyWith(color: fg, fontSize: 11)),
        ],
      ),
    ),
  );
  Widget phoneNav() => SafeArea(
    top: false,
    child: Align(
      alignment: Alignment.bottomCenter,
      child: Container(
        key: const Key('phone-bottom-bar'),
        height: phoneBarHeight,
        margin: const EdgeInsets.fromLTRB(24, 0, 24, 12),
        constraints: const BoxConstraints(maxWidth: 440),
        padding: const EdgeInsets.symmetric(horizontal: 8),
        decoration: BoxDecoration(
          color: panel,
          borderRadius: BorderRadius.circular(32),
          border: Border.all(color: line),
        ),
        child: Row(
          children: [
            Expanded(
              child: FocusBox(
                key: const Key('nav-Menu'),
                onPressed: () => scaffold.currentState?.openDrawer(),
                child: const SizedBox(
                  height: 44,
                  child: Icon(LucideIcons.menu, size: 21),
                ),
              ),
            ),
            for (final e in [
              ('Home', LucideIcons.house, 0),
              ('Search', LucideIcons.search, 2),
              ('MyList', LucideIcons.bookmark, 1),
              ('Schedule', LucideIcons.calendarDays, 3),
            ])
              Expanded(
                child: Tooltip(message: e.$1, child: nav(e.$1, e.$2, e.$3)),
              ),
          ],
        ),
      ),
    ),
  );
  Widget drawer() => Drawer(
    width: 320,
    backgroundColor: panel,
    shape: const RoundedRectangleBorder(side: BorderSide(color: line)),
    child: SafeArea(
      child: ListView(
        padding: const EdgeInsets.fromLTRB(24, 32, 24, 32),
        children: [
          const Brand(),
          const SizedBox(height: 36),
          const SectionLabel('Profile / Accounts', kanji: '人'),
          const SizedBox(height: 16),
          FocusBox(
            onPressed: () {
              Navigator.pop(context);
              choose(1);
            },
            isTV: widget.isTV,
            padding: const EdgeInsets.symmetric(vertical: 12),
            child: Row(
              children: [
                if (store.avatar.isEmpty)
                  const Icon(LucideIcons.userRound, color: muted)
                else
                  ClipOval(
                    child: CoverImage(store.avatar, width: 32, height: 32),
                  ),
                const SizedBox(width: 16),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        store.text('AniListUsername').isNotEmpty
                            ? store.text('AniListUsername')
                            : store.text('MyAnimeListUsername').isNotEmpty
                            ? store.text('MyAnimeListUsername')
                            : 'Your lists',
                        style: prose,
                      ),
                      const Text(
                        'PUBLIC ACCOUNTS · LOCAL PROGRESS',
                        style: TextStyle(
                          fontFamily: 'JetBrainsMono',
                          fontSize: 8,
                          color: muted,
                          letterSpacing: .7,
                        ),
                      ),
                    ],
                  ),
                ),
                const Icon(LucideIcons.chevronRight, size: 16, color: muted),
              ],
            ),
          ),
          const Divider(height: 48),
          const SectionLabel('Experimental', kanji: '映'),
          const SizedBox(height: 12),
          SwitchListTile(
            contentPadding: EdgeInsets.zero,
            title: const Text('Movies', style: prose),
            value: store.flag('ExperimentalMovies'),
            onChanged: (v) => store.set('ExperimentalMovies', v),
          ),
          if (store.flag('ExperimentalMovies'))
            AppButton(
              label: 'Browse movies',
              onPressed: () {
                Navigator.pop(context);
                setState(() {
                  selected = 2;
                  voiceQuery = '';
                  movies = true;
                });
              },
              isTV: widget.isTV,
            ),
          const Divider(height: 48),
          FocusBox(
            key: const Key('drawer-settings'),
            onPressed: () {
              Navigator.pop(context);
              unawaited(settings());
            },
            isTV: widget.isTV,
            padding: const EdgeInsets.symmetric(vertical: 16),
            child: Row(
              children: [
                const Icon(LucideIcons.settings2, color: muted),
                const SizedBox(width: 16),
                const Expanded(child: Text('Settings', style: prose)),
                const Icon(LucideIcons.chevronRight, color: muted, size: 16),
              ],
            ),
          ),
          const SizedBox(height: 48),
          const Text('LEAVE IT TO US.', style: mono),
        ],
      ),
    ),
  );
  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: store,
    builder: (context, _) {
      final contentKey =
          '${store.flag('AdultContent')}:${store.text('AniListUsername')}:${store.text('MyAnimeListUsername')}';
      final Widget page = switch (selected) {
        1 => MyListPage(
          key: ValueKey('list-$contentKey'),
          services: widget.services,
          isTV: widget.isTV,
          onOpen: open,
        ),
        2 => SearchPage(
          key: ValueKey('search-$contentKey-$voiceQuery-$movies'),
          initialQuery: voiceQuery,
          initialFormat: movies ? 'MOVIE' : null,
          services: widget.services,
          isTV: widget.isTV,
          onOpen: open,
          onVoice: voice,
        ),
        3 => SchedulePage(
          key: ValueKey('schedule-$contentKey'),
          services: widget.services,
          isTV: widget.isTV,
          onOpen: open,
        ),
        _ => HomePage(
          key: ValueKey('home-$contentKey'),
          services: widget.services,
          isTV: widget.isTV,
          onOpen: open,
        ),
      };
      return PopScope(
        canPop: false,
        onPopInvokedWithResult: (popped, result) {
          if (!popped) unawaited(back());
        },
        child: Focus(
          skipTraversal: true,
          onKeyEvent: (_, event) {
            if (widget.isTV &&
                event is KeyDownEvent &&
                scaffold.currentState?.isDrawerOpen != true) {
              if (event.logicalKey == LogicalKeyboardKey.arrowUp &&
                  bodyFocus.hasFocus) {
                homeFocus.requestFocus();
                return KeyEventResult.handled;
              }
              if (event.logicalKey == LogicalKeyboardKey.arrowLeft &&
                  topFocus.hasFocus) {
                scaffold.currentState?.openDrawer();
                return KeyEventResult.handled;
              }
            }
            return KeyEventResult.ignored;
          },
          child: Scaffold(
            key: scaffold,
            drawer: drawer(),
            body: SafeArea(
              bottom: false,
              child: Stack(
                children: [
                  Column(
                    children: [
                      if (widget.isTV)
                        tvNav()
                      else
                        Padding(
                          padding: const EdgeInsets.fromLTRB(24, 12, 24, 4),
                          child: Row(
                            children: [
                              const Brand(),
                              const Spacer(),
                              Text(
                                'お任せ',
                                style: mono.copyWith(
                                  color: muted,
                                  fontSize: 10,
                                ),
                              ),
                            ],
                          ),
                        ),
                      Expanded(
                        child: PageInsets(
                          bottom: widget.isTV
                              ? 27
                              : phoneBarHeight +
                                    phoneBarMargin +
                                    MediaQuery.paddingOf(context).bottom,
                          child: Focus(
                            focusNode: bodyFocus,
                            child: FocusTraversalGroup(
                              child: PosterNavigation(
                                onOpen: (a, tag) => open(a, heroTag: tag),
                                child: page,
                              ),
                            ),
                          ),
                        ),
                      ),
                    ],
                  ),
                  if (!widget.isTV) phoneNav(),
                ],
              ),
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
      Image.asset('assets/mark.png', width: 34, height: 34),
      const SizedBox(width: 12),
      Text(
        'OTAKASE',
        style: mono.copyWith(color: fg, fontSize: 13, letterSpacing: 3),
      ),
    ],
  );
}
