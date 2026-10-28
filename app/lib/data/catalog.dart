import 'dart:convert';

import 'package:http/http.dart' as http;

import 'models.dart';
import 'oauth_config.dart';

class CatalogFailure implements Exception {
  const CatalogFailure(this.message);
  final String message;
  @override
  String toString() => message;
}

const mediaFields =
    '''id idMal type title { romaji english native } synonyms isAdult
  coverImage { large extraLarge } bannerImage description(asHtml:false) averageScore
  format status seasonYear episodes duration genres startDate { year }
  nextAiringEpisode { episode airingAt }''';

class Catalog {
  Catalog({http.Client? client}) : client = client ?? http.Client();
  final http.Client client;
  final Map<String, ({DateTime expires, Json data})> _cache = {};
  void clear() => _cache.clear();
  Future<Json> graph(
    String query,
    Json variables, {
    Duration ttl = const Duration(minutes: 10),
  }) async {
    final key = jsonEncode([query, variables]);
    final cache = _cache[key];
    if (cache != null && cache.expires.isAfter(DateTime.now())) {
      return cache.data;
    }
    final http.Response response;
    try {
      response = await client
          .post(
            Uri.https('graphql.anilist.co'),
            headers: {
              'Content-Type': 'application/json',
              'Accept': 'application/json',
            },
            body: jsonEncode({'query': query, 'variables': variables}),
          )
          .timeout(const Duration(seconds: 20));
    } catch (_) {
      throw const CatalogFailure(
        'Unable to reach AniList. Check your connection and retry.',
      );
    }
    if (response.statusCode == 429) {
      throw const CatalogFailure(
        'AniList is rate limiting requests. Try again shortly.',
      );
    }
    if (response.statusCode != 200) {
      throw CatalogFailure(
        'AniList is unavailable (${response.statusCode}). Retry in a moment.',
      );
    }
    final Json body;
    try {
      body = (jsonDecode(response.body) as Map).cast<String, dynamic>();
    } catch (_) {
      throw const CatalogFailure('AniList returned an unreadable response.');
    }
    if ((body['errors'] as List? ?? []).isNotEmpty || body['data'] == null) {
      throw const CatalogFailure(
        'This AniList data is unavailable or private.',
      );
    }
    final data = (body['data'] as Map).cast<String, dynamic>();
    _cache[key] = (expires: DateTime.now().add(ttl), data: data);
    return data;
  }

  List<Anime> _media(dynamic items, bool adult) => (items as List? ?? [])
      .whereType<Map>()
      .map((e) => Anime.fromJson(e.cast<String, dynamic>()))
      .where((m) => adult || !m.isAdult)
      .toList();
  List<Airing> _airing(dynamic items, bool adult) => (items as List? ?? [])
      .whereType<Map>()
      .where((e) => e['media'] is Map)
      .map(
        (e) => Airing(
          Anime.fromJson((e['media'] as Map).cast<String, dynamic>()),
          number(e['episode']),
          DateTime.fromMillisecondsSinceEpoch(number(e['airingAt']) * 1000),
        ),
      )
      .where((e) => adult || !e.anime.isAdult)
      .toList();
  Future<HomeData> home({required bool adult}) async {
    final now = DateTime.now();
    final start = DateTime(now.year, now.month, now.day);
    final season = ['WINTER', 'SPRING', 'SUMMER', 'FALL'][(now.month - 1) ~/ 3];
    final query =
        '''query(\$adult:Boolean,\$season:MediaSeason,\$year:Int,\$now:Int,\$week:Int,\$start:Int,\$end:Int){
      trending:Page(perPage:15){media(type:ANIME,isAdult:\$adult,sort:TRENDING_DESC){$mediaFields}}
      popular:Page(perPage:20){media(type:ANIME,isAdult:\$adult,season:\$season,seasonYear:\$year,sort:POPULARITY_DESC){$mediaFields}}
      recent:Page(perPage:40){airingSchedules(airingAt_lesser:\$now,airingAt_greater:\$week,sort:TIME_DESC){episode airingAt media{$mediaFields}}}
      today:Page(perPage:40){airingSchedules(airingAt_greater:\$start,airingAt_lesser:\$end,sort:TIME){episode airingAt media{$mediaFields}}}
    }''';
    final data = await graph(query, {
      'adult': adult ? null : false,
      'season': season,
      'year': now.year,
      'now': now.millisecondsSinceEpoch ~/ 1000,
      'week':
          now.subtract(const Duration(days: 7)).millisecondsSinceEpoch ~/ 1000,
      'start': start.millisecondsSinceEpoch ~/ 1000,
      'end': start.add(const Duration(days: 1)).millisecondsSinceEpoch ~/ 1000,
    });
    return HomeData(
      trending: _media((data['trending'] as Map?)?['media'], adult),
      season: _media((data['popular'] as Map?)?['media'], adult),
      recent: _airing((data['recent'] as Map?)?['airingSchedules'], adult),
      today: _airing((data['today'] as Map?)?['airingSchedules'], adult),
    );
  }

