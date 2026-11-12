import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';
import 'package:qr_flutter/qr_flutter.dart';

import '../core.dart';
import '../data/account.dart';
import '../data/account_center.dart';
import '../data/account_sync.dart';
import '../data/account_vault.dart';
import '../data/models.dart';
import '../desktop/desktop_core.dart';
import '../services.dart';
import 'components.dart';

// The Otakase account card at the top of Accounts, the Devices page and the
// sign-in dialogs. Signed out, the app works exactly as before.

/// One tappable line in the account screens, laid out like Settings rows:
/// an icon, a title, a short line under it and a chevron.
class AccountRow extends StatelessWidget {
  const AccountRow({
    super.key,
    required this.icon,
    required this.title,
    this.subtitle,
    this.onTap,
    this.isTV = false,
    this.autofocus = false,
    this.danger = false,
    this.trailing,
  });
  final IconData icon;
  final String title;
  final String? subtitle;
  final VoidCallback? onTap;
  final bool isTV, autofocus, danger;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final color = onTap == null ? muted : (danger ? warn : fg);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 2),
      child: FocusBox(
        isTV: isTV,
        autofocus: autofocus,
        radius: 12,
        onPressed: onTap,
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 12),
        child: Row(
          children: [
            Icon(icon, size: 21, color: color),
            const SizedBox(width: 14),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    title,
                    style: uiStyle(
                      context,
                      weight: FontWeight.w500,
                      color: color,
                    ),
                  ),
                  if (subtitle != null) ...[
                    const SizedBox(height: 4),
                    Text(
                      subtitle!,
                      style: uiStyle(context, size: 15, color: muted),
                    ),
                  ],
                ],
              ),
            ),
            ?trailing,
            if (trailing == null && onTap != null)
              const Icon(LucideIcons.chevronRight, size: 18, color: muted),
          ],
        ),
      ),
    );
  }
}

/// A heading over a group of [AccountRow]s.
class AccountRowsHeading extends StatelessWidget {
  const AccountRowsHeading(this.title, {super.key});
  final String title;
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.fromLTRB(12, 20, 12, 6),
    child: SectionLabel(title),
  );
}

Future<bool?> _confirm(
  BuildContext context,
  String title,
  String text,
  String action,
) => showDialog<bool>(
  context: context,
  builder: (context) => AlertDialog(
    title: Text(title),
    content: Text(text),
    actions: [
      TextButton(
        autofocus: true,
        onPressed: () => Navigator.pop(context, false),
        child: const Text('Cancel'),
      ),
      FilledButton(
        onPressed: () => Navigator.pop(context, true),
        child: Text(action),
      ),
    ],
  ),
);

String _ago(int ms) {
  if (ms <= 0) return 'never';
  final d = DateTime.now().difference(DateTime.fromMillisecondsSinceEpoch(ms));
  if (d.inMinutes < 2) return 'just now';
  if (d.inHours < 1) return '${d.inMinutes} min ago';
  if (d.inDays < 1) return '${d.inHours} h ago';
  if (d.inDays < 30) return '${d.inDays} days ago';
  return 'over a month ago';
}

String accountSyncLine(AccountRun run) {
  if (run.sync.lastError != null) return 'Sync waits: ${run.sync.lastError}';
  if (run.vault.lastError != null) return 'Logins wait: ${run.vault.lastError}';
  if (!run.live.connected) return 'Saved on this device · connecting…';
  if (run.sync.lastSynced == null) return 'Syncing…';
  return 'In sync on every device';
}

/// The Otakase account card at the top of Accounts.
class OtakaseAccountSection extends StatefulWidget {
  const OtakaseAccountSection({
    super.key,
    required this.services,
    required this.isTV,
  });
  final AppServices services;
  final bool isTV;
  @override
  State<OtakaseAccountSection> createState() => _OtakaseAccountSectionState();
}

class _OtakaseAccountSectionState extends State<OtakaseAccountSection> {
  String? error;
  bool busy = false;

