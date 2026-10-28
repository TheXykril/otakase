import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:otakase/data/auth.dart';
import 'package:otakase/data/models.dart';
import 'package:otakase/data/synced_resume.dart';
import 'package:otakase/data/tracking.dart';

import 'auth_test.dart' show MemoryTokens, account, json;

const anime = Anime(id: 42, malId: 24, title: 'Story', episodes: 12);
void main() {
  test(
    'both trackers receive progress, status and score using native fields',
    () async {
      final writes = <String, Object>{};
      final auth = AuthService(
        storage: MemoryTokens(),
        client: MockClient((r) async {
          if (r.url.host == 'graphql.anilist.co') {
            final body = jsonDecode(r.body) as Map;
            writes['AniList'] = body['variables'];
            return json({
              'data': {
                'SaveMediaListEntry': {'id': 1},
              },
            });
          }
          writes['MAL'] = Uri.splitQueryString(r.body);
          return json({});
        }),
      );
      auth.accounts.addAll({
        'AniList': account('AniList'),
        'MAL': account('MAL'),
      });
      await TrackingService(auth)
          .update(anime, status: 'PAUSED', progress: 5, score: 80);
      expect((writes['AniList'] as Map)['score'], 80);
      expect((writes['AniList'] as Map)['progress'], 5);
      expect((writes['AniList'] as Map).containsKey('notes'), isFalse);
      expect(writes['MAL'], {
        'status': 'on_hold',
        'is_rewatching': 'false',
        'num_watched_episodes': '5',
        'score': '8',
      });
      auth.dispose();
    },
  );
  test(
    'resume reads both notes and comments and chooses the later episode',
    () async {
      final auth = AuthService(
        storage: MemoryTokens(),
        client: MockClient(
          (r) async => r.url.host == 'graphql.anilist.co'
              ? json({
                  'data': {
                    'Media': {
                      'mediaListEntry': {
                        'status': 'CURRENT',
                        'progress': 4,
                        'notes': 'my note\n[otakase:5@900]',
                      },
                    },
                  },
                })
              : json({
                  'my_list_status': {
                    'status': 'watching',
                    'num_episodes_watched': 5,
                    'comments': '[otakase:6@30]',
                  },
                }),
        ),
      );
      auth.accounts.addAll({
        'AniList': account('AniList'),
        'MAL': account('MAL'),
      });
      expect(
        await TrackingService(auth).resume(anime),
        const SyncedResume(6, 30),
      );
      auth.dispose();
    },
  );
  test(
    'resume writes preserve unrelated notes, missing list entry is untouched',
    () async {
      String? saved;
      var malWrites = 0;
      final auth = AuthService(
        storage: MemoryTokens(),
        client: MockClient((r) async {
          if (r.url.host == 'graphql.anilist.co') {
            final body = jsonDecode(r.body) as Map;
            if ((body['query'] as String).startsWith('mutation')) {
              saved = body['variables']['notes'] as String;
              return json({
                'data': {
                  'SaveMediaListEntry': {'id': 1},
                },
              });
            }
            return json({
              'data': {
                'Media': {
                  'mediaListEntry': {
                    'status': 'CURRENT',
                    'progress': 4,
                    'notes': 'line one\n[otakase:5@400]\nline two',
                  },
                },
              },
            });
          }
          if (r.method == 'PUT') malWrites++;
          return json({});
        }),
      );
      auth.accounts.addAll({
        'AniList': account('AniList'),
        'MAL': account('MAL'),
      });
      await TrackingService(auth).pushResume(anime, const SyncedResume(5, 754));
      expect(saved, 'line one\n[otakase:5@754]\nline two');
      expect(malWrites, 0);
      auth.dispose();
    },
  );
  test(
    'completion advances resume without lowering a later tracker progress',
    () async {
      Map? written;
      final auth = AuthService(
        storage: MemoryTokens(),
        client: MockClient((r) async {
          final body = jsonDecode(r.body) as Map;
          if ((body['query'] as String).startsWith('mutation')) {
            written = body['variables'] as Map;
            return json({
              'data': {
                'SaveMediaListEntry': {'id': 1},
              },
            });
          }
          return json({
            'data': {
              'Media': {
                'mediaListEntry': {
                  'status': 'CURRENT',
                  'progress': 8,
                  'notes': 'keep\n[otakase:9@300]',
                },
              },
            },
          });
        }),
      );
      auth.accounts['AniList'] = account('AniList');
      await TrackingService(auth).finish(anime, 5, 12);
      expect(written!['progress'], 8);
      expect(written!['notes'], 'keep\n[otakase:9@300]');
      await TrackingService(auth).finish(anime, 12, 12);
      expect(written!['status'], 'COMPLETED');
      expect(written!['notes'], contains('[otakase:12@done]'));
      auth.dispose();
    },
  );
  test('one failed tracker does not prevent writes to the other', () async {
    var malWrites = 0;
    final auth = AuthService(
      storage: MemoryTokens(),
      client: MockClient((r) async {
        if (r.url.host == 'graphql.anilist.co') return http.Response('', 503);
        malWrites++;
        return json({});
      }),
    );
    auth.accounts.addAll({
      'AniList': account('AniList'),
      'MAL': account('MAL'),
    });
    final tracking = TrackingService(auth);
    await expectLater(
      tracking.update(anime, progress: 4),
      throwsA(isA<LoginFailure>()),
    );
    expect(malWrites, 1);
    expect(tracking.lastError, contains('AniList'));
    auth.dispose();
  });
  test(
    'private AniList list deduplicates groups and filters 18+ when off',
    () async {
      final entry = {
        'status': 'CURRENT',
        'progress': 3,
        'media': {
          'id': 42,
          'title': {'romaji': 'Story'},
        },
      };
      final auth = AuthService(
        storage: MemoryTokens(),
        client: MockClient(
          (r) async => json({
            'data': {
              'MediaListCollection': {
                'lists': [
                  {
                    'entries': [
                      entry,
                      {
                        'status': 'PLANNING',
                        'media': {
                          'id': 43,
                          'title': {'romaji': 'Hidden'},
                          'isAdult': true,
                        },
                      },
                    ],
                  },
                  {
                    'entries': [entry],
                  },
                ],
              },
            },
          }),
        ),
      );
      auth.accounts['AniList'] = account('AniList');
      final rows = await TrackingService(auth).list('AniList', adult: false);
      expect(rows.entries.length, 1);
      expect(rows.entries.single.progress, 3);
      auth.dispose();
    },
  );
}
