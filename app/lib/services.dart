import 'core.dart';
import 'data/auth.dart';
import 'data/tracking.dart';
import 'data/catalog.dart';
import 'data/store.dart';
import 'data/list_sync.dart';
import 'data/profiles.dart';
import 'data/discord.dart';
import 'data/heritage.dart';
import 'data/models.dart';
import 'data/watch_rooms.dart';
import 'data/tv_remote.dart';

class AppServices {
  AppServices({
    required this.catalog,
    required this.store,
    required this.core,
    AuthService? auth,
    TVRemote? remoteControl,
    this.profiles,
    this.profileId = '1',
  }) : auth = auth ?? AuthService(client: catalog.client) {
    discord = DiscordService(
      client: catalog.client,
      storage: this.auth.storage,
      store: store,
    );
    heritage = Heritage(store, StatsRepository(this.auth), discord);
    watchRooms = WatchRooms(discord: discord, store: store);
    tvRemote =
        remoteControl ??
        TVRemote(
          storage: this.auth.storage,
          name: profiles?.active.name ?? 'Otakase TV',
        );
    tvRemote.command = (action, args) async {
      await playerCommand?.call(action, args);
    };
    tvRemote.status = () =>
        playerStatus?.call() ?? {'has_player': false, 'playing': false};
    resolver = SourceResolver(core, store, report: discord.reportProvider);
    tracking = TrackingService(this.auth, store: store);
    tracking.onProgressWritten = () => heritage.progressWritten = true;
    discord.remote = (action, args) async {
      await playerCommand?.call(action, args);
    };
    listSync = ListSync(tracking: tracking, store: store);
    this.auth.addListener(_adultPreference);
    _adultPreference();
  }
  void _adultPreference() => store.setAdultAllowed(
    auth.account('AniList')?.displayAdultContent ?? false,
  );
  int root = 0;
  void Function(int)? selectRoot;
  final AuthService auth;
  late final TrackingService tracking;
  late final ListSync listSync;
  late final DiscordService discord;
  late final Heritage heritage;
  late final WatchRooms watchRooms;
  late final TVRemote tvRemote;
  Json Function()? playerStatus;
  Future<void> Function(String action, Json args)? playerCommand;
  final ProfileManager? profiles;
  final String profileId;
  Future<void> Function()? stopPlayback;
  bool closed = false;
  Future<void> close() async {
    if (closed) return;
    await stopPlayback?.call();
    heritage.closed = true;
    await watchRooms.close();
    await tvRemote.close();
    await discord.close();
    closed = true;
    auth.removeListener(_adultPreference);
    listSync.dispose();
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
