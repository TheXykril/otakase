import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../core.dart';
import '../data/watch_party.dart';
import '../services.dart';
import 'community_page.dart';
import 'components.dart';
import 'device_status.dart';

/// The party's chat, reactions, votes and notes over the player, and vote
/// buttons while the controls show.
class PartyOverlay extends StatefulWidget {
  const PartyOverlay({
    super.key,
    required this.party,
    required this.controls,
    required this.isTV,
  });
  final WatchParties party;
  final bool controls, isTV;
  @override
  State<PartyOverlay> createState() => _PartyOverlayState();
}

class _PartyOverlayState extends State<PartyOverlay> {
  Timer? fade;
  @override
  void initState() {
    super.initState();
    // Lines and notes fade on their own; redraw now and then.
    fade = Timer.periodic(const Duration(seconds: 2), (_) {
      if (mounted && widget.party.active) setState(() {});
    });
  }

  @override
  void dispose() {
    fade?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: widget.party,
    builder: (context, _) {
      final p = widget.party;
      if (!p.active) return const SizedBox.shrink();
      final noteOn =
          p.noteAt != null &&
          DateTime.now().difference(p.noteAt!) < const Duration(seconds: 4);
      final lines = p.recentLines();
      return Positioned(
        left: 24,
        top: 72,
        child: IgnorePointer(
          ignoring: !widget.controls,
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 420),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (noteOn && p.note.isNotEmpty) _bubble(context, p.note),
                for (final l in lines)
                  _bubble(context, '${l.name}: ${l.text}', dim: true),
                if (widget.controls) ...[
                  const SizedBox(height: 8),
                  _bubble(
                    context,
                    p.hosting
                        ? 'Watch party · ${p.watching} watching along'
                        : 'Watch party · following the host${p.connected ? '' : ' · reconnecting'}',
                  ),
                  if (!widget.isTV)
                    Wrap(
                      spacing: 8,
                      children: [
                        TextButton(
                          onPressed: () => p.vote('skip'),
                          child: const Text('Vote skip (Alt+K)'),
                        ),
                        TextButton(
                          onPressed: () => p.vote('next'),
                          child: const Text('Vote next (Alt+N)'),
                        ),
                      ],
                    ),
                ],
              ],
            ),
          ),
        ),
      );
    },
  );

  Widget _bubble(BuildContext context, String text, {bool dim = false}) =>
      Container(
        margin: const EdgeInsets.only(bottom: 6),
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
        decoration: BoxDecoration(
          color: bgDeep.withValues(alpha: .72),
          borderRadius: BorderRadius.circular(KageRadius.toast),
        ),
        child: Text(
          text,
          style: uiStyle(context, size: 15, color: dim ? muted : fg),
        ),
      );
}

/// Community → Watch parties: the planned list, joining by link, and the
/// party this app is in.
class WatchPartiesPage extends StatefulWidget {
  const WatchPartiesPage({
    super.key,
    required this.services,
    required this.isTV,
  });
  final AppServices services;
  final bool isTV;
  @override
  State<WatchPartiesPage> createState() => _WatchPartiesPageState();
}

class _WatchPartiesPageState extends State<WatchPartiesPage> {
  List<PlannedParty>? parties;
  String? error;
  bool joining = false;
  final link = TextEditingController();

  WatchParties get party => widget.services.party;

  @override
  void initState() {
    super.initState();
    unawaited(load());
  }

  @override
  void dispose() {
    link.dispose();
    super.dispose();
  }

  Future<void> load() async {
    if (!widget.services.discord.linked) return;
    setState(() => error = null);
    try {
      final list = await party.planned();
      if (mounted) setState(() => parties = list);
    } catch (e) {
      if (mounted) setState(() => error = errorMessage(e));
    }
  }

  Future<void> join(String input) async {
    if (joining) return;
    setState(() => joining = true);
    try {
      await party.join(input);
      if (mounted) {
        communityMessage(
          context,
          party.hosting
              ? 'You’re hosting. Pick the show and episode: everyone follows your player.'
              : 'Joined. Your player opens when the host plays.',
        );
      }
    } catch (e) {
      if (mounted) communityMessage(context, errorMessage(e));
    } finally {
      if (mounted) setState(() => joining = false);
    }
  }

