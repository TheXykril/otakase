import 'core.dart';
import 'data/catalog.dart';
import 'data/store.dart';

class AppServices {
  AppServices({
    required this.catalog,
    required this.store,
    required this.core,
  }) {
    resolver = SourceResolver(core, store);
  }
  final Catalog catalog;
  final AppStore store;
  final CoreClient core;
  late final SourceResolver resolver;
}
