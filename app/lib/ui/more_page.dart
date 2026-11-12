import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:qr_flutter/qr_flutter.dart';

import '../core.dart';
import '../data/auth.dart';
import '../desktop/desktop_core.dart';
import '../data/models.dart';
import '../services.dart';
import 'catalog_pages.dart';
import 'components.dart';
import 'device_status.dart';
import 'seal.dart';
import 'settings.dart';
import 'downloads_page.dart';
import '../data/movies.dart';
import 'movies_pages.dart' show openTitle, titleCardsOf, traktSignIn;
import 'show_page.dart';
import 'profiles_page.dart';
import 'community_page.dart';
import 'community_extras_page.dart';
import 'watch_party_page.dart';
import 'tv_remote_page.dart';
import 'watch_rooms_page.dart';
import 'account_sync_status.dart';
import 'account_page.dart';

Future<void> openAccounts(
  BuildContext context,
  AppServices services,
  bool isTV,
) => openDetail(context, AccountsPage(services: services, isTV: isTV), isTV);

/// The names of every signed-in tracker: AniList, MAL, then Trakt.
List<String> accountNames(AppServices services) => [
  for (final a in services.auth.accounts.values) a.username,
  if (services.trakt.signedIn)
    services.trakt.username.isEmpty ? 'Trakt' : services.trakt.username,
];

/// The airing schedule as its own page, opened from More.
Future<void> openSchedule(
  BuildContext context,
  AppServices services,
  bool isTV,
) => Navigator.push(
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
        onOpen: (a) {
          // Series rows open their own page.
          if (titleCardsOf(services).title(a) case final title?) {
            unawaited(openTitle(context, services, title, isTV));
            return;
          }
          unawaited(
            Navigator.push(
              context,
              OtakaseRoute<void>(
                builder: (_) =>
                    ShowPage(services: services, anime: a, isTV: isTV),
              ),
            ),
          );
        },
      ),
    ),
  ),
);

/// Schedule shows anime airing, and the viewer's series once movies &
/// shows are on and Trakt is signed in.
bool scheduleShown(AppServices services) =>
    contentModeOf(services.store) != ContentMode.movies ||
    services.trakt.signedIn;

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
    if (isDesktop) return DesktopMore(services: services, onOpen: onOpen);
    final identity = accountNames(services).join(' · ');
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
                      AccountSyncStatus(services: services),
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
        // Nothing mentions 18+ unless the viewer's AniList allows it.
        if (services.store.adultAllowed) ...[
          const SizedBox(height: 12),
          Text(
            '18+ follows your AniList setting',
            style: uiStyle(context, size: 15, color: muted),
          ),
        ],
        const MoreGroupHeading('Watching'),
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
        if (!isTV)
          row(
            'Downloads',
            'Watch offline',
            LucideIcons.download,
            () => openDownloads(context, services, isTV),
          ),
        if (scheduleShown(services))
          row(
            'Schedule',
            'This week · ${DateTime.now().timeZoneName}',
            LucideIcons.calendarDays,
            () => openSchedule(context, services, isTV),
          ),
        const MoreGroupHeading('Your profile'),
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
          'Settings backup',
          'Keep your settings with the Discord bot',
          LucideIcons.cloudUpload,
          () => openDetail(
            context,
            BackupPage(services: services, isTV: isTV),
            isTV,
          ),
        ),
        row(
          'Display',
          '${contentLabels[services.store.text('Content')] ?? ''} · Shu / Dark',
          LucideIcons.palette,
          () => openSettings(context, services, isTV, section: 'Display'),
        ),
        const MoreGroupHeading('Devices'),
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
        const MoreGroupHeading('Community'),
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
          'Watch parties',
          services.party.active
              ? 'In ${services.party.show}'
              : 'Planned in the Discord · watch in sync',
          LucideIcons.partyPopper,
          () => openDetail(
            context,
            WatchPartiesPage(services: services, isTV: isTV),
            isTV,
          ),
        ),
        if (services.extras.club case final club?)
          row(
            'Server club',
            clubLabel(club),
            LucideIcons.flag,
            () => Navigator.push(
              context,
              OtakaseRoute<void>(
                builder: (_) => ShowPage(
                  services: services,
                  anime: Anime(id: club.anilistId, title: club.title),
                  isTV: isTV,
                ),
              ),
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
        const MoreGroupHeading('Help & app'),
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
        row(
          'Report a problem',
          'Post to the support forum',
          LucideIcons.lifeBuoy,
          () => openDetail(
            context,
            ReportPage(services: services, isTV: isTV),
            isTV,
          ),
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
      ],
    );
  }
}