  Future<void> answer(PlannedParty p, bool going) async {
    try {
      final r = await party.rsvp(p.id, going);
      if (!mounted) return;
      communityMessage(
        context,
        r.going
            ? 'You’re in (${r.count} going). You’ll get a reminder before it starts.'
            : 'Took you off the going list.',
      );
      await load();
    } catch (e) {
      if (mounted) communityMessage(context, errorMessage(e));
    }
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: Listenable.merge([
      party,
      widget.services.discord,
      widget.services.store,
    ]),
    builder: (context, _) {
      final linked = widget.services.discord.linked;
      final now = DateTime.now();
      return Scaffold(
        appBar: AppBar(
          title: const Text('Watch parties'),
          actions: [
            DeviceStatus(isTV: widget.isTV),
            const SizedBox(width: 16),
          ],
        ),
        body: ListView(
          padding: const EdgeInsets.all(24),
          children: [
            const PageHeading('Watch together', '宴'),
            const SizedBox(height: 12),
            if (!linked)
              LinkDiscordFirst(
                services: widget.services,
                isTV: widget.isTV,
                onLinked: () {
                  if (mounted) setState(() {});
                },
              )
            else ...[
              if (party.active) ...[
                SectionLabel(
                  party.hosting ? 'Hosting ${party.show}' : 'In ${party.show}',
                ),
                const SizedBox(height: 8),
                Text(
                  party.hosting
                      ? '${party.watching} watching along. Everyone follows your player.'
                      : 'Your player follows the host’s.',
                  style: uiStyle(context, color: muted),
                ),
                if (party.joinUrl != null) ...[
                  const SizedBox(height: 8),
                  SelectableText(
                    party.joinUrl!,
                    style: uiStyle(context, color: accentText),
                  ),
                ],
                const SizedBox(height: 12),
                Wrap(
                  spacing: 12,
                  runSpacing: 12,
                  children: [
                    if (party.joinUrl != null)
                      AppButton(
                        label: 'Copy invite link',
                        isTV: widget.isTV,
                        onPressed: () async {
                          await Clipboard.setData(
                            ClipboardData(text: party.joinUrl!),
                          );
                          if (context.mounted) {
                            communityMessage(context, 'Link copied.');
                          }
                        },
                      ),
                    AppButton(
                      label: party.watchWithMe && party.hosting
                          ? 'End the party'
                          : 'Leave the party',
                      isTV: widget.isTV,
                      autofocus: true,
                      onPressed: () => party.leave(),
                    ),
                  ],
                ),
                const SizedBox(height: 24),
              ],
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('Watch with me'),
                subtitle: const Text(
                  'While you watch, others in the Discord can join and follow your player. Ends 90 s after you stop.',
                ),
                value: widget.services.store.flag('DiscordWatchWithMe'),
                onChanged: (v) =>
                    widget.services.store.set('DiscordWatchWithMe', v),
              ),
              if (!widget.isTV) ...[
                const SizedBox(height: 12),
                Row(
                  children: [
                    Expanded(
                      child: TextField(
                        controller: link,
                        decoration: const InputDecoration(
                          labelText: 'Party link or event id',
                          hintText: 'Paste the Join in Otakase link',
                        ),
                        onSubmitted: join,
                      ),
                    ),
                    const SizedBox(width: 12),
                    AppButton(
                      label: joining ? 'Joining…' : 'Join',
                      onPressed: joining ? null : () => join(link.text),
                    ),
                  ],
                ),
              ],
              const SizedBox(height: 24),
              Row(
                children: [
                  const Expanded(child: SectionLabel('Planned in the Discord')),
                  IconButton(
                    tooltip: 'Refresh',
                    onPressed: load,
                    icon: const Icon(Icons.refresh),
                  ),
                ],
              ),
              if (error != null)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  child: Text(error!, style: uiStyle(context, color: muted)),
                )
              else if (parties == null)
                const Padding(
                  padding: EdgeInsets.all(24),
                  child: Center(child: CircularProgressIndicator()),
                )
              else if (parties!.isEmpty)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  child: Text(
                    'No watch parties are planned. Start one in the Discord with /watchparty.',
                    style: uiStyle(context, color: muted),
                  ),
                ),
              for (final p in parties ?? const <PlannedParty>[])
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 8),
                  child: Container(
                    padding: const EdgeInsets.all(14),
                    decoration: BoxDecoration(
                      color: panel,
                      borderRadius: BorderRadius.circular(KageRadius.tile),
                      border: Border.all(color: line),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          p.title,
                          style: uiStyle(context, weight: FontWeight.w600),
                        ),
                        Text(
                          [
                            if (p.live)
                              'on now'
                            else if (p.start != null)
                              partyStartLabel(p.start!, now),
                            '${p.going} going',
                            if (p.sync) 'synced',
                            if (p.host.isNotEmpty) 'host ${p.host}',
                            if (p.rsvp) 'you’re in',
                          ].join(' · '),
                          style: uiStyle(context, color: muted, size: 15),
                        ),
                        const SizedBox(height: 10),
                        Wrap(
                          spacing: 12,
                          runSpacing: 8,
                          children: [
                            AppButton(
                              label: p.rsvp ? 'Not going' : 'I’m in',
                              isTV: widget.isTV,
                              onPressed: () => answer(p, !p.rsvp),
                            ),
                            if (p.sync)
                              AppButton(
                                label: p.live ? 'Join now' : 'Join early',
                                filled: true,
                                isTV: widget.isTV,
                                onPressed: party.id == p.id
                                    ? null
                                    : () => join(p.id),
                              ),
                            if (p.eventUrl.startsWith('https://discord.com/'))
                              AppButton(
                                label: 'Open in Discord',
                                isTV: widget.isTV,
                                onPressed: () async {
                                  try {
                                    await widget.services.auth.launch(
                                      Uri.parse(p.eventUrl),
                                    );
                                  } catch (e) {
                                    if (context.mounted) {
                                      communityMessage(
                                        context,
                                        errorMessage(e),
                                      );
                                    }
                                  }
                                },
                              ),
                          ],
                        ),
                      ],
                    ),
                  ),
                ),
              if (party.lastError != null)
                Padding(
                  padding: const EdgeInsets.only(top: 12),
                  child: Text(
                    party.lastError!,
                    style: uiStyle(context, color: muted, size: 15),
                  ),
                ),
            ],
          ],
        ),
      );
    },
  );
}
