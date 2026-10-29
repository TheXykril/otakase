import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:qr_flutter/qr_flutter.dart';

import '../core.dart';
import '../data/discord.dart';
import '../data/heritage.dart';
import '../data/lan_page.dart';
import '../data/models.dart';
import '../services.dart';
import 'components.dart';
import 'device_status.dart';
import 'show_page.dart';

void communityMessage(BuildContext context, String text) {
  if (context.mounted) {
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(text)));
  }
}

class DiscordPage extends StatelessWidget {
  const DiscordPage({super.key, required this.services, required this.isTV});
  final AppServices services;
  final bool isTV;
  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: Listenable.merge([services.discord, services.store]),
    builder: (context, _) {
      final d = services.discord;
      return Scaffold(
        appBar: AppBar(
          title: const Text('Discord'),
          actions: [
            DeviceStatus(isTV: isTV),
            const SizedBox(width: 16),
          ],
        ),
        body: ListView(
          padding: const EdgeInsets.all(24),
          children: [
            const PageHeading('Your community', '縁'),
            const SizedBox(height: 20),
            Text(
              d.linked
                  ? 'Linked as ${d.name}'
                  : d.revoked
                  ? 'This profile’s link was revoked. Link again to continue.'
                  : 'Link this profile with /link-app in the Otakase Discord.',
              style: uiStyle(context),
            ),
            const SizedBox(height: 16),
            if (!d.linked)
              AppButton(
                label: 'Link Discord',
                autofocus: true,
                isTV: isTV,
                onPressed: () => showDialog<void>(
                  context: context,
                  builder: (_) =>
                      DiscordLinkDialog(services: services, isTV: isTV),
                ),
              )
            else ...[
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('Share what I’m watching'),
                subtitle: const Text(
                  'Off by default. Watch time is sent without title details.',
                ),
                value: services.store.flag('DiscordShareWatching'),
                onChanged: (v) => services.store.set('DiscordShareWatching', v),
              ),
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('Ask to share finished scores'),
                value: services.store.text('DiscordShareRatings') != 'never',
                onChanged: (v) => services.store.set(
                  'DiscordShareRatings',
                  v ? 'ask' : 'never',
                ),
              ),
              AppButton(
                label: 'Unlink Discord',
                isTV: isTV,
                onPressed: () async {
                  final yes = await showDialog<bool>(
                    context: context,
                    builder: (c) => AlertDialog(
                      title: const Text('Unlink this profile?'),
                      actions: [
                        TextButton(
                          autofocus: true,
                          onPressed: () => Navigator.pop(c, false),
                          child: const Text('Keep linked'),
                        ),
                        TextButton(
                          onPressed: () => Navigator.pop(c, true),
                          child: const Text('Unlink'),
                        ),
                      ],
                    ),
                  );
                  if (yes != true) return;
                  final online = await d.unlink();
                  if (context.mounted) {
                    communityMessage(
                      context,
                      online ? 'Discord unlinked.' : 'Local link removed. The bot was unreachable. Use /unlink-app in Discord to revoke it there.',
                    );
                  }
                },
              ),
            ],
            const SizedBox(height: 16),
            if (services.store.text('TouchGrass').isNotEmpty)
              Text(
                services.store.text('TouchGrass') == 'claimed'
                    ? 'Touch Grass · Claimed in Discord'
                    : 'Touch Grass · Waiting to claim when linked',
                style: uiStyle(context, color: accentText),
              ),
            if (d.lastError != null)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 12),
                child: Text(
                  d.lastError!,
                  style: uiStyle(context, color: muted, size: 15),
                ),
              ),
            const SizedBox(height: 20),
            Text(
              'From Discord',
              style: uiStyle(context, size: 22, weight: FontWeight.w600),
            ),
            if (d.inbox.isEmpty)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 16),
                child: Text(
                  'Queued shows and replies appear here.',
                  style: uiStyle(context, color: muted),
                ),
              ),
            for (final item in d.inbox)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 8),
                child: FocusBox(
                  isTV: isTV,
                  onPressed: () async {
                    if (number(item['anilist_id']) > 0) {
                      final a = Anime(
                        id: number(item['anilist_id']),
                        title: item['title'] as String? ?? 'From Discord',
                      );
                      await Navigator.push(
                        context,
                        OtakaseRoute<void>(
                          builder: (_) => ShowPage(
                            services: services,
                            anime: a,
                            isTV: isTV,
                          ),
                        ),
                      );
                    } else {
                      final url = Uri.tryParse(item['url'] as String? ?? '');
                      if (url != null &&
                          url.scheme == 'https' &&
                          const [
                            'discord.com',
                            'discord.gg',
                            'otakase.xyverion.com',
                            'bot.xyverion.com',
                          ].contains(url.host)) {
                        try {
                          await services.auth.launch(url);
                        } catch (e) {
                          if (context.mounted) {
                            communityMessage(context, e.toString());
                          }
                        }
                      }
                    }
                  },
                  padding: const EdgeInsets.all(12),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        item['title'] as String? ?? 'From Discord',
                        style: uiStyle(context, weight: FontWeight.w600),
                      ),
                      if ((item['text'] as String? ?? '').isNotEmpty)
                        Text(
                          item['text'] as String,
                          style: uiStyle(context, color: muted),
                        ),
                    ],
                  ),
                ),
              ),
          ],
        ),
      );
    },
  );
}

