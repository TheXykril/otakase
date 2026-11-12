import 'dart:async';

import 'data/remote_relay.dart';

import 'package:flutter/foundation.dart';

import 'core.dart';
import 'data/auth.dart';
import 'data/tracking.dart';
import 'data/catalog.dart';
import 'data/store.dart';
import 'data/list_sync.dart';
import 'data/profiles.dart';
import 'data/discord.dart';
import 'data/episode_alerts.dart';
import 'data/heritage.dart';
import 'data/models.dart';
import 'data/watch_party.dart';
import 'data/community_extras.dart';
import 'data/tv_remote.dart';
import 'data/app_updates.dart';
import 'data/downloads.dart';
import 'data/movies.dart';
import 'data/stay_awake.dart';
import 'data/trakt.dart';
import 'data/presence.dart';
import 'data/skip_times.dart';
import 'desktop/desktop_core.dart';
import 'desktop/desktop_updates.dart';

class AppServices {
  AppServices({
    required this.catalog,
    required this.store,
    required this.core,
    AuthService? auth,
    TVRemote? remoteControl,
    AppUpdates? appUpdates,
    this.profiles,
    this.profileId = '1',
    StayAwake? awake,
  }) : awake = awake ?? StayAwake.instance,
       auth = auth ?? AuthService(client: catalog.client) {
    updates =
        appUpdates ??
        AppUpdates(platform: isDesktop ? DesktopUpdatePlatform() : null);
    discord = DiscordService(
      client: catalog.client,
      storage: this.auth.storage,
      store: store,
    );
    heritage = Heritage(store, StatsRepository(this.auth));
    party = WatchParties(discord: discord, store: store);
    // A watch party follows anime only; a film playing is no party state.
    party.status = () {
      final s = playerStatus?.call();
      return s?['kind'] == 'movie' ? null : s;
    };
    party.command = (action, args) async {
      await playerCommand?.call(action, args);
    };
    extras = CommunityExtras(discord: discord, store: store);
    tvRemote =
        remoteControl ??
        TVRemote(storage: this.auth.storage, lookupName: localDeviceName);
    tvRemote.command = (action, args) async {
      await playerCommand?.call(action, args);
    };
    discord.addListener(_relayCheck);
    store.addListener(_relayCheck);
    tvRemote.status = () =>
        playerStatus?.call() ?? {'has_player': false, 'playing': false};
    resolver = SourceResolver(core, store, report: discord.reportProvider);
    skipTimes = SkipTimes(core, store);
    presence = DiscordPresence(core, store);
    tracking = TrackingService(this.auth, store: store);
    tracking.onProgressWritten = () => heritage.progressWritten = true;
    discord.remote = (action, args) async {
      await playerCommand?.call(action, args);
    };
    listSync = ListSync(tracking: tracking, store: store);
    trakt = Trakt(client: catalog.client, storage: this.auth.storage);
    movies = MovieService(trakt: trakt, core: core, store: store);
    downloads = DownloadManager(
      core: core,
      preferences: store.preferences,
      client: catalog.client,
    );
    this.auth.addListener(_adultPreference);
    _adultPreference();
    updates.addListener(_updateAwake);
    downloads.addListener(_downloadsAwake);
    store.addListener(_screenSetting);
    _screenSetting();
  }
  final StayAwake awake;
  void _updateAwake() => awake.busy(
    'update',
    updates.phase == UpdatePhase.downloading
        ? 'Downloading the Otakase update'
        : null,
  );
  void _downloadsAwake() {
    final running = downloads.items.where((i) => i.running).length;
    awake.busy(
      'downloads',
      running == 0
          ? null
          : 'Saving $running ${running == 1 ? 'download' : 'downloads'}',
    );
  }

  void _screenSetting() => awake.always = store.flag('KeepScreenOn');
  void _adultPreference() => store.setAdultAllowed(
    auth.account('AniList')?.displayAdultContent ?? false,
  );
  int root = 0;
  void Function(int)? selectRoot;
  final AuthService auth;
  late final TrackingService tracking;
  late final ListSync listSync;
  late final Trakt trakt;
  late final MovieService movies;
  late final DownloadManager downloads;
  late final DiscordService discord;
  late final SkipTimes skipTimes;
  late final DiscordPresence presence;
  late final Heritage heritage;
  late final WatchParties party;
  late final CommunityExtras extras;
  late final TVRemote tvRemote;

