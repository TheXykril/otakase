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
        leading: const AppBackButton(),
        actions: [
          TopBarEnd(isTV: isTV),
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

Future<void> openDetail(BuildContext context, Widget page, bool isTV) =>
    openSubPage<void>(context, page, isTV: isTV);

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
    }) => AppRow(
      key: ValueKey('more-$title'),
      autofocus: autofocus,
      isTV: isTV,
      icon: icon,
      title: title,
      subtitle: value,
      divider: true,
      onTap: action,
    );
    // The About row says when an update is waiting.
    return ListenableBuilder(
      listenable: services.updates,
      builder: (context, _) => ListView(
        key: const PageStorageKey('more-scroll'),
        padding: EdgeInsets.fromLTRB(
          isTV ? 48 : 4,
          12,
          isTV ? 48 : 4,
          contentBottom(context),
        ),
        children: [
          Padding(
            padding: EdgeInsets.symmetric(horizontal: isTV ? 0 : 12),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                const PageHeading('More', '設'),
                const SizedBox(height: 16),
                FocusBox(
                  key: const Key('more-Accounts'),
                  autofocus: true,
                  isTV: isTV,
                  radius: KageRadius.tile,
                  onPressed: () => openAccounts(context, services, isTV),
                  child: Container(
                    constraints: const BoxConstraints(minHeight: 84),
                    padding: const EdgeInsets.all(13),
                    decoration: BoxDecoration(
                      color: panel,
                      border: Border.all(color: line),
                      borderRadius: BorderRadius.circular(KageRadius.tile),
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
                                style: uiStyle(
                                  context,
                                  weight: FontWeight.w600,
                                ),
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
              ],
            ),
          ),
          for (final (heading, items) in moreDestinations(
            context,
            services,
            isTV: isTV,
            onOpen: onOpen,
          )) ...[
            MoreGroupHeading(heading),
            for (final d in items)
              row(
                d.title,
                d.subtitle,
                d.icon,
                d.open ??
                    () => openSettings(
                      context,
                      services,
                      isTV,
                      section: d.section,
                    ),
              ),
          ],
        ],
      ),
    );
  }
}

/// One place a More page row leads: a settings section ([section]) or a
/// page of its own ([open]).
class MoreDestination {
  const MoreDestination(
    this.title,
    this.icon, {
    this.subtitle = '',
    this.section,
    this.open,
  }) : assert((section == null) != (open == null));
  final String title;
  final String subtitle;
  final IconData icon;
  final String? section;
  final VoidCallback? open;
}

