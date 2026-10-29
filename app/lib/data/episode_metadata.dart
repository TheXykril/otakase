import 'dart:convert';

import 'package:http/http.dart' as http;

import 'models.dart';

int episodeNumber(Json episode) {
  final explicit = episode['episode'];
  final n = explicit is String ? int.tryParse(explicit) : number(explicit);
  if (n != null && n > 0) return n;
  final title = episode['title'] as String? ?? '';
  final match = RegExp(
    r'(?:\bEpisode\s*|\bEp\.?\s*|\bE\s*|^)(\d+)\b',
    caseSensitive: false,
  ).firstMatch(title);
  return int.tryParse(match?.group(1) ?? '') ?? 0;
}

bool _present(dynamic value) => value is String && value.trim().isNotEmpty;

List<Json> mergeEpisodeMetadata(List<Json> primary, List<Json> fallback) {
  final rows = <int, Json>{};
  for (final e in [...fallback, ...primary]) {
    final n = episodeNumber(e);
    if (n <= 0 || n > 10000) continue;
    final row = rows.putIfAbsent(n, () => {'episode': n});
    for (final key in ['title', 'thumbnail', 'airdate', 'url', 'site']) {
      if (_present(e[key])) row[key] = e[key];
    }
  }
  return (rows.keys.toList()..sort()).map((n) => rows[n]!).toList();
}

/// Episode titles and stills from the same public sources as the Discord
/// watch room, best first: AniList (passed in), Tenrai, Kitsu through AniMap,
/// ani.zip, then Jikan for titles only. A source only fills what the better
/// ones left empty.
class EpisodeMetadata {
  EpisodeMetadata({required this.client, required this.log, required this.now});
  final http.Client client;
  final void Function(String) log;
  final DateTime Function() now;
  final _cache = <String, ({DateTime expires, List<Json> rows})>{};
  final _pending = <String, Future<List<Json>>>{};
  void clear() => _cache.clear();

  Future<List<Json>> load(
    int id,
    List<Json> primary,
    int count, {
    int malId = 0,
  }) async {
    final rows = mergeEpisodeMetadata(primary, []);
    final byNumber = {for (final e in rows) episodeNumber(e): e};
    final complete =
        count > 0 &&
        rows.length >= count &&
        List.generate(count.clamp(0, 10000), (i) => i + 1).every(
          (n) =>
              _present(byNumber[n]?['title']) &&
              _present(byNumber[n]?['thumbnail']),
        );
    if ((id <= 0 && malId <= 0) || complete) return rows;
    final key = '$id:$malId';
    final cached = _cache[key];
    if (cached != null && cached.expires.isAfter(now())) {
      return mergeEpisodeMetadata(rows, cached.rows);
    }
    final future = _pending[key] ??= _fetchAll(key, id, malId);
    try {
      return mergeEpisodeMetadata(rows, await future);
    } finally {
      _pending.remove(key);
    }
  }

  Future<List<Json>> _fetchAll(String key, int id, int malId) async {
    final sources = await Future.wait([
      if (malId > 0) _source('Tenrai', () => _tenrai(malId)),
      if (malId > 0) _source('Kitsu', () => _kitsu(malId)),
      if (id > 0) _source('ani.zip', () => _aniZip(id)),
      if (malId > 0) _source('Jikan', () => _jikan(malId)),
    ]);
    if (sources.every((s) => s == null)) {
      final previous = _cache[key]?.rows ?? <Json>[];
      _cache[key] = (
        expires: now().add(const Duration(minutes: 1)),
        rows: previous,
      );
      return previous;
    }
    var rows = <Json>[];
    for (final source in sources.reversed) {
      rows = mergeEpisodeMetadata(source ?? [], rows);
    }
    _cache[key] = (expires: now().add(const Duration(hours: 6)), rows: rows);
    return rows;
  }

