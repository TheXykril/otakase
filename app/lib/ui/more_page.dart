import 'dart:async';

import 'package:flutter/material.dart';
import 'package:qr_flutter/qr_flutter.dart';

import '../data/auth.dart';
import '../data/models.dart';
import '../services.dart';
import 'catalog_pages.dart';
import 'components.dart';
import 'device_status.dart';
import 'seal.dart';
import 'settings.dart';
import 'show_page.dart';
import 'profiles_page.dart';
import 'community_page.dart';
import 'tv_remote_page.dart';
import 'watch_rooms_page.dart';

Future<void> openAccounts(
  BuildContext context,
  AppServices services,
  bool isTV,
) => openDetail(context, AccountsPage(services: services, isTV: isTV), isTV);
Future<void> openDetail(BuildContext context, Widget page, bool isTV) async {
  if (!isTV) {
    await Navigator.push(context, OtakaseRoute<void>(builder: (_) => page));
    return;
  }
  await showGeneralDialog<void>(
    context: context,
    barrierDismissible: true,
    barrierLabel: 'Close',
    barrierColor: bgDeep.withValues(alpha: .6),
    transitionDuration: MediaQuery.disableAnimationsOf(context)
        ? Duration.zero
        : const Duration(milliseconds: 240),
    pageBuilder: (context, a, b) => Align(
      alignment: Alignment.centerRight,
      child: SizedBox(
        width: (MediaQuery.sizeOf(context).width * .35).clamp(432, 560),
        child: page,
      ),
    ),
    transitionBuilder: (context, a, b, child) =>
        FadeTransition(opacity: a, child: child),
  );
}

