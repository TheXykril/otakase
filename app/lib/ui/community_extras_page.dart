import 'dart:async';

import 'package:flutter/material.dart';

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
      DeviceStatus(isTV: isTV),
      const SizedBox(width: 16),
    ],
  ),
  body: ListView(padding: const EdgeInsets.all(24), children: children),
);

Widget _notLinked(BuildContext context) => Text(
  'Link this profile to Discord first (More, then Discord).',
  style: uiStyle(context, color: muted),
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
        _notLinked(context)
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
          padding: EdgeInsets.all(24),
          child: Center(child: CircularProgressIndicator()),
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
  const ReportPage({super.key, required this.services, required this.isTV});
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
        details ? reportDiagnostics(widget.services.store) : '',
      );
      if (!mounted) return;
      setState(() {
        url = link;
        message =
            'Posted to the support forum. Replies come to you in Discord.';
      });
    } catch (e) {
      if (mounted) setState(() => message = e.toString());
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
      ] else ...[
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
            child: Text(
              reportDiagnostics(widget.services.store),
              style: mono.copyWith(fontSize: 13),
            ),
          ),
        Align(
          alignment: Alignment.centerLeft,
          child: AppButton(
            label: sending ? 'Sending…' : 'Send report',
            filled: true,
            isTV: widget.isTV,
            onPressed: sending || url.isNotEmpty ? null : send,
          ),
        ),
      ],
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

/// What the cheat code opens: a puzzle hunt the bot makes for the member.
class CheatPage extends StatefulWidget {
  const CheatPage({super.key, required this.services, required this.isTV});
  final AppServices services;
  final bool isTV;
  @override
  State<CheatPage> createState() => _CheatPageState();
}

class _CheatPageState extends State<CheatPage> {
  CheatView? view;
  String error = '', note = '';
  bool first = true, working = false;
  final answer = TextEditingController();

  @override
  void initState() {
    super.initState();
    if (widget.services.discord.linked) unawaited(ask(null));
  }

  @override
  void dispose() {
    answer.dispose();
    super.dispose();
  }

  Future<void> ask(String? text) async {
    setState(() => working = true);
    try {
      final x = widget.services.extras;
      final v = text == null ? await x.cheat() : await x.cheatAnswer(text);
      if (!mounted) return;
      setState(() {
        if (view != null) first = false;
        note = '';
        if (text == null &&
            view != null &&
            v.puzzle &&
            v.where == 'discord' &&
            v.step == view!.step) {
          note = 'Nothing yet.';
        } else if (text != null && v.correct && v.puzzle) {
          note = 'Correct.';
        } else if (text != null && !v.correct) {
          note = v.message;
          if (v.puzzle && v.wrongLeft > 0 && v.wrongLeft <= 5) {
            note += ' (${v.wrongLeft} wrong answers left on this trail)';
          }
        }
        view = v;
        answer.clear();
      });
    } on BotFailure catch (e) {
      if (mounted) {
        setState(
          () => error = e.status == 401
              ? 'This app is no longer linked to Discord. Link it again, then enter the code again.'
              : 'Nothing answers right now: ${e.message}.',
        );
      }
    } catch (_) {
      if (mounted) {
        setState(() => error = 'Nothing answers right now. Try again later.');
      }
    } finally {
      if (mounted) setState(() => working = false);
    }
  }

  (String, String) endText(CheatView v) {
    final until = v.until == null
        ? ''
        : '${_day(v.until!)}, ${v.until!.hour.toString().padLeft(2, '0')}:${v.until!.minute.toString().padLeft(2, '0')}';
    String or(String s, String f) => s.trim().isEmpty ? f : s;
    switch (v.status) {
      case 'found':
        if (v.redeemed) {
          return (
            'Code redeemed',
            'This week’s code (${v.code}) has been redeemed.\n\nThe next hunt opens $until.',
          );
        }
        return (
          'CODE FOUND',
          '${v.code}\n\n${or(v.message, 'It works once.')}\n\nRedeem it in Discord: /redeem code:${v.code}',
        );
      case 'wait':
        return (
          'The trail went cold',
          '${or(v.message, 'Too many wrong answers.')}\n\nTry again after $until.',
        );
    }
    return (
      'Nothing here',
      or(v.message, 'The hunt is over. Enter the code again for a new one.'),
    );
  }

