import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';

import '../services.dart';
import 'account.dart';
import 'account_live.dart';
import 'account_sync.dart';
import 'account_vault.dart';
import 'auth.dart';
import 'discord.dart';
import 'models.dart';
import 'profiles.dart';

// Ties the Otakase accounts on this device to its profiles: starts sync,
// the vault and the live connection for each account, follows the app to
// the background and back, and hands the active profile its account's
// Discord, inbox and phone remote.

/// Everything running for one account on this device.
class AccountRun {
  AccountRun(this.session, this.sync, this.vault, this.live);
  final AccountSession session;
  final AccountSync sync;
  final AccountVault vault;
  final AccountLive live;
  HubRemoteHost? host;

  Future<void> close() async {
    await host?.close();
    host = null;
    sync.dispose();
    vault.dispose();
    live.dispose();
  }
}

class _Bridge implements DiscordBridge {
  _Bridge(this.run);
  final AccountRun run;
  @override
  bool get ready => !run.session.signedOut && run.session.hasDiscord;
  @override
  String get name => run.session.name;
  @override
  String get base => run.session.base;
  @override
  Future<String> token() => run.session.accessToken();
  @override
  bool heartbeat(Json body) => run.live.heartbeat(body);
}

class AccountCenter extends ChangeNotifier with WidgetsBindingObserver {
  AccountCenter({
    required this.profiles,
    required this.preferences,
    required TokenStore tokens,
    required DeviceInfo device,
    http.Client? client,
    AccountManager? manager,
    LiveConnect? connect,
    this.observeLifecycle = true,
  }) : keys = TokenStoreKeys(tokens),
       connect = connect ?? liveSocket {
    this.manager =
        manager ??
        AccountManager(
          tokens: tokens,
          preferences: preferences,
          device: device,
          client: client,
          keys: keys,
        );
    this.manager
      ..onSignedIn = _signedIn
      ..onSignedOut = _signedOut;
  }
  final ProfileManager profiles;
  final SharedPreferences preferences;
  final TokenStoreKeys keys;
  final LiveConnect connect;
  final bool observeLifecycle;
  late final AccountManager manager;
  final runs = <String, AccountRun>{};
  AppServices? _services;
  bool closed = false, _foreground = true;

  /// Asks the person, on a first sign-in on a device that has profiles of
  /// its own, whether this device's profiles join the account. Set by the
  /// app shell; without it the account's profiles come to this device.
  Future<bool?> Function(AccountSession session)? askUpload;

  /// Shows a short line, set by the app shell.
  void Function(String text)? notify;

  @override
  void notifyListeners() {
    if (!closed) super.notifyListeners();
  }

  Future<void> initialize() async {
    await manager.initialize();
    AuthService.accountLink = (uri) async {
      try {
        await manager.accept(uri);
      } catch (e) {
        manager.lastError = e.toString();
        manager.notifyListeners();
      }
    };
    ProfileTokens.written = _loginWritten;
    for (final s in manager.accounts) {
      _start(s);
    }
    profiles.addListener(_profilesChanged);
    _profilesChanged();
    if (observeLifecycle) WidgetsBinding.instance.addObserver(this);
  }

  AccountRun? runFor(String account) => runs[account];

  /// The account the active profile belongs to, if any.
  AccountRun? get activeRun {
    final services = profiles.session;
    if (services == null) return null;
    final p = profiles.profiles
        .where((p) => p.id == services.profileId)
        .firstOrNull;
    return p == null || p.account.isEmpty ? null : runs[p.account];
  }

  AccountRun _start(AccountSession session) {
    final existing = runs[session.id];
    if (existing != null) return existing;
    final sync = AccountSync(
      session: session,
      profiles: profiles,
      preferences: preferences,
    );
    final vault = AccountVault(
      session: session,
      keys: keys,
      preferences: preferences,
      profileTokens: (id) => ProfileTokens(profiles.tokens, preferences, id),
      boundProfiles: () => [for (final p in profiles.boundTo(session.id)) p.id],
      onLoginsChanged: _loginsArrived,
    );
    final live = AccountLive(session: session, connect: connect);
    final run = AccountRun(session, sync, vault, live);
    runs[session.id] = run;
    live
      ..onSync = (version) {
        if (version != sync.version) unawaited(sync.sync());
      }
      ..onVault = () {
        if (sync.switches.on(SyncPart.logins)) unawaited(vault.sync());
      }
      ..onDevices = notifyListeners
      ..onInbox = (items) => _inbox(run, items);
    sync.addListener(notifyListeners);
    vault.addListener(notifyListeners);
    live.addListener(notifyListeners);
    session.addListener(notifyListeners);
    _wire(run);
    if (_foreground) {
      live.start();
      unawaited(sync.sync());
      if (sync.switches.on(SyncPart.logins)) unawaited(vault.sync());
    }
    return run;
  }

  Future<void> _signedIn(AccountSession session, {required bool first}) async {
    final run = _start(session);
    if (!first) return;
    var upload = true;
    try {
      final r = await session.request('GET', '/sync?since=0');
      final hasProfiles = (r['docs'] as List? ?? []).any(
        (d) =>
            d is Map &&
            d['kind'] == 'profile' &&
            (d['fields'] as Map?)?['_deleted'] != true,
      );
      final unbound = profiles.profiles
          .where((p) => p.account.isEmpty)
          .isNotEmpty;
      if (hasProfiles) {
        upload = unbound && (await askUpload?.call(session) ?? false);
      }
    } catch (_) {
      upload = false;
    }
    await run.sync.start(upload: upload);
    if (run.sync.switches.on(SyncPart.logins)) await run.vault.sync();
    _wire(run);
  }

