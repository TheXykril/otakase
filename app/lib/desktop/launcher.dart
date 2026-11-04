import 'dart:async';
import 'dart:io';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:window_manager/window_manager.dart';

import '../data/models.dart';
import '../data/movies.dart';
import '../data/trakt.dart';
import '../services.dart';
import '../ui/components.dart';
import '../ui/movie_player.dart';
import '../ui/movies_pages.dart';
import '../ui/seal.dart';
import 'desktop_core.dart';
import 'window_frame.dart';
import 'external_mpv.dart';
import 'tiling.dart';

/// True while the app shows the launcher instead of the full window. Set by
/// `otakase --launcher`; "Open full app" turns it off in the same process.
final desktopLauncher = ValueNotifier<bool>(false);

/// Reads the command line the desktop app was started with.
void readDesktopArguments(List<String> args) {
  desktopLauncher.value = args.contains('--launcher');
}

const launcherSize = Size(680, 560);

/// Window title in launcher mode, so tiling window managers can float it
/// with a rule.
const launcherTitle = 'Otakase Launcher';
const launcherPlayerSize = Size(1280, 760);

const launcherMinimumSize = Size(480, 360);

/// Blur during placement comes from the compositor moving the window.
bool _launcherPlaced = false;

Future<void> prepareLauncherWindow() async {
  _launcherPlaced = false;
  await windowManager.ensureInitialized();
  await windowManager.setOpacity(0);
  await windowManager.waitUntilReadyToShow(
    const WindowOptions(
      title: launcherTitle,
      size: launcherSize,
      minimumSize: launcherMinimumSize,
      center: true,
      backgroundColor: bg,
    ),
  );
  await _setLauncherWindowControls(true);
}

Future<void> revealLauncherWindow({required bool reduceMotion}) async {
  await showDesktopWindow();
  await placeOnTiler(size: launcherSize, raise: true);
  _launcherPlaced = true;

  if (!reduceMotion) {
    const frames = 9;
    for (var frame = 1; frame < frames; frame++) {
      await Future<void>.delayed(const Duration(milliseconds: 16));
      await windowManager.setOpacity(
        Curves.easeOutCubic.transform(frame / frames),
      );
    }
  }
  await windowManager.setOpacity(1);
}

Future<void> _setLauncherWindowControls(bool launcher) async {
  await windowManager.setResizable(!launcher);
  await windowManager.setMinimizable(!launcher);
  if (Platform.isWindows) await windowManager.setMaximizable(!launcher);
  if (Platform.isMacOS) await windowManager.setMovable(!launcher);
  await windowManager.setTitleBarStyle(
    TitleBarStyle.hidden,
    windowButtonVisibility: !launcher,
  );
  await windowManager.setAlwaysOnTop(launcher);
  await windowManager.setSkipTaskbar(launcher);
}

/// Restores normal window controls for the app and player.
Future<void> resizeDesktopWindow({
  required bool launcher,
  bool player = false,
}) async {
  if (!isDesktop) return;
  _launcherPlaced = false;
  final size = launcher
      ? launcherSize
      : (player ? launcherPlayerSize : fullAppSize);
  if (!launcher) await _setLauncherWindowControls(false);
  await windowManager.setTitle(launcher ? launcherTitle : 'Otakase');
  await windowManager.setMinimumSize(
    launcher || player ? launcherMinimumSize : fullAppMinimumSize,
  );
  await windowManager.setSize(size);
  await windowManager.center();
  await placeOnTiler(size: launcher || player ? size : null, raise: launcher);
  if (launcher) await _setLauncherWindowControls(true);
  _launcherPlaced = launcher;
}

/// One row of a launcher menu.
class LauncherItem {
  const LauncherItem(
    this.label, {
    required this.icon,
    required this.onSelect,
    this.detail = '',
    this.keepWhenFiltering = false,
  });
  final String label, detail;
  final IconData icon;
  final FutureOr<void> Function() onSelect;

  /// Stays visible whatever is typed (for example "Search for ...").
  final bool keepWhenFiltering;
}

