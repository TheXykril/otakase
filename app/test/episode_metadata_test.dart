import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/data/catalog.dart';
import 'package:otakase/data/episode_metadata.dart';
import 'package:otakase/data/models.dart';

import 'auth_test.dart' show json;

final recording = File('test/fixtures/ani_zip_1.json').readAsStringSync();
const show = Anime(id: 1, title: 'Cowboy Bebop');
Json details({bool complete = false, bool adult = false}) => {
  'data': {
    'Media': {
      'id': 1,
      'idMal': 1,
      'title': {'romaji': 'Cowboy Bebop'},
      'coverImage': {'large': 'cover.jpg'},
      'bannerImage': 'banner.jpg',
      'episodes': 3,
      'isAdult': adult,
      'streamingEpisodes': [
        {'title': 'Episode 1 - AniList title', 'thumbnail': 'anilist-1.jpg'},
        {
          'title': 'Episode 2 - AniList title',
          if (complete) 'thumbnail': 'anilist-2.jpg',
        },
        if (complete)
          {'title': 'Episode 3 - AniList title', 'thumbnail': 'anilist-3.jpg'},
      ],
    },
  },
};

http.Response utf8Json(Object body) =>
    http.Response.bytes(utf8.encode(jsonEncode(body)), 200);

void main() {
  test(
    'recorded ani.zip fills missing episode art and preserves AniList first',
    () async {
      var graphCalls = 0, zipCalls = 0;
      final logs = <String>[];
      final catalog = Catalog(
        log: logs.add,
        client: MockClient((r) async {
          if (r.url.host == 'graphql.anilist.co') {
            graphCalls++;
            expect(jsonDecode(r.body)['variables'], {'id': 1, 'adult': false});
            return json(details());
          }
          // Sources that do not know the show answer 404.
          if (r.url.host != 'api.ani.zip') return http.Response('', 404);
          expect(r.url.toString(), 'https://api.ani.zip/mappings?anilist_id=1');
          zipCalls++;
          return http.Response(
            recording,
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        }),
      );
      final result = await catalog.detail(show, adult: false);
      expect(logs, isEmpty);
      expect(result.banner, 'banner.jpg');
      expect(result.episodeArt.map(episodeNumber), [1, 2, 3]);
      expect(result.episodeArt[0]['title'], 'Episode 1 - AniList title');
      expect(result.episodeArt[0]['thumbnail'], 'anilist-1.jpg');
      expect(result.episodeArt[1]['title'], 'Episode 2 - AniList title');
      expect(
        result.episodeArt[1]['thumbnail'],
        'https://artworks.thetvdb.com/banners/episodes/76885/219121.jpg',
      );
      expect(result.episodeArt[2]['title'], 'Honky Tonk Women');
      expect(result.episodeArt[2]['airdate'], '1998-04-10');
      expect(
        result.episodeArt.any((e) => e['thumbnail'] == 'cover.jpg'),
        isFalse,
      );
      await catalog.detail(show, adult: false);
      expect(graphCalls, 1);
      expect(zipCalls, 1);
    },
  );

  test('complete AniList episode metadata needs no fallback request', () async {
    var calls = 0;
    final catalog = Catalog(
      client: MockClient((r) async {
        expect(r.url.host, 'graphql.anilist.co');
        calls++;
        return json(details(complete: true));
      }),
    );
    final result = await catalog.detail(show, adult: false);
    expect(result.episodeArt, hasLength(3));
    expect(calls, 1);
  });

  test(
    'failed episode metadata keeps real AniList rows and logs HTTP status',
    () async {
      final logs = <String>[];
      final catalog = Catalog(
        log: logs.add,
        client: MockClient(
          (r) async => r.url.host == 'graphql.anilist.co'
              ? json(details())
              : http.Response('', 503),
        ),
      );
      final result = await catalog.detail(show, adult: false);
      expect(result.episodeArt, hasLength(2));
      expect(result.episodeArt.first['thumbnail'], 'anilist-1.jpg');
      expect(result.episodeArt.last.containsKey('thumbnail'), isFalse);
      expect(logs, hasLength(4));
      expect(logs, everyElement(contains('HTTP 503')));
    },
  );

  for (final status in [429, 503]) {
    test('detail retries HTTP $status once with backoff', () async {
      var calls = 0;
      final waits = <Duration>[], logs = <String>[];
      final catalog = Catalog(
        log: logs.add,
        delay: (d) async {
          waits.add(d);
        },
        client: MockClient(
          (r) async => ++calls == 1
              ? http.Response('', status)
              : json(details(complete: true)),
        ),
      );
      expect((await catalog.detail(show, adult: false)).id, 1);
      expect(calls, 2);
      expect(waits.single, greaterThan(Duration.zero));
      expect(logs.single, contains('$status'));
    });
  }

  test(
    'last good details survive exhausted retries after cache expiry',
    () async {
      var clock = DateTime(2026, 10, 28), calls = 0;
      final logs = <String>[], waits = <Duration>[];
      final catalog = Catalog(
        now: () => clock,
        log: logs.add,
        delay: (d) async {
          waits.add(d);
        },
        client: MockClient(
          (r) async => ++calls == 1
              ? json(details(complete: true))
              : http.Response('', 502),
        ),
      );
      final good = await catalog.detail(show, adult: false);
      clock = clock.add(const Duration(minutes: 11));
      final fallback = await catalog.detail(show, adult: false);
      expect(fallback, same(good));
      expect(fallback.episodeArt, hasLength(3));
      expect(calls, 3);
      expect(waits, hasLength(1));
      expect(logs, hasLength(2));
      expect(logs.every((m) => m.contains('502')), isTrue);
    },
  );

  test(
    'GraphQL error logs its real message without retrying a permanent error',
    () async {
      var calls = 0;
      final logs = <String>[];
      final catalog = Catalog(
        log: logs.add,
        delay: (_) async => fail('No retry for validation errors'),
        client: MockClient((r) async {
          calls++;
          return json({
            'errors': [
              {
                'message': 'Unknown argument idMal on field Media.',
                'status': 400,
              },
            ],
          });
        }),
      );
      await expectLater(
        catalog.detail(show, adult: false),
        throwsA(isA<CatalogFailure>()),
      );
      expect(calls, 1);
      expect(logs.single, contains('Unknown argument idMal on field Media.'));
    },
  );

  test(
    'cached details cannot restore a title after 18+ filtering turns off',
    () async {
      var fail = false;
      final catalog = Catalog(
        log: (_) {},
        client: MockClient(
          (r) async => fail
              ? http.Response('', 400)
              : json(details(complete: true, adult: true)),
        ),
      );
      await catalog.detail(show, adult: true);
      fail = true;
      await expectLater(
        catalog.detail(show, adult: false),
        throwsA(isA<CatalogFailure>()),
      );
    },
  );

  test('sources fill gaps in the watch room order', () async {
    final catalog = Catalog(
      client: MockClient((r) async {
        switch (r.url.host) {
          case 'graphql.anilist.co':
            return json(details());
          case 'api.tenrai.org':
            return json({
              'data': [
                {
                  'mal_id': 2,
                  'title': 'Tenrai two',
                  'images': {
                    'jpg': {
                      'image_url': 'https://img1.ak.crunchyroll.com/2.jpg',
                    },
                  },
                },
              ],
              'pagination': {'has_next_page': false},
            });
          case 'animap.id':
            if (r.url.path == '/api/map/mal/1') {
              return json([
                {
                  'kitsu_id': [9],
                },
              ]);
            }
            expect(r.url.path, '/api/kitsu/9/episodes');
            return json({
              'episodes': [
                {
                  'number': 2,
                  'title_en_us': null,
                  'canonical_title': 'Kitsu two',
                  'thumbnail': 'https://media.kitsu.app/k2.png',
                },
                {
                  'number': 3,
                  'title_en_us': null,
                  'canonical_title': 'Kitsu three',
                  'thumbnail': 'http://insecure.example/k3.png',
                },
              ],
            });
          case 'api.jikan.moe':
            return utf8Json({
              'data': [
                {'mal_id': 3, 'title': 'Jikan three'},
                {'mal_id': 4, 'title': '', 'title_japanese': '第四話'},
              ],
              'pagination': {'has_next_page': false},
            });
          default:
            return http.Response('', 404);
        }
      }),
    );
    final result = await catalog.detail(
      const Anime(id: 1, title: 'Cowboy Bebop', episodes: 4),
      adult: false,
    );
    final rows = {for (final e in result.episodeArt) episodeNumber(e): e};
    expect(rows[1]!['thumbnail'], 'anilist-1.jpg');
    expect(rows[2]!['title'], 'Episode 2 - AniList title');
    expect(rows[2]!['thumbnail'], 'https://img1.ak.crunchyroll.com/2.jpg');
    expect(rows[3]!['title'], 'Kitsu three');
    expect(rows[3]!.containsKey('thumbnail'), isFalse);
    expect(rows[4]!['title'], '第四話');
  });
}
