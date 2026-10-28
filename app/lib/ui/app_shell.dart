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
  );
  final topFocus = FocusNode(debugLabel: 'Top navigation', skipTraversal: true);
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

  void open(Anime a) => Navigator.push(
    context,
    MaterialPageRoute<void>(
      builder: (_) =>
          ShowPage(services: widget.services, anime: a, isTV: widget.isTV),
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
    final query = await voice();
    if (query != null && mounted) {
      setState(() {
        voiceQuery = query;
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
            child: const Text('Stay'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(c, true),
            child: const Text('Exit'),
          ),
        ],
      ),
    );
    if (exit == true) await SystemNavigator.pop();
  }

  Widget nav(
    String label,
    IconData icon,
    int index, {
    bool compact = false,
  }) => FocusBox(
    key: ValueKey('nav-$label'),
    isTV: widget.isTV,
    focusNode: index == 0 ? homeFocus : null,
    autofocus: index == 0,
    scale: 1.02,
    radius: 28,
    onPressed: () => choose(index),
    padding: EdgeInsets.symmetric(horizontal: compact ? 10 : 18, vertical: 10),
    child: Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (widget.isTV)
          Text(
            label,
            style: TextStyle(
              color: selected == index ? Colors.black : Colors.white,
              fontWeight: FontWeight.w600,
              fontSize: 13,
            ),
          )
        else
          Icon(icon, size: 22, color: selected == index ? shu : Colors.white70),
        if (!widget.isTV)
          Container(
            margin: const EdgeInsets.only(top: 5),
            height: 2,
            width: 16,
            color: selected == index ? shu : Colors.transparent,
          ),
      ],
    ),
  );
  Widget tvNav() => Focus(
    focusNode: topFocus,
    child: Container(
      color: Colors.black,
      padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 10),
      child: Row(
        children: [
          FocusBox(
            isTV: true,
            onPressed: () => scaffold.currentState?.openDrawer(),
            padding: const EdgeInsets.all(8),
            child: const Brand(),
          ),
          const SizedBox(width: 20),
          IconButton(
            tooltip: 'Voice search',
            onPressed: voiceFromNav,
            icon: const Icon(Icons.mic_none, size: 21),
          ),
          for (final e in [
            ('Search', Icons.search, 2),
            ('Home', Icons.home_outlined, 0),
            ('MyList', Icons.bookmark_border, 1),
          ])
            Container(
              margin: const EdgeInsets.symmetric(horizontal: 3),
              decoration: BoxDecoration(
                color: selected == e.$3 ? Colors.white : Colors.transparent,
                borderRadius: BorderRadius.circular(28),
              ),
              child: nav(e.$1, e.$2, e.$3),
            ),
          const Spacer(),
          IconButton(
            key: const Key('nav-Schedule'),
            tooltip: 'Schedule',
            onPressed: () => choose(3),
            icon: Icon(
              Icons.calendar_today_outlined,
              color: selected == 3 ? shu : Colors.white70,
              size: 21,
            ),
          ),
          IconButton(
            key: const Key('nav-Settings'),
            tooltip: 'Settings',
            onPressed: settings,
            icon: const Icon(Icons.settings_outlined, size: 21),
          ),
          const SizedBox(width: 14),
          Text(
            clockTime(now),
            style: const TextStyle(color: Colors.white70, fontSize: 13),
          ),
        ],
      ),
    ),
  );
  Widget phoneNav() => SafeArea(
    child: Align(
      alignment: Alignment.bottomCenter,
      child: Container(
        margin: const EdgeInsets.fromLTRB(16, 0, 16, 12),
        constraints: const BoxConstraints(maxWidth: 520),
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
        decoration: BoxDecoration(
          color: const Color(0xf2222222),
          borderRadius: BorderRadius.circular(40),
          border: Border.all(color: Colors.white12),
        ),
        child: Row(
          mainAxisAlignment: MainAxisAlignment.spaceAround,
          children: [
            IconButton(
              key: const Key('nav-Menu'),
              tooltip: 'Menu',
              onPressed: () => scaffold.currentState?.openDrawer(),
              icon: const Icon(Icons.menu, size: 22),
            ),
            for (final e in [
              ('Home', Icons.home_outlined, 0),
              ('MyList', Icons.bookmark_border, 1),
              ('Search', Icons.search, 2),
              ('Schedule', Icons.calendar_today_outlined, 3),
            ])
              Tooltip(
                message: e.$1,
                child: nav(e.$1, e.$2, e.$3, compact: true),
              ),
            IconButton(
              key: const Key('nav-Settings'),
              tooltip: 'Settings',
              onPressed: settings,
              icon: const Icon(Icons.settings_outlined, size: 22),
            ),
          ],
        ),
      ),
    ),
  );
  Widget drawer() => Drawer(
    backgroundColor: const Color(0xff101010),
    width: 320,
    child: SafeArea(
      child: ListenableBuilder(
        listenable: store,
        builder: (context, _) => ListView(
          padding: const EdgeInsets.all(20),
          children: [
            const Brand(),
            const SizedBox(height: 28),
            Container(
              padding: const EdgeInsets.all(12),
              decoration: BoxDecoration(
                color: panel,
                borderRadius: BorderRadius.circular(30),
              ),
              child: Row(
                children: [
                  ClipOval(
                    child: store.avatar.isEmpty
                        ? const Icon(Icons.person_outline, size: 32)
                        : CoverImage(store.avatar, width: 36, height: 36),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Text(
                      store.text('AniListUsername').isNotEmpty
                          ? store.text('AniListUsername')
                          : store.text('MyAnimeListUsername').isNotEmpty
                          ? store.text('MyAnimeListUsername')
                          : 'Local profile',
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                  IconButton(
                    tooltip: 'Account settings',
                    onPressed: () {
                      Navigator.pop(context);
                      unawaited(settings());
                    },
                    icon: const Icon(Icons.chevron_right),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 28),
            const Text(
              'Sources',
              style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 10),
            for (final provider in store.order)
              ListTile(
                key: ValueKey('source-$provider'),
                leading: Icon(
                  store.activeSource == provider
                      ? Icons.radio_button_checked
                      : Icons.radio_button_off,
                  color: store.activeSource == provider ? shu : muted,
                  size: 18,
                ),
                title: Text(
                  provider,
                  style: TextStyle(
                    color: store.activeSource == provider ? shu : Colors.white,
                  ),
                ),
                subtitle: store.providerHealth[provider] == null
                    ? null
                    : Text(
                        store.providerHealth[provider]!,
                        style: const TextStyle(fontSize: 11),
                      ),
                enabled: store.enabledSources.contains(provider),
                onTap: () {
                  unawaited(store.set('Provider', provider));
                  Navigator.pop(context);
                },
              ),
            const Divider(height: 32),
            const Text(
              'Experimental',
              style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700),
            ),
            SwitchListTile(
              title: const Text('Movies'),
              subtitle: const Text(
                'ExperimentalMovies',
                style: TextStyle(fontSize: 11),
              ),
              value: store.flag('ExperimentalMovies'),
              onChanged: (v) => store.set('ExperimentalMovies', v),
            ),
            if (store.flag('ExperimentalMovies'))
              ListTile(
                title: const Text('Browse movies'),
                leading: const Icon(Icons.movie_outlined),
                onTap: () {
                  Navigator.pop(context);
                  setState(() {
                    selected = 2;
                    voiceQuery = '';
                    movies = true;
                  });
                },
              ),
            const SizedBox(height: 20),
            ListTile(
              leading: const Icon(Icons.settings_outlined),
              title: const Text('Settings'),
              onTap: () {
                Navigator.pop(context);
                unawaited(settings());
              },
            ),
          ],
        ),
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
          onSettings: settings,
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
                      if (widget.isTV) tvNav(),
                      Expanded(
                        child: Focus(
                          focusNode: bodyFocus,
                          child: FocusTraversalGroup(child: page),
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
      Image.asset(
        'assets/otakase.png',
        width: 34,
        height: 34,
        filterQuality: FilterQuality.medium,
      ),
      const SizedBox(width: 8),
      const Text(
        'Otakase',
        style: TextStyle(
          fontSize: 21,
          fontWeight: FontWeight.w700,
          letterSpacing: .3,
        ),
      ),
    ],
  );
}