/// A menu: a title, its rows and an optional row built from the typed text.
class LauncherMenu {
  LauncherMenu(
    this.title,
    this.items, {
    this.detail = '',
    this.typed,
    this.loading = false,
  });
  final String title, detail;
  final List<LauncherItem> items;
  final LauncherItem? Function(String query)? typed;
  bool loading;
}

/// The keyboard-first menu that stands in for the CLI's rofi menus: type to
/// filter, arrows to move, Enter to pick, Esc to go back.
class LauncherPage extends StatefulWidget {
  const LauncherPage({super.key, required this.services, this.onOpenApp});
  final AppServices services;

  /// Leaves the launcher for the full window.
  final VoidCallback? onOpenApp;
  @override
  State<LauncherPage> createState() => LauncherPageState();
}

class LauncherPageState extends State<LauncherPage> with WindowListener {
  final query = TextEditingController();
  final input = FocusNode(debugLabel: 'Launcher input');
  final scroll = ScrollController();
  final menus = <LauncherMenu>[];
  int selected = 0;
  String? message;
  List<ListEntry> watching = [];

  AppServices get services => widget.services;
  bool get adult => services.store.adultAllowed;
  ContentMode get content => contentModeOf(services.store);

  /// The profile shows anime (Content: anime or both).
  bool get showsAnime => content != ContentMode.movies;

  /// The profile shows films and series (Content: movies or both).
  bool get showsTitles => content != ContentMode.anime;

  /// Saves playback and closes the window. The full app's shell replaces
  /// this hook once the launcher turns into the full app.
  Future<void> quit() async {
    if (_quitting) return;
    _quitting = true;
    if (isDesktop) await windowManager.hide();
    await services.close().timeout(
      const Duration(seconds: 5),
      onTimeout: () {},
    );
    await closeDesktopWindow();
  }

  bool _quitting = false;

  /// True while a player runs from the launcher.
  bool playing = false;

  /// Like rofi, the launcher goes away when another window takes focus,
  /// but not while it plays or right after the compositor moved it.
  @override
  void onWindowBlur() {
    if (playing || !_launcherPlaced || !desktopLauncher.value || !mounted) {
      return;
    }
    if (ModalRoute.of(context)?.isCurrent == false) return;
    unawaited(quit());
  }

  @override
  void initState() {
    super.initState();
    if (isDesktop) windowManager.addListener(this);
    desktopCloseHook = quit;
    menus.add(mainMenu());
    query.addListener(() => setState(() => selected = 0));
    unawaited(loadLists());
  }

  @override
  void dispose() {
    if (isDesktop) windowManager.removeListener(this);
    if (desktopCloseHook == quit) desktopCloseHook = null;
    query.dispose();
    input.dispose();
    scroll.dispose();
    super.dispose();
  }

  Future<void> loadLists() async {
    if (showsTitles) unawaited(loadTitles());
    if (!showsAnime || !services.auth.hasAccounts) return;
    menus.first.loading = true;
    try {
      final page = await services.tracking.combinedList(adult: adult);
      watching = page.entries.where((e) => e.status == 'CURRENT').toList();
    } catch (e) {
      message = 'Lists could not load: $e';
    }
    if (!mounted) return;
    setState(() {
      menus[0] = mainMenu();
    });
  }

  /// Trakt's watchlist and progress, when signed in.
  Future<void> loadTitles() async {
    try {
      await services.movies.sync();
    } catch (_) {
      /* The saved lists still show. */
    }
    if (!mounted || menus.isEmpty) return;
    setState(() => menus[0] = mainMenu());
  }

