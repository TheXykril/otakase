import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/data/catalog.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/store.dart';
import 'package:otakase/services.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'auth_test.dart' show MemoryTokens, account, json;

Account ani(bool allowed) => Account(
  service: 'AniList',
  token: 'token',
  expires: DateTime.now().add(const Duration(days: 30)),
  username: 'viewer',
  userId: 7,
  displayAdultContent: allowed,
);
void main() {
  for (final scenario in ['no login', 'MAL only', 'AniList off']) {
    test(
      '$scenario keeps search, lists and Continue filtered despite old switches',
      () async {
        SharedPreferences.setMockInitialValues({
          'settings.v1': jsonEncode({
            'AdultContent': true,
            'ContinueWatchingAdult': true,
          }),
        });
        final store = AppStore(await SharedPreferences.getInstance());
        final auth = AuthService(storage: MemoryTokens());
        if (scenario == 'MAL only') auth.accounts['MAL'] = account('MAL');
        if (scenario == 'AniList off') auth.accounts['AniList'] = ani(false);
        final client = MockClient((r) async {
          final request = jsonDecode(r.body) as Map;
          expect(request['variables']['adult'], false);
          return json({
            'data': {
              'Page': {
                'media': [
                  {
                    'id': 1,
                    'title': {'romaji': 'Visible'},
                  },
                  {
                    'id': 2,
                    'title': {'romaji': 'Hidden'},
                    'isAdult': true,
                  },
                ],
                'pageInfo': {},
              },
            },
          });
        });
        final services = AppServices(
          catalog: Catalog(client: client),
          store: store,
          core: CoreClient(),
          auth: auth,
        );
        const hidden = Anime(id: 2, title: 'Hidden', isAdult: true);
        await store.progress(hidden, 1, 20, 100);
        expect(store.adultAllowed, isFalse);
        expect(store.continuing, isEmpty);
        expect(store.visible(hidden), isFalse);
        expect(
          (await services.catalog.search(
            'show',
            adult: store.adultAllowed,
          )).items.map((a) => a.id),
          [1],
        );
        expect(settingDefaults.containsKey('AdultContent'), isFalse);
        expect(settingDefaults.containsKey('ContinueWatchingAdult'), isFalse);
        auth.dispose();
      },
    );
  }
  test(
    'login reads Viewer options, caches value and logout turns 18+ off',
    () async {
      SharedPreferences.setMockInitialValues({});
      final storage = MemoryTokens();
      final auth = AuthService(
        storage: storage,
        client: MockClient((r) async {
          expect(
            (jsonDecode(r.body)['query'] as String),
            contains('options { displayAdultContent }'),
          );
          return json({
            'data': {
              'Viewer': {
                'id': 7,
                'name': 'viewer',
                'options': {'displayAdultContent': true},
              },
            },
          });
        }),
      );
      final store = AppStore(await SharedPreferences.getInstance());
      AppServices(
        catalog: Catalog(),
        store: store,
        core: CoreClient(),
        auth: auth,
      );
      final url = await auth.begin('AniList');
      await auth.accept(
        Uri(
          scheme: 'otakase',
          host: 'login',
          queryParameters: {
            'state': url.queryParameters['state']!,
            'access_token': 'token',
          },
        ),
      );
      expect(store.adultAllowed, isTrue);
      expect(
        storage.values['account.AniList'],
        contains('"displayAdultContent":true'),
      );
      await auth.logout('AniList');
      expect(store.adultAllowed, isFalse);
      auth.dispose();
    },
  );
  test('app start refreshes cached preference, lookup failure retains only account cache', () async {
    final storage = MemoryTokens();
    storage.values['account.AniList'] = jsonEncode(ani(true).toJson());
    final failed = AuthService(
      storage: storage,
      client: MockClient((r) async => json({}, 503)),
    );
    await failed.initialize(platformCallbacks: false);
    expect(failed.account('AniList')!.displayAdultContent, isTrue);
    failed.dispose();
    final updated = AuthService(
      storage: storage,
      client: MockClient(
        (r) async => json({
          'data': {
            'Viewer': {
              'options': {'displayAdultContent': false},
            },
          },
        }),
      ),
    );
    await updated.initialize(platformCallbacks: false);
    expect(updated.account('AniList')!.displayAdultContent, isFalse);
    updated.dispose();
    storage.values.clear();
    final empty = AuthService(
      storage: storage,
      client: MockClient((r) async => json({}, 503)),
    );
    await empty.initialize(platformCallbacks: false);
    expect(empty.account('AniList'), isNull);
    empty.dispose();
  });
}
