import 'dart:io';
import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

import '../core.dart';
import '../diagnostics.dart';
import '../services.dart';
import '../data/movies.dart';
import '../data/store.dart';
import 'movies_pages.dart';
import 'components.dart';
import 'device_status.dart';
import 'app_updates.dart';
import '../desktop/desktop_core.dart';
import '../desktop/launcher_shortcut.dart';
import '../desktop/system_install.dart';

Future<void> openSettings(
  BuildContext context,
  AppServices services,
  bool isTV, {
  String? section,
}) async {
  if (isTV) {
    await showGeneralDialog<void>(
      context: context,
      barrierDismissible: true,
      barrierLabel: 'Settings',
      barrierColor: const Color(0xb3080707),
      transitionDuration: MediaQuery.disableAnimationsOf(context)
          ? Duration.zero
          : const Duration(milliseconds: 240),
      pageBuilder: (context, a, b) => Align(
        alignment: Alignment.centerRight,
        child: SizedBox(
          width: (MediaQuery.sizeOf(context).width * .35).clamp(432, 560),
          child: ClipRRect(
            borderRadius: const BorderRadius.vertical(top: Radius.circular(20)),
            child: SettingsPage(
              services: services,
              isTV: true,
              section: section,
            ),
          ),
        ),
      ),
      transitionBuilder: (context, a, b, child) => SlideTransition(
        position: Tween<Offset>(
          begin: const Offset(1, 0),
          end: Offset.zero,
        ).animate(CurvedAnimation(parent: a, curve: Curves.easeOut)),
        child: child,
      ),
    );
  } else {
    await Navigator.push(
      context,
      OtakaseRoute<void>(
        builder: (_) =>
            SettingsPage(services: services, isTV: false, section: section),
      ),
    );
  }
}

// Friendly names for settings. The keys match the CLI config, so they stay
// the storage keys; only the label shown to the viewer changes.
const settingLabels = <String, String>{
  'SubsLanguage': 'Subtitle language',
  'AudioLanguages': 'Audio languages',
  'SubtitleFont': 'Subtitle font',
  'SubtitleSize': 'Subtitle size',
  'SubtitleColour': 'Subtitle colour',
  'SubtitleBackground': 'Subtitle background',
  'SubtitleOutline': 'Subtitle outline',
  'SubtitlePosition': 'Subtitle position',
  'SubOrDub': 'Sub or dub',
  'SubStyle': 'Subtitle style',
  'AutoAudioFallback': 'Fall back to other audio',
  'NextEpisodePrompt': 'Ask before next episode',
  'SkipOp': 'Skip openings',
  'SkipEd': 'Skip endings',
  'SkipRecap': 'Skip recaps',
  'NormalizeVolume': 'Even out volume',
  'SkipFiller': 'Skip filler episodes',
  'IntroDBSkipTimes': 'TheIntroDB skip times',
  'AnimeSkipTimes': 'Anime-Skip skip times',
  'ScoreOnCompletion': 'Rate when I finish a show',
  'DiscordPresence': 'Show what I watch in Discord',
  'SkipDelay': 'Skip delay',
  'Quality': 'Quality',
  'TorrentSources': 'Torrent sources',
  'TorrentOnMobileData': 'Torrents on mobile data',
  'PercentageToMarkComplete': 'Mark watched at',
  'ResumePlayback': 'Resume where you left off',
  'CheckUpdates': 'Check for updates',
  'ExternalMpv': 'Play in my own mpv',
  'MpvPath': 'mpv program',
  'DiscordWatchWithMe': 'Watch with me',
  'Notifications': 'Notifications',
  'Updates': 'Updates',
  'Diagnostics': 'Diagnostics',
  'About': 'About',
  'Content': 'Shows on this profile',
  'MovieLanguages': 'Source languages',
};

String settingLabel(String key) => settingLabels[key] ?? key;

class SettingsPage extends StatefulWidget {
  const SettingsPage({
    super.key,
    required this.services,
    required this.isTV,
    this.section,
    this.embedded = false,
  });
  final String? section;

  /// Shown inside the desktop settings page: no app bar, each group drawn
  /// as one card of rows.
  final bool embedded;
  final AppServices services;
  final bool isTV;
  @override
  State<SettingsPage> createState() => _SettingsPageState();
}