  AccountCenter? get center => widget.services.profiles?.accounts;

  @override
  void initState() {
    super.initState();
    center?.askUpload = (session) => askUpload(context, session);
  }

  Future<void> run(Future<void> Function() action) async {
    setState(() {
      error = null;
      busy = true;
    });
    try {
      await action();
    } catch (e) {
      if (mounted) setState(() => error = errorMessage(e));
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  Future<void> signIn() => run(() async {
    final c = center!;
    // A TV shows a QR code; a phone or computer signs in in the browser.
    if (widget.isTV && !isDesktop) {
      await showDialog<void>(
        context: context,
        builder: (_) => PairDialog(manager: c.manager),
      );
      return;
    }
    final url = await c.manager.begin(desktop: isDesktop);
    await widget.services.auth.launch(url);
    if (!mounted) return;
    await showDialog<void>(
      context: context,
      builder: (_) => _WaitDialog(manager: c.manager, url: url),
    );
    await c.manager.cancel();
  });

  @override
  Widget build(BuildContext context) {
    final c = center;
    if (c == null) return const SizedBox.shrink();
    return ListenableBuilder(
      listenable: Listenable.merge([c, c.manager]),
      builder: (context, _) {
        final p = widget.services.profiles!;
        final profile = p.profiles
            .where((x) => x.id == widget.services.profileId)
            .firstOrNull;
        final runFor = profile == null || profile.account.isEmpty
            ? null
            : c.runFor(profile.account);
        final others = c.manager.accounts
            .where((a) => a.id != runFor?.session.id)
            .toList();
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              'Otakase account',
              style: uiStyle(context, size: 21, weight: FontWeight.w600),
            ),
            const SizedBox(height: 12),
            if (runFor != null)
              ..._signedIn(context, c, runFor)
            else ...[
              Text(
                'Sign in once and your profiles, settings and tracker logins '
                'follow you to every phone, TV and computer. Optional: '
                'everything works without it.',
                style: uiStyle(context, size: 15, color: muted),
              ),
              const SizedBox(height: 8),
              for (final a in others)
                AccountRow(
                  icon: LucideIcons.userPlus,
                  title: 'Add this profile to ${a.name}',
                  subtitle: 'Use the account already signed in here',
                  isTV: widget.isTV,
                  onTap: busy
                      ? null
                      : () => run(() async {
                          await p.bind(widget.services.profileId, a.id);
                          await c.runFor(a.id)?.sync.sync();
                        }),
                ),
              AccountRow(
                key: const Key('account-sign-in'),
                icon: LucideIcons.logIn,
                title: others.isEmpty
                    ? 'Sign in with Discord or Google'
                    : 'Sign in to another account',
                subtitle: busy ? 'Waiting for the browser…' : null,
                isTV: widget.isTV,
                autofocus: widget.isTV,
                onTap: busy ? null : signIn,
              ),
            ],
            if (error ?? c.manager.lastError case final message?) ...[
              const SizedBox(height: 8),
              Text(message, style: uiStyle(context, size: 15, color: warn)),
            ],
            const Divider(height: 40),
          ],
        );
      },
    );
  }

  List<Widget> _signedIn(BuildContext context, AccountCenter c, AccountRun r) {
    final s = r.session;
    return [
      Row(
        children: [
          if (s.avatar.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(right: 12),
              child: CircleAvatar(
                radius: 18,
                backgroundImage: NetworkImage(s.avatar),
              ),
            ),
          Expanded(child: Text(s.name, style: uiStyle(context))),
          if (s.role != 'user')
            Text(
              s.role[0].toUpperCase() + s.role.substring(1),
              style: uiStyle(context, size: 14, color: accentText),
            ),
        ],
      ),
      const SizedBox(height: 4),
      Text(
        accountSyncLine(r),
        style: uiStyle(
          context,
          size: 15,
          color: r.sync.lastError != null || r.vault.lastError != null
              ? warn
              : muted,
        ),
      ),
      if (r.vault.waiting.isNotEmpty ||
          !r.vault.hasKey && r.vault.keyVersion > 0) ...[
        const SizedBox(height: 4),
        Text(
          r.vault.hasKey
              ? '${r.vault.waiting.length == 1 ? 'A device waits' : '${r.vault.waiting.length} devices wait'} for your logins. Open Devices to approve.'
              : 'Approve this device in Devices on one of your other devices to bring your tracker logins here.',
          style: uiStyle(context, size: 15, color: warn),
        ),
      ],
      const SizedBox(height: 8),
      AccountRow(
        icon: LucideIcons.monitorSmartphone,
        title: 'Devices',
        subtitle: r.vault.waiting.isNotEmpty && r.vault.hasKey
            ? '${r.vault.waiting.length} waiting for approval'
            : 'See, rename and remove your devices',
        isTV: widget.isTV,
        autofocus: widget.isTV,
        onTap: () => Navigator.push(
          context,
          MaterialPageRoute<void>(
            builder: (_) => DevicesPage(center: c, run: r, isTV: widget.isTV),
          ),
        ),
      ),
      if (!widget.isTV || isDesktop)
        AccountRow(
          icon: LucideIcons.tv,
          title: 'Add a TV',
          subtitle: 'Type the code the TV shows',
          isTV: widget.isTV,
          onTap: () => showDialog<void>(
            context: context,
            builder: (_) => _ApproveDialog(manager: c.manager, session: s),
          ),
        ),
      AccountRow(
        icon: LucideIcons.refreshCw,
        title: 'What syncs',
        subtitle: 'Choose what this device shares',
        isTV: widget.isTV,
        onTap: () => Navigator.push(
          context,
          MaterialPageRoute<void>(
            builder: (_) =>
                SyncSettingsPage(center: c, run: r, isTV: widget.isTV),
          ),
        ),
      ),
      AccountRow(
        key: const Key('account-sign-out'),
        icon: LucideIcons.logOut,
        title: 'Sign out',
        subtitle: 'Profiles stay on this device',
        isTV: widget.isTV,
        onTap: busy
            ? null
            : () async {
                final sure = await _confirm(
                  context,
                  'Sign out of ${s.name}?',
                  'Profiles stay on this device as local profiles. Your tracker logins stay too.',
                  'Sign out',
                );
                if (sure == true) await run(() => c.manager.signOut(s));
              },
      ),
    ];
  }
}