/// Desktop settings: sections in a sidebar, the chosen settings section
/// beside it as grouped cards. Pages that are more than settings, such as
/// Discord or Watch rooms, still open on their own.
class DesktopMore extends StatefulWidget {
  const DesktopMore({super.key, required this.services, required this.onOpen});
  final AppServices services;
  final void Function(Anime) onOpen;
  @override
  State<DesktopMore> createState() => _DesktopMoreState();
}

class _DesktopMoreState extends State<DesktopMore> {
  String section = 'Playback';

  AppServices get services => widget.services;

  Widget item(String title, IconData icon, {VoidCallback? open}) {
    final selected = open == null && section == title;
    return Padding(
      padding: const EdgeInsets.only(bottom: 4),
      child: FocusBox(
        key: ValueKey('more-$title'),
        isTV: true,
        radius: 12,
        onPressed: open ?? () => setState(() => section = title),
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
          decoration: BoxDecoration(
            color: selected ? selectedPanel : Colors.transparent,
            borderRadius: BorderRadius.circular(12),
            border: Border(
              left: BorderSide(
                color: selected ? shu : Colors.transparent,
                width: 3,
              ),
            ),
          ),
          child: Row(
            children: [
              Icon(icon, size: 20, color: selected ? fgBright : muted),
              const SizedBox(width: 14),
              Expanded(
                child: Text(
                  title,
                  style: uiStyle(
                    context,
                    weight: selected ? FontWeight.w600 : FontWeight.w500,
                    color: selected ? fgBright : fg,
                  ),
                ),
              ),
              if (open != null)
                const Icon(LucideIcons.arrowUpRight, size: 18, color: muted),
            ],
          ),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final identity = accountNames(services).join(' · ');
    void detail(Widget page) => unawaited(openDetail(context, page, true));
    final sidebar = ListView(
      padding: EdgeInsets.fromLTRB(48, 16, 0, contentBottom(context)),
      children: [
        Row(
          children: [
            Text(
              'Settings',
              style: uiStyle(context, size: 32, weight: FontWeight.w700),
            ),
            const SizedBox(width: 12),
            const ExcludeSemantics(
              child: Text(
                '設',
                style: TextStyle(
                  fontFamily: 'NotoSansJP',
                  fontSize: 26,
                  color: accentText,
                ),
              ),
            ),
          ],
        ),
        const SizedBox(height: 20),
        FocusBox(
          key: const Key('more-Accounts'),
          isTV: true,
          radius: 14,
          onPressed: () => openAccounts(context, services, true),
          child: Container(
            padding: const EdgeInsets.all(14),
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
                const SizedBox(width: 14),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        identity.isEmpty ? 'Your accounts' : identity,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: uiStyle(context, weight: FontWeight.w600),
                      ),
                      AccountSyncStatus(services: services),
                    ],
                  ),
                ),
              ],
            ),
          ),
        ),
        const MoreGroupHeading('Watching'),
        item('Playback', LucideIcons.circlePlay),
        item('Subtitles & languages', LucideIcons.captions),
        item(
          'Downloads',
          LucideIcons.download,
          open: () => openDownloads(context, services, true),
        ),
        if (scheduleShown(services))
          item(
            'Schedule',
            LucideIcons.calendarDays,
            open: () => openSchedule(context, services, true),
          ),
        const MoreGroupHeading('Your profile'),
        if (services.profiles case final manager?)
          item(
            'Profiles',
            LucideIcons.users,
            open: () => Navigator.push(
              context,
              OtakaseRoute<void>(
                builder: (_) => ProfilesPage(manager: manager),
              ),
            ),
          ),
        item(
          'Settings backup',
          LucideIcons.cloudUpload,
          open: () => detail(BackupPage(services: services, isTV: true)),
        ),
        item('Display', LucideIcons.palette),
        const MoreGroupHeading('Devices'),
        item(
          'Phone remote',
          LucideIcons.tv,
          open: () => detail(TVRemotePage(services: services, isTV: true)),
        ),
        const MoreGroupHeading('Community'),
        item(
          'Discord',
          LucideIcons.messagesSquare,
          open: () => detail(DiscordPage(services: services, isTV: true)),
        ),
        item(
          'Watch rooms',
          LucideIcons.users,
          open: () => detail(WatchRoomsPage(services: services, isTV: true)),
        ),
        item(
          'Watch parties',
          LucideIcons.partyPopper,
          open: () => detail(WatchPartiesPage(services: services, isTV: true)),
        ),
        if (services.extras.club case final club?)
          item(
            'Server club · ${clubLabel(club)}',
            LucideIcons.flag,
            open: () =>
                widget.onOpen(Anime(id: club.anilistId, title: club.title)),
          ),
        item(
          'Time capsule',
          LucideIcons.mail,
          open: () => detail(CapsulePage(services: services, isTV: true)),
        ),
        const MoreGroupHeading('Help & app'),
        item(
          'Diagnostics',
          LucideIcons.activity,
          open: () => Navigator.push(
            context,
            OtakaseRoute<void>(
              builder: (_) => DiagnosticsHub(services: services, isTV: true),
            ),
          ),
        ),
        item(
          'Report a problem',
          LucideIcons.lifeBuoy,
          open: () => detail(ReportPage(services: services, isTV: true)),
        ),
        item('About & updates', LucideIcons.info),
      ],
    );
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SizedBox(width: 400, child: sidebar),
        const SizedBox(width: 48),
        Expanded(
          child: Align(
            alignment: Alignment.topLeft,
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 1400),
              child: Padding(
                padding: const EdgeInsets.only(top: 72, right: 48),
                child: SettingsPage(
                  key: ValueKey('desktop-settings-$section'),
                  services: services,
                  isTV: true,
                  section: section,
                  embedded: true,
                ),
              ),
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
  // The first button of each tracker, kept across sign in and log out so the
  // remote stays on that tracker when its buttons change.
  final serviceFocus = {
    for (final service in ['AniList', 'MAL', 'Trakt'])
      service: FocusNode(debugLabel: 'Accounts $service'),
  };

  @override
  void initState() {
    super.initState();
    unawaited(widget.services.trakt.load());
  }

  @override
  void dispose() {
    backFocus.dispose();
    for (final node in serviceFocus.values) {
      node.dispose();
    }
    super.dispose();
  }

  Future<void> logout(String service) async {
    await widget.services.auth.logout(service);
    if (!mounted || !widget.isTV) return;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && ModalRoute.isCurrentOf(context) == true) {
        serviceFocus[service]!.requestFocus();
      }
    });
  }

  Future<void> traktLogout() async {
    await widget.services.trakt.signOut();
    if (!mounted || !widget.isTV) return;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && ModalRoute.isCurrentOf(context) == true) {
        serviceFocus['Trakt']!.requestFocus();
      }
    });
  }

  /// Trakt signs in through the browser on a computer and with a code
  /// entered at trakt.tv on phones and TVs.
  Future<void> traktLogin() async {
    await traktSignIn(context, widget.services, widget.isTV);
    if (!mounted || !widget.isTV) return;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && ModalRoute.isCurrentOf(context) == true) {
        serviceFocus['Trakt']!.requestFocus();
      }
    });
  }

  Future<void> login(String service) async {
    setState(() => error = null);
    try {
      final auth = widget.services.auth;
      // Desktop signs in through the browser on the same computer; only a
      // real TV needs a phone.
      final phone = widget.isTV && !isDesktop;
      final url = await auth.begin(service, tv: phone, desktop: isDesktop);
      if (!mounted) {
        await auth.cancelLogin();
        return;
      }
      var opened = true;
      if (!phone) {
        try {
          await auth.launch(url);
        } on LoginFailure {
          // The dialog offers the link to copy into any browser instead.
          opened = false;
        }
      }
      if (!mounted) return;
      await showDialog<void>(
        context: context,
        builder: (context) => _LoginDialog(
          auth: auth,
          url: url,
          service: service,
          isTV: phone,
          opened: opened,
        ),
      );
      await auth.cancelLogin();
    } catch (e) {
      if (mounted) setState(() => error = errorMessage(e));
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

  /// Trakt keeps films and series in sync, laid out like AniList and MAL.
  Widget traktSection() {
    final trakt = widget.services.trakt;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          'Trakt',
          style: uiStyle(context, size: 21, weight: FontWeight.w600),
        ),
        const SizedBox(height: 12),
        if (trakt.signedIn) ...[
          if (trakt.username.isNotEmpty)
            Text(trakt.username, style: uiStyle(context)),
          Text(
            traktSyncStatus(widget.services),
            style: uiStyle(
              context,
              size: 15,
              color:
                  widget.services.movies.lastError != null ||
                      trakt.lastError != null
                  ? warn
                  : muted,
            ),
          ),
          const SizedBox(height: 12),
          AppButton(
            key: const Key('accounts-trakt'),
            label: 'Log out of Trakt',
            focusNode: serviceFocus['Trakt'],
            onPressed: traktLogout,
            isTV: widget.isTV,
          ),
        ] else ...[
          Text(
            'Keeps your movie and show watchlist, what you watched and where '
            'you stopped in sync.',
            style: uiStyle(context, size: 15, color: muted),
          ),
          const SizedBox(height: 12),
          AppButton(
            key: const Key('accounts-trakt'),
            label: 'Sign in to Trakt',
            focusNode: serviceFocus['Trakt'],
            isTV: widget.isTV,
            filled: true,
            onPressed: traktLogin,
          ),
        ],
        if (trakt.lastError case final message?) ...[
          const SizedBox(height: 8),
          Text(message, style: uiStyle(context, size: 15, color: warn)),
        ],
      ],
    );
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
          widget.services.trakt,
          widget.services.tracking,
          widget.services.movies,
        ]),
        builder: (context, _) => ListView(
          padding: const EdgeInsets.all(24),
          children: [
            OtakaseAccountSection(services: widget.services, isTV: widget.isTV),
            // Only means something once both lists are signed in.
            if (widget.services.auth.signedIn('AniList') &&
                widget.services.auth.signedIn('MAL'))
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
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
                      : widget.services.tracking.progressStatus(service),
                  style: uiStyle(
                    context,
                    size: 15,
                    color:
                        account.expired ||
                            widget.services.tracking.needsAttention(service)
                        ? warn
                        : muted,
                  ),
                ),
                const SizedBox(height: 12),
                Wrap(
                  spacing: 12,
                  runSpacing: 12,
                  children: [
                    if (account.expired)
                      AppButton(
                        label: 'Sign in again',
                        focusNode: serviceFocus[service],
                        autofocus: service == 'AniList',
                        onPressed: () => login(service),
                        isTV: widget.isTV,
                      ),
                    AppButton(
                      label: 'Log out of $service',
                      focusNode: account.expired ? null : serviceFocus[service],
                      autofocus: !account.expired && service == 'AniList',
                      onPressed: () => logout(service),
                      isTV: widget.isTV,
                    ),
                  ],
                ),
              ] else ...[
                Text(
                  'Keeps your anime list, episode progress and scores in '
                  'sync with $service.',
                  style: uiStyle(context, size: 15, color: muted),
                ),
                const SizedBox(height: 12),
                Align(
                  alignment: AlignmentDirectional.centerStart,
                  child: AppButton(
                    label: 'Sign in to $service',
                    focusNode: serviceFocus[service],
                    isTV: widget.isTV,
                    autofocus: service == 'AniList',
                    filled: true,
                    onPressed: () => login(service),
                  ),
                ),
              ],
              const Divider(height: 40),
            ],
            traktSection(),
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
    this.opened = true,
  });
  final AuthService auth;
  final Uri url;
  final String service;
  final bool isTV;

  /// False when the browser did not open; the link is offered to copy.
  final bool opened;
  @override
  State<_LoginDialog> createState() => _LoginDialogState();
}