  LauncherMenu mainMenu() {
    // The opening menu is Home: 18+ titles stay off it even when watched.
    final resumable = services.store.continuing
        .where((h) => !h.anime.isAdult)
        .toList();
    final shown = watching.where((e) => !e.anime.isAdult).toList();
    final last = showsAnime ? resumable.firstOrNull : null;
    final listed = {for (final e in shown) e.anime.key};
    final movies = services.movies;
    final continuing = showsTitles ? movies.continuing : <MovieEntry>[];
    final continued = {for (final e in continuing) e.title.key};
    // The newest thing watched comes first, anime or not.
    final titleFirst =
        continuing.isNotEmpty &&
        (last == null || continuing.first.updated >= last.updated);
    return LauncherMenu(
      'Otakase',
      [
        if (titleFirst) continueTitle(continuing.first, detail: 'Enter'),
        if (last != null)
          LauncherItem(
            'Continue: ${last.anime.title} · Ep ${last.episode}',
            icon: LucideIcons.rotateCcw,
            detail: 'Enter',
            onSelect: () => play(last.anime, last.episode),
          ),
        for (final e in continuing.skip(titleFirst ? 1 : 0)) continueTitle(e),
        for (final e in shown)
          LauncherItem(
            e.anime.title,
            icon: LucideIcons.tv,
            detail: e.anime.episodes > 0
                ? '${e.progress}/${e.anime.episodes}'
                : '${e.progress}',
            onSelect: () => openShow(e.anime, e.progress),
          ),
        if (showsAnime)
          for (final h in resumable.skip(1))
            if (!listed.contains(h.anime.key))
              LauncherItem(
                h.anime.title,
                icon: LucideIcons.history,
                detail: 'Ep ${h.episode}',
                onSelect: () => openShow(h.anime, math.max(0, h.episode - 1)),
              ),
        for (final t in movies.watchlist)
          if (showsTitles && !continued.contains(t.key))
            LauncherItem(
              t.title,
              icon: t.isShow ? LucideIcons.tv : LucideIcons.clapperboard,
              detail: 'Watchlist',
              onSelect: () => openTitle(t),
            ),
        LauncherItem(
          switch (content) {
            ContentMode.anime => 'Search anime',
            ContentMode.movies => 'Search movies & shows',
            ContentMode.both => 'Search anime, movies & shows',
          },
          icon: LucideIcons.search,
          detail: 'type a title',
          onSelect: () {
            input.requestFocus();
            setState(() => message = 'Type a title, then Enter.');
          },
        ),
        if (showsTitles)
          LauncherItem(
            'Trending movies & shows',
            icon: LucideIcons.flame,
            onSelect: trending,
          ),
        LauncherItem(
          'Open full app',
          icon: LucideIcons.appWindow,
          detail: 'Ctrl+W',
          onSelect: () => widget.onOpenApp?.call(),
        ),
        LauncherItem('Quit', icon: LucideIcons.x, onSelect: quit),
      ],
      typed: (q) => LauncherItem(
        'Search for “$q”',
        icon: LucideIcons.search,
        keepWhenFiltering: true,
        onSelect: () => search(q),
      ),
    );
  }

  void push(LauncherMenu menu) {
    setState(() {
      menus.add(menu);
      selected = 0;
      message = null;
      query.clear();
    });
    input.requestFocus();
  }

  bool back() {
    if (query.text.isNotEmpty) {
      query.clear();
      return true;
    }
    if (menus.length > 1) {
      setState(() {
        menus.removeLast();
        // The main menu shows what changed meanwhile (watchlist, progress).
        if (menus.length == 1) menus[0] = mainMenu();
        selected = 0;
        message = null;
      });
      return true;
    }
    return false;
  }

  Future<void> search(String q) async {
    final menu = LauncherMenu('Search', [], detail: q, loading: true);
    push(menu);
    final failures = <String>[];
    // Anime and Trakt are asked together; each shows what it found.
    final found = await Future.wait([
      if (showsAnime)
        services.catalog
            .search(q, adult: adult)
            .then<List<LauncherItem>>(
              (page) => [
                for (final a in page.items)
                  LauncherItem(
                    a.title,
                    icon: LucideIcons.tv,
                    detail: [
                      if (showsTitles) 'Anime',
                      if (a.year > 0) '${a.year}',
                      if (a.episodes > 0) '${a.episodes} eps',
                    ].join(' · '),
                    onSelect: () => openShow(a, 0),
                  ),
              ],
            )
            .catchError((Object e) {
              failures.add('Anime search failed: $e');
              return <LauncherItem>[];
            }),
      if (showsTitles)
        services.trakt
            .search(q)
            .then<List<LauncherItem>>(
              (titles) => [for (final t in titles) titleRow(t)],
            )
            .catchError((Object e) {
              failures.add('Movie search failed: $e');
              return <LauncherItem>[];
            }),
    ]);
    menu.items.addAll(found.expand((rows) => rows));
    if (failures.isNotEmpty) {
      message = failures.join('\n');
    } else if (menu.items.isEmpty) {
      message = showsTitles ? 'Nothing matches.' : 'No matching anime.';
    }
    if (mounted) setState(() => menu.loading = false);
  }

