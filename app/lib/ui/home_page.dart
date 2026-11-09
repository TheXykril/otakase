import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../data/models.dart';
import '../data/continuation.dart';
import '../data/movies.dart';
import '../data/tracking.dart';
import '../services.dart';
import '../desktop/desktop_core.dart';
import '../desktop/desktop_home.dart';
import 'components.dart';
import 'discovery_home.dart';
import 'movies_pages.dart' show titleCardsOf, titleCaption;

/// Two lists that are each newest first, merged newest first while keeping
/// each list's own order.
List<WatchEntry> mergeRecent(List<WatchEntry> a, List<WatchEntry> b) {
  final out = <WatchEntry>[];
  var i = 0, j = 0;
  while (i < a.length || j < b.length) {
    if (j >= b.length || i < a.length && a[i].updated >= b[j].updated) {
      out.add(a[i++]);
    } else {
      out.add(b[j++]);
    }
  }
  return out;
}

class NativeTitle extends StatelessWidget {
  const NativeTitle(this.title, {super.key, required this.isTV});
  final String title;
  final bool isTV;
  @override
  Widget build(BuildContext context) => title.trim().isEmpty
      ? const SizedBox.shrink()
      : ExcludeSemantics(
          child: Container(
            padding: EdgeInsets.symmetric(
              horizontal: isTV ? 6 : 4,
              vertical: 8,
            ),
            decoration: BoxDecoration(
              color: bgDeep.withValues(alpha: .94),
              borderRadius: BorderRadius.circular(isTV ? 5 : 4),
            ),
            child: Text(
              title.characters.join('\n'),
              maxLines: isTV ? 8 : 9,
              overflow: TextOverflow.clip,
              textAlign: TextAlign.center,
              style: TextStyle(
                fontFamily: 'NotoSansJP',
                fontSize: isTV ? 20 : 15,
                height: isTV ? 1.2 : 1.3,
                color: fgBright,
              ),
            ),
          ),
        );
}