/// First sign-in on a device that has profiles of its own.
Future<bool?> askUpload(BuildContext context, AccountSession session) =>
    showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text('Welcome back, ${session.name}'),
        content: const Text(
          'Your account already has profiles. Add them to this device, or add '
          "this device's profiles and settings to your account?",
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text("Add this device's"),
          ),
          FilledButton(
            autofocus: true,
            onPressed: () => Navigator.pop(context, false),
            child: const Text("Use my account's"),
          ),
        ],
      ),
    );

class _WaitDialog extends StatefulWidget {
  const _WaitDialog({required this.manager, required this.url});
  final AccountManager manager;
  final Uri url;
  @override
  State<_WaitDialog> createState() => _WaitDialogState();
}

class _WaitDialogState extends State<_WaitDialog> {
  late final int signIns;
  bool copied = false;
  bool closed = false;

  @override
  void initState() {
    super.initState();
    signIns = widget.manager.signIns;
    widget.manager.addListener(_check);
  }

  void _check() {
    if (widget.manager.signIns != signIns && mounted && !closed) {
      closed = true;
      Navigator.pop(context);
    }
  }

  @override
  void dispose() {
    widget.manager.removeListener(_check);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Sign in to Otakase'),
    content: Text(
      copied
          ? 'Link copied. Open it in any browser on this device.'
          : 'Finish signing in in your browser. This closes on its own.',
    ),
    actions: [
      TextButton(
        onPressed: () async {
          await Clipboard.setData(ClipboardData(text: widget.url.toString()));
          if (mounted) setState(() => copied = true);
        },
        child: const Text('Copy link'),
      ),
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('Cancel'),
      ),
    ],
  );
}

