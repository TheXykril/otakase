import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:otakase/data/store.dart';
import 'package:shared_preferences/shared_preferences.dart';

// List caches live apart from settings, and source health only reaches the
// pages that show it.
void main() {
  test('old list caches move out of settings', () async {
    SharedPreferences.setMockInitialValues({
      'settings.v1': jsonEncode({
        'Quality': '720',
        'ListCache.AniList': {'identity': 'a', 'entries': []},
        'PublicListCache.MAL': {'username': 'b', 'entries': []},
      }),
    });
    final preferences = await SharedPreferences.getInstance();
    final store = AppStore(preferences);
    await Future<void>.delayed(Duration.zero);
    expect(store.isSet('ListCache.AniList'), isFalse);
    expect(store.text('Quality'), '720');
    expect((store.cache('ListCache.AniList') as Map)['identity'], 'a');
    expect((store.cache('PublicListCache.MAL') as Map)['username'], 'b');
    final saved = jsonDecode(preferences.getString('settings.v1')!) as Map;
    expect(saved.keys, ['Quality']);
  });

  test('caches and source health do not rebuild the app', () async {
    SharedPreferences.setMockInitialValues({});
    final store = AppStore(await SharedPreferences.getInstance());
    var app = 0, health = 0;
    store.addListener(() => app++);
    store.healthChanged.addListener(() => health++);
    await store.setCache('ListCache.AniList', {'entries': []});
    store.health('Source', 'Working');
    store.health('Source', 'Working');
    expect(app, 0);
    expect(health, 1);
    expect(store.providerHealth['Source'], 'Working');
    await store.setCache('ListCache.AniList', null);
    expect(store.cache('ListCache.AniList'), isNull);
  });
}