class ResumeCard extends StatelessWidget {
  const ResumeCard({
    super.key,
    required this.entry,
    required this.isTV,
    required this.onResume,
    required this.onOpen,
    this.autofocus = true,
    this.showArtwork = true,
  });
  final WatchEntry entry;
  final bool isTV, autofocus, showArtwork;
  final VoidCallback onResume, onOpen;
  @override
  Widget build(BuildContext context) {
    final anime = entry.anime;
    final scaled = MediaQuery.textScalerOf(context).scale(1);
    final knownDuration = entry.duration > 0;
    final height = isTV
        ? math.max(292.0, 292 * scaled)
        : math.max(
            MediaQuery.sizeOf(context).height < 800 ? 244.0 : 292.0,
            292 * scaled,
          );
    final titleStyle = anime.title.length > 38
        ? uiStyle(
            context,
            size: isTV ? 36 : 28,
            weight: FontWeight.w700,
            height: 1.2,
          )
        : display.copyWith(
            fontSize: isTV ? 45 : 33,
            height: isTV ? 49 / 45 : 1,
          );
    return Container(
      key: const Key('resume-card'),
      height: height,
      margin: EdgeInsets.symmetric(horizontal: isTV ? 0 : 20),
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(16),
        border: isTV ? null : Border.all(color: const Color(0xff4a342a)),
      ),
      child: ClipRRect(
        borderRadius: BorderRadius.circular(isTV ? 0 : 16),
        child: GestureDetector(
          behavior: HitTestBehavior.opaque,
          onTap: onOpen,
          child: Stack(
            fit: StackFit.expand,
            children: [
              if (showArtwork) HeroArt(anime, height: height),
              Positioned(
                top: 13,
                left: isTV ? 48 : 14,
                child: Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 8,
                    vertical: 4,
                  ),
                  color: bgDeep.withValues(alpha: .95),
                  child: Text(
                    entry.position > 0
                        ? 'Continue watching'
                        : 'Your next episode',
                    style: uiStyle(context, size: 15),
                  ),
                ),
              ),
              if (anime.nativeTitle.isNotEmpty)
                Positioned(
                  right: isTV ? 48 : 10,
                  top: 16,
                  child: NativeTitle(anime.nativeTitle, isTV: isTV),
                ),
              if (isTV)
                Positioned(
                  right: 100,
                  bottom: 36,
                  child: Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 10,
                      vertical: 2,
                    ),
                    decoration: BoxDecoration(
                      color: bgDeep.withValues(alpha: .9),
                      borderRadius: BorderRadius.circular(9),
                    ),
                    child: Text(
                      entry.episode.toString().padLeft(2, '0'),
                      style: display.copyWith(fontSize: 54),
                    ),
                  ),
                ),
              Align(
                alignment: Alignment.bottomLeft,
                child: Padding(
                  padding: EdgeInsets.fromLTRB(
                    isTV ? 48 : 18,
                    48,
                    isTV ? 300 : 42,
                    20,
                  ),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        'EP ${entry.episode.toString().padLeft(2, '0')}',
                        style: mono.copyWith(
                          fontSize: isTV ? 20 : 15,
                          color: fg,
                        ),
                      ),
                      const SizedBox(height: 8),
                      Text(
                        anime.title,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: titleStyle,
                      ),
                      const SizedBox(height: 12),
                      Wrap(
                        crossAxisAlignment: WrapCrossAlignment.center,
                        spacing: 12,
                        runSpacing: 4,
                        children: [
                          AppButton(
                            key: const Key('home-resume'),
                            label: entry.position > 0 ? 'Resume' : 'Play',
                            icon: LucideIcons.play,
                            filled: true,
                            isTV: isTV,
                            autofocus: autofocus,
                            onPressed: onResume,
                          ),
                          if (isTV)
                            AppButton(
                              label: 'Details',
                              icon: LucideIcons.info,
                              isTV: true,
                              onPressed: onOpen,
                            ),
                          if (knownDuration && entry.position > 0)
                            Container(
                              color: bgDeep.withValues(alpha: .95),
                              padding: const EdgeInsets.all(4),
                              child: Text(
                                '${((entry.duration - entry.position).clamp(0, entry.duration) / 60).ceil()} min left',
                                style: uiStyle(context, size: 15),
                              ),
                            ),
                        ],
                      ),
                    ],
                  ),
                ),
              ),
              if (knownDuration && entry.position > 0)
                Positioned(
                  left: 0,
                  right: 0,
                  bottom: 0,
                  child: LinearProgressIndicator(
                    value: (entry.position / entry.duration).clamp(0, 1),
                    minHeight: 3,
                    color: shu,
                    backgroundColor: line,
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}

class HomePage extends StatefulWidget {
  const HomePage({
    super.key,
    required this.services,
    required this.isTV,
    required this.onOpen,
    required this.onResume,
    required this.onSearch,
    required this.onAccounts,
    this.active = true,
    this.topInset = 0,
  });
  final AppServices services;
  final bool isTV, active;
  final double topInset;
  final void Function(Anime) onOpen;
  final void Function(WatchEntry) onResume;
  final VoidCallback onSearch, onAccounts;
  @override
  State<HomePage> createState() => _HomePageState();
}

class _HomePageState extends State<HomePage> {
  WatchEntry? focused;
  Timer? artTimer;
  Timer? clock;
  String identity = '';
  final snapshots = <String, List<ListEntry>>{};
  final blocks = <String, GlobalKey<_AccountBlockState>>{};
  final discovery = GlobalKey<DiscoveryHomeState>();
  bool refreshScheduled = false;
  HomeData? featured;

  /// Whether films and series share Home with anime on this profile.
  bool get showsTitles =>
      contentModeOf(widget.services.store) == ContentMode.both;
  bool titlesSynced = false;

  void titlesChanged() {
    if (mounted && showsTitles) setState(() {});
  }

  /// Pulls the film and series watchlist and progress once titles show.
  Future<void> syncTitles() async {
    titlesSynced = true;
    try {
      await widget.services.trakt.load();
      await widget.services.movies.sync();
    } catch (_) {
      // Home keeps what is saved on this device.
    }
  }

  @override
  void initState() {
    super.initState();
    widget.services.movies.addListener(titlesChanged);
    if (isDesktop) unawaited(loadFeatured());
    clock = Timer.periodic(const Duration(minutes: 1), (_) {
      if (mounted) setState(() {});
    });
  }

  @override
  void didUpdateWidget(HomePage old) {
    super.didUpdateWidget(old);
    if (widget.active && !old.active) {
      scheduleRefresh();
    }
  }

  void scheduleRefresh() {
    if (refreshScheduled) return;
    refreshScheduled = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      refreshScheduled = false;
      if (mounted && widget.active) unawaited(refresh());
    });
  }

  /// Trending picks for the desktop hero.
  Future<void> loadFeatured() async {
    try {
      // Home never shows 18+ titles, whatever the account allows.
      final data = await widget.services.catalog.home(adult: false);
      if (mounted) setState(() => featured = data);
    } catch (_) {
      // The hero falls back to what you are watching.
    }
  }

  List<HeroPick> heroPicks(List<WatchEntry> continuing) {
    final picks = <HeroPick>[];
    for (final e in continuing.take(2)) {
      final left = e.duration > 0 ? ((e.duration - e.position) / 60).ceil() : 0;
      picks.add(
        HeroPick(
          anime: e.anime,
          kicker:
              'Continue · Ep ${e.episode}${left > 0 && e.position > 0 ? ' · $left min left' : ''}',
          action: e.position > 0
              ? 'Resume episode ${e.episode}'
              : 'Play episode ${e.episode}',
          onPlay: () => widget.onResume(e),
          onDetails: () => widget.onOpen(e.anime),
        ),
      );
    }
    final seen = picks.map((p) => p.anime.key).toSet();
    var rank = 0;
    for (final a in featured?.trending ?? const <Anime>[]) {
      rank++;
      if (picks.length >= 5) break;
      if (a.isAdult || !seen.add(a.key)) continue;
      picks.add(
        HeroPick(
          anime: a,
          kicker: 'Trending #$rank',
          action: 'Play episode 1',
          onPlay: () => widget.onResume(WatchEntry(anime: a)),
          onDetails: () => widget.onOpen(a),
        ),
      );
    }
    return picks;
  }

  Future<void> refresh({bool forceSync = false}) async {
    if (isDesktop) unawaited(loadFeatured());
    await Future.wait([
      if (showsTitles) syncTitles(),
      if (discovery.currentState case final page?) page.load(refresh: true),
      ...blocks.values.map(
        (key) =>
            key.currentState?.load(forceSync: forceSync) ??
            Future<void>.value(),
      ),
    ]);
  }

  void focusEntry(ListEntry entry) {
    artTimer?.cancel();
    artTimer = Timer(
      MediaQuery.disableAnimationsOf(context)
          ? Duration.zero
          : const Duration(milliseconds: 180),
      () {
        if (!mounted) return;
        if (entry.anime.airedEpisodes > 0 &&
            entry.progress >= entry.anime.airedEpisodes) {
          setState(() => focused = null);
          return;
        }
        setState(
          () => focused = WatchEntry(
            anime: entry.anime,
            episode: entry.progress + 1,
          ),
        );
      },
    );
  }

  @override
  void dispose() {
    widget.services.movies.removeListener(titlesChanged);
    artTimer?.cancel();
    clock?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final s = widget.services, store = s.store;
    final stamp =
        '${s.auth.accounts.values.map((a) => '${a.service}:${a.userId}').join(',')}:${store.text('AniListUsername')}:${store.text('MyAnimeListUsername')}:${store.adultAllowed}';
    if (identity != stamp) {
      identity = stamp;
      snapshots.clear();
      focused = null;
    }
    final continuation = buildContinuation(
      snapshots.values.expand((e) => e).toList(),
      store.history,
      // 18+ titles stay off Home even when watched; lists and search keep them.
      adult: false,
      now: DateTime.now(),
    );
    final continuing = continuation.continueWatching;
    final resume = continuing.firstOrNull;
    // Films and series join the Continue watching row and get a watchlist
    // row; the big resume card stays for anime.
    final titles = showsTitles;
    if (titles && !titlesSynced) unawaited(syncTitles());
    final cards = titleCardsOf(s);
    final titleProgress = <String, MovieEntry>{};
    final titleEntries = <WatchEntry>[
      if (titles)
        for (final e in s.movies.continuing)
          WatchEntry(
            anime: cards.card(e.title),
            episode: e.episode,
            position: e.position,
            duration: e.duration,
            updated: e.updated,
          ),
    ];
    if (titles) {
      for (final e in s.movies.continuing) {
        titleProgress[cards.card(e.title).key] = e;
      }
    }
    final shelf = mergeRecent(
      isDesktop ? continuing : continuing.skip(1).toList(),
      titleEntries,
    );
    final watchlist = titles ? s.movies.watchlist : const [];
    final stage = widget.isTV && focused != null && !focused!.anime.isAdult
        ? focused
        : resume;
    final configured = ['AniList', 'MAL'].where(s.auth.signedIn).toList();
    final combined = s.auth.signedIn('AniList') && s.auth.signedIn('MAL');
    final lists = combined ? ['AniList'] : configured;
    return RefreshIndicator(
      onRefresh: () => refresh(forceSync: true),
      color: shu,
      child: ListView(
        key: const PageStorageKey('home-scroll'),
        padding: EdgeInsets.only(bottom: contentBottom(context)),
        children: [
          if (isDesktop) ...[
            DesktopHero(
              picks: heroPicks(continuing),
              topInset: widget.topInset,
            ),
            DesktopContinueRow(
              entries: shelf,
              onResume: widget.onResume,
              label: (e) => switch (titleProgress[e.anime.key]) {
                final t? => titleCaption(t),
                _ => null,
              },
            ),
          ],
          if (!isDesktop &&
              widget.isTV &&
              (stage != null || configured.isNotEmpty))
            TVHomeHero(
              key: const Key('tv-home-hero'),
              anime: stage?.anime,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  SizedBox(height: widget.topInset),
                  const Padding(
                    padding: EdgeInsets.fromLTRB(48, 12, 48, 16),
                    child: PageHeading('Back to your story', '続'),
                  ),
                  if (stage != null)
                    ResumeCard(
                      entry: stage,
                      isTV: true,
                      showArtwork: false,
                      onResume: () => widget.onResume(stage),
                      onOpen: () => widget.onOpen(stage.anime),
                    ),
                ],
              ),
            ),
          if (!widget.isTV && (stage != null || configured.isNotEmpty))
            Padding(
              padding: EdgeInsets.fromLTRB(
                widget.isTV ? 48 : 20,
                12,
                widget.isTV ? 48 : 20,
                16,
              ),
              child: const PageHeading('Back to your story', '続'),
            ),
          if (!widget.isTV && stage != null)
            ResumeCard(
              entry: stage,
              isTV: widget.isTV,
              onResume: () => widget.onResume(stage),
              onOpen: () => widget.onOpen(stage.anime),
            ),
          for (final service in lists)
            AccountBlock(
              key: blocks.putIfAbsent(
                combined ? 'Combined' : service,
                () => GlobalKey<_AccountBlockState>(),
              ),
              service: service,
              combineTrackers: combined,
              services: s,
              isTV: widget.isTV,
              onOpen: widget.onOpen,
              onFocused: focusEntry,
              onCategoryChanged: scheduleRefresh,
              onLoaded: (entries) {
                if (mounted) {
                  setState(
                    () => snapshots[combined ? 'Combined' : service] = entries,
                  );
                }
              },
              autofocus: stage == null && service == lists.first,
            ),
          if (!isDesktop && shelf.isNotEmpty)
            PosterRow(
              title: 'Continue watching',
              items: shelf.map((e) => e.anime).toList(),
              isTV: widget.isTV,
              onOpen: widget.onOpen,
              progress: (a) {
                if (titleProgress[a.key] case final t?) {
                  return t.progress > 0 ? t.progress : null;
                }
                final h = shelf.firstWhere((e) => e.anime.key == a.key);
                return h.duration > 0 ? h.position / h.duration : null;
              },
              captions: (a) => switch (titleProgress[a.key]) {
                final t? => titleCaption(t),
                _ =>
                  'Episode ${shelf.firstWhere((e) => e.anime.key == a.key).episode}',
              },
            ),
          if (continuation.upNext.isNotEmpty)
            PosterRow(
              title: 'Up next',
              items: continuation.upNext.map((e) => e.anime).toList(),
              isTV: widget.isTV,
              onOpen: widget.onOpen,
              captions: (a) => continuation.upNext
                  .firstWhere((e) => e.anime.key == a.key)
                  .caption(DateTime.now()),
            ),
          if (watchlist.isNotEmpty)
            PosterRow(
              key: const Key('home-title-watchlist'),
              title: 'Movies & shows watchlist',
              items: [for (final t in watchlist) cards.card(t)],
              isTV: widget.isTV,
              onOpen: widget.onOpen,
              captions: (a) => switch (cards.title(a)) {
                final t? when t.isShow => 'Series',
                final t? when t.year > 0 => '${t.year}',
                _ => 'Movie',
              },
            ),
          if (configured.isEmpty)
            DiscoveryHome(
              key: discovery,
              services: s,
              isTV: widget.isTV,
              hasResume: stage != null,
              showIntro: !isDesktop,
              topInset: stage == null && !isDesktop ? widget.topInset : 0,
              onOpen: widget.onOpen,
              onSearch: widget.onSearch,
              onAccounts: widget.onAccounts,
            ),
        ],
      ),
    );
  }
}

