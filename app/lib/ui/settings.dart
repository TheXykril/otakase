import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

import '../core.dart';
import '../diagnostics.dart';
import '../services.dart';
import '../data/movies.dart';
import '../data/store.dart';
import 'components.dart';
import 'device_status.dart';
import 'app_updates.dart';
import '../desktop/desktop_core.dart';
import '../desktop/launcher_shortcut.dart';
import '../desktop/system_install.dart';
import '../host.dart';

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
  'HideEpisodeSpoilers': 'Hide episode spoilers',
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
  'PercentageToMarkComplete': 'Count movies & shows as watched at',
  'ResumePlayback': 'Resume where you left off',
  'CheckUpdates': 'Check for updates',
  'ExternalMpv': 'Play in my own mpv',
  'MpvPath': 'mpv program',
  'DiscordWatchWithMe': 'Watch with me',
  'Notifications': 'Notifications',
  'Updates': 'Updates',
  'Diagnostics': 'Diagnostics',
  'About': 'About',
  'Content': 'This profile shows',
  'MovieLanguages': 'Movie & show languages',
};

String settingLabel(String key) => settingLabels[key] ?? key;

/// Plain names for stored values the viewer picks from a list. The stored
/// value stays the same; only what the viewer reads changes.
const settingValueNames = <String, Map<String, String>>{
  'SubtitleColour': {
    '#FFFFFF': 'White',
    '#FFFFCC': 'Cream',
    '#FFF3A3': 'Yellow',
    '#D84836': 'Red',
    '#A6E3FF': 'Light blue',
  },
  'SubtitleBackground': {
    '#00000000': 'None',
    '#80000000': 'See-through dark',
    '#CC000000': 'Dark',
  },
  'SubStyle': {'soft': 'Original styling', 'plain': 'My subtitle settings'},
  'SubOrDub': {'sub': 'Subtitled', 'dub': 'Dubbed'},
  'Quality': {
    'best': 'Best available',
    '1080': '1080p',
    '720': '720p',
    '480': '480p',
    '360': '360p',
  },
};

/// How a stored setting value reads in settings.
String settingValue(AppStore store, String key) {
  final raw = store.text(key);
  return settingValueNames[key]?[raw] ?? raw;
}

/// The section that holds the Content choice and the movie sources.
const contentSection = 'What you watch';

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
    final names = settingValueNames[key] ?? const <String, String>{};
    String name(String v) => names[v] ?? v;
    final result = await chooseValue(
      context,
      settingLabel(key),
      values.map(name).toList(),
      selected: name(store.text(key)),
    );
    if (result != null) {
      await store.set(key, values.firstWhere((v) => name(v) == result));
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
          'Languages to try in order, separated by commas. Type off for no subtitles.',
          value: store.text('SubsLanguage'),
          onTap: () => edit('SubsLanguage'),
        ),
        row(
          'AudioLanguages',
          'Audio languages to try in order, separated by commas.',
          value: store.text('AudioLanguages'),
          onTap: () => edit('AudioLanguages'),
        ),
        row(
          'SubtitleFont',
          'Built into the app. Covers Japanese text and on-screen signs.',
          value: store.text('SubtitleFont'),
        ),
        row(
          'SubtitleSize',
          'How big subtitle text is.',
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
          'Colour of subtitle text.',
          value: settingValue(store, 'SubtitleColour'),
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
          'Box behind subtitle text.',
          value: settingValue(store, 'SubtitleBackground'),
          onTap: () => choose('SubtitleBackground', [
            '#00000000',
            '#80000000',
            '#CC000000',
          ]),
        ),
        row(
          'SubtitleOutline',
          'How thick the edge around subtitle text is.',
          value: store.text('SubtitleOutline'),
          onTap: () => numberChoice('SubtitleOutline', [0, 1, 2, 3, 4, 5]),
        ),
        row(
          'SubtitlePosition',
          'Height on screen. Higher numbers sit lower.',
          value: store.text('SubtitlePosition'),
          onTap: () =>
              numberChoice('SubtitlePosition', [10, 25, 50, 75, 90, 95, 100]),
        ),
        row(
          'SubStyle',
          'Original styling keeps the fonts and signs the subtitles came with. '
              'My subtitle settings uses the size, colour and position above.',
          value: settingValue(store, 'SubStyle'),
          onTap: () => choose('SubStyle', ['soft', 'plain']),
        ),
        row(
          'SubOrDub',
          'Which version to play when an episode has both.',
          value: settingValue(store, 'SubOrDub'),
          onTap: () => choose('SubOrDub', ['sub', 'dub']),
        ),
        toggle(
          'AutoAudioFallback',
          'Play the dubbed version when no subtitled one is found, or the other way round.',
          icon: LucideIcons.globe,
        ),
      ]),
      group(contentSection, LucideIcons.clapperboard, [
        row(
          'Content',
          'Anime, movies & shows, or both. Each profile keeps its own '
              'choice, so one can show only movies and another only anime.',
          value: contentLabels[store.text('Content')] ?? store.text('Content'),
          icon: LucideIcons.users,
          onTap: chooseContent,
        ),
        if (contentModeOf(store) != ContentMode.anime) ...[
          row(
            'MovieLanguages',
            'Movies and episodes are looked for in the first language, then '
                'the next. vidsrc is English; 8Filmai and Filmukas are Lithuanian.',
            value: widget.services.movies.languages
                .map(languageTitle)
                .join(', then '),
            icon: LucideIcons.languages,
            onTap: chooseMovieLanguages,
          ),
          FutureBuilder(
            // Asked only while this group shows; the core lists the sources.
            future: widget.section == null || widget.section == contentSection
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
                      source.series ? 'movies and shows' : 'movies only',
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
        ],
      ]),
      group('Playback', LucideIcons.circlePlay, [
        toggle(
          'HideEpisodeSpoilers',
          'Blur unwatched episode artwork and hide episode titles until you reveal them.',
          icon: LucideIcons.eyeOff,
        ),
        toggle(
          'NextEpisodePrompt',
          'Wait for you at the end of an episode. Off counts down and plays the next one.',
        ),
        toggle('SkipOp', 'Jump past the opening when its times are known.'),
        toggle('SkipEd', 'Jump past the ending when its times are known.'),
        toggle('SkipRecap', 'Jump past recaps when their times are known.'),
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
          'Leave out known filler episodes when the next episode plays on its own.',
        ),
        toggle(
          'IntroDBSkipTimes',
          'Also get skip times from TheIntroDB. AniSkip is always used.',
          icon: LucideIcons.flag,
        ),
        toggle(
          'AnimeSkipTimes',
          'Also get skip times from Anime-Skip.',
          icon: LucideIcons.flag,
        ),
        toggle(
          'ScoreOnCompletion',
          'Ask for a score after the last episode of a show that has ended.',
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
          'Picture quality to play when there is a choice.',
          value: settingValue(store, 'Quality'),
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
        toggle(
          'ResumePlayback',
          'Start where you stopped last time on this device.',
        ),
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
                Host.macOS ? 'Adds the otakase command to your terminal.' : 'Adds Otakase to your app menu and the otakase command to your terminal.',
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
          'A movie or show episode you stop after this point counts as watched. '
              'Anime counts once the episode ends.',
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
          'Look for a new version when the app starts.',
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