class MorePage extends StatelessWidget {
  const MorePage({
    super.key,
    required this.services,
    required this.isTV,
    required this.onOpen,
  });
  final AppServices services;
  final bool isTV;
  final void Function(Anime) onOpen;
  @override
  Widget build(BuildContext context) {
    final accounts = services.auth.accounts.values;
    final identity = accounts.isNotEmpty
        ? accounts.map((a) => a.username).join(' · ')
        : '';
    Widget row(
      String title,
      String value,
      IconData icon,
      VoidCallback action, {
      bool autofocus = false,
    }) => Container(
      decoration: const BoxDecoration(
        border: Border(bottom: BorderSide(color: line)),
      ),
      child: FocusBox(
        key: ValueKey('more-$title'),
        autofocus: autofocus,
        isTV: isTV,
        onPressed: action,
        padding: const EdgeInsets.symmetric(vertical: 14, horizontal: 8),
        child: ConstrainedBox(
          constraints: BoxConstraints(minHeight: isTV ? 64 : 75),
          child: Row(
            children: [
              Icon(icon, size: isTV ? 24 : 23, color: muted),
              const SizedBox(width: 16),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: [
                    Text(
                      title,
                      style: uiStyle(context, weight: FontWeight.w600),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      value,
                      style: uiStyle(context, size: 15, color: muted),
                    ),
                  ],
                ),
              ),
              const Icon(LucideIcons.chevronRight, size: 20, color: muted),
            ],
          ),
        ),
      ),
    );
    return ListView(
      key: const PageStorageKey('more-scroll'),
      padding: EdgeInsets.fromLTRB(
        isTV ? 48 : 20,
        12,
        isTV ? 48 : 20,
        contentBottom(context),
      ),
      children: [
        const PageHeading('More', '設'),
        const SizedBox(height: 16),
        FocusBox(
          key: const Key('more-Accounts'),
          autofocus: true,
          isTV: isTV,
          radius: 14,
          onPressed: () => openAccounts(context, services, isTV),
          child: Container(
            constraints: const BoxConstraints(minHeight: 84),
            padding: const EdgeInsets.all(13),
            decoration: BoxDecoration(
              color: panel,
              border: Border.all(color: line),
              borderRadius: BorderRadius.circular(14),
            ),
            child: Row(
              children: [
                if (services.profiles case final manager?)
                  ProfileAvatar(manager.active, size: 38)
                else
                  const OtakaseSeal(size: 38),
                const SizedBox(width: 16),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        identity.isEmpty ? 'Your accounts' : identity,
                        style: uiStyle(context, weight: FontWeight.w600),
                      ),
                      Text(
                        accounts.isNotEmpty
                            ? 'Tracker sync${accounts.any((a) => a.expired) ? ' · Login needs attention' : ''}'
                            : identity.isEmpty
                            ? 'Local progress'
                            : 'Public lists, read only',
                        style: uiStyle(context, size: 15, color: muted),
                      ),
                    ],
                  ),
                ),
                Text(
                  'Accounts',
                  style: uiStyle(context, size: 15, color: accentText),
                ),
              ],
            ),
          ),
        ),
        const SizedBox(height: 12),
        Text(
          '18+ follows your AniList setting',
          style: uiStyle(context, size: 15, color: muted),
        ),
        if (services.tracking.lastError != null)
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: Text(
              'Progress is saved locally. ${services.tracking.lastError}',
              style: uiStyle(context, size: 15, color: warn),
            ),
          ),
        const SizedBox(height: 20),
        if (services.profiles case final manager?)
          row(
            'Profiles',
            manager.active.name,
            LucideIcons.users,
            () => Navigator.push(
              context,
              OtakaseRoute<void>(
                builder: (_) => ProfilesPage(manager: manager),
              ),
            ),
          ),
        row(
          'Playback',
          'Resume, skips & quality',
          LucideIcons.circlePlay,
          () => openSettings(context, services, isTV, section: 'Playback'),
        ),
        row(
          'Subtitles & languages',
          '${services.store.text('SubsLanguage')} · ${services.store.text('SubOrDub')}',
          LucideIcons.captions,
          () => openSettings(
            context,
            services,
            isTV,
            section: 'Subtitles & languages',
          ),
        ),
        row(
          'Schedule',
          'This week · ${DateTime.now().timeZoneName}',
          LucideIcons.calendarDays,
          () => Navigator.push(
            context,
            OtakaseRoute<void>(
              builder: (_) => Scaffold(
                appBar: AppBar(
                  leading: const BackButton(),
                  actions: [
                    DeviceStatus(isTV: isTV),
                    const SizedBox(width: 24),
                  ],
                ),
                body: SchedulePage(
                  services: services,
                  isTV: isTV,
                  onOpen: (a) => Navigator.push(
                    context,
                    OtakaseRoute<void>(
                      builder: (_) =>
                          ShowPage(services: services, anime: a, isTV: isTV),
                    ),
                  ),
                ),
              ),
            ),
          ),
        ),
        row(
          'Discord',
          services.discord.linked ? services.discord.name : 'Link this profile',
          LucideIcons.messagesSquare,
          () => openDetail(
            context,
            DiscordPage(services: services, isTV: isTV),
            isTV,
          ),
        ),
        row(
          'Time capsule',
          'Opens April 12',
          LucideIcons.mail,
          () => openDetail(
            context,
            CapsulePage(services: services, isTV: isTV),
            isTV,
          ),
        ),
        row(
          'Watch rooms',
          'Join your Discord community',
          LucideIcons.users,
          () => openDetail(
            context,
            WatchRoomsPage(services: services, isTV: isTV),
            isTV,
          ),
        ),
        row(
          isTV ? 'Phone remote' : 'Control TV',
          'Pair on your local network',
          LucideIcons.tv,
          () => openDetail(
            context,
            TVRemotePage(services: services, isTV: isTV),
            isTV,
          ),
        ),
        row(
          'Appearance',
          'Shu / Dark',
          LucideIcons.palette,
          () => openSettings(context, services, isTV, section: 'Appearance'),
        ),
        ListenableBuilder(
          listenable: services.updates,
          builder: (context, _) => row(
            'About & updates',
            services.updates.available
                ? 'Update available · ${services.updates.update!.label}'
                : 'Otakase · Android phone and TV',
            LucideIcons.info,
            () => openSettings(
              context,
              services,
              isTV,
              section: 'About & updates',
            ),
          ),
        ),
        row(
          'Diagnostics',
          'Playback checks',
          LucideIcons.activity,
          () => Navigator.push(
            context,
            OtakaseRoute<void>(
              builder: (_) => DiagnosticsHub(services: services, isTV: isTV),
            ),
          ),
        ),
      ],
    );
  }
}

class AccountsPage extends StatefulWidget {
  const AccountsPage({super.key, required this.services, required this.isTV});
  final AppServices services;
  final bool isTV;
  @override
  State<AccountsPage> createState() => _AccountsPageState();
}

class _AccountsPageState extends State<AccountsPage> {
  String? error;
  final backFocus = FocusNode(debugLabel: 'Accounts back');

  @override
  void dispose() {
    backFocus.dispose();
    super.dispose();
  }