class AccountBlock extends StatefulWidget {
  const AccountBlock({
    super.key,
    required this.service,
    required this.services,
    required this.isTV,
    required this.onOpen,
    this.onFocused,
    this.onLoaded,
    this.onCategoryChanged,
    this.autofocus = false,
    this.combineTrackers = false,
  });
  final String service;
  final AppServices services;
  final bool isTV, autofocus, combineTrackers;
  final void Function(Anime) onOpen;
  final void Function(ListEntry)? onFocused;
  final void Function(List<ListEntry>)? onLoaded;
  final VoidCallback? onCategoryChanged;
  @override
  State<AccountBlock> createState() => _AccountBlockState();
}

class _AccountBlockState extends State<AccountBlock> {
  List<ListEntry> entries = [];
  String category = 'CURRENT', stamp = '';
  bool busy = true;
  String? error;
  List<String> notLoaded = [];
  int generation = 0;
  final trigger = FocusNode(debugLabel: 'List category');
  final scroll = ScrollController();
  final triggerKey = GlobalKey();
  String get username =>
      widget.services.auth.account(widget.service)?.username ??
      widget.services.store.text(
        widget.service == 'MAL' ? 'MyAnimeListUsername' : 'AniListUsername',
      );
  Map<String, String> get categories =>
      widget.service == 'MAL' ? malStatuses : anilistStatuses;
  String get listKey => widget.combineTrackers ? 'Combined' : widget.service;
  String get label => widget.combineTrackers ? 'AniList + MAL' : widget.service;
  String get signature =>
      '${widget.services.auth.account(widget.service)?.token}:${widget.combineTrackers ? widget.services.auth.account('MAL')?.token : ''}:$username:${widget.services.store.flag('AdultContent')}:${widget.services.tracking.revision}';
  @override
  void initState() {
    super.initState();
    category = widget.services.store.text('ListCategory.$listKey');
    if (!categories.containsKey(category)) {
      category = categories.keys.elementAt(1);
    }
    unawaited(load());
  }

