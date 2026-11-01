import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:window_manager/window_manager.dart';

import '../data/models.dart';
import '../player.dart';
import '../services.dart';
import '../ui/components.dart';
import '../ui/seal.dart';
import 'desktop_core.dart';
import 'window_frame.dart';

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

/// Switches the window between launcher and full app sizes.
Future<void> resizeDesktopWindow({required bool launcher}) async {
  if (!isDesktop) return;
  await windowManager.setTitle(launcher ? launcherTitle : 'Otakase');
  await windowManager.setAlwaysOnTop(launcher);
  await windowManager.setMinimumSize(
    launcher ? const Size(480, 360) : const Size(960, 600),
  );
  await windowManager.setSize(launcher ? launcherSize : const Size(1440, 900));
  await windowManager.center();
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

class LauncherPageState extends State<LauncherPage> {
  final query = TextEditingController();
  final input = FocusNode(debugLabel: 'Launcher input');
  final scroll = ScrollController();
  final menus = <LauncherMenu>[];
  int selected = 0;
  String? message;
  List<ListEntry> watching = [];

  AppServices get services => widget.services;
  bool get adult => services.store.adultAllowed;

  /// Saves playback and closes the window. The full app's shell replaces
  /// this hook once the launcher turns into the full app.
  Future<void> quit() async {
    await services.close().timeout(
      const Duration(seconds: 5),
      onTimeout: () {},
    );
    await closeDesktopWindow();
  }

  @override
  void initState() {
    super.initState();
    desktopCloseHook = quit;
    menus.add(mainMenu());
    query.addListener(() => setState(() => selected = 0));
    unawaited(loadLists());
  }

  @override
  void dispose() {
    if (desktopCloseHook == quit) desktopCloseHook = null;
    query.dispose();
    input.dispose();
    scroll.dispose();
    super.dispose();
  }

  Future<void> loadLists() async {
    if (!services.auth.hasAccounts) return;
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

  LauncherMenu mainMenu() {
    final last = services.store.continuing.firstOrNull;
    final listed = {for (final e in watching) e.anime.key};
    return LauncherMenu(
      'Otakase',
      [
        if (last != null)
          LauncherItem(
            'Continue: ${last.anime.title} · Ep ${last.episode}',
            icon: LucideIcons.rotateCcw,
            detail: 'Enter',
            onSelect: () => play(last.anime, last.episode),
          ),
        for (final e in watching)
          LauncherItem(
            e.anime.title,
            icon: LucideIcons.tv,
            detail: e.anime.episodes > 0
                ? '${e.progress}/${e.anime.episodes}'
                : '${e.progress}',
            onSelect: () => openShow(e.anime, e.progress),
          ),
        for (final h in services.store.continuing.skip(1))
          if (!listed.contains(h.anime.key))
            LauncherItem(
              h.anime.title,
              icon: LucideIcons.history,
              detail: 'Ep ${h.episode}',
              onSelect: () => openShow(h.anime, math.max(0, h.episode - 1)),
            ),
        LauncherItem(
          'Search anime',
          icon: LucideIcons.search,
          detail: 'type a title',
          onSelect: () {
            input.requestFocus();
            setState(() => message = 'Type a title, then Enter.');
          },
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
    try {
      final page = await services.catalog.search(q, adult: adult);
      menu.items.addAll([
        for (final a in page.items)
          LauncherItem(
            a.title,
            icon: LucideIcons.tv,
            detail: [
              if (a.year > 0) '${a.year}',
              if (a.episodes > 0) '${a.episodes} eps',
            ].join(' · '),
            onSelect: () => openShow(a, 0),
          ),
      ]);
      if (menu.items.isEmpty) message = 'No matching anime.';
    } catch (e) {
      message = 'Search failed: $e';
    }
    if (mounted) setState(() => menu.loading = false);
  }

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

  Future<void> play(Anime anime, int episode) async {
    if (isDesktop) {
      await windowManager.setAlwaysOnTop(false);
      await windowManager.setSize(launcherPlayerSize);
      await windowManager.center();
    }
    if (!mounted) return;
    await Navigator.push(
      context,
      OtakaseRoute<void>(
        builder: (_) => TVViewport(
          child: PlayerPage(
            services: services,
            anime: anime,
            episode: episode,
            isTV: true,
            mode: services.store.text('SubOrDub'),
          ),
        ),
      ),
    );
    if (isDesktop) await resizeDesktopWindow(launcher: true);
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
            WindowDragArea(
              child: Padding(
                padding: EdgeInsets.fromLTRB(
                  16 + windowButtonsLeading,
                  14,
                  16,
                  10,
                ),
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
                    const SizedBox(width: 8),
                    const WindowButtons(),
                  ],
                ),
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
