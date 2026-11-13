import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../core.dart';
import '../data/community_extras.dart';
import '../data/discord.dart';
import '../data/models.dart';
import '../services.dart';
import 'components.dart';
import 'device_status.dart';

String _day(DateTime d) {
  const months = [
    'Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', //
    'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec',
  ];
  return '${d.day} ${months[d.month - 1]}';
}

String clubLabel(ServerClub c) =>
    '${c.title} · ep ${c.episode} by ${c.deadline == null ? '?' : _day(c.deadline!)}';

Scaffold _page(
  BuildContext context,
  String title,
  bool isTV,
  List<Widget> children,
) => Scaffold(
  appBar: AppBar(
    title: Text(title),
    actions: [
      TopBarEnd(isTV: isTV),
      const SizedBox(width: 16),
    ],
  ),
  body: ListView(padding: const EdgeInsets.all(24), children: children),
);

/// Community → Report a problem: posts to the support forum as the linked
/// member, with the app details attached.
class ReportPage extends StatefulWidget {
  const ReportPage({
    super.key,
    required this.services,
    required this.isTV,
    this.initialTitle = '',
    this.initialBody = '',
    this.playbackDetails = '',
  });
  final String initialTitle, initialBody, playbackDetails;
  final AppServices services;
  final bool isTV;
  @override
  State<ReportPage> createState() => _ReportPageState();
}

class _ReportPageState extends State<ReportPage> {
  final title = TextEditingController(), body = TextEditingController();
  bool sending = false, details = true;
  String message = '', url = '';

  @override
  void initState() {
    super.initState();
    title.text = String.fromCharCodes(widget.initialTitle.runes.take(100));
    body.text = widget.initialBody;
  }

  String get diagnostics =>
      reportDiagnostics(widget.services.store, extra: widget.playbackDetails);

  String get reportText => [
    title.text,
    body.text,
    if (details) diagnostics,
  ].where((part) => part.isNotEmpty).join('\n\n');

  @override
  void dispose() {
    title.dispose();
    body.dispose();
    super.dispose();
  }

  Future<void> send() async {
    if (title.text.trim().isEmpty) {
      setState(() => message = 'Say in a few words what went wrong.');
      return;
    }
    setState(() {
      sending = true;
      message = '';
    });
    try {
      final link = await widget.services.extras.report(
        title.text,
        body.text,
        details ? diagnostics : '',
      );
      if (!mounted) return;
      setState(() {
        url = link;
        message =
            'Posted to the support forum. Replies come to you in Discord.';
      });
    } catch (e) {
      if (mounted) setState(() => message = errorMessage(e));
    } finally {
      if (mounted) setState(() => sending = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final linked = widget.services.discord.linked;
    return _page(context, 'Report a problem', widget.isTV, [
      const PageHeading('Tell us what broke', '報'),
      const SizedBox(height: 12),
      if (!linked) ...[
        Text(
          'Link this profile to Discord to send it from here, or post in the support forum yourself.',
          style: uiStyle(context, color: muted),
        ),
        const SizedBox(height: 12),
        Align(
          alignment: Alignment.centerLeft,
          child: AppButton(
            label: 'Open the Discord',
            isTV: widget.isTV,
            autofocus: true,
            onPressed: () =>
                widget.services.auth.launch(Uri.parse(discordInviteUrl)),
          ),
        ),
      ],
      RemoteTextInput(
        isTV: widget.isTV,
        builder: (focus) => TextField(
          focusNode: focus,
          controller: title,
          maxLength: 100,
          decoration: const InputDecoration(
            labelText: 'What went wrong',
            hintText: 'Episode 3 of Frieren won’t play',
          ),
        ),
      ),
      RemoteTextInput(
        isTV: widget.isTV,
        multiline: true,
        builder: (focus) => TextField(
          focusNode: focus,
          controller: body,
          maxLength: 4000,
          minLines: 3,
          maxLines: 8,
          decoration: const InputDecoration(
            labelText: 'More details (optional)',
          ),
        ),
      ),
      SwitchListTile(
        contentPadding: EdgeInsets.zero,
        title: const Text('Attach app details'),
        subtitle: const Text(
          'Version, system, source order and how the sources did this run.',
        ),
        value: details,
        onChanged: (v) => setState(() => details = v),
      ),
      if (details)
        Padding(
          padding: const EdgeInsets.only(bottom: 12),
          child: Text(diagnostics, style: mono.copyWith(fontSize: 13)),
        ),
      Align(
        alignment: Alignment.centerLeft,
        child: AppButton(
          label: sending ? 'Sending…' : 'Send report',
          filled: true,
          isTV: widget.isTV,
          onPressed: !linked || sending || url.isNotEmpty ? null : send,
        ),
      ),
      const SizedBox(height: 12),
      Align(
        alignment: Alignment.centerLeft,
        child: AppButton(
          label: 'Copy report',
          isTV: widget.isTV,
          onPressed: () async {
            await Clipboard.setData(ClipboardData(text: reportText));
            if (mounted) {
              setState(
                () => message = 'Report copied. You can paste it into support.',
              );
            }
          },
        ),
      ),
      if (message.isNotEmpty)
        Padding(
          padding: const EdgeInsets.only(top: 16),
          child: Text(message, style: uiStyle(context, color: accentText)),
        ),
      if (url.startsWith('https://discord.com/'))
        Padding(
          padding: const EdgeInsets.only(top: 8),
          child: Align(
            alignment: Alignment.centerLeft,
            child: AppButton(
              label: 'Open the post',
              isTV: widget.isTV,
              onPressed: () => widget.services.auth.launch(Uri.parse(url)),
            ),
          ),
        ),
    ]);
  }
}

/// The server club row: plays the club's show.
Widget? clubTile(
  BuildContext context,
  AppServices s,
  bool isTV,
  void Function(Anime) open,
) {
  final c = s.extras.club;
  if (c == null) return null;
  return ListTile(
    contentPadding: EdgeInsets.zero,
    leading: const Icon(Icons.groups_outlined),
    title: const Text('Server club'),
    subtitle: Text('${clubLabel(c)} · ${c.caughtUp} of ${c.joined} caught up'),
    onTap: () => open(Anime(id: c.anilistId, title: c.title)),
  );
}

/// The Community extras for the Discord page: club and report.
class CommunityExtrasSection extends StatelessWidget {
  const CommunityExtrasSection({
    super.key,
    required this.services,
    required this.isTV,
    required this.open,
  });
  final AppServices services;
  final bool isTV;
  final void Function(Anime) open;
  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: services.extras,
    builder: (context, _) {
      final club = clubTile(context, services, isTV, open);
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          ?club,
          Wrap(
            spacing: 12,
            runSpacing: 12,
            children: [
              AppButton(
                label: 'Report a problem',
                isTV: isTV,
                onPressed: () => Navigator.push(
                  context,
                  OtakaseRoute<void>(
                    builder: (_) => ReportPage(services: services, isTV: isTV),
                  ),
                ),
              ),
            ],
          ),
        ],
      );
    },
  );
}