  @override
  void didUpdateWidget(AccountBlock old) {
    super.didUpdateWidget(old);
    if (stamp != signature) unawaited(load());
  }

  @override
  void dispose() {
    trigger.dispose();
    scroll.dispose();
    super.dispose();
  }

  Future<void> load({bool forceSync = false}) async {
    final g = ++generation;
    stamp = signature;
    setState(() {
      busy = true;
      error = null;
      notLoaded = [];
    });
    try {
      final s = widget.services;
      List<ListEntry> result;
      List<String> failed = [];
      if (widget.combineTrackers) {
        final page = await s.tracking.combinedList(
          adult: s.store.flag('AdultContent'),
        );
        result = page.entries;
        failed = page.notLoaded;
        if (mounted && g == generation) {
          s.listSync.afterLoad(page, force: forceSync);
        }
      } else if (s.auth.signedIn(widget.service)) {
        final page = await s.tracking.list(
          widget.service,
          adult: s.store.flag('AdultContent'),
        );
        result = page.entries;
        if (page.fromCache) failed = [widget.service];
      } else {
        final requestedUsername = username;
        final all = <String, ListEntry>{};
        for (var p = 1; ; p++) {
          final r = await s.catalog.publicList(
            widget.service,
            username,
            'ALL',
            adult: s.store.flag('AdultContent'),
            page: p,
          );
          for (final e in r.entries) {
            all[e.anime.key] = e;
          }
          if (!r.hasMore) break;
        }
        result = all.values.toList();
        if (mounted && g == generation && username == requestedUsername) {
          await s.store.set('PublicListCache.${widget.service}', {
            'username': requestedUsername,
            'entries': result.map((e) => e.toJson()).toList(),
          });
        }
      }
      // The full list still feeds sync above; Home only drops 18+ rows.
      result = result.where((e) => !e.anime.isAdult).toList();
      if (mounted && g == generation) {
        setState(() {
          entries = result;
          notLoaded = failed;
        });
        widget.onLoaded?.call(result);
        if (widget.isTV && result.isNotEmpty && widget.autofocus) {
          widget.onFocused?.call(result.first);
        }
      }
    } catch (e) {
      if (mounted && g == generation) {
        final cached = widget.services.store.value(
          'PublicListCache.${widget.service}',
        );
        if (!widget.services.auth.signedIn(widget.service) &&
            cached is Map &&
            cached['username'] == username) {
          try {
            final rows = (cached['entries'] as List)
                .map(
                  (e) => ListEntry.fromJson((e as Map).cast<String, dynamic>()),
                )
                .where((e) => !e.anime.isAdult)
                .toList();
            setState(() {
              entries = rows;
              notLoaded = [widget.service];
            });
            widget.onLoaded?.call(rows);
          } catch (_) {
            setState(() => error = e.toString());
          }
        } else {
          setState(() => error = e.toString());
        }
      }
    } finally {
      if (mounted && g == generation) setState(() => busy = false);
    }
  }