class _SettingsPageState extends State<SettingsPage> {
  final systemInstall = SystemInstall();
  final scope = RequestScope();
  AppStore get store => widget.services.store;
  @override
  void dispose() {
    scope.cancel();
    super.dispose();
  }

  Future<void> edit(String key, {String? title}) async {
    final controller = TextEditingController(text: store.text(key));
    final result = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(title ?? settingLabel(key)),
        content: RemoteTextInput(
          isTV: widget.isTV,
          builder: (focus) => TextField(
            focusNode: focus,
            autofocus: true,
            controller: controller,
            decoration: const InputDecoration(hintText: 'Enter a value'),
            onSubmitted: (v) => Navigator.pop(context, v),
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(context, controller.text),
            child: const Text('Save'),
          ),
        ],
      ),
    );
    if (result != null) {
      await store.set(key, result.trim());
      widget.services.catalog.clear();
    }
    // Dialog transitions can still paint the text field after its future completes.
    await Future<void>.delayed(const Duration(milliseconds: 250));
    controller.dispose();
  }

  Future<void> choose(String key, List<String> values) async {
    final result = await chooseValue(
      context,
      settingLabel(key),
      values,
      selected: store.text(key),
    );
    if (result != null) {
      await store.set(key, result);
    }
  }

  Future<void> numberChoice(String key, List<int> values) async {
    final result = await chooseValue(
      context,
      settingLabel(key),
      values.map((v) => '$v').toList(),
      selected: store.text(key),
    );
    if (result != null) {
      await store.set(key, int.parse(result));
    }
  }

  Future<void> chooseContent() async {
    final labels = contentLabels.values.toList();
    final result = await chooseValue(
      context,
      settingLabel('Content'),
      labels,
      selected: contentLabels[store.text('Content')] ?? '',
    );
    if (result == null) return;
    final key = contentLabels.entries.firstWhere((e) => e.value == result).key;
    await store.set('Content', key);
  }

  Future<void> chooseMovieLanguages() async {
    final names = movieLanguageNames.values.toList();
    final orders = [
      for (final first in names) [first, ...names.where((n) => n != first)],
    ];
    String label(List<String> order) =>
        order.map(languageTitle).join(', then ');
    final result = await chooseValue(
      context,
      settingLabel('MovieLanguages'),
      orders.map(label).toList(),
      selected: label(widget.services.movies.languages),
    );
    if (result == null) return;
    await store.set(
      'MovieLanguages',
      orders.firstWhere((o) => label(o) == result),
    );
  }

  Future<void> toggleMovieSource(String name) async {
    final off = {...store.strings('DisabledMovieSources')};
    if (!off.remove(name)) off.add(name);
    await store.set('DisabledMovieSources', off.toList()..sort());
  }

  Widget group(String title, IconData icon, List<Widget> children) =>
      widget.section != null && widget.section != title
      ? const SizedBox.shrink()
      : widget.embedded
      ? Padding(
          padding: const EdgeInsets.only(bottom: 28),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Padding(
                padding: const EdgeInsets.only(left: 4, bottom: 10),
                child: Text(
                  title.toUpperCase(),
                  style: mono.copyWith(fontSize: 15, letterSpacing: 2.5),
                ),
              ),
              Container(
                padding: const EdgeInsets.symmetric(vertical: 6),
                decoration: BoxDecoration(
                  color: panel,
                  border: Border.all(color: line),
                  borderRadius: BorderRadius.circular(16),
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    for (var i = 0; i < children.length; i++) ...[
                      if (i > 0)
                        const Divider(height: 1, indent: 16, endIndent: 16),
                      children[i],
                    ],
                  ],
                ),
              ),
            ],
          ),
        )
      : Padding(
          padding: const EdgeInsets.only(top: 24, bottom: 8),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(24, 8, 24, 12),
                child: Column(
                  children: [
                    const Divider(),
                    const SizedBox(height: 16),
                    Row(
                      children: [
                        Icon(icon, color: muted, size: 18),
                        const SizedBox(width: 12),
                        Expanded(child: SectionLabel(title)),
                      ],
                    ),
                  ],
                ),
              ),
              ...children,
            ],
          ),
        );
  Widget row(
    String key,
    String description, {
    VoidCallback? onTap,
    String? value,
    IconData icon = LucideIcons.slidersHorizontal,
    bool enabled = true,
  }) => Padding(
    padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 2),
    child: FocusBox(
      isTV: widget.isTV,
      radius: 12,
      onPressed: enabled ? onTap : null,
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 12),
      child: Row(
        children: [
          Icon(icon, size: 21, color: enabled ? fg : muted),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  settingLabel(key),
                  style: uiStyle(
                    context,
                    weight: FontWeight.w500,
                    color: enabled ? fg : muted,
                  ),
                ),
                if (value != null)
                  Padding(
                    padding: const EdgeInsets.only(top: 5),
                    child: Text(
                      value,
                      style: uiStyle(
                        context,
                        size: 15,
                        color: enabled ? accentText : muted,
                        weight: FontWeight.w500,
                      ),
                    ),
                  ),
                const SizedBox(height: 5),
                Text(
                  description,
                  style: uiStyle(context, size: 15, color: muted),
                ),
              ],
            ),
          ),
          if (onTap != null)
            const Icon(LucideIcons.chevronRight, size: 18, color: muted),
        ],
      ),
    ),
  );
  Widget toggle(
    String key,
    String description, {
    IconData icon = LucideIcons.slidersHorizontal,
  }) => Padding(
    padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 2),
    child: FocusBox(
      isTV: widget.isTV,
      radius: 12,
      onPressed: () => store.set(key, !store.flag(key)),
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      child: Row(
        children: [
          Icon(icon, size: 21, color: fg),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  settingLabel(key),
                  style: uiStyle(context, weight: FontWeight.w500),
                ),
                const SizedBox(height: 5),
                Text(
                  description,
                  style: uiStyle(context, size: 15, color: muted),
                ),
              ],
            ),
          ),
          ExcludeFocus(
            child: IgnorePointer(
              child: Switch(
                value: store.flag(key),
                activeThumbColor: fgBright,
                onChanged: (_) {},
              ),
            ),
          ),
        ],
      ),
    ),
  );
  @override
  Widget build(BuildContext context) => widget.embedded
      ? ListenableBuilder(
          listenable: store,
          builder: (context, _) => content(context),
        )
      : Scaffold(
          backgroundColor: panel,
          appBar: AppBar(
            backgroundColor: panel,
            title: Text(widget.section ?? 'Settings'),
            actions: [
              DeviceStatus(isTV: widget.isTV),
              const SizedBox(width: 24),
            ],
            leading: IconButton(
              tooltip: 'Back',
              autofocus: true,
              onPressed: () => Navigator.pop(context),
              icon: const Icon(LucideIcons.arrowLeft),
            ),
          ),
          body: SafeArea(
            top: false,
            child: ListenableBuilder(
              listenable: store,
              builder: (context, child) => content(context),
            ),
          ),
        );

  Widget content(BuildContext context) => ListView(
    padding: EdgeInsets.only(bottom: contentBottom(context)),
    children: [
      if (widget.section == null && !widget.embedded)
        const Padding(
          padding: EdgeInsets.symmetric(horizontal: 24),
          child: PageHeading('Settings', '設'),
        ),
      group('Appearance', LucideIcons.palette, [
        row('Theme', 'Warm dark appearance.', value: 'Shu / Dark'),
      ]),
      group('Subtitles & languages', LucideIcons.captions, [
        row(
          'SubsLanguage',
          'Subtitle language priority, comma separated. Use off to disable.',
          value: store.text('SubsLanguage'),
          onTap: () => edit('SubsLanguage'),
        ),
        row(
          'AudioLanguages',
          'Preferred audio language order.',
          value: store.text('AudioLanguages'),
          onTap: () => edit('AudioLanguages'),
        ),
        row(
          'SubtitleFont',
          'Bundled font for Japanese text and ASS signs.',
          value: store.text('SubtitleFont'),
        ),
        row(
          'SubtitleSize',
          'Size for subtitle text.',
          value: store.text('SubtitleSize'),
          onTap: () => numberChoice('SubtitleSize', [
            24,
            30,
            36,
            40,
            44,
            48,
            54,
            60,
            72,
          ]),
        ),
        row(
          'SubtitleColour',
          'Text colour.',
          value: store.text('SubtitleColour'),
          onTap: () => choose('SubtitleColour', [
            '#FFFFFF',
            '#FFFFCC',
            '#FFF3A3',
            '#D84836',
            '#A6E3FF',
          ]),
        ),
        row(
          'SubtitleBackground',
          'Subtitle background colour.',
          value: store.text('SubtitleBackground'),
          onTap: () => choose('SubtitleBackground', [
            '#00000000',
            '#80000000',
            '#CC000000',
          ]),
        ),
        row(
          'SubtitleOutline',
          'Outline thickness.',
          value: store.text('SubtitleOutline'),
          onTap: () => numberChoice('SubtitleOutline', [0, 1, 2, 3, 4, 5]),
        ),
        row(
          'SubtitlePosition',
          'Vertical placement, higher values move text down.',
          value: store.text('SubtitlePosition'),
          onTap: () =>
              numberChoice('SubtitlePosition', [10, 25, 50, 75, 90, 95, 100]),
        ),
        row(
          'SubStyle',
          'soft keeps the release styling (ASS signs); plain uses the subtitle settings below.',
          value: store.text('SubStyle'),
          onTap: () => choose('SubStyle', ['soft', 'plain']),
        ),
        row(
          'SubOrDub',
          'Preferred stream.',
          value: store.text('SubOrDub').toUpperCase(),
          onTap: () => choose('SubOrDub', ['sub', 'dub']),
        ),
        toggle(
          'AutoAudioFallback',
          'Try the other stream when unavailable. Unstable.',
          icon: LucideIcons.globe,
        ),
      ]),
      group('Movies & shows', LucideIcons.clapperboard, [
        row(
          'Content',
          'Each profile keeps its own choice, so one can show only movies '
              'and another only anime.',
          value: contentLabels[store.text('Content')] ?? store.text('Content'),
          icon: LucideIcons.users,
          onTap: chooseContent,
        ),
        row(
          'MovieLanguages',
          'A film or episode is looked for in the first language, then the '
              'next. vidsrc is English; 8Filmai and Filmukas are Lithuanian.',
          value: widget.services.movies.languages
              .map(languageTitle)
              .join(', then '),
          icon: LucideIcons.languages,
          onTap: chooseMovieLanguages,
        ),
        FutureBuilder(
          // Asked only while this group shows; the core lists the sources.
          future: widget.section == null || widget.section == 'Movies & shows'
              ? widget.services.movies.sources().catchError(
                  (_) => knownMovieSources,
                )
              : null,
          initialData: knownMovieSources,
          builder: (context, snap) => Column(
            children: [
              for (final source in snap.data ?? knownMovieSources)
                row(
                  source.label,
                  [
                    languageTitle(source.languageName),
                    source.series ? 'movies and series' : 'movies',
                    ?store.providerHealth['movie:${source.name}'],
                  ].join(' · '),
                  value: widget.services.movies.sourceEnabled(source.name)
                      ? 'On'
                      : 'Off',
                  icon: LucideIcons.server,
                  onTap: () => toggleMovieSource(source.name),
                ),
            ],
          ),
        ),
        ListenableBuilder(
          listenable: widget.services.trakt,
          builder: (context, _) {
            final trakt = widget.services.trakt;
            return row(
              'Trakt',
              trakt.signedIn
                  ? 'Watchlist, watched and progress sync. Tap to sign out.'
                  : 'Sign in to sync your watchlist, what you watched and where you stopped.',
              value: trakt.signedIn
                  ? (trakt.username.isEmpty ? 'Signed in' : trakt.username)
                  : 'Not signed in',
              icon: LucideIcons.listVideo,
              onTap: () async {
                if (!trakt.signedIn) {
                  await traktSignIn(context, widget.services, widget.isTV);
                  return;
                }
                final yes = await showDialog<bool>(
                  context: context,
                  builder: (c) => AlertDialog(
                    title: const Text('Sign out of Trakt?'),
                    actions: [
                      TextButton(
                        autofocus: true,
                        onPressed: () => Navigator.pop(c, false),
                        child: const Text('Stay'),
                      ),
                      TextButton(
                        onPressed: () => Navigator.pop(c, true),
                        child: const Text('Sign out'),
                      ),
                    ],
                  ),
                );
                if (yes == true) await trakt.signOut();
              },
            );
          },
        ),
      ]),
      group('Playback', LucideIcons.circlePlay, [
        toggle(
          'NextEpisodePrompt',
          'Ask before the next episode. Off enables the end countdown.',
        ),
        toggle('SkipOp', 'Automatically skip verified intro ranges.'),
        toggle('SkipEd', 'Automatically skip verified ending ranges.'),
        toggle('SkipRecap', 'Automatically skip verified recap ranges.'),
        toggle(
          'NormalizeVolume',
          'Raise quiet dialogue and soften loud effects.',
          icon: LucideIcons.audioLines,
        ),
        row(
          'SkipDelay',
          'Wait this many seconds before an automatic skip.',
          value: '${store.integer('SkipDelay')} s',
          onTap: () => numberChoice('SkipDelay', [0, 1, 2, 3, 5, 10]),
        ),
        toggle(
          'SkipFiller',
          'Skip verified filler when advancing automatically.',
        ),
        toggle(
          'IntroDBSkipTimes',
          'Also look skip times up in TheIntroDB. AniSkip is always asked.',
          icon: LucideIcons.flag,
        ),
        toggle(
          'AnimeSkipTimes',
          'Also look skip times up in Anime-Skip.',
          icon: LucideIcons.flag,
        ),
        toggle(
          'ScoreOnCompletion',
          'Ask for a score after the last episode of a finished show.',
          icon: LucideIcons.star,
        ),
        if (isDesktop)
          toggle(
            'DiscordPresence',
            store.adultAllowed
                ? 'Your Discord status shows the show and episode playing. 18+ titles never show.'
                : 'Your Discord status shows the show and episode playing.',
            icon: LucideIcons.messageCircle,
          ),
        row(
          'Quality',
          'Default video height, or best.',
          value: store.text('Quality'),
          onTap: () => choose('Quality', ['best', '1080', '720', '480', '360']),
        ),
        toggle(
          'TorrentSources',
          'Play from other viewers when no site has the episode. '
              'They can see your IP address.',
          icon: LucideIcons.share2,
        ),
        toggle(
          'TorrentOnMobileData',
          'Torrents download the whole episode. Off waits for Wi-Fi.',
          icon: LucideIcons.signal,
        ),
        toggle('ResumePlayback', 'Resume locally saved positions.'),
        if (isDesktop) ...[
          toggle(
            'ExternalMpv',
            'Open episodes in your installed mpv, with your mpv.conf, scripts and keys.',
            icon: LucideIcons.squareArrowOutUpRight,
          ),
          if (store.flag('ExternalMpv'))
            row(
              'MpvPath',
              'The mpv to run: a name on PATH or a full path.',
              value: store.text('MpvPath'),
              onTap: () => edit('MpvPath'),
            ),
          if (systemInstall.available)
            FutureBuilder<bool>(
              future: systemInstall.installed(),
              builder: (context, done) => row(
                'Add to apps and terminal',
                Platform.isMacOS
                    ? 'Adds the otakase command to your terminal.'
                    : 'Adds Otakase to your app menu and the otakase command to your terminal.',
                value: done.data == true ? 'Added' : '',
                icon: LucideIcons.squareTerminal,
                onTap: () async {
                  await runSystemInstall(context, systemInstall);
                  if (mounted) setState(() {});
                },
              ),
            ),
          row(
            'Launcher shortcut',
            'A key that opens the launcher from anywhere.',
            value: '',
            onTap: () => Navigator.push(
              context,
              OtakaseRoute<void>(
                builder: (_) => LauncherShortcutPage(services: widget.services),
              ),
            ),
          ),
        ],
        row(
          'PercentageToMarkComplete',
          'Mark an episode watched on stop after this percentage.',
          value: '${store.integer('PercentageToMarkComplete')}%',
          onTap: () => numberChoice('PercentageToMarkComplete', [
            70,
            75,
            80,
            85,
            90,
            95,
            100,
          ]),
        ),
      ]),
      group('About & updates', LucideIcons.shield, [
        toggle(
          'CheckUpdates',
          'Check for app updates at startup.',
          icon: LucideIcons.download,
        ),
        AppUpdatePanel(
          updates: widget.services.updates,
          isTV: widget.isTV,
          autoCheck: store.flag('CheckUpdates'),
        ),
      ]),
    ],
  );
}

