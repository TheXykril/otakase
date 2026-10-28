import 'dart:async';

import 'package:flutter/material.dart';

import '../core.dart';
import '../diagnostics.dart';
import '../services.dart';
import '../data/store.dart';
import 'components.dart';

Future<void> openSettings(
  BuildContext context,
  AppServices services,
  bool isTV,
) async {
  if (isTV) {
    await showGeneralDialog<void>(
      context: context,
      barrierDismissible: true,
      barrierLabel: 'Settings',
      barrierColor: const Color(0xb3080707),
      transitionDuration: const Duration(milliseconds: 180),
      pageBuilder: (context, a, b) => Align(
        alignment: Alignment.centerRight,
        child: SizedBox(
          width: (MediaQuery.sizeOf(context).width * .4).clamp(360, 620),
          child: SettingsPage(services: services, isTV: true),
        ),
      ),
      transitionBuilder: (context, a, b, child) => SlideTransition(
        position: Tween<Offset>(
          begin: const Offset(1, 0),
          end: Offset.zero,
        ).animate(a),
        child: child,
      ),
    );
  } else {
    await Navigator.push(
      context,
      OtakaseRoute<void>(
        builder: (_) => SettingsPage(services: services, isTV: false),
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
  'SkipFiller': 'Skip filler episodes',
  'SkipDelay': 'Skip delay',
  'Quality': 'Quality',
  'PercentageToMarkComplete': 'Mark watched at',
  'AdultContent': 'AdultContent',
  'ExperimentalMovies': 'Movies (experimental)',
  'CheckUpdates': 'Check for updates',
  'Notifications': 'Notifications',
  'Updates': 'Updates',
  'Diagnostics': 'Diagnostics',
  'About': 'About',
};

String settingLabel(String key) => settingLabels[key] ?? key;

class SettingsPage extends StatefulWidget {
  const SettingsPage({super.key, required this.services, required this.isTV});
  final AppServices services;
  final bool isTV;
  @override
  State<SettingsPage> createState() => _SettingsPageState();
}

class _SettingsPageState extends State<SettingsPage> {
  final scope = RequestScope();
  String? updateMessage;
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
        content: TextField(
          autofocus: true,
          controller: controller,
          decoration: const InputDecoration(hintText: 'Enter a value'),
          onSubmitted: (v) => Navigator.pop(context, v),
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

  Widget group(String title, IconData icon, List<Widget> children) => Padding(
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
      radius: 10,
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
                  style: TextStyle(
                    fontSize: 20,
                    fontWeight: FontWeight.w500,
                    color: enabled ? fg : muted,
                  ),
                ),
                const SizedBox(height: 5),
                Text(
                  description,
                  style: const TextStyle(
                    color: muted,
                    fontSize: 15,
                    height: 1.5,
                  ),
                ),
                if (value != null)
                  Padding(
                    padding: const EdgeInsets.only(top: 5),
                    child: Text(
                      value,
                      style: TextStyle(
                        color: enabled ? shu : muted,
                        fontSize: 12,
                        fontFamily: 'JetBrainsMono',
                      ),
                    ),
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
      radius: 10,
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
                  style: const TextStyle(
                    fontSize: 20,
                    fontWeight: FontWeight.w500,
                  ),
                ),
                const SizedBox(height: 5),
                Text(
                  description,
                  style: const TextStyle(
                    color: muted,
                    fontSize: 15,
                    height: 1.5,
                  ),
                ),
              ],
            ),
          ),
          ExcludeFocus(
            child: IgnorePointer(
              child: Switch(
                value: store.flag(key),
                activeThumbColor: shu,
                onChanged: (_) {},
              ),
            ),
          ),
        ],
      ),
    ),
  );
  @override
  Widget build(BuildContext context) => Scaffold(
    backgroundColor: panel,
    appBar: AppBar(
      backgroundColor: panel,
      title: const Text('OTAKASE'),
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
        builder: (context, child) => ListView(
          padding: EdgeInsets.only(bottom: contentBottom(context)),
          children: [
            const Padding(
              padding: EdgeInsets.symmetric(horizontal: 24),
              child: PageHeading('Settings', '設'),
            ),
            group('Subtitle & Languages', LucideIcons.captions, [
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
                onTap: () =>
                    numberChoice('SubtitleOutline', [0, 1, 2, 3, 4, 5]),
              ),
              row(
                'SubtitlePosition',
                'Vertical placement, higher values move text down.',
                value: store.text('SubtitlePosition'),
                onTap: () => numberChoice('SubtitlePosition', [
                  10,
                  25,
                  50,
                  75,
                  90,
                  95,
                  100,
                ]),
              ),
              row(
                'SubStyle',
                'Original ASS styling or language selection when playback starts.',
                value: store.text('SubStyle'),
                onTap: () => choose('SubStyle', ['ask', 'soft']),
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
            group('Playback', LucideIcons.circlePlay, [
              toggle(
                'NextEpisodePrompt',
                'Ask before the next episode. Off enables the end countdown.',
              ),
              toggle('SkipOp', 'Automatically skip verified intro ranges.'),
              toggle('SkipEd', 'Automatically skip verified ending ranges.'),
              toggle('SkipRecap', 'Automatically skip verified recap ranges.'),
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
              row(
                'Quality',
                'Default video height, or best.',
                value: store.text('Quality'),
                onTap: () =>
                    choose('Quality', ['best', '1080', '720', '480', '360']),
              ),
              toggle('ResumePlayback', 'Resume locally saved positions.'),
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
            group('Accounts', LucideIcons.userRound, [
              row(
                'AniList username',
                'Read a public list. Login and tracker writes come later.',
                value: store.text('AniListUsername').isEmpty
                    ? 'Not connected'
                    : store.text('AniListUsername'),
                icon: LucideIcons.userRound,
                onTap: () => edit('AniListUsername', title: 'AniList username'),
              ),
              row(
                'MAL username',
                'Read a public list. Login and tracker writes come later.',
                value: store.text('MyAnimeListUsername').isEmpty
                    ? 'Not connected'
                    : store.text('MyAnimeListUsername'),
                icon: LucideIcons.userRound,
                onTap: () => edit('MyAnimeListUsername', title: 'MAL username'),
              ),
              row(
                'TV login from phone',
                'Account login is coming later.',
                enabled: false,
                icon: LucideIcons.qrCode,
              ),
              row(
                'Discord link',
                'Account pairing is coming later.',
                enabled: false,
                icon: LucideIcons.link,
              ),
            ]),
            group('Content', LucideIcons.shield, [
              toggle(
                'AdultContent',
                'Show 18+ titles and artwork. Off by default.',
                icon: LucideIcons.eye,
              ),
              toggle(
                'ExperimentalMovies',
                'Show the Movies section in the drawer.',
              ),
              row(
                'Notifications',
                'New episode alerts are coming later.',
                enabled: false,
                icon: LucideIcons.bell,
              ),
              toggle(
                'CheckUpdates',
                'Check published Android releases.',
                icon: LucideIcons.download,
              ),
              row(
                'Updates',
                updateMessage ?? 'Check for a published Android APK.',
                onTap: () async {
                  final message = await widget.services.catalog.updateStatus();
                  if (mounted) setState(() => updateMessage = message);
                },
                icon: LucideIcons.refreshCw,
              ),
              row(
                'Diagnostics',
                'Header-gated HLS clip and native ASS rendering probe.',
                onTap: () => Navigator.push(
                  context,
                  OtakaseRoute<void>(
                    builder: (_) => DiagnosticsHub(
                      services: widget.services,
                      isTV: widget.isTV,
                    ),
                  ),
                ),
                icon: LucideIcons.flaskConical,
              ),
              row(
                'About',
                'Otakase 0.1.0 · Android phone and TV',
                value: '任',
                icon: LucideIcons.info,
              ),
            ]),
          ],
        ),
      ),
    ),
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
  bool checking = false;
  @override
  void initState() {
    super.initState();
    unawaited(
      widget.services.resolver
          .providerOrder()
          .then((v) {
            if (mounted) setState(() => sources = v);
          })
          .catchError((_) {}),
    );
  }

  @override
  void dispose() {
    scope.cancel();
    super.dispose();
  }

  Future<void> check() async {
    setState(() => checking = true);
    for (final provider in sources) {
      if (scope.cancelled) break;
      widget.services.store.health(provider, 'Checking');
      try {
        await widget.services.core.request(
          {
            'operation': 'search',
            'provider': provider,
            'mode': widget.services.store.text('SubOrDub'),
            'query': 'Frieren',
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
      title: const Text('DIAGNOSTICS'),
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
            'Playback chooses a copy automatically and remembers what worked for each show.',
            style: prose.copyWith(color: muted),
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
          const SectionLabel('Core order'),
          const SizedBox(height: 12),
          for (var i = 0; i < sources.length; i++)
            Container(
              padding: const EdgeInsets.symmetric(vertical: 16),
              decoration: const BoxDecoration(
                border: Border(bottom: BorderSide(color: line)),
              ),
              child: Row(
                children: [
                  Text('${i + 1}'.padLeft(2, '0'), style: mono),
                  const SizedBox(width: 20),
                  Expanded(child: Text(sources[i], style: prose)),
                  Text(
                    (widget.services.store.providerHealth[sources[i]] ??
                            'Not checked')
                        .toUpperCase(),
                    style: mono.copyWith(letterSpacing: .5, fontSize: 9),
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
