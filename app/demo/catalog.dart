// The shows the website demo and the site renders use: the ones in
// test/fixtures/site/shows.json, with a made-up watch history.
import 'package:otakase/data/catalog.dart';
import 'package:otakase/data/models.dart';

/// The episode the leading show is paused in.
const resumeAt = 3;

/// [url] turns a picture's file name into the address it is loaded from.
Anime showFromJson(Map j, String Function(String) url) {
  final eps = (j['eps'] as List).cast<Map>();
  final total = j['episodes'] as int? ?? eps.length;
  return Anime(
    id: j['id'] as int,
    title: (j['title'] as String).replaceAll('`', '’').replaceAll("'", '’'),
    nativeTitle: j['native'] as String? ?? '',
    cover: url(j['cover'] as String),
    coverLarge: url(j['cover'] as String),
    banner: url(j['banner'] as String),
    description: j['desc'] as String? ?? '',
    format: j['format'] as String? ?? 'TV',
    status: total > eps.length ? 'RELEASING' : 'FINISHED',
    year: j['year'] as int? ?? 0,
    episodes: total,
    nextEpisode: total > eps.length && total < 500 ? eps.length + 1 : 0,
    duration: 24,
    score: ((j['score'] as int?) ?? 0) / 10,
    genres: (j['genres'] as List).cast<String>(),
    studio: j['studio'] as String? ?? '',
    episodeArt: [
      for (final e in eps)
        {
          'episode': e['n'],
          'title': e['t'],
          'thumbnail': url(e['img'] as String),
        },
    ],
  );
}

class DemoCatalog extends Catalog {
  DemoCatalog(this.shows, this.main, {super.client});
  final List<Anime> shows;
  final Anime main;
  List<Anime> get others => shows.where((s) => s.id != main.id).toList();
  @override
  Future<HomeData> home({required bool adult}) async => HomeData(
    trending: [main, ...others],
    season: [...others.reversed, main],
    recent: [
      Airing(main, resumeAt, DateTime(2026, 11, 2, 19, 30)),
      for (final a in others) Airing(a, 5, DateTime(2026, 11, 2)),
    ],
  );
  @override
  Future<CatalogPage> search(
    String query, {
    required bool adult,
    int page = 1,
    String? format,
    String? season,
    int? year,
    String? genre,
    String? status,
    String? sort,
  }) async => CatalogPage([main, ...others]);
  @override
  Future<List<String>> genres({required bool adult}) async => ['Action'];
  @override
  Future<Anime> detail(Anime anime, {required bool adult}) async {
    final a = shows.firstWhere((s) => s.id == anime.id, orElse: () => main);
    return Anime(
      id: a.id,
      title: a.title,
      nativeTitle: a.nativeTitle,
      titles: [if (a.nativeTitle.isNotEmpty) a.nativeTitle],
      cover: a.cover,
      coverLarge: a.coverLarge,
      banner: a.banner,
      description: a.description,
      format: a.format,
      status: a.status,
      year: a.year,
      episodes: a.episodes,
      nextEpisode: a.nextEpisode,
      duration: 24,
      score: a.score,
      genres: a.genres,
      studio: a.studio,
      episodeArt: a.episodeArt,
      related: others.take(3).toList(),
      recommended: others.skip(3).toList(),
    );
  }

  @override
  Future<List<Airing>> schedule(
    DateTime day, {
    required bool adult,
    int page = 1,
  }) async => [Airing(main, resumeAt, day)];
}