/// A TV signs in with a QR code that any phone browser can approve.
class PairDialog extends StatefulWidget {
  const PairDialog({super.key, required this.manager});
  final AccountManager manager;
  @override
  State<PairDialog> createState() => _PairDialogState();
}

class _PairDialogState extends State<PairDialog> {
  PairCode? pair;
  String? error;
  Timer? timer;
  bool polling = false;

  @override
  void initState() {
    super.initState();
    unawaited(_new());
  }

  Future<void> _new() async {
    timer?.cancel();
    setState(() {
      pair = null;
      error = null;
    });
    try {
      final p = await widget.manager.startPair();
      if (!mounted) return;
      setState(() => pair = p);
      timer = Timer.periodic(
        const Duration(seconds: 3),
        (_) => unawaited(_poll()),
      );
    } catch (e) {
      if (mounted) setState(() => error = errorMessage(e));
    }
  }

  Future<void> _poll() async {
    final p = pair;
    if (p == null || polling) return;
    polling = true;
    try {
      final session = await widget.manager.pollPair(p);
      if (session != null && mounted) {
        timer?.cancel();
        Navigator.pop(context);
      }
    } on AccountFailure catch (e) {
      if (e.status == 410 || e.status == 404) {
        await _new();
      } else if (mounted) {
        setState(() => error = e.message);
      }
    } catch (_) {
      /* Try again on the next tick. */
    } finally {
      polling = false;
    }
  }

  @override
  void dispose() {
    timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final p = pair;
    return AlertDialog(
      title: const Text('Sign in to Otakase'),
      content: SizedBox(
        width: 380,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (p == null && error == null)
                const Padding(
                  padding: EdgeInsets.all(40),
                  child: CircularProgressIndicator(),
                ),
              if (p != null) ...[
                Container(
                  color: Colors.white,
                  padding: const EdgeInsets.all(12),
                  child: QrImageView(
                    data: p.url,
                    size: 240,
                    backgroundColor: Colors.white,
                  ),
                ),
                const SizedBox(height: 16),
                Text(
                  p.spaced,
                  style: uiStyle(context, size: 34, weight: FontWeight.w700),
                ),
                const SizedBox(height: 8),
                Text(
                  'Scan with your phone and sign in with Discord or Google. '
                  'Or open Accounts in Otakase on your phone, pick Add a TV '
                  'and type the digits.',
                  textAlign: TextAlign.center,
                  style: uiStyle(context, size: 15, color: muted),
                ),
              ],
              if (error != null)
                Text(error!, style: uiStyle(context, color: warn)),
            ],
          ),
        ),
      ),
      actions: [
        if (error != null)
          TextButton(onPressed: _new, child: const Text('New code')),
        TextButton(
          autofocus: true,
          onPressed: () => Navigator.pop(context),
          child: const Text('Cancel'),
        ),
      ],
    );
  }
}

class _ApproveDialog extends StatefulWidget {
  const _ApproveDialog({required this.manager, required this.session});
  final AccountManager manager;
  final AccountSession session;
  @override
  State<_ApproveDialog> createState() => _ApproveDialogState();
}

class _ApproveDialogState extends State<_ApproveDialog> {
  final code = TextEditingController();
  String? message;
  bool busy = false, done = false;

  @override
  void dispose() {
    code.dispose();
    super.dispose();
  }