  Future<void> _signedOut(AccountSession session) async {
    final run = runs.remove(session.id);
    await profiles.unbindAll(session.id);
    await run?.close();
    await keys.forget(session.id);
    // Signing out here keeps the vault key, so signing in again on this
    // device brings the logins back without an approval.
    if (session.removed) await keys.forgetVault(session.id);
    _wireActive();
    notifyListeners();
  }

  // ---- The active profile ------------------------------------------------

  void _profilesChanged() {
    if (closed) return;
    final services = profiles.session;
    if (!identical(services, _services)) {
      _services?.store.removeListener(_storeChanged);
      _services?.hostsRemote.removeListener(_wireActive);
      _services = services;
      services?.store.addListener(_storeChanged);
      services?.hostsRemote.addListener(_wireActive);
    }
    for (final run in runs.values) {
      run.sync.changed();
    }
    _wireActive();
  }

  void _storeChanged() => activeRun?.sync.changed(resume: true);

  void _wireActive() {
    for (final run in runs.values) {
      _wire(run);
    }
    final services = profiles.session;
    if (services == null) return;
    final run = activeRun;
    services.discord.bridge = run == null ? null : _Bridge(run);
    services.accountRoute = run == null ? null : () => HubRoute(run.live);
    if (run != null) unawaited(_moveOldBackup(services, run));
  }

  bool _moving = false;

  /// Settings backups lived with the Discord bot before accounts synced
  /// settings. Once per account, an old one is taken into the open profile
  /// (settings still at their defaults only) and deleted from the bot.
  Future<void> _moveOldBackup(AppServices services, AccountRun run) async {
    final done = 'account.${run.session.id}.botBackupMoved';
    if (_moving || closed || preferences.getBool(done) == true) return;
    if (!run.sync.switches.on(SyncPart.settings)) return;
    _moving = true;
    try {
      final n = await services.extras.moveOldBackup();
      if (n == null) return;
      await preferences.setBool(done, true);
      if (n > 0) {
        notify?.call('Your old settings backup moved to your Otakase account');
      }
    } catch (_) {
      /* Tried again the next time the profile opens. */
    } finally {
      _moving = false;
    }
  }

  /// Every account on a TV or computer answers the account's phones.
  void _wire(AccountRun run) {
    final services = profiles.session;
    final bool hosting = services?.hostsRemote.value ?? false;
    final remoteId = services?.tvRemote.id ?? '';
    if (run.live.hostsRemote != hosting || run.live.remoteId != remoteId) {
      run.live
        ..hostsRemote = hosting
        ..remoteId = remoteId;
      unawaited(run.live.restart());
    }
    if (hosting && services != null) {
      if (run.host?.remote != services.tvRemote) {
        unawaited(run.host?.close());
        run.host = HubRemoteHost(run.live, services.tvRemote);
      }
    } else if (run.host != null) {
      unawaited(run.host!.close());
      run.host = null;
    }
  }

  Future<void> _inbox(AccountRun run, List<Json> items) async {
    final services = profiles.session;
    if (services == null || !identical(activeRun, run)) {
      // Delivered again when a profile of this account is open.
      throw StateError('No profile of this account is open.');
    }
    await services.discord.handleInbox(items);
  }

  // ---- Logins ------------------------------------------------------------

  void _loginWritten(String profile, String key, String? value) {
    if (value == null || value.isEmpty || !vaultLogins.contains(key)) return;
    final p = profiles.profiles.where((p) => p.id == profile).firstOrNull;
    final run = p == null ? null : runs[p.account];
    if (run == null || !run.sync.switches.on(SyncPart.logins)) return;
    run.vault.changed(profile, key, value);
  }

  Future<void> _loginsArrived(String profile) async {
    final services = profiles.session;
    if (services == null || services.profileId != profile) return;
    await services.auth.reloadAccounts();
    await services.trakt.reload();
    notify?.call('Signed in to your trackers from your Otakase account');
  }

  // ---- Background and back -------------------------------------------------

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    switch (state) {
      case AppLifecycleState.resumed:
        _foreground = true;
        for (final run in runs.values) {
          run.live.start();
          unawaited(run.sync.sync());
        }
      case AppLifecycleState.paused:
      case AppLifecycleState.hidden:
        if (!_foreground) return;
        _foreground = false;
        for (final run in runs.values) {
          unawaited(run.sync.flush());
          unawaited(run.live.stop());
        }
      default:
    }
  }

  Future<void> close() async {
    if (closed) return;
    closed = true;
    if (observeLifecycle) WidgetsBinding.instance.removeObserver(this);
    profiles.removeListener(_profilesChanged);
    _services?.store.removeListener(_storeChanged);
    _services?.hostsRemote.removeListener(_wireActive);
    if (ProfileTokens.written == _loginWritten) ProfileTokens.written = null;
    for (final run in runs.values) {
      await run.sync.flush().catchError((Object _) {});
      await run.close();
    }
    runs.clear();
    await manager.close();
    super.dispose();
  }
}