class DiagnosticsHub extends StatefulWidget {
  const DiagnosticsHub({super.key, required this.services, required this.isTV});
  final AppServices services;
  final bool isTV;
  @override
  State<DiagnosticsHub> createState() => _DiagnosticsHubState();
}

class _DiagnosticsHubState extends State<DiagnosticsHub> {
  final scope = RequestScope();
  List<String> sources = [];
  // Shown only while the viewer's AniList allows 18+ content.
  List<String> adultSources = [];
  bool checking = false;
  @override
  void initState() {
    super.initState();
    widget.services.store.addListener(loadAdult);
    unawaited(
      widget.services.resolver
          .providerOrder()
          .then((v) {
            if (mounted) setState(() => sources = v);
          })
          .catchError((_) {}),
    );
    loadAdult();
  }

  void loadAdult() {
    unawaited(
      widget.services.resolver
          .adultOrder()
          .then((v) {
            if (mounted && !listEquals(v, adultSources)) {
              setState(() => adultSources = v);
            }
          })
          .catchError((_) {}),
    );
  }

  @override
  void dispose() {
    widget.services.store.removeListener(loadAdult);
    scope.cancel();
    super.dispose();
  }

  Future<void> check() async {
    setState(() => checking = true);
    final adult = widget.services.store.adultAllowed ? adultSources : [];
    for (final provider in [...sources, ...adult]) {
      if (scope.cancelled) break;
      widget.services.store.health(provider, 'Checking');
      try {
        await widget.services.core.request(
          {
            'operation': 'search',
            'provider': provider,
            'mode': widget.services.store.text('SubOrDub'),
            // A title each kind of source is sure to carry.
            'query': adult.contains(provider) ? 'Overflow' : 'Frieren',
          },
          scope: scope,
          timeout: const Duration(seconds: 15),
        );
        widget.services.store.health(provider, 'Working');
      } catch (_) {
        widget.services.store.health(provider, 'Unavailable');
      }
    }
    if (mounted) setState(() => checking = false);
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: const Text('Diagnostics'),
      leading: IconButton(
        tooltip: 'Back',
        onPressed: () => Navigator.pop(context),
        icon: const Icon(LucideIcons.arrowLeft),
      ),
    ),
    body: ListenableBuilder(
      listenable: widget.services.store,
      builder: (context, _) => ListView(
        padding: EdgeInsets.fromLTRB(24, 16, 24, contentBottom(context)),
        children: [
          const PageHeading('Diagnostics', '設'),
          Text(
            'Playback chooses a source automatically and remembers what worked for each show.',
            style: uiStyle(context, color: muted),
          ),
          const SizedBox(height: 24),
          AppButton(
            label: 'HLS / ASS probe',
            onPressed: () => Navigator.push(
              context,
              OtakaseRoute<void>(
                builder: (_) => const DiagnosticsPage(probe: true),
              ),
            ),
            isTV: widget.isTV,
          ),
          const SizedBox(height: 32),
          const SectionLabel('Sources'),
          const SizedBox(height: 12),
          for (var i = 0; i < sources.length; i++)
            Container(
              padding: const EdgeInsets.symmetric(vertical: 16),
              decoration: const BoxDecoration(
                border: Border(bottom: BorderSide(color: line)),
              ),
              child: Row(
                children: [
                  Text(
                    '${i + 1}'.padLeft(2, '0'),
                    style: uiStyle(context, color: muted),
                  ),
                  const SizedBox(width: 20),
                  // Sources are never named to the viewer.
                  Expanded(
                    child: Text('Source ${i + 1}', style: uiStyle(context)),
                  ),
                  Text(
                    widget.services.store.providerHealth[sources[i]] ??
                        'Not checked',
                    style: uiStyle(context, size: 15, color: muted),
                  ),
                ],
              ),
            ),
          if (widget.services.store.adultAllowed)
            for (final source in adultSources)
              Container(
                padding: const EdgeInsets.symmetric(vertical: 16),
                decoration: const BoxDecoration(
                  border: Border(bottom: BorderSide(color: line)),
                ),
                child: Row(
                  children: [
                    Text('18', style: uiStyle(context, color: muted)),
                    const SizedBox(width: 20),
                    Expanded(
                      child: Text('18+ source', style: uiStyle(context)),
                    ),
                    Text(
                      widget.services.store.providerHealth[source] ??
                          'Not checked',
                      style: uiStyle(context, size: 15, color: muted),
                    ),
                  ],
                ),
              ),
          const SizedBox(height: 24),
          AppButton(
            label: checking ? 'Checking' : 'Check all',
            onPressed: checking ? null : check,
            isTV: widget.isTV,
          ),
        ],
      ),
    ),
  );
}