  /// The TV this phone is controlling, kept across pages so the now
  /// playing bar can show it anywhere in the app.
  final remoteLink = ValueNotifier<RemoteLink?>(null);

  /// Starts controlling [tv], closing any other TV's connection.
  RemoteLink controlTV(
    TVDevice tv, {
    Future<List<TVDevice>> Function()? findTVs,
  }) {
    final old = remoteLink.value;
    if (old != null && old.tv.id == tv.id) return old;
    unawaited(old?.close());
    final link = RemoteLink(
      tvRemote,
      tv,
      findTVs: findTVs ?? discoverTVs,
      relay: store.flag('RemoteAnywhere') ? relay : null,
      phoneName: tvRemote.name,
    );
    remoteLink.value = link..start();
    return link;
  }

  /// The phone remote away from home: through the Otakase account when this
  /// profile belongs to one, else through the Discord bot.
  AwayRoute get relay => accountRoute?.call() ?? botRelay;

  /// Set by the account center while this profile's account is online.
  AwayRoute? Function()? accountRoute;

  /// The phone remote through the Discord bot, for TVs this phone can't
  /// reach on the local network and TVs on the same Discord account.
  late final botRelay = RelayClient(
    base: discord.baseUrl,
    token: () => discord.linked ? discord.linkToken : '',
    list: () => discord.request('GET', '/v1/remote/tvs'),
  );

  RelayHost? _relayHost;
  bool _hostsRemote = false;

  /// New episode alerts for the shows this profile watches.
  late final episodeAlerts = EpisodeAlerts(store.preferences);

  /// Whether this TV or computer answers phone remotes away from home.
  final hostsRemote = ValueNotifier<bool>(false);

  /// On a TV or computer: keeps a connection to the Discord bot while the
  /// profile is linked and RemoteAnywhere is on, so the member's phones
  /// reach it away from home.
  void hostRemote(bool on) {
    _hostsRemote = on;
    hostsRemote.value = on && store.flag('RemoteAnywhere');
    _relayCheck();
  }

  void _relayCheck() {
    hostsRemote.value = _hostsRemote && !closed && store.flag('RemoteAnywhere');
    // The Otakase account carries the remote when the profile has one.
    final want =
        _hostsRemote &&
        !closed &&
        discord.linked &&
        !discord.viaAccount &&
        accountRoute?.call() == null &&
        store.flag('RemoteAnywhere');
    if (want && _relayHost == null) {
      _relayHost = RelayHost(
        remote: tvRemote,
        base: discord.baseUrl,
        token: () => discord.linked ? discord.linkToken : '',
      )..start();
    } else if (!want && _relayHost != null) {
      unawaited(_relayHost!.close());
      _relayHost = null;
    }
  }

  /// Stops controlling the TV.
  Future<void> leaveTV() async {
    final old = remoteLink.value;
    remoteLink.value = null;
    await old?.close();
  }

  late final AppUpdates updates;

  /// A show the last player finished, to be scored once it closes.
  Anime? ratingDue;

  /// A film or show just finished, to ask a score for as the player closes.
  MediaTitle? titleRatingDue;
  Json Function()? playerStatus;
  Future<void> Function(String action, Json args)? playerCommand;
  final ProfileManager? profiles;
  final String profileId;
  Future<void> Function()? stopPlayback;
  bool closed = false;
  Future<void> close() async {
    if (closed) return;
    await stopPlayback?.call();
    episodeAlerts.dispose();
    heritage.closed = true;
    await party.close();
    extras.dispose();
    await leaveTV();
    discord.removeListener(_relayCheck);
    store.removeListener(_relayCheck);
    await _relayHost?.close();
    _relayHost = null;
    await tvRemote.close();
    await presence.clear();
    await discord.close();
    closed = true;
    updates.removeListener(_updateAwake);
    downloads.removeListener(_downloadsAwake);
    store.removeListener(_screenSetting);
    awake
      ..busy('update', null)
      ..busy('downloads', null)
      ..always = false;
    updates.dispose();
    auth.removeListener(_adultPreference);
    listSync.dispose();
    movies.dispose();
    trakt.dispose();
    downloads.dispose();
    await auth.cancelLogin();
    auth.dispose();
    if (auth.storage case final ProfileTokens scoped) await scoped.deactivate();
    tracking.dispose();
    catalog.client.close();
    store.dispose();
  }

  final Catalog catalog;
  final AppStore store;
  final CoreClient core;
  late final SourceResolver resolver;
}