  Future<void> approve() async {
    setState(() {
      busy = true;
      message = null;
    });
    try {
      final name = await widget.manager.approve(widget.session, code.text);
      if (!mounted) return;
      setState(() {
        done = true;
        message = '$name is signed in.';
      });
    } catch (e) {
      if (mounted) setState(() => message = errorMessage(e));
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Add a TV'),
    content: Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Text(
          'On the TV, open Accounts and pick Otakase account. Type the 6 digits it shows.',
        ),
        const SizedBox(height: 12),
        TextField(
          controller: code,
          autofocus: true,
          enabled: !done,
          keyboardType: TextInputType.number,
          maxLength: 7,
          decoration: const InputDecoration(hintText: '123 456'),
          onSubmitted: (_) => approve(),
        ),
        if (message != null)
          Text(
            message!,
            style: uiStyle(context, size: 15, color: done ? ok : warn),
          ),
      ],
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: Text(done ? 'Done' : 'Cancel'),
      ),
      if (!done)
        FilledButton(
          onPressed: busy ? null : approve,
          child: const Text('Add TV'),
        ),
    ],
  );
}

/// Devices on the account: name, type, last seen; rename, remove, and
/// approve new ones for the token vault.
class DevicesPage extends StatefulWidget {
  const DevicesPage({
    super.key,
    required this.center,
    required this.run,
    required this.isTV,
  });
  final AccountCenter center;
  final AccountRun run;
  final bool isTV;
  @override
  State<DevicesPage> createState() => _DevicesPageState();
}

class _DevicesPageState extends State<DevicesPage> {
  List<Json> devices = const [];
  String? error;
  bool loading = true;
  String ownCheck = '';

  AccountSession get session => widget.run.session;

  @override
  void initState() {
    super.initState();
    widget.run.live.addListener(_reloadOnline);
    unawaited(load());
    unawaited(widget.run.vault.sync());
    unawaited(
      widget.run.vault.ownCheck().then((c) {
        if (mounted) setState(() => ownCheck = c);
      }),
    );
  }

  void _reloadOnline() {
    if (mounted) setState(() {});
  }

  @override
  void dispose() {
    widget.run.live.removeListener(_reloadOnline);
    super.dispose();
  }

  Future<void> load() async {
    try {
      final r = await session.request('GET', '/devices');
      if (!mounted) return;
      setState(() {
        devices = [
          for (final d in r['devices'] as List? ?? const [])
            if (d is Map) d.cast<String, dynamic>(),
        ];
        loading = false;
        error = null;
      });
    } catch (e) {
      if (mounted) {
        setState(() {
          loading = false;
          error = errorMessage(e);
        });
      }
    }
  }

  Future<void> act(Future<void> Function() action) async {
    try {
      await action();
      await load();
    } catch (e) {
      if (mounted) setState(() => error = errorMessage(e));
    }
  }

