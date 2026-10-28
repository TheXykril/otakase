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
      barrierColor: Colors.black54,
      transitionDuration: const Duration(milliseconds: 180),
      pageBuilder: (context, a, b) => Align(
        alignment: Alignment.centerRight,
        child: SizedBox(
          width: (MediaQuery.sizeOf(context).width * .38).clamp(360, 620),
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
      MaterialPageRoute<void>(
        builder: (_) => SettingsPage(services: services, isTV: false),
      ),
    );
  }
}

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
        title: Text(title ?? key),
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
      key,
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
      key,
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
          padding: const EdgeInsets.fromLTRB(22, 8, 22, 12),
          child: Row(
            children: [
              Icon(icon, color: shu, size: 20),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  title,
                  style: const TextStyle(
                    fontSize: 17,
                    fontWeight: FontWeight.w700,
                  ),
                ),
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
    IconData icon = Icons.tune,
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
          Icon(icon, size: 21, color: enabled ? Colors.white70 : muted),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  key,
                  style: TextStyle(
                    fontSize: 14,
                    fontWeight: FontWeight.w600,
                    color: enabled ? Colors.white : muted,
                  ),
                ),
                const SizedBox(height: 5),
                Text(
                  description,
                  style: const TextStyle(
                    color: muted,
                    fontSize: 11,
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
                      ),
                    ),
                  ),
              ],
            ),
          ),
          if (onTap != null)
            const Icon(Icons.chevron_right, size: 18, color: muted),
        ],
      ),
    ),
  );
  Widget toggle(String key, String description, {IconData icon = Icons.tune}) =>
      Padding(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 2),
        child: FocusBox(
          isTV: widget.isTV,
          radius: 10,
          onPressed: () => store.set(key, !store.flag(key)),
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
          child: Row(
            children: [
              Icon(icon, size: 21, color: Colors.white70),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      key,
                      style: const TextStyle(
                        fontSize: 14,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                    const SizedBox(height: 5),
                    Text(
                      description,
                      style: const TextStyle(
                        color: muted,
                        fontSize: 11,
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
  Future<void> health(String provider) async {
    store.health(provider, 'Checking');
    try {
      await widget.services.core.request(
        {
          'operation': 'search',
          'provider': provider,
          'mode': store.text('SubOrDub'),
          'query': 'Frieren',
        },
        scope: scope,
        timeout: const Duration(seconds: 15),
      );
      if (mounted) store.health(provider, 'Working');
    } catch (_) {
      if (mounted) store.health(provider, 'Unavailable');
    }
  }

  Future<void> providerOrder() async {
    await showDialog<void>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Provider order'),
        content: SizedBox(
          width: 440,
          height: 390,
          child: ListenableBuilder(
            listenable: store,
            builder: (context, child) => ListView(
              children: store.order.map((provider) {
                final index = store.order.indexOf(provider);
                return Row(
                  children: [
                    Expanded(child: Text(provider)),
                    IconButton(
                      tooltip: 'Move $provider up',
                      onPressed: index == 0
                          ? null
                          : () {
                              final order = store.order;
                              order.removeAt(index);
                              order.insert(index - 1, provider);
                              unawaited(store.set('ProviderOrder', order));
                            },
                      icon: const Icon(Icons.arrow_upward),
                    ),
                    IconButton(
                      tooltip: 'Move $provider down',
                      onPressed: index == store.order.length - 1
                          ? null
                          : () {
                              final order = store.order;
                              order.removeAt(index);
                              order.insert(index + 1, provider);
                              unawaited(store.set('ProviderOrder', order));
                            },
                      icon: const Icon(Icons.arrow_downward),
                    ),
                  ],
                );
              }).toList(),
            ),
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('Done'),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    backgroundColor: panel,
    appBar: AppBar(
      backgroundColor: panel,
      title: const Text('Settings'),
      leading: IconButton(
        tooltip: 'Back',
        autofocus: true,
        onPressed: () => Navigator.pop(context),
        icon: const Icon(Icons.arrow_back),
      ),
    ),
    body: SafeArea(
      top: false,
      child: ListenableBuilder(
        listenable: store,
        builder: (context, child) => ListView(
          padding: const EdgeInsets.only(bottom: 35),
          children: [
            group('Subtitle & Languages', Icons.subtitles_outlined, [
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
                icon: Icons.language,
              ),
            ]),
            group('Playback', Icons.play_circle_outline, [
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
            group('Accounts', Icons.person_outline, [
              row(
                'AniList username',
                'Read a public list. Login and tracker writes come later.',
                value: store.text('AniListUsername').isEmpty
                    ? 'Not connected'
                    : store.text('AniListUsername'),
                icon: Icons.person_outline,
                onTap: () => edit('AniListUsername', title: 'AniList username'),
              ),
              row(
                'MAL username',
                'Read a public list. Login and tracker writes come later.',
                value: store.text('MyAnimeListUsername').isEmpty
                    ? 'Not connected'
                    : store.text('MyAnimeListUsername'),
                icon: Icons.person_outline,
                onTap: () => edit('MyAnimeListUsername', title: 'MAL username'),
              ),
              row(
                'TV login from phone',
                'Account login is coming later.',
                enabled: false,
                icon: Icons.qr_code,
              ),
              row(
                'Discord link',
                'Account pairing is coming later.',
                enabled: false,
                icon: Icons.link,
              ),
            ]),
            group('Sources', Icons.dns_outlined, [
              row(
                'Provider order',
                'Try the selected source first.',
                value: store.order.join(' › '),
                onTap: providerOrder,
              ),
              toggle(
                'AutoFallback',
                'Try other enabled sources if matching or resolution fails.',
              ),
              for (final provider in store.order)
                Padding(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 22,
                    vertical: 5,
                  ),
                  child: Row(
                    children: [
                      Expanded(
                        child: FocusBox(
                          isTV: widget.isTV,
                          padding: const EdgeInsets.all(10),
                          onPressed: () {
                            final disabled = store.strings('DisabledProviders');
                            if (disabled.contains(provider)) {
                              disabled.remove(provider);
                            } else {
                              disabled.add(provider);
                            }
                            unawaited(store.set('DisabledProviders', disabled));
                          },
                          child: Row(
                            children: [
                              Icon(
                                store.enabledSources.contains(provider)
                                    ? Icons.check_box_outlined
                                    : Icons.check_box_outline_blank,
                                color: store.enabledSources.contains(provider)
                                    ? shu
                                    : muted,
                              ),
                              const SizedBox(width: 9),
                              Expanded(
                                child: Column(
                                  crossAxisAlignment: CrossAxisAlignment.start,
                                  children: [
                                    Text(provider),
                                    Text(
                                      store.providerHealth[provider] ??
                                          'Not checked',
                                      style: const TextStyle(
                                        color: muted,
                                        fontSize: 11,
                                      ),
                                    ),
                                  ],
                                ),
                              ),
                            ],
                          ),
                        ),
                      ),
                      IconButton(
                        tooltip: 'Check $provider',
                        onPressed: () => health(provider),
                        icon: const Icon(Icons.refresh, size: 20),
                      ),
                    ],
                  ),
                ),
            ]),
            group('Content', Icons.shield_outlined, [
              toggle(
                'AdultContent',
                'Show 18+ titles and artwork. Off by default.',
                icon: Icons.visibility_outlined,
              ),
              toggle(
                'ExperimentalMovies',
                'Show experimental sources in the drawer.',
              ),
              row(
                'Notifications',
                'New episode alerts are coming later.',
                enabled: false,
                icon: Icons.notifications_none,
              ),
              toggle(
                'CheckUpdates',
                'Check published Android releases.',
                icon: Icons.system_update_outlined,
              ),
              row(
                'Updates',
                updateMessage ?? 'Check for a published Android APK.',
                onTap: () async {
                  final message = await widget.services.catalog.updateStatus();
                  if (mounted) setState(() => updateMessage = message);
                },
                icon: Icons.update,
              ),
              row(
                'Diagnostics',
                'Header-gated HLS clip and native ASS rendering probe.',
                onTap: () => Navigator.push(
                  context,
                  MaterialPageRoute<void>(
                    builder: (_) => const DiagnosticsPage(probe: true),
                  ),
                ),
                icon: Icons.science_outlined,
              ),
              row(
                'About',
                'Otakase 0.1.0 · Android phone and TV',
                value: '任',
                icon: Icons.info_outline,
              ),
            ]),
          ],
        ),
      ),
    ),
  );
}