  LauncherItem titleRow(MediaTitle t) => LauncherItem(
    t.title,
    icon: t.isShow ? LucideIcons.tv : LucideIcons.clapperboard,
    detail: [
      t.isShow ? 'Series' : 'Movie',
      if (t.year > 0) '${t.year}',
    ].join(' · '),
    onSelect: () => openTitle(t),
  );

  /// What is trending and popular on Trakt, films and series mixed.
  Future<void> trending() async {
    final menu = LauncherMenu('Trending movies & shows', [], loading: true);
    push(menu);
    try {
      final rows = await shelvesOf(services).load();
      final seen = <String>{};
      for (final list in rows.values) {
        for (final t in list) {
          if (seen.add(t.key)) menu.items.add(titleRow(t));
        }
      }
    } catch (e) {
      message = 'Trakt could not be reached.';
    }
    if (mounted) setState(() => menu.loading = false);
  }

  /// A Continue row for a film or series in progress.
  LauncherItem continueTitle(MovieEntry e, {String? detail}) => LauncherItem(
    e.title.isShow
        ? 'Continue: ${e.title.title} · ${e.code}'
        : 'Continue: ${e.title.title}',
    icon: LucideIcons.rotateCcw,
    detail: detail ?? (e.title.isShow ? 'Series' : 'Movie'),
    onSelect: () async {
      if (!e.title.isShow) return playTitle(e.title);
      final list = await episodesOf(e.title);
      final at = list
          .where((x) => x.season == e.season && x.number == e.episode)
          .firstOrNull;
      await playTitle(
        e.title,
        episode: at ?? TitleEpisode(season: e.season, number: e.episode),
        episodes: list,
      );
    },
  );

  Future<List<TitleEpisode>> episodesOf(MediaTitle t) async {
    try {
      return await services.trakt.episodes(t);
    } catch (_) {
      if (mounted) setState(() => message = 'Episodes could not be loaded.');
      return [];
    }
  }

  /// A film's or series' menu: play or carry on, pick an episode, keep it on
  /// the watchlist.
  Future<void> openTitle(MediaTitle t) async {
    final movies = services.movies;
    final menu = LauncherMenu(
      t.title,
      [],
      detail: [
        t.isShow ? 'Series' : 'Movie',
        if (t.year > 0) '${t.year}',
      ].join(' · '),
      loading: t.isShow,
    );
    LauncherItem watchlistRow() {
      final on = movies.onWatchlist(t);
      return LauncherItem(
        on ? 'Remove from watchlist' : 'Add to watchlist',
        icon: on ? LucideIcons.bookmarkMinus : LucideIcons.bookmarkPlus,
        onSelect: () async {
          await movies.setWatchlist(t, !on);
          if (!mounted) return;
          setState(() {
            final i = menu.items.indexWhere(
              (r) => r.label.endsWith('watchlist'),
            );
            if (i >= 0) menu.items[i] = watchlistRow();
            message = on ? 'Removed from the watchlist.' : 'On the watchlist.';
          });
        },
      );
    }

    final open = LauncherItem(
      'Open in full app',
      icon: LucideIcons.externalLink,
      onSelect: () => widget.onOpenApp?.call(),
    );
    if (!t.isShow) {
      final e = movies.entry(t);
      menu.items.addAll([
        LauncherItem(
          e != null && e.started ? 'Resume' : 'Play',
          icon: LucideIcons.play,
          onSelect: () => playTitle(t),
        ),
        watchlistRow(),
        open,
      ]);
      push(menu);
      return;
    }
    push(menu);
    final list = await episodesOf(t);
    final next = nextTitleEpisode(movies, t, list);
    menu.items.addAll([
      if (next != null)
        LauncherItem(
          'Play next: ${next.label}',
          icon: LucideIcons.play,
          onSelect: () => playTitle(t, episode: next, episodes: list),
        ),
      if (list.isNotEmpty)
        LauncherItem(
          'Choose episode…',
          icon: LucideIcons.list,
          detail: '${list.length} episodes',
          onSelect: () => chooseTitleEpisode(t, list, next),
        ),
      watchlistRow(),
      open,
    ]);
    if (mounted) setState(() => menu.loading = false);
  }