  Future<void> pick(BuildContext anchor) async {
    final box =
        (triggerKey.currentContext ?? anchor).findRenderObject()! as RenderBox;
    final overlay =
        Overlay.of(context).context.findRenderObject()! as RenderBox;
    final rect = box.localToGlobal(Offset.zero, ancestor: overlay) & box.size;
    final selected = await showMenu<String>(
      context: context,
      requestFocus: true,
      initialValue: category,
      color: panel,
      constraints: BoxConstraints(
        minWidth: widget.isTV ? 286 : 260,
        maxWidth: widget.isTV ? 286 : 260,
        maxHeight:
            MediaQuery.sizeOf(context).height -
            MediaQuery.paddingOf(context).vertical -
            32,
      ),
      position: RelativeRect.fromRect(rect, Offset.zero & overlay.size),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(13)),
      items: categories.entries
          .map(
            (e) => PopupMenuItem<String>(
              value: e.key,
              height: widget.isTV ? 56 : 44,
              child: _CategoryInitialFocus(
                selected: category == e.key,
                child: Row(
                  children: [
                    Expanded(
                      child: Text(
                        e.value,
                        style: uiStyle(
                          context,
                          weight: category == e.key
                              ? FontWeight.w600
                              : FontWeight.w400,
                        ),
                      ),
                    ),
                    Text(
                      '${entries.where((v) => e.key == 'ALL' || v.status == e.key).length}',
                      style: mono.copyWith(fontSize: widget.isTV ? 20 : 15),
                    ),
                    const SizedBox(width: 12),
                    SizedBox(
                      width: 19,
                      child: category == e.key
                          ? const Icon(
                              LucideIcons.check,
                              color: accentText,
                              size: 19,
                            )
                          : null,
                    ),
                  ],
                ),
              ),
            ),
          )
          .toList(),
    );
    if (selected != null && selected != category && mounted) {
      setState(() => category = selected);
      unawaited(widget.services.store.set('ListCategory.$listKey', selected));
      widget.onCategoryChanged?.call();
    }
    if (mounted) trigger.requestFocus();
  }

  @override
  Widget build(BuildContext context) {
    final shown = entries
        .where((e) => category == 'ALL' || e.status == category)
        .toList();
    final side = widget.isTV ? 48.0 : 20.0;
    return Padding(
      padding: const EdgeInsets.only(top: 24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: EdgeInsets.symmetric(horizontal: side),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'Your list',
                  style: uiStyle(
                    context,
                    size: widget.isTV ? 26 : 21,
                    weight: FontWeight.w600,
                  ),
                ),
                Text(
                  '$label · $username${widget.services.auth.signedIn(widget.service) ? '' : ' · Public, read only'}',
                  style: uiStyle(context, size: 15, color: muted),
                ),
                for (final service in notLoaded)
                  TextButton(
                    key: ValueKey('retry-$service'),
                    onPressed: busy ? null : load,
                    child: Text(
                      '$service not loaded, retry',
                      style: uiStyle(context, size: 15, color: muted),
                    ),
                  ),
                const SizedBox(height: 12),
                SizedBox(
                  width: widget.isTV ? 232 : double.infinity,
                  child: Builder(
                    builder: (anchor) => FocusBox(
                      key: triggerKey,
                      focusNode: trigger,
                      autofocus: widget.autofocus,
                      isTV: widget.isTV,
                      onPressed: () => pick(anchor),
                      radius: 9,
                      child: Container(
                        constraints: BoxConstraints(
                          minHeight: widget.isTV ? 48 : 44,
                        ),
                        padding: const EdgeInsets.symmetric(horizontal: 13),
                        decoration: BoxDecoration(
                          border: Border.all(color: line),
                          borderRadius: BorderRadius.circular(9),
                        ),
                        child: Row(
                          children: [
                            Container(width: 3, height: 19, color: shu),
                            const SizedBox(width: 12),
                            Expanded(
                              child: Text(
                                categories[category]!,
                                style: uiStyle(
                                  context,
                                  weight: FontWeight.w600,
                                ),
                              ),
                            ),
                            Text(
                              '${shown.length}',
                              style: mono.copyWith(
                                fontSize: widget.isTV ? 20 : 15,
                              ),
                            ),
                            const SizedBox(width: 8),
                            const Icon(LucideIcons.chevronDown, size: 20),
                          ],
                        ),
                      ),
                    ),
                  ),
                ),
              ],
            ),
          ),
          if (busy && entries.isEmpty)
            const Padding(
              padding: EdgeInsets.all(20),
              child: SkeletonBlock(height: 184),
            ),
          if (error != null)
            FailureView(message: error!, onRetry: load, isTV: widget.isTV),
          if (!busy && error == null && notLoaded.length < 2 && shown.isEmpty)
            Padding(
              padding: EdgeInsets.symmetric(horizontal: side, vertical: 12),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    'No ${categories[category]!.toLowerCase()} titles.',
                    style: uiStyle(context, color: muted),
                  ),
                  TextButton(
                    onPressed: () => pick(context),
                    child: const Text('Choose category'),
                  ),
                ],
              ),
            ),
          if (shown.isNotEmpty)
            LayoutBuilder(
              builder: (context, box) {
                final cardWidth = isDesktop
                    ? fittedCardWidth(
                        box.maxWidth,
                        200,
                        padding: side - 7,
                        gap: 26,
                      )
                    : widget.isTV
                    ? 143.0
                    : 132.0;
                return shelf(context, shown, cardWidth, side);
              },
            ),
        ],
      ),
    );
  }

  Widget shelf(
    BuildContext context,
    List<ListEntry> shown,
    double cardWidth,
    double side,
  ) => SizedBox(
    height:
        (isDesktop
            ? cardWidth * 194 / 143
            : widget.isTV
            ? 194
            : 184) +
        126 * MediaQuery.textScalerOf(context).scale(1),
    child: RemoteFocusRow(
      isTV: widget.isTV,
      controller: scroll,
      itemCount: shown.length,
      itemWidth: cardWidth,
      spacing: widget.isTV ? 26 : 12,
      padding: side - 7,
      onUp: trigger.requestFocus,
      child: ListView.separated(
        key: PageStorageKey('$listKey-$category'),
        controller: scroll,
        scrollDirection: Axis.horizontal,
        clipBehavior: Clip.none,
        padding: EdgeInsets.fromLTRB(side - 7, 15, side, 8),
        itemCount: shown.length,
        findItemIndexCallback: (key) {
          final index = shown.indexWhere(
            (entry) => ValueKey('shelf-${entry.anime.key}') == key,
          );
          return index < 0 ? null : index;
        },
        separatorBuilder: (_, i) => SizedBox(width: widget.isTV ? 26 : 12),
        itemBuilder: (_, i) {
          final e = shown[i];
          return RemoteFocusItem(
            key: ValueKey('shelf-${e.anime.key}'),
            index: i,
            child: PosterCard(
              key: ValueKey('$listKey-$category-${e.anime.key}'),
              anime: e.anime,
              cropped: true,
              width: cardWidth,
              isTV: widget.isTV,
              onOpen: () => widget.onOpen(e.anime),
              onFocused: () {
                widget.onFocused?.call(e);
              },
              caption: e.progress <= 0
                  ? 'Not started'
                  : e.anime.episodes > 0
                  ? '${e.progress}/${e.anime.episodes}'
                  : '${e.progress} watched',
              progress: episodeProgress(
                widget.services.store.entry(e.anime),
                watched: e.progress,
              ),
            ),
          );
        },
      ),
    ),
  );
}

class _CategoryInitialFocus extends StatefulWidget {
  const _CategoryInitialFocus({required this.selected, required this.child});
  final bool selected;
  final Widget child;
  @override
  State<_CategoryInitialFocus> createState() => _CategoryInitialFocusState();
}

class _CategoryInitialFocusState extends State<_CategoryInitialFocus> {
  @override
  void initState() {
    super.initState();
    if (widget.selected) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) Focus.of(context).requestFocus();
      });
    }
  }

  @override
  Widget build(BuildContext context) => widget.child;
}

/// How far into the episode in progress the viewer is, or null when nothing is
/// half watched: a finished episode (the store clears its position) or one the
/// tracker already counts as watched has no line.
double? episodeProgress(WatchEntry? h, {int watched = 0}) =>
    h == null ||
        h.episode <= watched ||
        h.position <= 0 ||
        h.duration <= 0 ||
        h.watched.contains(h.episode)
    ? null
    : (h.position / h.duration).clamp(0.0, 1.0);
