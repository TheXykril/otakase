import 'package:html/parser.dart' as html;

typedef Json = Map<String, dynamic>;
String plainText(String? value) =>
    html.parse(value ?? '').body?.text.trim() ?? '';
int number(dynamic value) => value is num ? value.toInt() : 0;

class Anime {
  const Anime({
    required this.id,
    required this.title,
    this.malId = 0,
    this.nativeTitle = '',
    this.titles = const [],
    this.cover = '',
    this.banner = '',
    this.coverLarge = '',
    this.description = '',
    this.format = '',
    this.status = '',
    this.year = 0,
    this.episodes = 0,
    this.duration = 0,
    this.score = 0,
    this.isAdult = false,
    this.nextEpisode = 0,
    this.nextAiringAt = 0,
    this.genres = const [],
    this.studio = '',
    this.episodeArt = const [],
    this.related = const [],
    this.recommended = const [],
  });
  final int id, malId, year, episodes, duration, nextEpisode, nextAiringAt;
  final String nativeTitle;
  final String title,
      cover,
      coverLarge,
      banner,
      description,
      format,
      status,
      studio;
  final double score;
  final bool isAdult;
  final List<String> titles, genres;
  final List<Json> episodeArt;
  final List<Anime> related, recommended;
  String get key => id > 0 ? 'al:$id' : 'mal:$malId';
  int get airedEpisodes => nextEpisode > 0
      ? nextEpisode - 1
      : status == 'NOT_YET_RELEASED'
      ? 0
      : episodes;
  String get formatLabel => format.replaceAll('_', ' ');
  factory Anime.fromJson(Json j) {
    final t = (j['title'] as Map?)?.cast<String, dynamic>() ?? {};
    final names = [
      t['english'],
      t['romaji'],
      t['native'],
      ...(j['synonyms'] as List? ?? []),
    ].whereType<String>().where((s) => s.trim().isNotEmpty).toSet().toList();
    final next = (j['nextAiringEpisode'] as Map?) ?? {};
    final cover = (j['coverImage'] as Map?) ?? {};
    List<Anime> nested(dynamic list) => (list as List? ?? [])
        .whereType<Map>()
        .map((v) => Anime.fromJson(v.cast<String, dynamic>()))
        .toList();
    final related = ((j['relations'] as Map?)?['edges'] as List? ?? [])
        .whereType<Map>()
        .map((e) => e['node'])
        .whereType<Map>()
        .where((m) => m['type'] == null || m['type'] == 'ANIME')
        .toList();
    final recommended =
        ((j['recommendations'] as Map?)?['nodes'] as List? ?? [])
            .whereType<Map>()
            .map((e) => e['mediaRecommendation'])
            .whereType<Map>()
            .toList();
    final studios = ((j['studios'] as Map?)?['nodes'] as List? ?? [])
        .whereType<Map>();
    return Anime(
      nativeTitle: t['native'] as String? ?? '',
      id: number(j['id']),
      malId: number(j['idMal']),
      title: names.isEmpty ? 'Untitled' : names.first,
      titles: names,
      cover: (cover['large'] ?? cover['extraLarge'] ?? '') as String,
      coverLarge: (cover['extraLarge'] ?? cover['large'] ?? '') as String,
      banner: (j['bannerImage'] ?? '') as String,
      description: plainText(j['description'] as String?),
      format: j['format'] as String? ?? '',
      status: j['status'] as String? ?? '',
      year: number(j['seasonYear'] ?? (j['startDate'] as Map?)?['year']),
      episodes: number(j['episodes']),
      duration: number(j['duration']),
      score: number(j['averageScore']) / 10,
      isAdult: j['isAdult'] == true,
      nextEpisode: number(next['episode']),
      nextAiringAt: number(next['airingAt']),
      genres: (j['genres'] as List? ?? []).whereType<String>().toList(),
      studio: studios.isEmpty ? '' : studios.first['name'] as String? ?? '',
      episodeArt: (j['streamingEpisodes'] as List? ?? [])
          .whereType<Map>()
          .map((e) => e.cast<String, dynamic>())
          .toList(),
      related: nested(related),
      recommended: nested(recommended),
    );
  }
  factory Anime.fromMal(Json j) {
    final alternative = (j['alternative_titles'] as Map?) ?? {};
    final names = [
      j['title'],
      alternative['en'],
      alternative['ja'],
      ...(alternative['synonyms'] as List? ?? []),
    ].whereType<String>().where((s) => s.isNotEmpty).toSet().toList();
    return Anime(
      nativeTitle: alternative['ja'] as String? ?? '',
      id: 0,
      malId: number(j['id']),
      title: names.isEmpty ? 'Untitled' : names.first,
      titles: names,
      cover:
          ((j['main_picture'] as Map?)?['large'] ??
                  (j['main_picture'] as Map?)?['medium'] ??
                  '')
              as String,
      description: plainText(j['synopsis'] as String?),
      format: (j['media_type'] as String? ?? '').toUpperCase(),
      status: switch (j['status']) {
        'currently_airing' => 'RELEASING',
        'not_yet_aired' => 'NOT_YET_RELEASED',
        _ => 'FINISHED',
      },
      year: number((j['start_season'] as Map?)?['year']),
      episodes: number(j['num_episodes']),
      duration: number(j['average_episode_duration']) ~/ 60,
      score: (j['mean'] as num? ?? 0).toDouble(),
      isAdult: j['nsfw'] == 'black' || j['rating'] == 'rx',
      genres: (j['genres'] as List? ?? [])
          .whereType<Map>()
          .map((g) => g['name'] as String)
          .toList(),
    );
  }
  Json toJson() => {
    'id': id,
    'idMal': malId,
    'title': {
      'native': nativeTitle,
      'english': title,
      'romaji': titles.length > 1 ? titles[1] : title,
    },
    'synonyms': titles,
    'coverImage': {'large': cover, 'extraLarge': coverLarge},
    'bannerImage': banner,
    'description': description,
    'format': format,
    'status': status,
    'seasonYear': year,
    'episodes': episodes,
    'duration': duration,
    'averageScore': (score * 10).round(),
    'isAdult': isAdult,
    'nextAiringEpisode': nextEpisode > 0
        ? {'episode': nextEpisode, 'airingAt': nextAiringAt}
        : null,
    'genres': genres,
    'studios': {
      'nodes': [
        {'name': studio},
      ],
    },
    'streamingEpisodes': episodeArt,
  };
}