  Future<CatalogPage> search(
    String query, {
    required bool adult,
    int page = 1,
    String? format,
    String? season,
    int? year,
    String? genre,
    String? status,
  }) async {
    final data = await graph(
      '''query(\$q:String,\$adult:Boolean,\$page:Int,\$format:MediaFormat,\$season:MediaSeason,\$year:Int,\$genre:String,\$status:MediaStatus){Page(page:\$page,perPage:30){pageInfo{hasNextPage}media(search:\$q,type:ANIME,isAdult:\$adult,format:\$format,season:\$season,seasonYear:\$year,genre:\$genre,status:\$status,sort:${query.trim().isEmpty ? 'POPULARITY_DESC' : 'SEARCH_MATCH'}){$mediaFields}}}''',
      {
        'q': query.trim().isEmpty ? null : query.trim(),
        'adult': adult ? null : false,
        'page': page,
        'format': format,
        'season': season,
        'year': year,
        'genre': genre,
        if (status != 'ALL') 'status': status,
      },
    );
    final p = (data['Page'] as Map);
    return CatalogPage(
      _media(p['media'], adult),
      hasMore: (p['pageInfo'] as Map?)?['hasNextPage'] == true,
    );
  }

  Future<List<String>> genres({required bool adult}) async {
    final d = await graph('query {GenreCollection}', {});
    return (d['GenreCollection'] as List)
        .whereType<String>()
        .where((g) => adult || g != 'Hentai')
        .toList();
  }

  Future<Anime> detail(Anime anime, {required bool adult}) async {
    final d = await graph(
      '''query(\$id:Int,\$mal:Int,\$adult:Boolean){Media(id:\$id,idMal:\$mal,type:ANIME,isAdult:\$adult){$mediaFields
      studios(isMain:true){nodes{name}}
      streamingEpisodes{title thumbnail url site}
      relations{edges{relationType node{$mediaFields}}}
      recommendations(perPage:15,sort:RATING_DESC){nodes{mediaRecommendation{$mediaFields}}}
    }}''',
      {
        'id': anime.id > 0 ? anime.id : null,
        'mal': anime.id > 0 ? null : anime.malId,
        'adult': adult ? null : false,
      },
    );
    final result = Anime.fromJson((d['Media'] as Map).cast<String, dynamic>());
    if (!adult && result.isAdult) {
      throw const CatalogFailure('This title is unavailable.');
    }
    return result;
  }

  Future<List<Airing>> schedule(
    DateTime day, {
    required bool adult,
    int page = 1,
  }) async {
    final start = DateTime(day.year, day.month, day.day);
    final end = DateTime(day.year, day.month, day.day + 1);
    final d = await graph(
      '''query(\$from:Int,\$to:Int,\$page:Int){Page(page:\$page,perPage:50){airingSchedules(airingAt_greater:\$from,airingAt_lesser:\$to,sort:TIME){episode airingAt media{$mediaFields}}}}''',
      {
        'from': start.millisecondsSinceEpoch ~/ 1000,
        'to': end.millisecondsSinceEpoch ~/ 1000,
        'page': page,
      },
    );
    return _airing((d['Page'] as Map?)?['airingSchedules'], adult);
  }

  Future<AccountPage> publicList(
    String service,
    String username,
    String status, {
    required bool adult,
    int page = 1,
  }) async {
    if (service == 'MAL') return _malList(username, status, adult, page);
    final d = await graph(
      '''query(\$name:String){MediaListCollection(userName:\$name,type:ANIME){lists{entries{status progress media{$mediaFields}}}} User(name:\$name){avatar{medium}}}''',
      {'name': username},
    );
    final all = <String, ListEntry>{};
    for (final list
        in ((d['MediaListCollection'] as Map?)?['lists'] as List? ?? [])) {
      for (final e in (list as Map)['entries'] as List? ?? []) {
        final row = e as Map;
        if (row['media'] is! Map) continue;
        final a = Anime.fromJson((row['media'] as Map).cast<String, dynamic>());
        if ((adult || !a.isAdult) &&
            (status == 'ALL' || row['status'] == status)) {
          all[a.key] = ListEntry(
            a,
            row['status'] as String,
            number(row['progress']),
          );
        }
      }
    }
    final values = all.values.toList();
    return AccountPage(
      values.skip((page - 1) * 50).take(50).toList(),
      hasMore: values.length > page * 50,
      avatar:
          ((d['User'] as Map?)?['avatar'] as Map?)?['medium'] as String? ?? '',
    );
  }

