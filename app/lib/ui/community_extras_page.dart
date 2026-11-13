import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../core.dart';
import '../data/community_extras.dart';
import '../data/discord.dart';
import '../data/models.dart';
import '../services.dart';
import 'community_page.dart';
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

/// Community → Settings backup.
class BackupPage extends StatefulWidget {
  const BackupPage({super.key, required this.services, required this.isTV});
  final AppServices services;
  final bool isTV;
  @override
  State<BackupPage> createState() => _BackupPageState();
}

class _BackupPageState extends State<BackupPage> {
  bool working = false;
  String message = '';

  Future<void> run(Future<String> Function() action) async {
    if (working) return;
    setState(() => working = true);
    String text;
    try {
      text = await action();
    } catch (e) {
      text = e is BotFailure ? e.toString() : 'That didn’t work: $e';
    }
    if (mounted) {
      setState(() {
        working = false;
        message = text;
      });
    }
  }

  Future<String> restore() async {
    final x = widget.services.extras;
    final backup = await x.fetchBackup();
    if (backup == null) {
      return 'There is no backup yet. Back up on the device that has your settings.';
    }
    if (!mounted) return '';
    final from = [
      if (backup.fromOS.isNotEmpty) 'from ${backup.fromOS}',
      if (backup.at != null) 'made ${_day(backup.at!)}',
    ].join(', ');
    final yes = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('Restore settings?'),
        content: Text(
          'The backup${from.isEmpty ? '' : ' ($from)'} replaces the settings it has. Accounts and sign-ins stay as they are.',
        ),
        actions: [
          TextButton(
            autofocus: true,
            onPressed: () => Navigator.pop(c, false),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(c, true),
            child: const Text('Restore'),
          ),
        ],
      ),
    );
    if (yes != true) return '';
    final n = await x.restore(backup.config);
    return n == 0
        ? 'The backup has no settings this app knows.'
        : 'Restored $n settings.';
  }

  @override
  Widget build(BuildContext context) {
    final linked = widget.services.discord.linked;
    return _page(context, 'Settings backup', widget.isTV, [
      const PageHeading('Keep your settings', '控'),
      const SizedBox(height: 12),
      Text(
        'Keeps a copy of your settings with the Otakase Discord bot, for your Discord account, so a new phone, TV or computer can get them back. Sign-ins and keys never leave this device.',
        style: uiStyle(context, color: muted),
      ),
      const SizedBox(height: 20),
      if (!linked)
        LinkDiscordFirst(
          services: widget.services,
          isTV: widget.isTV,
          onLinked: () {
            if (mounted) setState(() {});
          },
        )
      else
        Wrap(
          spacing: 12,
          runSpacing: 12,
          children: [
            AppButton(
              label: 'Back up now',
              filled: true,
              autofocus: true,
              isTV: widget.isTV,
              onPressed: working
                  ? null
                  : () => run(() async {
                      final n = await widget.services.extras.backUp();
                      return 'Backed up $n settings.';
                    }),
            ),
            AppButton(
              label: 'Restore',
              isTV: widget.isTV,
              onPressed: working ? null : () => run(restore),
            ),
            AppButton(
              label: 'Delete backup',
              isTV: widget.isTV,
              onPressed: working
                  ? null
                  : () => run(() async {
                      await widget.services.extras.deleteBackup();
                      return 'Backup deleted.';
                    }),
            ),
          ],
        ),
      if (working)
        const Padding(
          padding: EdgeInsets.symmetric(vertical: 24),
          child: WorkingLine(),
        ),
      if (message.isNotEmpty)
        Padding(
          padding: const EdgeInsets.only(top: 16),
          child: Text(message, style: uiStyle(context, color: accentText)),
        ),
    ]);
  }
}

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
        AppButton(
          label: 'Open the Discord',
          isTV: widget.isTV,
          autofocus: true,
          onPressed: () =>
              widget.services.auth.launch(Uri.parse(discordInviteUrl)),
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

/// The Community extras for the Discord page: club, backup, report.
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
                label: 'Settings backup',
                isTV: isTV,
                onPressed: () => Navigator.push(
                  context,
                  OtakaseRoute<void>(
                    builder: (_) => BackupPage(services: services, isTV: isTV),
                  ),
                ),
              ),
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