class DiscordLinkDialog extends StatefulWidget {
  const DiscordLinkDialog({
    super.key,
    required this.services,
    required this.isTV,
  });
  final AppServices services;
  final bool isTV;
  @override
  State<DiscordLinkDialog> createState() => _DiscordLinkDialogState();
}

class _DiscordLinkDialogState extends State<DiscordLinkDialog> {
  DiscordLinkCode? code;
  final phone = LanPage();
  Uri? url;
  String? error;
  Timer? timer;
  bool polling = false;
  @override
  void initState() {
    super.initState();
    unawaited(begin());
  }

  Future<void> begin() async {
    timer?.cancel();
    widget.services.discord.cancelLink();
    setState(() {
      error = null;
      code = null;
      url = null;
    });
    try {
      final pending = await widget.services.discord.beginLink();
      if (!mounted) return;
      setState(() => code = pending);
      if (widget.isTV) {
        try {
          final uri = await phone.open(
            title: 'Link Discord',
            message:
                'In the Otakase Discord, run:\n/link-app code:${pending.code}\n\nReturn to your TV after linking. This code expires in 10 minutes.',
          );
          if (!mounted) {
            await phone.closeServer();
            return;
          }
          setState(() => url = uri);
        } catch (e) {
          if (mounted) setState(() => error = e.toString());
        }
      }
      timer = Timer.periodic(const Duration(seconds: 3), (_) => poll());
      unawaited(poll());
    } catch (e) {
      if (mounted) setState(() => error = e.toString());
    }
  }

  Future<void> poll() async {
    if (polling || code == null) return;
    polling = true;
    try {
      final done = await widget.services.discord.pollLink(code!);
      if (done && mounted) {
        Navigator.pop(context);
        return;
      }
    } catch (e) {
      if (e is BotFailure && e.status == 410) timer?.cancel();
      if (mounted) setState(() => error = e.toString());
    } finally {
      polling = false;
    }
  }

  @override
  void dispose() {
    timer?.cancel();
    widget.services.discord.cancelLink();
    unawaited(phone.close());
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Link Discord'),
    content: SizedBox(
      width: 420,
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (code == null && error == null)
              const CircularProgressIndicator(),
            if (code != null) ...[
              Text(
                'Run this command in the Otakase Discord:',
                style: uiStyle(context),
              ),
              const SizedBox(height: 12),
              SelectableText(
                '/link-app code:${code!.code}',
                style: mono.copyWith(fontSize: 20, color: accentText),
              ),
              const SizedBox(height: 12),
              if (url != null) ...[
                Center(
                  child: ColoredBox(
                    color: Colors.white,
                    child: QrImageView(data: url.toString(), size: 184),
                  ),
                ),
                const SizedBox(height: 12),
                Text(
                  'Scan with your phone on the same Wi-Fi.',
                  style: uiStyle(context, color: muted),
                ),
              ],
              Text(
                'Waiting for Discord. The code expires in 10 minutes.',
                style: uiStyle(context, color: muted, size: 15),
              ),
            ],
            if (error != null)
              Text(error!, style: uiStyle(context, color: warn)),
          ],
        ),
      ),
    ),
    actions: [
      TextButton(
        autofocus: true,
        onPressed: () => Navigator.pop(context),
        child: const Text('Cancel'),
      ),
      if (code != null && !widget.isTV)
        TextButton(
          onPressed: () async {
            try {
              await widget.services.auth.launch(Uri.parse(discordInviteUrl));
            } catch (e) {
              if (mounted) setState(() => error = e.toString());
            }
          },
          child: const Text('Open Discord'),
        ),
      if (error != null)
        TextButton(onPressed: begin, child: const Text('New code')),
    ],
  );
}