  Future<void> rename(Json d) async {
    final controller = TextEditingController(text: d['name'] as String? ?? '');
    final name = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Rename device'),
        content: TextField(
          controller: controller,
          autofocus: true,
          maxLength: 48,
          onSubmitted: (v) => Navigator.pop(context, v),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, controller.text),
            child: const Text('Save'),
          ),
        ],
      ),
    );
    controller.dispose();
    if (name == null || name.trim().isEmpty) return;
    await act(
      () => session.request(
        'PATCH',
        '/devices/${d['id']}',
        body: {'name': name.trim()},
      ),
    );
  }

  Future<void> deviceMenu(Json d) async {
    final mine = d['id'] == session.deviceId;
    final choice = await showModalBottomSheet<String>(
      context: context,
      backgroundColor: panel,
      builder: (context) => SafeArea(
        child: Padding(
          padding: const EdgeInsets.all(12),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              AccountRow(
                icon: LucideIcons.pencil,
                title: 'Rename',
                isTV: widget.isTV,
                autofocus: widget.isTV,
                onTap: () => Navigator.pop(context, 'rename'),
              ),
              AccountRow(
                icon: mine ? LucideIcons.logOut : LucideIcons.x,
                title: mine ? 'Sign out of this device' : 'Remove device',
                danger: true,
                isTV: widget.isTV,
                onTap: () => Navigator.pop(context, 'remove'),
              ),
            ],
          ),
        ),
      ),
    );
    if (choice == 'rename') await rename(d);
    if (choice == 'remove') await remove(d);
  }

  Future<void> remove(Json d) async {
    final mine = d['id'] == session.deviceId;
    final sure = await confirm(
      mine ? 'Sign out of this device?' : 'Remove ${d['name']}?',
      mine
          ? 'Profiles stay on this device as local profiles. Your tracker logins stay too.'
          : 'It is signed out of your account and stops getting your settings and logins.',
      mine ? 'Sign out' : 'Remove',
    );
    if (sure != true) return;
    if (mine) {
      await act(() => widget.center.manager.signOut(session));
      if (mounted) Navigator.pop(context);
      return;
    }
    await act(() async {
      await session.request('DELETE', '/devices/${d['id']}');
      // Whoever removes a device gives the others a new vault key.
      await widget.run.vault.sync();
    });
  }

  Future<bool?> confirm(String title, String text, String action) =>
      showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          title: Text(title),
          content: Text(text),
          actions: [
            TextButton(
              autofocus: true,
              onPressed: () => Navigator.pop(context, false),
              child: const Text('Cancel'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, true),
              child: Text(action),
            ),
          ],
        ),
      );

  Future<void> approve(WaitingDevice w) async {
    final sure = await confirm(
      'Give ${w.name} your logins?',
      'Check that ${w.name} shows the code ${w.check} under Devices. Only approve your own devices.',
      'Approve',
    );
    if (sure == true) await act(() => widget.run.vault.approve(w));
  }

  Future<void> export() async {
    await act(() async {
      final r = await session.request(
        'GET',
        '/me/export',
        timeout: const Duration(seconds: 30),
      );
      r.remove('_status');
      final text = const JsonEncoder.withIndent('  ').convert(r);
      final folder = isDesktop ? await getDownloadsDirectory() : null;
      if (folder != null) {
        final file = File(
          '${folder.path}${Platform.pathSeparator}otakase-account.json',
        );
        await file.writeAsString(text);
        if (mounted) setState(() => error = 'Saved to ${file.path}');
      } else {
        await Clipboard.setData(ClipboardData(text: text));
        if (mounted) {
          setState(
            () => error =
                'Your account data is copied. Paste it anywhere to keep it.',
          );
        }
      }
    });
  }

  Future<void> attach(String provider) => act(() async {
    final r = await session.request(
      'POST',
      '/attach',
      body: {'provider': provider},
    );
    await widget.center.profiles.session?.auth.launch(
      Uri.parse(r['url'] as String),
    );
  });

  Future<void> deleteAccount() async {
    final sure = await confirm(
      'Delete your Otakase account?',
      'This removes your synced profiles, settings and sealed logins from the server for every device. '
          'Profiles on this device stay as local profiles. It cannot be undone.',
      'Delete account',
    );
    if (sure != true) return;
    await act(() => widget.center.manager.deleteAccount(session));
    if (mounted) Navigator.pop(context);
  }

  @override
  Widget build(BuildContext context) {
    final online = {for (final d in widget.run.live.online) d.device};
    final vault = widget.run.vault;
    return Scaffold(
      backgroundColor: panel,
      appBar: AppBar(title: Text('Devices · ${session.name}')),
      body: ListenableBuilder(
        listenable: vault,
        builder: (context, _) => ListView(
          padding: const EdgeInsets.fromLTRB(12, 12, 12, 24),
          children: [
            if (error != null)
              Padding(
                padding: const EdgeInsets.fromLTRB(12, 0, 12, 8),
                child: Text(error!, style: uiStyle(context, color: warn)),
              ),
            if (vault.waiting.isNotEmpty && vault.hasKey) ...[
              const AccountRowsHeading('Waiting for your logins'),
              for (final w in vault.waiting)
                AccountRow(
                  icon: LucideIcons.shieldCheck,
                  title: 'Approve ${w.name}',
                  subtitle: 'It shows the code ${w.check}',
                  isTV: widget.isTV,
                  onTap: () => approve(w),
                ),
            ],
            if (!vault.hasKey && vault.keyVersion > 0 && ownCheck.isNotEmpty)
              Padding(
                padding: const EdgeInsets.fromLTRB(12, 8, 12, 0),
                child: Text(
                  'To bring your tracker logins here, open Devices on another of your devices and approve this one. It shows the code $ownCheck.',
                  style: uiStyle(context, size: 15, color: warn),
                ),
              ),
            const AccountRowsHeading('Your devices'),
            if (loading)
              const Padding(
                padding: EdgeInsets.all(16),
                child: Center(child: CircularProgressIndicator()),
              ),
            for (final d in devices)
              AccountRow(
                icon: d['is_tv'] == 1
                    ? LucideIcons.tv
                    : d['os'] == 'android'
                    ? LucideIcons.smartphone
                    : LucideIcons.monitor,
                title:
                    '${d['name']}${d['id'] == session.deviceId ? ' · this device' : ''}',
                subtitle: online.contains(d['id'])
                    ? 'Online now'
                    : 'Last seen ${_ago(number(d['last_seen']))}',
                isTV: widget.isTV,
                onTap: () => deviceMenu(d),
              ),
            const AccountRowsHeading('Sign-ins'),
            AccountRow(
              icon: LucideIcons.messageCircle,
              title: session.hasDiscord ? 'Discord' : 'Add Discord',
              subtitle: session.hasDiscord
                  ? 'Linked'
                  : 'Sign in with Discord too',
              isTV: widget.isTV,
              onTap: session.hasDiscord ? null : () => attach('discord'),
            ),
            AccountRow(
              icon: LucideIcons.mail,
              title: 'Add Google',
              subtitle: 'Sign in with Google too',
              isTV: widget.isTV,
              onTap: () => attach('google'),
            ),
            const AccountRowsHeading('Your data'),
            AccountRow(
              icon: LucideIcons.download,
              title: 'Export my data',
              subtitle: isDesktop
                  ? 'Saves everything the account keeps to Downloads'
                  : 'Copies everything the account keeps',
              isTV: widget.isTV,
              onTap: export,
            ),
            AccountRow(
              icon: LucideIcons.trash2,
              title: 'Delete account',
              subtitle: 'Removes it for every device. Cannot be undone.',
              danger: true,
              isTV: widget.isTV,
              onTap: deleteAccount,
            ),
          ],
        ),
      ),
    );
  }
}

