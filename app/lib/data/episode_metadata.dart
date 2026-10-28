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

class EpisodeMetadata {
  EpisodeMetadata({required this.client, required this.log, required this.now});
  final http.Client client;
  final void Function(String) log;
  final DateTime Function() now;
  final _cache = <int, ({DateTime expires, List<Json> rows})>{};
  final _pending = <int, Future<List<Json>>>{};
  void clear() => _cache.clear();

  Future<List<Json>> load(int id, List<Json> primary, int count) async {
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
    if (id <= 0 || complete) return rows;
    final cached = _cache[id];
    if (cached != null && cached.expires.isAfter(now())) {
      return mergeEpisodeMetadata(rows, cached.rows);
    }
    final future = _pending[id] ??= _fetch(id);
    try {
      return mergeEpisodeMetadata(rows, await future);
    } finally {
      _pending.remove(id);
    }
  }

  Future<List<Json>> _fetch(int id) async {
    try {
      final response = await client
          .get(Uri.https('api.ani.zip', '/mappings', {'anilist_id': '$id'}))
          .timeout(const Duration(seconds: 8));
      if (response.statusCode != 200) {
        throw CatalogMetadataFailure('HTTP ${response.statusCode}');
      }
      final data = jsonDecode(response.body) as Map;
      final episodes = data['episodes'] as Map? ?? {};
      final rows = <Json>[];
      for (final e in episodes.entries) {
        final n = int.tryParse(e.key.toString());
        if (n == null || n <= 0 || n > 10000 || e.value is! Map) continue;
        final value = e.value as Map;
        rows.add({
          'episode': n,
          'title': (value['title'] as Map?)?['en'],
          'thumbnail': value['image'],
          'airdate': value['airdate'],
        });
      }
      _cache[id] = (expires: now().add(const Duration(hours: 6)), rows: rows);
      return rows;
    } catch (e) {
      log('Episode metadata al:$id: $e');
      final previous = _cache[id]?.rows ?? <Json>[];
      _cache[id] = (
        expires: now().add(const Duration(minutes: 1)),
        rows: previous,
      );
      return previous;
    }
  }
}

class CatalogMetadataFailure implements Exception {
  const CatalogMetadataFailure(this.message);
  final String message;
  @override
  String toString() => message;
}
