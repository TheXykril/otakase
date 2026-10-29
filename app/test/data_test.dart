import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/core.dart';
import 'package:otakase/data/catalog.dart';
import 'package:otakase/data/oauth_config.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/store.dart';
import 'package:shared_preferences/shared_preferences.dart';

Json media(int id, {bool adult = false}) => {
  'id': id,
  'idMal': id,
  'title': {'romaji': 'Show $id'},
  'isAdult': adult,
  'averageScore': 85,
  'episodes': 12,
  'duration': 24,
  'description': '<b>A show</b>',
};
Future<AppStore> store() async {
  SharedPreferences.setMockInitialValues({});
  return AppStore(await SharedPreferences.getInstance());
}

class FixtureCore extends CoreClient {
  final calls = <Json>[];
  @override
  Future<Json> request(
    Json input, {
    void Function(int)? onHandle,
    RequestScope? scope,
    Duration timeout = const Duration(seconds: 40),
  }) async {
    scope?.check();
    calls.add(input);
    if (input['operation'] == 'providers') {
      return {'providers': sourceNames};
    }
    if (input['provider'] == 'anikoto') {
      throw const CoreFailure('not_found', 'No match');
    }
    if (input['operation'] == 'match') {
      return {
        'show': {'id': '42'},
      };
    }
    return {
      'streams': [
        {
          'url': 'https://video.example/stream.m3u8',
          'headers': {'Origin': 'https://source.example'},
          'referrer': 'https://source.example/player',
          'subtitles': [
            {
              'url': 'https://video.example/sub.ass',
              'language': 'en',
              'label': 'English',
            },
          ],
        },
      ],
    };
  }
}

void main() {
  test(
    'volume leveller is on by default and clears when switched off',
    () async {
      final s = await store();
      expect(audioFilter(s), volumeLeveller);
      expect(volumeLeveller, contains('dynaudnorm'));
      await s.set('NormalizeVolume', false);
      expect(audioFilter(s), '');
    },
  );

  test(
    'AniList adult opt-in and catalog response filtering, cached queries',
    () async {
      var count = 0;
      final catalog = Catalog(
        client: MockClient((request) async {
          count++;
          final body = jsonDecode(request.body) as Map;
          expect(body['variables']['adult'], count == 1 ? false : null);
          return http.Response(
            jsonEncode({
              'data': {
                'Page': {
                  'media': [media(1), media(2, adult: true)],
                  'pageInfo': {'hasNextPage': true},
                },
              },
            }),
            200,
          );
        }),
      );
      expect((await catalog.search('test', adult: false)).items.length, 1);
      await catalog.search('test', adult: false);
      expect(count, 1);
      expect((await catalog.search('test', adult: true)).items.length, 2);
      expect(count, 2);
    },
  );
  test(
    'MAL public list uses app client id, status and paging without tokens',
    () async {
      final catalog = Catalog(
        client: MockClient((request) async {
          expect(request.headers['X-MAL-CLIENT-ID'], malId);
          expect(request.headers.containsKey('Authorization'), false);
          expect(request.url.queryParameters['status'], 'watching');
          expect(request.url.queryParameters['offset'], '50');
          return http.Response(
            jsonEncode({
              'data': [
                {
                  'node': {
                    'id': 1,
                    'title': 'Show',
                    'num_episodes': 12,
                    'mean': 8.5,
                  },
                  'list_status': {
                    'status': 'watching',
                    'num_episodes_watched': 4,
                  },
                },
              ],
              'paging': {},
            }),
            200,
          );
        }),
      );
      final list = await catalog.publicList(
        'MAL',
        'viewer',
        'watching',
        adult: false,
        page: 2,
      );
      expect(list.entries.single.progress, 4);
      expect(list.entries.single.anime.score, 8.5);
    },
  );
  test(
    'public AniList custom groups deduplicate and filter status/adult',
    () async {
      final entry = {'status': 'CURRENT', 'progress': 3, 'media': media(1)};
      final catalog = Catalog(
        client: MockClient(
          (request) async => http.Response(
            jsonEncode({
              'data': {
                'MediaListCollection': {
                  'lists': [
                    {
                      'entries': [
                        entry,
                        {'status': 'CURRENT', 'media': media(2, adult: true)},
                      ],
                    },
                    {
                      'entries': [entry],
                    },
                  ],
                },
                'User': {
                  'avatar': {'medium': ''},
                },
              },
            }),
            200,
          ),
        ),
      );
      final list = await catalog.publicList(
        'AniList',
        'viewer',
        'CURRENT',
        adult: false,
      );
      expect(list.entries.length, 1);
      expect(list.entries.single.progress, 3);
    },
  );
  test('progress and watched episodes survive reload; adult history defaults hidden', () async {
    final s = await store();
    final a = Anime.fromJson(media(1));
    await s.progress(a, 2, 155, 1440);
    await s.markWatched(a, 1, true);
    final reloaded = AppStore(s.preferences);
    expect(reloaded.entry(a)!.position, 155);
    expect(reloaded.entry(a)!.watched, [1]);
    final adult = Anime.fromJson(media(2, adult: true));
    await reloaded.progress(adult, 1, 10, 100);
    expect(reloaded.history.length, 1);
    reloaded.setAdultAllowed(true);
    expect(reloaded.history.length, 2);
    await reloaded.progress(a, 2, 1440, 1440, completed: true);
    expect(reloaded.entry(a)!.watched, [1, 2]);
  });
  test('core order falls back silently, remembers a working show copy and keeps hints', () async {
    final s = await store();
    await s.set('Provider', 'reanime');
    await s.set('ProviderOrder', [
      'reanime',
      ...sourceNames.where((p) => p != 'reanime'),
    ]);
    await s.set('AutoAudioFallback', false);
    final c = FixtureCore(), scope = RequestScope();
    final resolved = await SourceResolver(
      c,
      s,
    ).resolve(Anime.fromJson(media(1)), 1, scope: scope, status: (_) {});
    expect(c.calls.first['operation'], 'providers');
    expect(
      c.calls.where((c) => c['operation'] == 'match').first['provider'],
      'anikoto',
    );
    expect(resolved.provider, 'megavid');
    expect(resolved.streams.single.headers['Origin'], 'https://source.example');
    expect(resolved.streams.single.subtitles.single['url'], endsWith('.ass'));
    await s.rememberSource(Anime.fromJson(media(1)), 'megavid');
    final reloaded = AppStore(s.preferences);
    final second = FixtureCore();
    final preferred = await SourceResolver(second, reloaded).resolve(
      Anime.fromJson(media(1)),
      1,
      scope: RequestScope(),
      status: (_) {},
    );
    expect(preferred.provider, 'megavid');
    expect(
      second.calls.where((c) => c['operation'] == 'match').first['provider'],
      'megavid',
    );
    scope.cancel();
    await expectLater(
      SourceResolver(
        c,
        s,
      ).resolve(Anime.fromJson(media(1)), 1, scope: scope, status: (_) {}),
      throwsA(isA<CoreFailure>()),
    );
  });
}