/// Every More destination, in groups. The phone and TV list and the
/// computer sidebar both draw this list, so they never drift apart.
List<(String, List<MoreDestination>)> moreDestinations(
  BuildContext context,
  AppServices services, {
  required bool isTV,
  required void Function(Anime) onOpen,
}) {
  void detail(Widget page) => unawaited(openDetail(context, page, isTV));
  void push(Widget page) => unawaited(
    Navigator.push(context, OtakaseRoute<void>(builder: (_) => page)),
  );
  final store = services.store;
  final updates = services.updates;
  return [
    (
      'Watching',
      [
        const MoreDestination(
          'Playback',
          LucideIcons.circlePlay,
          subtitle: 'Resume, skips & quality',
          section: 'Playback',
        ),
        MoreDestination(
          'Subtitles & languages',
          LucideIcons.captions,
          subtitle: '${store.text('SubsLanguage')} · ${store.text('SubOrDub')}',
          section: 'Subtitles & languages',
        ),
        // TVs have no downloads; computers render with isTV on.
        if (!isTV || isDesktop)
          MoreDestination(
            'Downloads',
            LucideIcons.download,
            subtitle: 'Watch offline',
            open: () => openDownloads(context, services, isTV),
          ),
        if (scheduleShown(services))
          MoreDestination(
            'Schedule',
            LucideIcons.calendarDays,
            subtitle: 'This week · ${DateTime.now().timeZoneName}',
            open: () => openSchedule(context, services, isTV),
          ),
      ],
    ),
    (
      'Your profile',
      [
        if (services.profiles case final manager?)
          MoreDestination(
            'Profiles',
            LucideIcons.users,
            subtitle: manager.active.name,
            open: () => push(ProfilesPage(manager: manager)),
          ),
        MoreDestination(
          'Settings backup',
          LucideIcons.cloudUpload,
          subtitle: 'Keep your settings with the Discord bot',
          open: () => detail(BackupPage(services: services, isTV: isTV)),
        ),
        MoreDestination(
          'Display',
          LucideIcons.palette,
          subtitle:
              '${contentLabels[store.text('Content')] ?? ''} · Shu / Dark',
          section: 'Display',
        ),
      ],
    ),
    (
      'Devices',
      [
        MoreDestination(
          isTV ? 'Phone remote' : 'Control TV',
          LucideIcons.tv,
          subtitle: 'Pair on your local network',
          open: () => detail(TVRemotePage(services: services, isTV: isTV)),
        ),
      ],
    ),
    (
      'Community',
      [
        MoreDestination(
          'Discord',
          LucideIcons.messagesSquare,
          subtitle: services.discord.linked
              ? services.discord.name
              : 'Link this profile',
          open: () => detail(DiscordPage(services: services, isTV: isTV)),
        ),
        MoreDestination(
          'Watch parties',
          LucideIcons.partyPopper,
          subtitle: services.party.active
              ? 'In ${services.party.show}'
              : 'Planned in the Discord · watch in sync',
          open: () => detail(WatchPartiesPage(services: services, isTV: isTV)),
        ),
        if (services.extras.club case final club?)
          MoreDestination(
            'Server club',
            LucideIcons.flag,
            subtitle: clubLabel(club),
            open: () => onOpen(Anime(id: club.anilistId, title: club.title)),
          ),
      ],
    ),
    (
      'Help & app',
      [
        MoreDestination(
          'Diagnostics',
          LucideIcons.activity,
          subtitle: 'Playback checks',
          open: () => push(DiagnosticsHub(services: services, isTV: isTV)),
        ),
        MoreDestination(
          'Report a problem',
          LucideIcons.lifeBuoy,
          subtitle: 'Post to the support forum',
          open: () => detail(ReportPage(services: services, isTV: isTV)),
        ),
        MoreDestination(
          'About & updates',
          LucideIcons.info,
          subtitle: updates.available
              ? 'Update available · ${updates.update!.label}'
              : 'Otakase · Android phone and TV',
          section: 'About & updates',
        ),
      ],
    ),
  ];
}