class _LoginDialogState extends State<_LoginDialog> {
  bool copied = false;
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
            if (widget.isTV && widget.auth.loginCode != null) ...[
              const SizedBox(height: 12),
              Text(
                'Check code ${widget.auth.loginCode}',
                key: const Key('login-check-code'),
                style: mono.copyWith(fontSize: 26, color: fgBright),
              ),
              const SizedBox(height: 4),
              Text(
                'Only finish on your phone if its page shows this same code.',
                style: uiStyle(context, size: 15, color: muted),
              ),
              const SizedBox(height: 8),
            ],
            Text(
              widget.isTV
                  ? 'Scan with your phone. Keep both devices on the same home network, then finish sign in on the page.'
                  : widget.opened
                  ? 'Finish sign in in your browser. Return here after approval.'
                  : "Your browser didn't open. Copy the link, paste it into a "
                        'browser on this computer and finish sign in there.',
              style: uiStyle(context),
            ),
            if (!widget.isTV)
              Wrap(
                spacing: 8,
                children: [
                  TextButton(
                    onPressed: () async {
                      try {
                        await widget.auth.launch(widget.url);
                      } on LoginFailure {
                        /* Copy link still works. */
                      }
                    },
                    child: const Text('Open browser'),
                  ),
                  TextButton(
                    onPressed: () async {
                      await Clipboard.setData(
                        ClipboardData(text: widget.url.toString()),
                      );
                      if (mounted) setState(() => copied = true);
                    },
                    child: Text(copied ? 'Copied' : 'Copy link'),
                  ),
                ],
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

class MoreGroupHeading extends StatelessWidget {
  const MoreGroupHeading(this.title, {super.key});
  final String title;
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.fromLTRB(8, 24, 8, 8),
    child: Semantics(
      header: true,
      child: Text(
        title,
        style: uiStyle(
          context,
          size: 15,
          color: muted,
          weight: FontWeight.w600,
        ),
      ),
    ),
  );
}