class Airing {
  const Airing(this.anime, this.episode, this.at);
  final Anime anime;
  final int episode;
  final DateTime at;
}

class CatalogPage {
  const CatalogPage(this.items, {this.hasMore = false});
  final List<Anime> items;
  final bool hasMore;
}

class HomeData {
  const HomeData({
    this.trending = const [],
    this.season = const [],
    this.recent = const [],
    this.today = const [],
  });
  final List<Anime> trending, season;
  final List<Airing> recent, today;
}

class ListEntry {
  const ListEntry(
    this.anime,
    this.status,
    this.progress, {
    this.notes = '',
    this.score = 0,
    this.isRepeating = false,
  });
  final String notes;
  final double score;
  final bool isRepeating;
  final Anime anime;
  final String status;
  final int progress;
}

class AccountPage {
  const AccountPage(this.entries, {this.hasMore = false, this.avatar = ''});
  final List<ListEntry> entries;
  final bool hasMore;
  final String avatar;
}

class WatchEntry {
  WatchEntry({
    required this.anime,
    this.episode = 1,
    this.position = 0,
    this.duration = 0,
    this.watched = const [],
    this.updated = 0,
  });
  final Anime anime;
  final int episode, position, duration, updated;
  final List<int> watched;
  factory WatchEntry.fromJson(Json j) => WatchEntry(
    anime: Anime.fromJson((j['anime'] as Map).cast<String, dynamic>()),
    episode: number(j['episode']),
    position: number(j['position']),
    duration: number(j['duration']),
    updated: number(j['updated']),
    watched: (j['watched'] as List? ?? []).map(number).toList(),
  );
  Json toJson() => {
    'anime': anime.toJson(),
    'episode': episode,
    'position': position,
    'duration': duration,
    'updated': updated,
    'watched': watched,
  };
}

class SkipSpan {
  const SkipSpan(this.type, this.start, this.end);
  final String type;
  final double start, end;
  bool contains(double seconds) => seconds >= start && seconds < end;
  String get label => switch (type) {
    'op' => 'Skip Intro',
    'recap' => 'Skip Recap',
    _ => 'Skip Ending',
  };
}

/// AniList reads an explicit null variable as a filter on null, so a search
/// with format: null finds nothing and Media(idMal: null) finds no show.
/// Unset variables are left out instead.
Json graphVariables(Json variables) =>
    Map.fromEntries(variables.entries.where((e) => e.value != null));