  /// One source's rows, or null when it failed. A show the source does not
  /// know is an empty answer, not a failure worth logging.
  Future<List<Json>?> _source(
    String name,
    Future<List<Json>> Function() fetch,
  ) async {
    try {
      return await fetch();
    } on _Missing {
      return const [];
    } catch (e) {
      log('Episode metadata $name: $e');
      return null;
    }
  }

  Future<dynamic> _get(Uri uri) async {
    final response = await client.get(uri).timeout(const Duration(seconds: 8));
    if (response.statusCode == 404) throw const _Missing();
    if (response.statusCode != 200) {
      throw CatalogMetadataFailure('HTTP ${response.statusCode}');
    }
    return jsonDecode(utf8.decode(response.bodyBytes));
  }

  static String? _image(dynamic value) {
    if (value is! String) return null;
    final uri = Uri.tryParse(value);
    return uri != null && uri.scheme == 'https' && uri.host.isNotEmpty
        ? value
        : null;
  }

  Future<List<Json>> _paged(
    String host,
    String path,
    Json? Function(Map e) row,
  ) async {
    final rows = <Json>[];
    for (var page = 1; page <= 4; page++) {
      final data = await _get(Uri.https(host, path, {'page': '$page'}));
      final items = (data as Map)['data'] as List? ?? [];
      for (final e in items.whereType<Map>()) {
        final r = row(e);
        if (r != null) rows.add(r);
      }
      if ((data['pagination'] as Map?)?['has_next_page'] != true ||
          items.isEmpty) {
        break;
      }
    }
    return rows;
  }

  Future<List<Json>> _tenrai(int malId) =>
      _paged('api.tenrai.org', '/v1/anime/$malId/episodes', (e) {
        final images = e['images'] as Map?;
        return {
          'episode': number(e['mal_id']),
          'title': e['title'],
          'thumbnail':
              _image((images?['jpg'] as Map?)?['image_url']) ??
              _image((images?['webp'] as Map?)?['image_url']) ??
              _image(e['image_url']),
        };
      });

  Future<List<Json>> _jikan(int malId) =>
      _paged('api.jikan.moe', '/v4/anime/$malId/episodes', (e) {
        final title = e['title'] as String? ?? '';
        return {
          'episode': number(e['mal_id']),
          'title': title.trim().isNotEmpty ? title : e['title_japanese'],
        };
      });

  Future<List<Json>> _kitsu(int malId) async {
    final mapping = await _get(Uri.https('animap.id', '/api/map/mal/$malId'));
    if (mapping is! List || mapping.length != 1) return [];
    final kitsu = (mapping.first as Map)['kitsu_id'] as List? ?? [];
    if (kitsu.length != 1 || number(kitsu.first) <= 0) return [];
    final data = await _get(
      Uri.https('animap.id', '/api/kitsu/${number(kitsu.first)}/episodes'),
    );
    return [
      // Kitsu absolute numbers are safer than relative season numbering.
      for (final e
          in ((data as Map)['episodes'] as List? ?? []).whereType<Map>())
        {
          'episode': number(e['number']),
          'title': _present(e['title_en_us'])
              ? e['title_en_us']
              : e['canonical_title'],
          'thumbnail': _image(e['thumbnail']),
        },
    ];
  }

  Future<List<Json>> _aniZip(int id) async {
    final data = await _get(
      Uri.https('api.ani.zip', '/mappings', {'anilist_id': '$id'}),
    );
    final episodes = (data as Map)['episodes'] as Map? ?? {};
    return [
      for (final e in episodes.entries)
        if (int.tryParse(e.key.toString()) case final n?
            when n > 0 && n <= 10000 && e.value is Map)
          {
            'episode': n,
            'title': ((e.value as Map)['title'] as Map?)?['en'],
            'thumbnail': _image((e.value as Map)['image']),
            'airdate': (e.value as Map)['airdate'],
          },
    ];
  }
}

class _Missing implements Exception {
  const _Missing();
}

class CatalogMetadataFailure implements Exception {
  const CatalogMetadataFailure(this.message);
  final String message;
  @override
  String toString() => message;
}