  Future<AccountPage> _malList(
    String username,
    String status,
    bool adult,
    int page,
  ) async {
    final uri = Uri.https(
      'api.myanimelist.net',
      '/v2/users/$username/animelist',
      {
        if (status != 'ALL') 'status': status,
        'sort': 'list_updated_at',
        'limit': '50',
        'offset': '${(page - 1) * 50}',
        'nsfw': '$adult',
        'fields': 'list_status,num_episodes,media_type,mean,average_episode_duration,start_season,nsfw,rating,alternative_titles,synopsis,status,genres',
      },
    );
    final http.Response response;
    try {
      response = await client
          .get(
            uri,
            headers: {'X-MAL-CLIENT-ID': malId, 'Accept': 'application/json'},
          )
          .timeout(const Duration(seconds: 20));
    } catch (_) {
      throw const CatalogFailure(
        'Unable to reach MyAnimeList. Retry when connected.',
      );
    }
    if (response.statusCode != 200) {
      throw const CatalogFailure(
        'This MAL list is unavailable or private. Check the username.',
      );
    }
    final d = (jsonDecode(response.body) as Map).cast<String, dynamic>();
    final rows = <ListEntry>[];
    for (final item in d['data'] as List? ?? []) {
      final node = (item['node'] as Map).cast<String, dynamic>();
      final list = (item['list_status'] as Map?) ?? {};
      final anime = Anime.fromMal(node);
      if (!adult && (anime.isAdult || node['nsfw'] == 'black')) continue;
      rows.add(
        ListEntry(
          anime,
          list['status'] as String? ?? status,
          number(list['num_episodes_watched']),
        ),
      );
    }
    return AccountPage(rows, hasMore: (d['paging'] as Map?)?['next'] != null);
  }

  Future<List<SkipSpan>> skipTimes(
    int malId,
    int episode,
    double duration,
  ) async {
    if (malId <= 0 || duration <= 0) return [];
    try {
      final uri = Uri.https(
        'api.aniskip.com',
        '/v2/skip-times/$malId/$episode',
        {
          'types': ['op', 'ed', 'recap'],
          'episodeLength': '${duration.round()}',
        },
      );
      final r = await client.get(uri).timeout(const Duration(seconds: 6));
      if (r.statusCode != 200) return [];
      final data = jsonDecode(r.body) as Map;
      final out = <SkipSpan>[];
      for (final e in data['results'] as List? ?? []) {
        final length = (e['episodeLength'] as num?)?.toDouble() ?? 0;
        final span = (e['interval'] as Map?) ?? {};
        final start = (span['startTime'] as num?)?.toDouble() ?? -1;
        final end = (span['endTime'] as num?)?.toDouble() ?? -1;
        if ((length - duration).abs() <= 20 &&
            start >= 0 &&
            end > start &&
            end <= duration) {
          out.add(SkipSpan(e['skipType'] as String, start, end));
        }
      }
      return out;
    } catch (_) {
      return [];
    }
  }

  Future<bool> isFiller(int malId, int episode) async {
    if (malId <= 0) return false;
    try {
      final r = await client
          .get(Uri.https('api.jikan.moe', '/v4/anime/$malId/episodes/$episode'))
          .timeout(const Duration(seconds: 5));
      return r.statusCode == 200 &&
          (jsonDecode(r.body)['data'] as Map?)?['filler'] == true;
    } catch (_) {
      return false;
    }
  }

  Future<String> updateStatus() async {
    try {
      final r = await client
          .get(
            Uri.https(
              'api.github.com',
              '/repos/TheXykril/otakase/releases/latest',
            ),
            headers: {'Accept': 'application/vnd.github+json'},
          )
          .timeout(const Duration(seconds: 10));
      if (r.statusCode == 404) {
        return 'No Android release has been published yet.';
      }
      if (r.statusCode != 200) return 'Unable to check releases right now.';
      final j = jsonDecode(r.body) as Map;
      final apk = (j['assets'] as List? ?? []).where(
        (a) => (a['name'] as String).endsWith('.apk'),
      );
      return apk.isEmpty
          ? 'No Android APK in the latest release.'
          : 'Android release available: ${j['tag_name']}. Visit the Otakase repository to install.';
    } catch (_) {
      return 'Unable to check releases while offline.';
    }
  }
}