  @override
  Widget build(BuildContext context) {
    final v = view;
    final children = <Widget>[];
    if (!widget.services.discord.linked) {
      children.addAll([
        Text(
          'CHEAT ACTIVATED',
          style: uiStyle(
            context,
            size: 26,
            weight: FontWeight.w700,
            color: accentText,
          ),
        ),
        const SizedBox(height: 12),
        Text(
          'Something stirs, but it doesn’t know who you are yet.\n\nLink Otakase to Discord first (More, then Discord), then enter the code again.',
          style: uiStyle(context),
        ),
      ]);
    } else if (error.isNotEmpty) {
      children.addAll([
        Text(
          'CHEAT ACTIVATED',
          style: uiStyle(
            context,
            size: 26,
            weight: FontWeight.w700,
            color: accentText,
          ),
        ),
        const SizedBox(height: 12),
        Text(error, style: uiStyle(context)),
      ]);
    } else if (v == null) {
      children.add(
        const Padding(
          padding: EdgeInsets.all(32),
          child: Center(child: CircularProgressIndicator()),
        ),
      );
    } else if (!v.puzzle) {
      final (t, b) = endText(v);
      children.addAll([
        Text(
          first && v.status == 'found' && !v.redeemed
              ? 'CHEAT ACTIVATED · $t'
              : t,
          style: uiStyle(
            context,
            size: 26,
            weight: FontWeight.w700,
            color: accentText,
          ),
        ),
        const SizedBox(height: 16),
        SelectableText(b, style: mono.copyWith(color: fg, fontSize: 16)),
      ]);
    } else {
      children.addAll([
        Text(
          '${first ? 'CHEAT ACTIVATED · ' : ''}${v.kind} · step ${v.step} of ${v.steps}',
          style: uiStyle(
            context,
            size: 24,
            weight: FontWeight.w700,
            color: accentText,
          ),
        ),
        const SizedBox(height: 16),
        SelectableText(v.text, style: mono.copyWith(color: fg, fontSize: 16)),
        const SizedBox(height: 16),
        for (final (i, link) in v.links.indexed)
          if (link.startsWith('https://'))
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton(
                onPressed: () => widget.services.auth.launch(Uri.parse(link)),
                child: Text(
                  v.links.length == 1 ? 'Open the link' : 'Open link ${i + 1}',
                ),
              ),
            ),
        const SizedBox(height: 8),
        if (v.where == 'discord')
          Align(
            alignment: Alignment.centerLeft,
            child: AppButton(
              label: 'I did it in Discord, check',
              filled: true,
              autofocus: true,
              isTV: widget.isTV,
              onPressed: working ? null : () => ask(null),
            ),
          )
        else
          Row(
            children: [
              Expanded(
                child: RemoteTextInput(
                  isTV: widget.isTV,
                  builder: (focus) => TextField(
                    focusNode: focus,
                    autofocus: true,
                    controller: answer,
                    maxLength: 200,
                    decoration: const InputDecoration(labelText: 'Your answer'),
                    onSubmitted: (t) {
                      if (t.trim().isNotEmpty && !working) {
                        unawaited(ask(t.trim()));
                      }
                    },
                  ),
                ),
              ),
              const SizedBox(width: 12),
              AppButton(
                label: 'Answer',
                isTV: widget.isTV,
                onPressed: working
                    ? null
                    : () {
                        if (answer.text.trim().isNotEmpty) {
                          unawaited(ask(answer.text.trim()));
                        }
                      },
              ),
            ],
          ),
        if (note.isNotEmpty)
          Padding(
            padding: const EdgeInsets.only(top: 12),
            child: Text(note, style: uiStyle(context, color: muted)),
          ),
      ]);
    }
    return _page(context, 'Cheat code', widget.isTV, children);
  }
}

/// Opens the cheat hunt; used by the Konami code and the search box.
Future<void> openCheat(BuildContext context, AppServices s, bool isTV) =>
    Navigator.push(
      context,
      OtakaseRoute<void>(
        builder: (_) => CheatPage(services: s, isTV: isTV),
      ),
    );

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
