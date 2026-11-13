import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../core.dart';
import '../data/models.dart';
import '../data/continuation.dart';
import '../data/movies.dart';
import '../data/tracking.dart';
import '../services.dart';
import '../desktop/desktop_core.dart';
import '../desktop/desktop_home.dart';
import 'components.dart';
import 'discovery_home.dart';
import '../data/trakt.dart' show MediaTitle;
import 'movies_pages.dart' show shelvesOf, titleCardsOf, titleCaption;

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
              borderRadius: BorderRadius.circular(KageRadius.spine),
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
    this.label,
    this.kicker,
    this.action,
    this.onMenu,
  });
  final WatchEntry entry;

  /// Long press, right click or the remote's Info key: more for it.
  final VoidCallback? onMenu;

  /// Replace the corner note and the play button's name, for a trending
  /// pick in the banner.
  final String? kicker, action;

  /// Replaces the episode line, for a film or series.
  final String? label;
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
    return CardMenu(
      onMenu: onMenu,
      child: Container(
        key: const Key('resume-card'),
        height: height,
        margin: EdgeInsets.symmetric(horizontal: isTV ? 0 : 20),
        decoration: BoxDecoration(
          borderRadius: BorderRadius.circular(KageRadius.panel),
          border: isTV ? null : Border.all(color: resumeEdge),
        ),
        child: ClipRRect(
          borderRadius: BorderRadius.circular(isTV ? 0 : KageRadius.panel),
          child: GestureDetector(
            behavior: HitTestBehavior.opaque,
            onTap: onOpen,
            onLongPress: onMenu,
            onSecondaryTap: onMenu,
            child: Stack(
              fit: StackFit.expand,
              children: [
                if (showArtwork) HeroArt(anime, height: height),
                // The page heading already says this is where you left off,
                // so a card you are partway through needs no corner note.
                if (kicker != null || entry.position == 0)
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
                        kicker ??
                            (label == 'MOVIE'
                                ? 'Up next'
                                : 'Your next episode'),
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
                if (isTV && label == null)
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
                        borderRadius: BorderRadius.circular(KageRadius.tag),
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
                          label ??
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
                              label:
                                  action ??
                                  (entry.position > 0 ? 'Resume' : 'Play'),
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

  /// How often Continue watching was worked out, for tests.
  @visibleForTesting
  static int continuationBuilds = 0;
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
  late final AppLifecycleListener lifecycle = AppLifecycleListener(
    onResume: () {
      if (mounted && showsTitles) {
        unawaited(widget.services.movies.syncIfStale());
      }
    },
  );

  /// Whether films and series share Home with anime on this profile.
  bool get showsTitles =>
      contentModeOf(widget.services.store) == ContentMode.both;
  bool titlesSynced = false;

  /// Which pick the phone and TV banner shows. It only moves when the
  /// viewer swipes or picks a dot: a resume card that changes on its own is
  /// easy to mis-tap.
  int banner = 0;
  void moveBanner(int step) => setState(() => banner += step);

  void titlesChanged() {
    if (mounted && showsTitles) setState(() {});
  }

  // Continue watching and Up next are worked out once per change of the
  // lists, the history or the minute, not on every rebuild.
  int snapshotsVersion = 0, storeVersion = 0, minute = 0;
  Object? continuationKey;
  Continuation? continuationCache;
  void storeChanged() => storeVersion++;

  Continuation continuationNow() {
    final key = (identity, snapshotsVersion, storeVersion, minute);
    if (key == continuationKey && continuationCache != null) {
      return continuationCache!;
    }
    HomePage.continuationBuilds++;
    final s = widget.services, store = s.store;
    final result = buildContinuation(
      snapshots.values.expand((e) => e).toList(),
      store.history,
      // 18+ titles stay off Home even when watched; lists and search keep
      // them.
      adult: false,
      now: DateTime.now(),
    );
    continuationKey = key;
    continuationCache = result;
    // Telling the alerts what is coming happens after the frame, not
    // while building it.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      s.episodeAlerts.watch(result.upNext, on: store.flag('EpisodeAlerts'));
    });
    return result;
  }

  /// Pulls the film and series watchlist and progress once titles show.
  Future<void> syncTitles({bool force = false}) async {
    titlesSynced = true;
    try {
      await widget.services.trakt.load();
      await widget.services.movies.sync(force: force);
    } catch (_) {
      // Home keeps what is saved on this device.
    }
  }

  @override
  void initState() {
    super.initState();
    lifecycle;
    widget.services.movies.addListener(titlesChanged);
    widget.services.store.addListener(storeChanged);
    if (isDesktop) unawaited(loadFeatured());
    clock = Timer.periodic(const Duration(minutes: 1), (_) {
      if (mounted) setState(() => minute++);
      // Picks up films and episodes watched on other devices meanwhile.
      if (mounted && widget.active && showsTitles) {
        unawaited(
          widget.services.movies.syncIfStale(gap: const Duration(minutes: 5)),
        );
      }
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

  /// Trending films and series for the desktop hero, alternating.
  List<MediaTitle> featuredTitles = [];

  /// Trending picks for the desktop hero.
  /// Whether trending picks were asked for. Phones and TVs only need them
  /// once the banner shows something to resume.
  bool featureRequested = false;

  Future<void> loadFeatured() async {
    featureRequested = true;
    if (showsTitles) {
      unawaited(
        shelvesOf(widget.services)
            .load()
            .then((rows) {
              final films = rows['Trending movies'] ?? const <MediaTitle>[];
              final shows = rows['Trending shows'] ?? const <MediaTitle>[];
              if (mounted) {
                setState(
                  () => featuredTitles = [
                    for (
                      var i = 0;
                      i < math.max(films.length, shows.length);
                      i++
                    ) ...[
                      if (i < films.length) films[i],
                      if (i < shows.length) shows[i],
                    ],
                  ],
                );
              }
            })
            .catchError((Object _) {}),
      );
    }
    try {
      // Home never shows 18+ titles, whatever the account allows.
      final data = await widget.services.catalog.home(adult: false);
      if (mounted) setState(() => featured = data);
    } catch (_) {
      // The hero falls back to what you are watching.
    }
  }

  List<HeroPick> heroPicks(
    List<WatchEntry> continuing,
    Map<String, MovieEntry> titleProgress,
  ) {
    final picks = <HeroPick>[];
    final cards = titleCardsOf(widget.services);
    for (final e in continuing.take(2)) {
      if (titleProgress[e.anime.key] case final t?) {
        final started = t.started;
        final what = t.title.isShow && t.episode > 0 ? ' ${t.code}' : '';
        picks.add(
          HeroPick(
            entry: e,
            label: t.title.isShow && t.episode > 0 ? t.code : 'MOVIE',
            anime: e.anime,
            kicker: 'Continue · ${titleCaption(t)}',
            action: started ? 'Resume$what' : 'Play$what',
            onPlay: () => widget.onResume(e),
            onDetails: () => widget.onOpen(e.anime),
          ),
        );
        continue;
      }
      final left = e.duration > 0 ? ((e.duration - e.position) / 60).ceil() : 0;
      picks.add(
        HeroPick(
          entry: e,
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
    final anime = [
      for (final (i, a) in (featured?.trending ?? const <Anime>[]).indexed)
        if (!a.isAdult && !seen.contains(a.key)) (i + 1, a),
    ];
    final films = [
      if (showsTitles)
        for (final t in featuredTitles)
          if (!seen.contains(cards.card(t).key)) t,
    ];
    // Trending anime and films take turns after what you are watching.
    for (var i = 0; picks.length < 5; i++) {
      if (i >= anime.length && i >= films.length) break;
      if (i < anime.length) {
        final (rank, a) = anime[i];
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
      if (i < films.length && picks.length < 5) {
        final t = films[i], card = cards.card(t);
        picks.add(
          HeroPick(
            anime: card,
            label: t.isShow ? 'SERIES' : 'MOVIE',
            kicker: t.isShow ? 'Trending series' : 'Trending movie',
            action: t.isShow ? 'Play episode 1' : 'Play movie',
            onPlay: () => widget.onResume(WatchEntry(anime: card)),
            onDetails: () => widget.onOpen(card),
          ),
        );
      }
    }
    return picks;
  }

  Future<void> refresh({bool forceSync = false}) async {
    if (isDesktop || featureRequested) unawaited(loadFeatured());
    await Future.wait([
      if (showsTitles) syncTitles(force: forceSync),
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
      MediaQuery.disableAnimationsOf(context) ? Duration.zero : KageMotion.base,
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

  /// A TV remote resting on a Continue watching card puts it in the hero.
  void focusWatch(WatchEntry entry) {
    artTimer?.cancel();
    artTimer = Timer(
      MediaQuery.disableAnimationsOf(context) ? Duration.zero : KageMotion.base,
      () {
        if (mounted) setState(() => focused = entry);
      },
    );
  }

  /// The hero pick for what a TV remote rests on: its Continue watching pick
  /// when it has one, otherwise the next episode from the list.
  HeroPick focusPick(WatchEntry e, List<HeroPick> picks) =>
      picks.where((p) => p.anime.key == e.anime.key).firstOrNull ??
      HeroPick(
        entry: e,
        anime: e.anime,
        kicker: 'On your list · Ep ${e.episode}',
        action: 'Play episode ${e.episode}',
        onPlay: () => widget.onResume(e),
        onDetails: () => widget.onOpen(e.anime),
      );

  @override
  void dispose() {
    widget.services.movies.removeListener(titlesChanged);
    widget.services.store.removeListener(storeChanged);
    lifecycle.dispose();
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
      snapshotsVersion++;
      focused = null;
    }
    final continuation = continuationNow();
    // Films and series join Continue watching, the big resume card and the
    // desktop hero, and the Trakt watchlist gets a row.
    final titles = showsTitles;
    if (titles && !titlesSynced) {
      titlesSynced = true;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) unawaited(syncTitles());
      });
    }
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
    final continuing = mergeRecent(
      continuation.continueWatching
          .where(
            (e) => !s.store.continueHidden(e.anime.key, e.episode, e.updated),
          )
          .toList(),
      titleEntries,
    );
    // Long press, right click or the remote's Info key on a Continue
    // watching card offers to take it off the row.
    Future<void> removeContinue(Anime card) async {
      const remove = 'Remove from Continue watching';
      final choice = await chooseValue(context, card.title, [
        remove,
        'Cancel',
      ], selected: 'Cancel');
      if (choice != remove || !mounted) return;
      if (titleProgress[card.key] case final t?) {
        await s.movies.hideContinue(t);
      } else if (continuing.where((e) => e.anime.key == card.key).firstOrNull
          case final h?) {
        await s.store.hideContinue(h.anime.key, h.episode, h.updated);
      }
      if (mounted) setState(() {});
    }

    final resume = continuing.firstOrNull;
    // Desktop and TV share one composition: a big hero, then wide Continue
    // watching cards. The phone keeps its banner and poster shelf.
    final wide = isDesktop || widget.isTV;
    final shelf = wide ? continuing : continuing.skip(1).toList();
    final watchlist = titles && s.trakt.signedIn ? s.movies.watchlist : null;
    String? titleLabel(WatchEntry e) => switch (titleProgress[e.anime.key]) {
      final t? => t.title.isShow && t.episode > 0 ? t.code : 'MOVIE',
      _ => null,
    };
    // The phone and TV banner takes turns like the desktop one: what you
    // are watching, then trending picks. It only shows once there is
    // something to resume; a TV list focus still takes it over.
    if (resume != null && !featureRequested) {
      featureRequested = true;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) unawaited(loadFeatured());
      });
    }
    final picks = resume == null
        ? const <HeroPick>[]
        : heroPicks(continuing, titleProgress);
    final pick = picks.isEmpty ? null : picks[banner % picks.length];
    final tvFocus = widget.isTV && focused != null && !focused!.anime.isAdult;
    final stage = tvFocus
        ? focused
        : pick == null
        ? null
        : pick.entry ?? WatchEntry(anime: pick.anime);
    Widget bannerCard(WatchEntry stage, {required bool tv}) {
      final shown = tvFocus ? null : pick;
      final card = ResumeCard(
        entry: stage,
        label: shown?.label ?? titleLabel(stage),
        kicker: shown == null || shown.entry != null ? null : shown.kicker,
        action: shown == null || shown.entry != null ? null : shown.action,
        isTV: widget.isTV,
        showArtwork: !tv,
        onResume: shown?.onPlay ?? () => widget.onResume(stage),
        onOpen: shown?.onDetails ?? () => widget.onOpen(stage.anime),
        onMenu: shown == null || shown.entry != null
            ? () => unawaited(removeContinue(stage.anime))
            : null,
      );
      return Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          GestureDetector(
            onHorizontalDragEnd: picks.length < 2
                ? null
                : (d) {
                    final v = d.primaryVelocity ?? 0;
                    if (v.abs() > 200) moveBanner(v < 0 ? 1 : -1);
                  },
            child: card,
          ),
          if (picks.length > 1 && !tvFocus)
            Padding(
              padding: EdgeInsets.fromLTRB(
                widget.isTV ? 48 : 20,
                12,
                widget.isTV ? 48 : 20,
                0,
              ),
              child: Row(
                key: const Key('home-banner-dots'),
                mainAxisAlignment: MainAxisAlignment.end,
                children: [
                  for (var i = 0; i < picks.length; i++)
                    GestureDetector(
                      onTap: () => moveBanner(i - banner % picks.length),
                      child: AnimatedContainer(
                        duration: KageMotion.slow,
                        margin: const EdgeInsets.only(left: 8),
                        width: i == banner % picks.length ? 26 : 8,
                        height: 8,
                        decoration: BoxDecoration(
                          color: i == banner % picks.length
                              ? shu
                              : fgBright.withValues(alpha: .3),
                          borderRadius: BorderRadius.circular(KageRadius.full),
                        ),
                      ),
                    ),
                ],
              ),
            ),
        ],
      );
    }

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
          if (wide)
            Builder(
              builder: (context) {
                final all = heroPicks(continuing, titleProgress);
                if (isDesktop) {
                  return DesktopHero(picks: all, topInset: widget.topInset);
                }
                // The TV hero shows what the remote rests on, or what you
                // are watching; a guest with nothing yet gets the discovery
                // intro below instead.
                final shown = tvFocus
                    ? [focusPick(focused!, all)]
                    : resume == null
                    ? const <HeroPick>[]
                    : all;
                if (shown.isEmpty && configured.isEmpty) {
                  return const SizedBox.shrink();
                }
                return DesktopHero(
                  key: const Key('tv-home-hero'),
                  picks: shown,
                  topInset: widget.topInset,
                  remote: true,
                );
              },
            ),
          if (wide) ...[
            DesktopContinueRow(
              entries: shelf,
              onResume: widget.onResume,
              onMenu: (e) => unawaited(removeContinue(e.anime)),
              onFocused: widget.isTV && !isDesktop ? focusWatch : null,
              minCard: isDesktop ? 380 : 260,
              label: (e) => switch (titleProgress[e.anime.key]) {
                final t? => titleCaption(t),
                _ => null,
              },
            ),
          ],
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
          if (!widget.isTV && stage != null) bannerCard(stage, tv: false),
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
                  setState(() {
                    snapshots[combined ? 'Combined' : service] = entries;
                    snapshotsVersion++;
                  });
                }
              },
              autofocus: stage == null && service == lists.first,
            ),
          if (!wide && shelf.isNotEmpty)
            PosterRow(
              title: 'Continue watching',
              items: shelf.map((e) => e.anime).toList(),
              isTV: widget.isTV,
              onOpen: widget.onOpen,
              onMenu: (a) => unawaited(removeContinue(a)),
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
          if (watchlist != null && watchlist.isEmpty)
            Padding(
              key: const Key('home-title-watchlist'),
              padding: EdgeInsets.fromLTRB(
                widget.isTV ? 48 : 20,
                28,
                widget.isTV ? 48 : 20,
                4,
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const SectionLabel('Movies & shows watchlist'),
                  const SizedBox(height: 8),
                  Text(
                    'Add movies and shows to your Trakt watchlist and they show up here.',
                    style: uiStyle(context, color: muted),
                  ),
                ],
              ),
            )
          else if (watchlist != null)
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
          // Without AniList or MAL, shows added to the list on this
          // device wait here.
          if (configured.isEmpty && store.localList('PLANNING').isNotEmpty)
            PosterRow(
              key: const Key('home-local-planning'),
              title: 'Plan to watch',
              items: store.localList('PLANNING'),
              isTV: widget.isTV,
              onOpen: widget.onOpen,
            ),
          if (configured.isEmpty)
            DiscoveryHome(
              key: discovery,
              services: s,
              titles: titles,
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
          await s.store.setCache('PublicListCache.${widget.service}', {
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
        final cached = widget.services.store.cache(
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
            setState(() => error = errorMessage(e));
          }
        } else {
          setState(() => error = errorMessage(e));
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
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(KageRadius.menu),
      ),
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
                      radius: KageRadius.input,
                      child: Container(
                        constraints: BoxConstraints(
                          minHeight: widget.isTV ? 48 : 44,
                        ),
                        padding: const EdgeInsets.symmetric(horizontal: 13),
                        decoration: BoxDecoration(
                          border: Border.all(color: line),
                          borderRadius: BorderRadius.circular(KageRadius.input),
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
