import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../data/models.dart';
import '../data/tracking.dart';
import '../services.dart';
import 'components.dart';

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
    this.autofocus = true,
  });
  final WatchEntry entry;
  final bool isTV, autofocus;
  final VoidCallback onResume;
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
        child: Stack(
          fit: StackFit.expand,
          children: [
            HeroArt(anime, height: height),
            Positioned(
              top: 13,
              left: isTV ? 48 : 14,
              child: Container(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
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
                      style: mono.copyWith(fontSize: isTV ? 20 : 15, color: fg),
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
  });
  final AppServices services;
  final bool isTV;
  final void Function(Anime) onOpen;
  final void Function(WatchEntry) onResume;
  final VoidCallback onSearch, onAccounts;
  @override
  State<HomePage> createState() => _HomePageState();
}

class _HomePageState extends State<HomePage> {
  WatchEntry? focused;
  Timer? artTimer;
  void focusEntry(ListEntry entry) {
    artTimer?.cancel();
    artTimer = Timer(
      MediaQuery.disableAnimationsOf(context)
          ? Duration.zero
          : const Duration(milliseconds: 180),
      () {
        if (!mounted) return;
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
    artTimer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final s = widget.services, store = s.store;
    final continuing = store.continuing;
    final resume = continuing.firstOrNull;
    final stage =
        widget.isTV && focused != null && store.visible(focused!.anime)
        ? focused
        : resume;
    final configured = ['AniList', 'MAL']
        .where(
          (service) =>
              s.auth.signedIn(service) ||
              store
                  .text(
                    service == 'MAL'
                        ? 'MyAnimeListUsername'
                        : 'AniListUsername',
                  )
                  .trim()
                  .isNotEmpty,
        )
        .toList();
    return ListView(
      key: const PageStorageKey('home-scroll'),
      padding: EdgeInsets.only(bottom: contentBottom(context)),
      children: [
        Padding(
          padding: EdgeInsets.fromLTRB(
            widget.isTV ? 48 : 20,
            12,
            widget.isTV ? 48 : 20,
            16,
          ),
          child: const PageHeading('Back to your story', '続'),
        ),
        if (stage != null)
          ResumeCard(
            entry: stage,
            isTV: widget.isTV,
            onResume: () => widget.onResume(stage),
          ),
        for (final service in configured)
          AccountBlock(
            key: ValueKey('account-$service'),
            service: service,
            services: s,
            isTV: widget.isTV,
            onOpen: widget.onOpen,
            onFocused: focusEntry,
            autofocus: stage == null && service == configured.first,
          ),
        if (continuing.length > 1)
          PosterRow(
            title: 'Continue watching',
            items: continuing.skip(1).map((e) => e.anime).toList(),
            isTV: widget.isTV,
            onOpen: widget.onOpen,
            progress: (a) {
              final h = store.entry(a)!;
              return h.duration > 0 ? h.position / h.duration : null;
            },
            captions: (a) => 'Episode ${store.entry(a)!.episode}',
          ),
        if (configured.isEmpty)
          Padding(
            padding: EdgeInsets.symmetric(
              horizontal: widget.isTV ? 48 : 20,
              vertical: 24,
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'Your lists belong here.',
                  style: uiStyle(context, size: 21, weight: FontWeight.w600),
                ),
                const SizedBox(height: 8),
                Text(
                  'Sign in to sync your progress, or add a public username through More.',
                  style: uiStyle(context, color: muted),
                ),
                const SizedBox(height: 12),
                Wrap(
                  spacing: 12,
                  children: [
                    AppButton(
                      label: 'Accounts',
                      isTV: widget.isTV,
                      onPressed: widget.onAccounts,
                    ),
                    AppButton(
                      label: 'Search anime',
                      isTV: widget.isTV,
                      autofocus: stage == null,
                      onPressed: widget.onSearch,
                    ),
                  ],
                ),
              ],
            ),
          ),
      ],
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
    this.autofocus = false,
  });
  final String service;
  final AppServices services;
  final bool isTV, autofocus;
  final void Function(Anime) onOpen;
  final void Function(ListEntry)? onFocused;
  @override
  State<AccountBlock> createState() => _AccountBlockState();
}

class _AccountBlockState extends State<AccountBlock> {
  List<ListEntry> entries = [];
  String category = 'CURRENT', stamp = '';
  bool busy = true;
  String? error;
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
  String get signature =>
      '${widget.services.auth.account(widget.service)?.token}:$username:${widget.services.store.flag('AdultContent')}:${widget.services.tracking.revision}';
  @override
  void initState() {
    super.initState();
    category = widget.services.store.text('ListCategory.${widget.service}');
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

  Future<void> load() async {
    final g = ++generation;
    stamp = signature;
    setState(() {
      busy = true;
      error = null;
    });
    try {
      final s = widget.services;
      List<ListEntry> result;
      if (s.auth.signedIn(widget.service)) {
        result = (await s.tracking.list(
          widget.service,
          adult: s.store.flag('AdultContent'),
        )).entries;
      } else {
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
      }
      if (mounted && g == generation) {
        setState(() => entries = result);
        if (widget.isTV && result.isNotEmpty && widget.autofocus) {
          widget.onFocused?.call(result.first);
        }
      }
    } catch (e) {
      if (mounted && g == generation) setState(() => error = e.toString());
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
          )
          .toList(),
    );
    if (selected != null && mounted) {
      setState(() => category = selected);
      unawaited(
        widget.services.store.set('ListCategory.${widget.service}', selected),
      );
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
                  '${widget.service} · $username${widget.services.auth.signedIn(widget.service) ? '' : ' · Public, read only'}',
                  style: uiStyle(context, size: 15, color: muted),
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
          if (!busy && error == null && shown.isEmpty)
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
            SizedBox(
              height:
                  (widget.isTV ? 194 : 184) +
                  126 * MediaQuery.textScalerOf(context).scale(1),
              child: ListView.separated(
                key: PageStorageKey('${widget.service}-$category'),
                controller: scroll,
                scrollDirection: Axis.horizontal,
                clipBehavior: Clip.none,
                padding: EdgeInsets.fromLTRB(side - 7, 15, side, 8),
                itemCount: shown.length,
                separatorBuilder: (_, i) =>
                    SizedBox(width: widget.isTV ? 26 : 12),
                itemBuilder: (_, i) {
                  final e = shown[i];
                  return PosterCard(
                    key: ValueKey('${widget.service}-$category-${e.anime.key}'),
                    anime: e.anime,
                    cropped: true,
                    width: widget.isTV ? 143 : 132,
                    isTV: widget.isTV,
                    onOpen: () => widget.onOpen(e.anime),
                    onFocused: () {
                      widget.onFocused?.call(e);
                    },
                    caption: e.progress > 0
                        ? '${e.progress} watched'
                        : 'Not started',
                    progress: e.progress > 0 && e.anime.episodes > 0
                        ? e.progress / e.anime.episodes
                        : null,
                  );
                },
              ),
            ),
        ],
      ),
    );
  }
}