  Future<void> login(String service) async {
    setState(() => error = null);
    try {
      final auth = widget.services.auth;
      final url = await auth.begin(service, tv: widget.isTV);
      if (!mounted) {
        await auth.cancelLogin();
        return;
      }
      if (!widget.isTV) await auth.launch(url);
      if (!mounted) return;
      await showDialog<void>(
        context: context,
        builder: (context) => _LoginDialog(
          auth: auth,
          url: url,
          service: service,
          isTV: widget.isTV,
        ),
      );
      await auth.cancelLogin();
    } catch (e) {
      if (mounted) setState(() => error = e.toString());
    } finally {
      if (mounted && widget.isTV) {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted && ModalRoute.isCurrentOf(context) == true) {
            backFocus.requestFocus();
          }
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    backgroundColor: panel,
    appBar: AppBar(
      title: const Text('Accounts'),
      leading: widget.isTV
          ? IconButton(
              focusNode: backFocus,
              tooltip: MaterialLocalizations.of(context).backButtonTooltip,
              icon: const BackButtonIcon(),
              onPressed: () => Navigator.maybePop(context),
            )
          : const BackButton(),
      actions: [
        DeviceStatus(isTV: widget.isTV),
        const SizedBox(width: 24),
      ],
    ),
    body: SafeArea(
      top: false,
      child: ListenableBuilder(
        listenable: Listenable.merge([
          widget.services.auth,
          widget.services.store,
          widget.services.listSync,
        ]),
        builder: (context, _) => ListView(
          padding: const EdgeInsets.all(24),
          children: [
            SwitchListTile(
              title: const Text('Sync AniList and MAL'),
              subtitle: Text(widget.services.listSync.status),
              value: widget.services.store.flag('SyncAniListMAL'),
              onChanged: (value) =>
                  widget.services.store.set('SyncAniListMAL', value),
            ),
            if (widget.services.listSync.ready)
              TextButton(
                onPressed: widget.services.listSync.running
                    ? null
                    : widget.services.listSync.refresh,
                child: const Text('Sync now'),
              ),
            if (error != null)
              Text(error!, style: uiStyle(context, color: warn)),
            if (widget.services.auth.lastError != null)
              Text(
                widget.services.auth.lastError!,
                style: uiStyle(context, color: warn),
              ),
            for (final service in ['AniList', 'MAL']) ...[
              Text(
                service,
                style: uiStyle(context, size: 21, weight: FontWeight.w600),
              ),
              const SizedBox(height: 12),
              if (widget.services.auth.account(service)
                  case final Account account) ...[
                Text(account.username, style: uiStyle(context)),
                Text(
                  account.expired
                      ? 'Login expired. Sign in again to sync.'
                      : 'Signed in · Tracker sync',
                  style: uiStyle(
                    context,
                    size: 15,
                    color: account.expired ? warn : ok,
                  ),
                ),
                const SizedBox(height: 12),
                Wrap(
                  spacing: 12,
                  children: [
                    if (account.expired)
                      AppButton(
                        label: 'Sign in again',
                        onPressed: () => login(service),
                        isTV: widget.isTV,
                      ),
                    AppButton(
                      label: 'Log out of $service',
                      onPressed: () async {
                        await widget.services.auth.logout(service);
                      },
                      isTV: widget.isTV,
                    ),
                  ],
                ),
              ] else ...[
                AppButton(
                  label: 'Sign in to $service',
                  isTV: widget.isTV,
                  autofocus: service == 'AniList',
                  filled: true,
                  onPressed: () => login(service),
                ),
              ],
              const Divider(height: 40),
            ],
          ],
        ),
      ),
    ),
  );
}

class _LoginDialog extends StatefulWidget {
  const _LoginDialog({
    required this.auth,
    required this.url,
    required this.service,
    required this.isTV,
  });
  final AuthService auth;
  final Uri url;
  final String service;
  final bool isTV;
  @override
  State<_LoginDialog> createState() => _LoginDialogState();
}

class _LoginDialogState extends State<_LoginDialog> {
  StreamSubscription<String>? subscription;
  @override
  void initState() {
    super.initState();
    subscription = widget.auth.completed.listen((s) {
      if (mounted && s == widget.service) Navigator.pop(context);
    });
  }

  @override
  void dispose() {
    subscription?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text('Sign in to ${widget.service}'),
    content: SizedBox(
      width: 360,
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (widget.isTV)
              Container(
                color: Colors.white,
                padding: const EdgeInsets.all(12),
                child: QrImageView(
                  data: widget.url.toString(),
                  size: 260,
                  backgroundColor: Colors.white,
                ),
              ),
            Text(
              widget.isTV
                  ? 'Scan with your phone. Keep both devices on the same home network, then finish sign in on the page.'
                  : 'Finish sign in in your browser. Return here after approval.',
              style: uiStyle(context),
            ),
            if (!widget.isTV)
              TextButton(
                onPressed: () => widget.auth.launch(widget.url),
                child: const Text('Open browser'),
              ),
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
    ],
  );
}