  void chooseTitleEpisode(
    MediaTitle t,
    List<TitleEpisode> list,
    TitleEpisode? next,
  ) {
    final movies = services.movies;
    push(
      LauncherMenu(t.title, [
        for (final x in list)
          LauncherItem(
            x.label,
            icon: movies.watched(t, season: x.season, episode: x.number)
                ? LucideIcons.check
                : LucideIcons.play,
            detail: identical(x, next) ? 'next' : '',
            onSelect: () => playTitle(t, episode: x, episodes: list),
          ),
      ], detail: 'Choose episode'),
    );
    final at = next == null ? -1 : list.indexOf(next);
    if (at > 0) setState(() => selected = at);
  }

  Future<void> playTitle(
    MediaTitle t, {
    TitleEpisode? episode,
    List<TitleEpisode> episodes = const [],
  }) => runPlayer(
    MoviePlayerPage(
      services: services,
      isTV: true,
      request: titlePlay(services, t, episode: episode, episodes: episodes),
    ),
  );

  void openShow(Anime anime, int progress) {
    final saved = services.store.continuing
        .where((c) => c.anime.key == anime.key)
        .firstOrNull;
    // A search result knows nothing about progress; the history does.
    if (saved != null) progress = math.max(progress, saved.episode - 1);
    final next = progress + 1;
    final total = anime.episodes;
    final finished = total > 0 && progress >= total;
    push(
      LauncherMenu(anime.title, [
        if (!finished)
          LauncherItem(
            saved != null && saved.episode == next
                ? 'Play next: resume episode $next'
                : 'Play next: episode $next',
            icon: LucideIcons.play,
            onSelect: () => play(anime, next),
          ),
        LauncherItem(
          'Choose episode…',
          icon: LucideIcons.list,
          detail: total > 0 ? '$total episodes' : '',
          onSelect: () => chooseEpisode(anime, progress),
        ),
        if (progress > 0)
          LauncherItem(
            'Replay episode $progress',
            icon: LucideIcons.rotateCcw,
            onSelect: () => play(anime, progress),
          ),
        LauncherItem(
          'Open in full app',
          icon: LucideIcons.externalLink,
          onSelect: () => widget.onOpenApp?.call(),
        ),
      ], detail: total > 0 ? 'Ep $progress/$total' : 'Ep $progress'),
    );
  }

  void chooseEpisode(Anime anime, int progress) {
    final total = anime.episodes > 0
        ? anime.episodes
        : math.max(progress + 1, 1);
    push(
      LauncherMenu(
        anime.title,
        [
          for (var i = 1; i <= total; i++)
            LauncherItem(
              'Episode $i',
              icon: i <= progress ? LucideIcons.check : LucideIcons.play,
              detail: i == progress + 1 ? 'next' : '',
              onSelect: () => play(anime, i),
            ),
        ],
        detail: 'Choose episode',
        typed: (q) {
          final n = int.tryParse(q);
          return n != null && n > 0
              ? LauncherItem(
                  'Episode $n',
                  icon: LucideIcons.play,
                  keepWhenFiltering: true,
                  onSelect: () => play(anime, n),
                )
              : null;
        },
      ),
    );
    // Start on the next episode, like the CLI's episode menu.
    setState(() => selected = math.min(progress, total - 1));
  }

  Future<void> play(Anime anime, int episode) => runPlayer(
    playerFor(
      services: services,
      anime: anime,
      episode: episode,
      isTV: true,
      mode: services.store.text('SubOrDub'),
    ),
  );

