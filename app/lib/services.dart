import 'core.dart';
import 'data/auth.dart';
import 'data/tracking.dart';
import 'data/catalog.dart';
import 'data/store.dart';
import 'data/list_sync.dart';

class AppServices {
  AppServices({
    required this.catalog,
    required this.store,
    required this.core,
    AuthService? auth,
  }) : auth = auth ?? AuthService(client: catalog.client) {
    resolver = SourceResolver(core, store);
    tracking = TrackingService(this.auth);
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
  final Catalog catalog;
  final AppStore store;
  final CoreClient core;
  late final SourceResolver resolver;
}