Future<String?> communityText(
  BuildContext context, {
  required String title,
  required bool isTV,
  String initial = '',
  int maxLength = 500,
}) => showDialog<String>(
  context: context,
  builder: (_) => CommunityTextDialog(
    title: title,
    isTV: isTV,
    initial: initial,
    maxLength: maxLength,
  ),
);

class CommunityTextDialog extends StatefulWidget {
  const CommunityTextDialog({
    super.key,
    required this.title,
    required this.isTV,
    this.initial = '',
    this.maxLength = 500,
  });
  final String title, initial;
  final bool isTV;
  final int maxLength;
  @override
  State<CommunityTextDialog> createState() => _CommunityTextDialogState();
}

class _CommunityTextDialogState extends State<CommunityTextDialog> {
  late final text = TextEditingController(text: widget.initial);
  bool voiceAvailable = false;
  String? error;
  @override
  void initState() {
    super.initState();
    unawaited(voiceStatus());
  }

  Future<void> voiceStatus() async {
    try {
      final r = await const MethodChannel('otakase/device')
          .invokeMapMethod<String, dynamic>('status');
      if (mounted) {
        setState(() => voiceAvailable = r?['voiceAvailable'] == true);
      }
    } on PlatformException {
      /* Voice is optional on this device. */
    } on MissingPluginException {
      /* Voice is optional on this device. */
    }
  }

  Future<void> typeOnPhone() async {
    final phone = LanPage();
    try {
      final url = await phone.open(
        title: widget.title,
        message: 'Write your message, then send it to your TV.',
        textInput: true,
        initial: text.text,
        maxLength: widget.maxLength,
      );
      if (!mounted) return;
      String? received;
      final subscription = phone.input.listen((v) {
        received = v;
        if (mounted) Navigator.pop(context);
      });
      await showDialog<void>(
        context: context,
        builder: (c) => AlertDialog(
          title: const Text('Type on phone'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              ColoredBox(
                color: Colors.white,
                child: QrImageView(data: url.toString(), size: 184),
              ),
              const SizedBox(height: 16),
              const Text(
                'Scan with your phone on the same Wi-Fi. This page closes in 10 minutes.',
              ),
            ],
          ),
          actions: [
            TextButton(
              autofocus: true,
              onPressed: () => Navigator.pop(c),
              child: const Text('Back'),
            ),
          ],
        ),
      );
      await subscription.cancel();
      if (mounted && received != null) setState(() => text.text = received!);
    } catch (e) {
      if (mounted) setState(() => error = e.toString());
    } finally {
      await phone.close();
    }
  }

  @override
  void dispose() {
    text.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(widget.title),
    content: SizedBox(
      width: 480,
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              key: const Key('capsule-text'),
              controller: text,
              minLines: 3,
              maxLines: 8,
              maxLength: widget.maxLength,
              decoration: const InputDecoration(labelText: 'Your message'),
              onChanged: (_) => setState(() {}),
            ),
            if (widget.isTV)
              AppButton(
                label: 'Type on phone',
                isTV: true,
                onPressed: typeOnPhone,
              ),
            if (voiceAvailable)
              TextButton.icon(
                icon: const Icon(LucideIcons.mic),
                label: const Text('Use voice'),
                onPressed: () async {
                  try {
                    final value = await CoreClient.channel.invokeMethod<String>(
                      'voiceSearch',
                    );
                    if (value != null && mounted) {
                      setState(
                        () => text.text = clipRunes(value, widget.maxLength),
                      );
                    }
                  } catch (e) {
                    if (mounted) setState(() => error = e.toString());
                  }
                },
              ),
            if (error != null)
              Text(error!, style: uiStyle(context, color: warn)),
          ],
        ),
      ),
    ),
    actions: [
      TextButton(
        autofocus: true,
        onPressed: () => Navigator.pop(context),
        child: const Text('Cancel'),
      ),
      TextButton(
        onPressed: text.text.trim().isEmpty
            ? null
            : () => Navigator.pop(
                context,
                clipRunes(text.text.trim(), widget.maxLength),
              ),
        child: const Text('Save'),
      ),
    ],
  );
}

Future<void> offerRatingShare(
  BuildContext context,
  AppServices services,
  Anime anime,
  double score,
  bool isTV,
) async {
  if (!services.discord.ratingWanted(anime, score)) return;
  final choice = await chooseValue(
    context,
    'Share your score in the Otakase Discord?',
    [
      'Share my score',
      'Share with a few words',
      'Not this time',
      'Don’t ask again',
    ],
    selected: 'Not this time',
  );
  if (choice == 'Don’t ask again') {
    await services.store.set('DiscordShareRatings', 'never');
    return;
  }
  if (choice != 'Share my score' && choice != 'Share with a few words' ||
      !context.mounted) {
    return;
  }
  String text = '';
  if (choice == 'Share with a few words') {
    final value = await communityText(
      context,
      title: 'A few words about it',
      isTV: isTV,
      maxLength: 300,
    );
    if (value == null) return;
    text = value;
  }
  try {
    await services.discord.shareRating(anime, score, text: text);
    if (context.mounted) {
      communityMessage(context, 'Shared your score in the Otakase Discord.');
    }
  } catch (e) {
    if (context.mounted) {
      communityMessage(context, 'Could not share your score: $e');
    }
  }
}