  /// Grows the window to a player, plays, then comes back to the menu.
  Future<void> runPlayer(Widget player) async {
    if (playing) return;
    playing = true;
    await resizeDesktopWindow(launcher: false, player: true);
    if (!mounted) return;
    await Navigator.push(
      context,
      OtakaseRoute<void>(
        builder: (_) => DesktopFrame(child: TVViewport(child: player)),
      ),
    );
    if (isDesktop) await resizeDesktopWindow(launcher: true);
    playing = false;
    if (!mounted) return;
    // Progress moved on; rebuild the lists from the new state.
    setState(() {
      menus
        ..clear()
        ..add(mainMenu());
      selected = 0;
    });
    input.requestFocus();
    unawaited(loadLists());
  }

  List<LauncherItem> visible() {
    final menu = menus.last;
    final q = query.text.trim();
    final rows = q.isEmpty
        ? menu.items
        : menu.items
              .where(
                (i) =>
                    i.keepWhenFiltering ||
                    i.label.toLowerCase().contains(q.toLowerCase()),
              )
              .toList();
    final extra = q.isEmpty ? null : menu.typed?.call(q);
    if (extra == null) return rows;
    // Enter after typing runs the typed row (search, episode number) unless
    // a row's name starts with the typed text, like rofi's best match.
    final lower = q.toLowerCase();
    final match = rows.any(
      (i) => !i.keepWhenFiltering && i.label.toLowerCase().startsWith(lower),
    );
    return match ? [...rows, extra] : [extra, ...rows];
  }