/// Desktop settings: sections in a sidebar, the chosen settings section
/// beside it as grouped cards. Pages that are more than settings, such as
/// Discord, still open on their own.
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
      child: AppRow(
        key: ValueKey('more-$title'),
        isTV: true,
        icon: icon,
        title: title,
        selected: selected,
        external: open != null,
        trailing: open == null ? const SizedBox.shrink() : null,
        onTap: open ?? () => setState(() => section = title),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final identity = accountNames(services).join(' · ');
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
          radius: KageRadius.tile,
          onPressed: () => openAccounts(context, services, true),
          child: Container(
            padding: const EdgeInsets.all(14),
            decoration: BoxDecoration(
              color: panel,
              border: Border.all(color: line),
              borderRadius: BorderRadius.circular(KageRadius.tile),
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
        for (final (heading, items) in moreDestinations(
          context,
          services,
          isTV: true,
          onOpen: widget.onOpen,
        )) ...[
          MoreGroupHeading(heading),
          for (final d in items) item(d.title, d.icon, open: d.open),
        ],
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

  /// One tracker: icon, name and status on one line, its buttons small
  /// underneath, a hairline below. Same shape for AniList, MAL and Trakt.
  Widget tracker({
    required String name,
    required IconData icon,
    required String status,
    bool alert = false,
    String? detail,
    required List<Widget> buttons,
  }) {
    final tv = widget.isTV && !isDesktop;
    return Container(
      key: ValueKey('tracker-$name'),
      padding: EdgeInsets.symmetric(vertical: tv ? 16 : 12),
      decoration: const BoxDecoration(
        border: Border(bottom: BorderSide(color: hairline)),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: const EdgeInsets.only(top: 2),
            child: Icon(icon, size: tv ? 24 : 20, color: muted),
          ),
          SizedBox(width: tv ? 16 : 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(name, style: uiStyle(context, weight: FontWeight.w600)),
                const SizedBox(height: 2),
                Text(
                  status,
                  style: uiStyle(
                    context,
                    size: 15,
                    color: alert ? warn : muted,
                  ),
                ),
                if (detail != null) ...[
                  const SizedBox(height: 2),
                  Text(detail, style: uiStyle(context, size: 15, color: warn)),
                ],
                const SizedBox(height: KageSpace.s3),
                Wrap(
                  spacing: KageSpace.s2,
                  runSpacing: KageSpace.s2,
                  children: buttons,
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  /// Trakt keeps films and series in sync, laid out like AniList and MAL.
  Widget traktSection() {
    final trakt = widget.services.trakt;
    final failing =
        widget.services.movies.lastError != null || trakt.lastError != null;
    return tracker(
      name: 'Trakt',
      icon: LucideIcons.clapperboard,
      status: trakt.signedIn
          ? [
              if (trakt.username.isNotEmpty) trakt.username,
              traktSyncStatus(widget.services),
            ].join(' · ')
          : 'Not connected. Keeps your movie and show watchlist, what you '
                'watched and where you stopped in sync.',
      alert: trakt.signedIn && failing,
      detail: trakt.lastError,
      buttons: [
        AppButton(
          key: const Key('accounts-trakt'),
          label: trakt.signedIn ? 'Log out of Trakt' : 'Sign in to Trakt',
          focusNode: serviceFocus['Trakt'],
          size: ButtonSize.sm,
          isTV: widget.isTV,
          onPressed: trakt.signedIn ? traktLogout : traktLogin,
        ),
      ],
    );
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    backgroundColor: panel,
    appBar: AppBar(
      // One surface: the bar matches the page under it.
      backgroundColor: panel,
      title: const Text('Accounts'),
      leading: widget.isTV
          ? AppBackButton(focusNode: backFocus)
          : const AppBackButton(),
      actions: [
        TopBarEnd(isTV: widget.isTV),
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
            const SizedBox(height: KageSpace.s4),
            const GroupHeading('Trackers'),
            if (error != null)
              Text(error!, style: uiStyle(context, color: warn)),
            if (widget.services.auth.lastError != null)
              Text(
                widget.services.auth.lastError!,
                style: uiStyle(context, color: warn),
              ),
            for (final service in ['AniList', 'MAL'])
              if (widget.services.auth.account(service)
                  case final Account account)
                tracker(
                  name: service,
                  icon: LucideIcons.listChecks,
                  status: account.expired
                      ? '${account.username} · Login expired. Sign in again '
                            'to sync.'
                      : '${account.username} · '
                            '${widget.services.tracking.progressStatus(service)}',
                  alert:
                      account.expired ||
                      widget.services.tracking.needsAttention(service),
                  buttons: [
                    if (account.expired)
                      AppButton(
                        label: 'Sign in again',
                        focusNode: serviceFocus[service],
                        autofocus: service == 'AniList',
                        size: ButtonSize.sm,
                        filled: true,
                        onPressed: () => login(service),
                        isTV: widget.isTV,
                      ),
                    AppButton(
                      label: 'Log out of $service',
                      focusNode: account.expired ? null : serviceFocus[service],
                      autofocus: !account.expired && service == 'AniList',
                      size: ButtonSize.sm,
                      onPressed: () => logout(service),
                      isTV: widget.isTV,
                    ),
                  ],
                )
              else
                tracker(
                  name: service,
                  icon: LucideIcons.listChecks,
                  status:
                      'Not connected. Keeps your anime list, episode '
                      'progress and scores in sync with $service.',
                  buttons: [
                    AppButton(
                      label: 'Sign in to $service',
                      focusNode: serviceFocus[service],
                      isTV: widget.isTV,
                      autofocus: service == 'AniList',
                      size: ButtonSize.sm,
                      onPressed: () => login(service),
                    ),
                  ],
                ),
            traktSection(),
            // Only means something once both lists are signed in.
            if (widget.services.auth.signedIn('AniList') &&
                widget.services.auth.signedIn('MAL'))
              AppRow(
                title: 'Sync AniList and MAL',
                subtitle: widget.services.listSync.status,
                toggle: widget.services.store.flag('SyncAniListMAL'),
                isTV: widget.isTV,
                divider: true,
                onTap: () => widget.services.store.set(
                  'SyncAniListMAL',
                  !widget.services.store.flag('SyncAniListMAL'),
                ),
              ),
            if (widget.services.listSync.ready)
              AppRow(
                title: 'Sync now',
                icon: LucideIcons.refreshCw,
                isTV: widget.isTV,
                enabled: !widget.services.listSync.running,
                onTap: widget.services.listSync.refresh,
              ),
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
  Widget build(BuildContext context) => GroupHeading(title);
}
