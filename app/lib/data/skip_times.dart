import 'dart:math';

import '../core.dart';
import 'models.dart';
import 'store.dart';

/// Opening, ending and recap times from every skip time service: AniSkip,
/// TheIntroDB and Anime-Skip, asked by the core at once. Times a viewer marks
/// go to AniSkip, where they help everyone.
class SkipTimes {
  SkipTimes(this.core, this.store);
  final CoreClient core;
  final AppStore store;

  /// Display names for the services.
  static const sourceNames = {
    'aniskip': 'AniSkip',
    'theintrodb': 'TheIntroDB',
    'anime-skip': 'Anime-Skip',
  };

  Future<List<SkipSpan>> lookup(
    Anime anime,
    int episode,
    double length, {
    RequestScope? scope,
  }) async {
    if (episode <= 0 || (anime.malId <= 0 && anime.id <= 0)) return [];
    try {
      final d = await core.request(
        {
          'operation': 'skiptimes_lookup',
          'mal_id': anime.malId,
          'anilist_id': anime.id,
          'episode': episode,
          'length': length,
          'introdb': store.flag('IntroDBSkipTimes'),
          'anime_skip': store.flag('AnimeSkipTimes') ? 'auto' : '',
        },
        scope: scope,
        timeout: const Duration(seconds: 30),
      );
      return [
        for (final s in (d['spans'] as List? ?? []).whereType<Map>())
          SkipSpan(
            s['type'] as String? ?? '',
            (s['start'] as num?)?.toDouble() ?? -1,
            (s['end'] as num?)?.toDouble() ?? -1,
            source: s['source'] as String? ?? '',
            id: s['id'] as String? ?? '',
          ),
      ].where((s) => s.end > s.start && s.start >= 0).toList();
    } catch (_) {
      return [];
    }
  }

  /// Opening, recap and credits of one series episode from TheIntroDB,
  /// which knows shows by their TMDB id.
  Future<List<SkipSpan>> lookupShow(
    int tmdb,
    int season,
    int episode,
    double length, {
    RequestScope? scope,
  }) async {
    if (tmdb <= 0 || episode <= 0 || !store.flag('IntroDBSkipTimes')) {
      return [];
    }
    try {
      final d = await core.request(
        {
          'operation': 'skiptimes_show',
          'tmdb_id': tmdb,
          'season': season,
          'episode': episode,
          'length': length,
        },
        scope: scope,
        timeout: const Duration(seconds: 30),
      );
      return [
        for (final s in (d['spans'] as List? ?? []).whereType<Map>())
          SkipSpan(
            s['type'] as String? ?? '',
            (s['start'] as num?)?.toDouble() ?? -1,
            (s['end'] as num?)?.toDouble() ?? -1,
            source: s['source'] as String? ?? '',
          ),
      ].where((s) => s.end > s.start && s.start >= 0).toList();
    } catch (_) {
      return [];
    }
  }

  /// The random id AniSkip groups this viewer's submissions under. Made once
  /// per profile; it says nothing about who the viewer is.
  Future<String> submitter() async {
    final saved = store.text('AniSkipSubmitter');
    if (RegExp(
      r'^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$',
    ).hasMatch(saved)) {
      return saved;
    }
    final id = newUuid(Random.secure());
    await store.set('AniSkipSubmitter', id);
    return id;
  }

  /// Sends a marked span. Throws with words for the viewer when AniSkip
  /// or the checks refuse it.
  Future<String> submit(
    Anime anime,
    int episode,
    String type,
    double start,
    double end,
    double length,
  ) async {
    final d = await core.request({
      'operation': 'skiptimes_submit',
      'mal_id': anime.malId,
      'episode': episode,
      'type': type,
      'start': start,
      'end': end,
      'length': length,
      'submitter': await submitter(),
    }, timeout: const Duration(seconds: 30));
    final voted = Set<String>.from(store.value('SkipVotes') as List? ?? []);
    final id = d['id'] as String? ?? '';
    // Your own times are counted as your vote already.
    if (id.isNotEmpty) await store.set('SkipVotes', [...voted, id]);
    return id;
  }

  bool voted(SkipSpan span) =>
      span.id.isNotEmpty &&
      (store.value('SkipVotes') as List? ?? []).contains(span.id);

  Future<void> vote(SkipSpan span, {required bool up}) async {
    if (span.id.isEmpty || voted(span)) return;
    await core.request({
      'operation': 'skiptimes_vote',
      'id': span.id,
      'up': up,
    }, timeout: const Duration(seconds: 20));
    final votes = List<String>.from(store.value('SkipVotes') as List? ?? []);
    // The newest 500 are enough to stop a second vote on a span still shown.
    await store.set(
      'SkipVotes',
      [...votes, span.id].reversed.take(500).toList(),
    );
  }
}

/// Selects markers for the current cut. If a complete pair cannot be
/// assembled, keep the single source with the most usable marker types.
List<SkipSpan> selectSkipSpans(
  List<SkipSpan> provider,
  List<SkipSpan> alternatives,
  double duration,
) {
  final sources = <String, Map<String, SkipSpan>>{};
  for (final span in [...provider, ...alternatives]) {
    if (!span.start.isFinite ||
        !span.end.isFinite ||
        span.start < 0 ||
        span.start >= span.end ||
        span.end > duration) {
      continue;
    }
    sources
        .putIfAbsent(span.source, () => {})
        .putIfAbsent(span.type, () => span);
  }
  final openings = sources.values.map((s) => s['op']).nonNulls;
  final endings = sources.values.map((s) => s['ed']).nonNulls;
  for (final op in openings) {
    for (final ed in endings) {
      if (op.end > ed.start) continue;
      final recap = sources.values
          .map((s) => s['recap'])
          .nonNulls
          .where(
            (span) =>
                (span.end <= op.start || span.start >= op.end) &&
                (span.end <= ed.start || span.start >= ed.end),
          )
          .firstOrNull;
      return [op, ed, ?recap]..sort((a, b) => a.start.compareTo(b.start));
    }
  }
  List<SkipSpan> best = [];
  for (final source in sources.values) {
    final op = source['op'];
    final ed = source['ed'];
    final usable = [
      for (final span in source.values)
        if (span != ed || op == null || op.end <= span.start) span,
    ];
    if (usable.length > best.length) best = usable;
  }
  return best..sort((a, b) => a.start.compareTo(b.start));
}

/// A random version 4 UUID.
String newUuid(Random random) {
  final b = List<int>.generate(16, (_) => random.nextInt(256));
  b[6] = (b[6] & 0x0f) | 0x40;
  b[8] = (b[8] & 0x3f) | 0x80;
  final h = b.map((v) => v.toRadixString(16).padLeft(2, '0')).join();
  return '${h.substring(0, 8)}-${h.substring(8, 12)}-${h.substring(12, 16)}-'
      '${h.substring(16, 20)}-${h.substring(20)}';
}