  void move(int by, int count) {
    if (count == 0) return;
    setState(() => selected = (selected + by).clamp(0, count - 1));
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!scroll.hasClients) return;
      const row = 44.0;
      final top = selected * row;
      final view = scroll.position.viewportDimension;
      if (top < scroll.offset) {
        scroll.jumpTo(top);
      } else if (top + row > scroll.offset + view) {
        scroll.jumpTo(top + row - view);
      }
    });
  }

  KeyEventResult key(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent && event is! KeyRepeatEvent) {
      return KeyEventResult.ignored;
    }
    final rows = visible();
    final k = event.logicalKey;
    if (k == LogicalKeyboardKey.arrowDown) {
      move(1, rows.length);
    } else if (k == LogicalKeyboardKey.arrowUp) {
      move(-1, rows.length);
    } else if (k == LogicalKeyboardKey.pageDown) {
      move(8, rows.length);
    } else if (k == LogicalKeyboardKey.pageUp) {
      move(-8, rows.length);
    } else if (k == LogicalKeyboardKey.enter ||
        k == LogicalKeyboardKey.numpadEnter) {
      if (event is KeyDownEvent && selected < rows.length) {
        unawaited(Future.sync(rows[selected].onSelect));
      }
    } else if (k == LogicalKeyboardKey.escape) {
      if (event is KeyDownEvent && !back()) unawaited(quit());
    } else if (k == LogicalKeyboardKey.keyW &&
        HardwareKeyboard.instance.isControlPressed) {
      widget.onOpenApp?.call();
    } else {
      return KeyEventResult.ignored;
    }
    return KeyEventResult.handled;
  }

  @override
  Widget build(BuildContext context) {
    final menu = menus.last;
    final rows = visible();
    if (selected >= rows.length) selected = math.max(0, rows.length - 1);
    return Scaffold(
      backgroundColor: bg,
      body: Container(
        decoration: BoxDecoration(
          border: Border.all(color: line),
          color: bg,
        ),
        child: Column(
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 14, 16, 10),
              child: Row(
                children: [
                  if (menus.length > 1)
                    IconButton(
                      tooltip: 'Back',
                      icon: const Icon(LucideIcons.chevronLeft, size: 18),
                      onPressed: back,
                    )
                  else
                    const OtakaseSeal(size: 22),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Text(
                      menu.title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: uiStyle(
                        context,
                        size: 15,
                        weight: FontWeight.w600,
                        color: fgBright,
                      ),
                    ),
                  ),
                  if (menu.detail.isNotEmpty)
                    Text(
                      menu.detail,
                      style: mono.copyWith(fontSize: 12, color: muted),
                    ),
                ],
              ),
            ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 12),
              child: Focus(
                onKeyEvent: key,
                child: TextField(
                  key: const Key('launcher-input'),
                  controller: query,
                  focusNode: input,
                  autofocus: true,
                  style: uiStyle(context, size: 17),
                  decoration: const InputDecoration(
                    hintText: 'Search or pick…',
                    prefixIcon: Icon(LucideIcons.search, size: 18),
                  ),
                ),
              ),
            ),
            if (menu.loading)
              const LinearProgressIndicator(minHeight: 2, color: shu),
            Expanded(
              child: ListView.builder(
                key: const Key('launcher-list'),
                controller: scroll,
                padding: const EdgeInsets.symmetric(vertical: 8),
                itemCount: rows.length,
                itemExtent: 44,
                itemBuilder: (context, i) => _LauncherRow(
                  item: rows[i],
                  selected: i == selected,
                  onHover: () => setState(() => selected = i),
                  onTap: () {
                    setState(() => selected = i);
                    unawaited(Future.sync(rows[i].onSelect));
                    input.requestFocus();
                  },
                ),
              ),
            ),
            if (message != null)
              Padding(
                padding: const EdgeInsets.fromLTRB(18, 0, 18, 8),
                child: Text(
                  message!,
                  style: uiStyle(context, size: 13, color: warn),
                ),
              ),
            Container(
              decoration: const BoxDecoration(
                border: Border(top: BorderSide(color: line)),
              ),
              padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 8),
              child: Row(
                children: [
                  for (final hint in const [
                    '↑↓ move',
                    'Enter pick',
                    'Esc back',
                  ]) ...[
                    Text(hint, style: mono.copyWith(fontSize: 11)),
                    const SizedBox(width: 16),
                  ],
                  const Spacer(),
                  Text('Ctrl+W full app', style: mono.copyWith(fontSize: 11)),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _LauncherRow extends StatelessWidget {
  const _LauncherRow({
    required this.item,
    required this.selected,
    required this.onHover,
    required this.onTap,
  });
  final LauncherItem item;
  final bool selected;
  final VoidCallback onHover, onTap;
  @override
  Widget build(BuildContext context) => MouseRegion(
    cursor: SystemMouseCursors.click,
    onHover: (_) => onHover(),
    child: GestureDetector(
      onTap: onTap,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 18),
        decoration: BoxDecoration(
          color: selected ? selectedPanel : Colors.transparent,
          border: Border(
            left: BorderSide(
              color: selected ? shu : Colors.transparent,
              width: 3,
            ),
          ),
        ),
        child: Row(
          children: [
            Icon(item.icon, size: 18, color: selected ? fgBright : muted),
            const SizedBox(width: 12),
            Expanded(
              child: Text(
                item.label,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: uiStyle(
                  context,
                  size: 15,
                  color: selected ? fgBright : fg,
                ),
              ),
            ),
            if (item.detail.isNotEmpty)
              Text(item.detail, style: mono.copyWith(fontSize: 12)),
          ],
        ),
      ),
    ),
  );
}

/// The series episode to carry on with: the one stopped in, else the one
/// after the last watched, else the first not watched.
TitleEpisode? nextTitleEpisode(
  MovieService movies,
  MediaTitle title,
  List<TitleEpisode> episodes,
) {
  if (episodes.isEmpty) return null;
  final e = movies.entry(title);
  if (e != null && e.episode > 0) {
    final i = episodes.indexWhere(
      (x) => x.season == e.season && x.number == e.episode,
    );
    if (i >= 0) {
      if (!movies.watched(title, season: e.season, episode: e.episode)) {
        return episodes[i];
      }
      if (i + 1 < episodes.length) return episodes[i + 1];
    }
  }
  return episodes.firstWhere(
    (x) => !movies.watched(title, season: x.season, episode: x.number),
    orElse: () => episodes.first,
  );
}
