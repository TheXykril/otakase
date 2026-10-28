import 'core.dart';
import 'data/auth.dart';
import 'data/tracking.dart';
import 'data/catalog.dart';
import 'data/store.dart';
import 'data/list_sync.dart';
import 'data/profiles.dart';

class AppServices {
  AppServices({
    required this.catalog,
    required this.store,
    required this.core,
    AuthService? auth,
    this.profiles,
    this.profileId = '1',
  }) : auth = auth ?? AuthService(client: catalog.client) {
    resolver = SourceResolver(core, store);
    tracking = TrackingService(this.auth, store: store);
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
  final ProfileManager? profiles;
  final String profileId;
  Future<void> Function()? stopPlayback;
  bool closed = false;
  Future<void> close() async {
    if (closed) return;
    await stopPlayback?.call();
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