/// The per-part sync switches for this device.
class SyncSettingsPage extends StatefulWidget {
  const SyncSettingsPage({
    super.key,
    required this.center,
    required this.run,
    required this.isTV,
  });
  final AccountCenter center;
  final AccountRun run;
  final bool isTV;
  @override
  State<SyncSettingsPage> createState() => _SyncSettingsPageState();
}

class _SyncSettingsPageState extends State<SyncSettingsPage> {
  @override
  Widget build(BuildContext context) {
    final switches = widget.run.sync.switches;
    return Scaffold(
      backgroundColor: panel,
      appBar: AppBar(title: const Text('What syncs')),
      body: ListView(
        padding: const EdgeInsets.all(24),
        children: [
          Text(
            'Choose what this device shares with your account. Watch '
            'progress always stays with AniList, MAL and Trakt.',
            style: uiStyle(context, size: 15, color: muted),
          ),
          for (final part in SyncPart.values)
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              autofocus: widget.isTV && part == SyncPart.values.first,
              title: Text(part.label),
              value: switches.on(part),
              onChanged: (value) async {
                await switches.set(part, value);
                setState(() {});
                if (value) {
                  await widget.run.sync.sync();
                  if (part == SyncPart.logins) await widget.run.vault.sync();
                }
              },
            ),
        ],
      ),
    );
  }
}
