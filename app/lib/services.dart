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

class AppServices {
  AppServices({
    required this.catalog,
    required this.store,
    required this.core,
    AuthService? auth,
    this.profiles,
    this.profileId = '1',
  }) : auth = auth ?? AuthService(client: catalog.client) {
    discord = DiscordService(
      client: catalog.client,
      storage: this.auth.storage,
      store: store,
    );
    heritage = Heritage(store, StatsRepository(this.auth), discord);
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
  Future<void> Function(String action, Json args)? playerCommand;
  final ProfileManager? profiles;
  final String profileId;
  Future<void> Function()? stopPlayback;
  bool closed = false;
  Future<void> close() async {
    if (closed) return;
    await stopPlayback?.call();
    heritage.closed = true;
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