class CapsulePage extends StatefulWidget {
  const CapsulePage({super.key, required this.services, required this.isTV});
  final AppServices services;
  final bool isTV;
  @override
  State<CapsulePage> createState() => _CapsulePageState();
}

class _CapsulePageState extends State<CapsulePage> {
  Json? mine;
  String? error;
  bool busy = false;
  @override
  void initState() {
    super.initState();
    unawaited(load());
  }

  Future<void> load() async {
    if (!widget.services.discord.linked) return;
    setState(() => busy = true);
    try {
      final r = await widget.services.heritage.communityMine();
      if (mounted) {
        setState(() {
          mine = r;
          error = null;
        });
      }
    } catch (e) {
      if (mounted) setState(() => error = e.toString());
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  Future<void> personal() async {
    final h = widget.services.heritage;
    final stats = h.stats.snapshot();
    final text = await communityText(
      context,
      title: 'A letter to future you',
      isTV: widget.isTV,
      initial: h.pending?['text'] as String? ?? '',
    );
    if (text == null) return;
    await h.writeLetter(text, snapshot: await stats);
    if (mounted) setState(() => error = null);
  }

  Future<void> community() async {
    var initial = mine?['text'] as String? ?? '';
    while (true) {
      if (!mounted) return;
      final text = await communityText(
        context,
        title: 'For the community',
        isTV: widget.isTV,
        initial: initial,
        maxLength: 300,
      );
      if (text == null) return;
      initial = text;
      try {
        await widget.services.heritage.writeCommunity(text);
        await load();
        return;
      } catch (e) {
        if (!mounted) return;
        setState(() => error = e.toString());
        if (e is! BotFailure || e.status != 422) return;
        await showDialog<void>(
          context: context,
          builder: (c) => AlertDialog(
            title: const Text('Revise your message'),
            content: Text(e.message),
            actions: [
              TextButton(
                autofocus: true,
                onPressed: () => Navigator.pop(c),
                child: const Text('Edit'),
              ),
            ],
          ),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final h = widget.services.heritage;
    final year = nextBirthday(h.now());
    return Scaffold(
      appBar: AppBar(
        title: const Text('Time capsule'),
        actions: [
          DeviceStatus(isTV: widget.isTV),
          const SizedBox(width: 16),
        ],
      ),
      body: ListView(
        padding: const EdgeInsets.all(24),
        children: [
          const PageHeading('For a future day', '封'),
          const SizedBox(height: 20),
          Text(
            'Opens April 12, $year',
            style: uiStyle(context, size: 22, weight: FontWeight.w600),
          ),
          const SizedBox(height: 12),
          Text(
            'A letter to future you, beside your stats from today.',
            style: uiStyle(context, color: muted),
          ),
          const SizedBox(height: 16),
          if (h.pending != null) ...[
            Text(h.pending!['text'] as String, style: uiStyle(context)),
            const SizedBox(height: 16),
          ],
          AppButton(
            label: h.pending == null ? 'Write a letter' : 'Replace your letter',
            autofocus: true,
            isTV: widget.isTV,
            onPressed: personal,
          ),
          const SizedBox(height: 28),
          Text(
            'Community capsule',
            style: uiStyle(context, size: 22, weight: FontWeight.w600),
          ),
          const SizedBox(height: 12),
          Text(
            'Anyone might read it on April 12. Leave out names, links and contact details.',
            style: uiStyle(context, color: muted),
          ),
          const SizedBox(height: 16),
          if (!widget.services.discord.linked)
            Text(
              'Link Discord to leave a community message.',
              style: uiStyle(context),
            )
          else ...[
            if (mine != null)
              Text(mine!['text'] as String? ?? '', style: uiStyle(context)),
            AppButton(
              label: mine == null
                  ? 'Leave a message'
                  : 'Replace community message',
              isTV: widget.isTV,
              onPressed: busy ? null : community,
            ),
            if (mine != null)
              TextButton(
                onPressed: busy
                    ? null
                    : () async {
                        try {
                          await h.removeCommunity();
                          await load();
                        } catch (e) {
                          if (mounted) setState(() => error = e.toString());
                        }
                      },
                child: const Text('Remove community message'),
              ),
          ],
          if (busy) const LinearProgressIndicator(),
          if (error != null) Text(error!, style: uiStyle(context, color: warn)),
        ],
      ),
    );
  }
}

String statsDifference(int before, int after) =>
    '$before → $after${after > before
        ? ' (+${after - before})'
        : after < before
        ? ' (−${before - after})'
        : ''}';
Future<void> birthdayCapsule(
  BuildContext context,
  AppServices s,
  bool isTV,
) async {
  final h = s.heritage, date = s.heritage.now();
  if (!isBirthday(date) || h.closed) return;
  final due = h.dueLetters(date.year);
  final today = due.isEmpty ? null : await h.stats.snapshot();
  for (final index in due) {
    if (!context.mounted || h.closed) return;
    final letter = h.letters[index];
    final then = letter['stats'] is Map
        ? WatchStats.fromJson((letter['stats'] as Map).cast<String, dynamic>())
        : null;
    await showDialog<void>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('From a past you'),
        content: SizedBox(
          width: 460,
          child: SingleChildScrollView(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(
                  letter['text'] as String? ?? '',
                  style: uiStyle(c, size: 21),
                ),
                const SizedBox(height: 20),
                if (then != null) ...[
                  Text(
                    today == null ? 'When you wrote this' : 'Then and now',
                    style: uiStyle(c, weight: FontWeight.w600),
                  ),
                  for (final row in {
                    'Anime': today == null
                        ? '${then.anime}'
                        : statsDifference(then.anime, today.anime),
                    'Episodes': today == null
                        ? '${then.episodes}'
                        : statsDifference(then.episodes, today.episodes),
                    'Minutes watched': today == null
                        ? '${then.minutes}'
                        : statsDifference(then.minutes, today.minutes),
                    'Completed': today == null
                        ? '${then.completed}'
                        : statsDifference(then.completed, today.completed),
                    if (then.meanScore > 0)
                      'Mean score': today == null
                          ? '${then.meanScore}'
                          : '${then.meanScore} → ${today.meanScore}',
                    if (then.topGenre.isNotEmpty)
                      'Top genre': today == null
                          ? then.topGenre
                          : then.topGenre == today.topGenre
                          ? '${then.topGenre}, still'
                          : '${then.topGenre} → ${today.topGenre}',
                  }.entries)
                    Text('${row.key} · ${row.value}', style: uiStyle(c)),
                ],
              ],
            ),
          ),
        ),
        actions: [
          TextButton(
            autofocus: true,
            onPressed: () => Navigator.pop(c),
            child: const Text('Keep going'),
          ),
        ],
      ),
    );
    await h.markOpened(index);
  }
  if (!context.mounted || h.closed) return;
  try {
    final note = await h.birthdayCommunity(date.year);
    if (note != null &&
        (note['text'] as String? ?? '').isNotEmpty &&
        context.mounted &&
        !h.closed) {
      final report = await showDialog<bool>(
        context: context,
        builder: (c) => AlertDialog(
          title: const Text('From the community capsule'),
          content: Text(note['text'] as String),
          actions: [
            if (s.discord.linked)
              TextButton(
                onPressed: () => Navigator.pop(c, true),
                child: const Text('Report'),
              ),
            TextButton(
              autofocus: true,
              onPressed: () => Navigator.pop(c, false),
              child: const Text('Keep going'),
            ),
          ],
        ),
      );
      if (report == true) await h.reportCommunity(note['id'] as String);
    }
  } catch (e) {
    if (context.mounted) communityMessage(context, e.toString());
  }
  if (number(h.capsule['offered']) >= date.year ||
      h.pending != null ||
      !context.mounted ||
      h.closed) {
    return;
  }
  await h.markOffered(date.year);
  if (!context.mounted) return;
  final write = await showDialog<bool>(
    context: context,
    builder: (c) => AlertDialog(
      title: Text('Otakase turns ${date.year - 2026} today'),
      content: Text(
        'Leave a message for next year’s you? It opens April 12, ${date.year + 1}.',
      ),
      actions: [
        TextButton(
          autofocus: true,
          onPressed: () => Navigator.pop(c, false),
          child: const Text('Later'),
        ),
        TextButton(
          onPressed: () => Navigator.pop(c, true),
          child: const Text('Write a letter'),
        ),
      ],
    ),
  );
  if (write == true && context.mounted) {
    await Navigator.push(
      context,
      OtakaseRoute<void>(
        builder: (_) => CapsulePage(services: s, isTV: isTV),
      ),
    );
  }
}
