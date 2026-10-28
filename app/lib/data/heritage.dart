import 'dart:async';

import 'package:flutter/foundation.dart';

import 'auth.dart';
import 'discord.dart';
import 'models.dart';
import 'store.dart';

const episodeMilestones = [50, 100, 250, 500, 1000, 2500, 5000, 10000];
({int play, int record}) milestoneToCelebrate(
  int total,
  int celebrated,
  bool seen,
) {
  final reached = episodeMilestones.where((m) => m <= total).lastOrNull ?? 0;
  if (!seen) return (play: 0, record: reached);
  if (reached > celebrated) return (play: reached, record: reached);
  return (play: 0, record: celebrated);
}

DateTime capsuleDate({
  DateTime? current,
  String override = const String.fromEnvironment('OTAKASE_CAPSULE_DATE'),
  bool debug = kDebugMode,
}) {
  final now = current ?? DateTime.now();
  if (debug && RegExp(r'^\d{4}-\d{2}-\d{2}$').hasMatch(override)) {
    final day = DateTime.tryParse(override);
    if (day != null &&
        '${day.year.toString().padLeft(4, '0')}-${day.month.toString().padLeft(2, '0')}-${day.day.toString().padLeft(2, '0')}' ==
            override) {
      return DateTime(
        day.year,
        day.month,
        day.day,
        now.hour,
        now.minute,
        now.second,
      );
    }
  }
  return now;
}

bool isBirthday(DateTime t) => t.month == 4 && t.day == 12;
int nextBirthday(DateTime t) =>
    t.month < 4 || t.month == 4 && t.day < 12 ? t.year : t.year + 1;

class WatchStats {
  const WatchStats({
    this.anime = 0,
    this.episodes = 0,
    this.minutes = 0,
    this.completed = 0,
    this.meanScore = 0,
    this.topGenre = '',
  });
  final int anime, episodes, minutes, completed;
  final double meanScore;
  final String topGenre;
  Json toJson() => {
    'anime': anime,
    'episodes': episodes,
    'minutes': minutes,
    'completed': completed,
    if (meanScore > 0) 'mean_score': meanScore,
    if (topGenre.isNotEmpty) 'top_genre': topGenre,
  };
  factory WatchStats.fromJson(Json j) => WatchStats(
    anime: number(j['anime']),
    episodes: number(j['episodes']),
    minutes: number(j['minutes']),
    completed: number(j['completed']),
    meanScore: (j['mean_score'] as num? ?? 0).toDouble(),
    topGenre: j['top_genre'] as String? ?? '',
  );
  factory WatchStats.anilist(Json j) => WatchStats(
    anime: number(j['count']),
    episodes: number(j['episodesWatched']),
    minutes: number(j['minutesWatched']),
    completed:
        (j['statuses'] as List? ?? [])
            .whereType<Map>()
            .where((s) => s['status'] == 'COMPLETED')
            .map((s) => number(s['count']))
            .firstOrNull ??
        0,
    meanScore: (j['meanScore'] as num? ?? 0).toDouble(),
    topGenre:
        ((j['genres'] as List? ?? []).firstOrNull as Map?)?['genre']
            as String? ??
        '',
  );
  factory WatchStats.mal(Json j) => WatchStats(
    anime: number(j['num_items']),
    episodes: number(j['num_episodes']),
    minutes: ((j['num_days_watched'] as num? ?? 0) * 24 * 60).round(),
    completed: number(j['num_items_completed']),
    meanScore: (j['mean_score'] as num? ?? 0).toDouble(),
  );
}

class StatsRepository {
  StatsRepository(this.auth);
  final AuthService auth;
  Future<int> totalEpisodes() async {
    if (auth.signedIn('AniList')) {
      try {
        final r = await auth.graph(
          'query { Viewer { statistics { anime { episodesWatched } } } }',
          {},
        );
        return number(r['Viewer']?['statistics']?['anime']?['episodesWatched']);
      } catch (_) {
        if (!auth.signedIn('MAL')) rethrow;
      }
    }
    final r = await auth.mal('GET', '/users/@me', {
      'fields': 'anime_statistics',
    });
    return number(r['anime_statistics']?['num_episodes']);
  }

  Future<WatchStats?> snapshot() async {
    if (!auth.hasAccounts) return null;
    try {
      return await _snapshot().timeout(const Duration(seconds: 8));
    } catch (_) {
      return null;
    }
  }

  Future<WatchStats> _snapshot() async {
    if (auth.signedIn('AniList')) {
      try {
        final r = await auth.graph(
          'query { Viewer { statistics { anime { count episodesWatched minutesWatched meanScore genres(limit:3,sort:COUNT_DESC){genre count} statuses(sort:COUNT_DESC){status count} } } } }',
          {},
        );
        return WatchStats.anilist(
          (r['Viewer']['statistics']['anime'] as Map).cast<String, dynamic>(),
        );
      } catch (_) {
        if (!auth.signedIn('MAL')) rethrow;
      }
    }
    final r = await auth.mal('GET', '/users/@me', {
      'fields': 'anime_statistics',
    });
    return WatchStats.mal(
      (r['anime_statistics'] as Map).cast<String, dynamic>(),
    );
  }
}

class Heritage {
  Heritage(this.store, this.stats, this.discord, {DateTime Function()? now})
    : now = now ?? capsuleDate;
  final AppStore store;
  final StatsRepository stats;
  final DiscordService discord;
  final DateTime Function() now;
  bool progressWritten = false, closed = false;
  Json get capsule =>
      (store.value('TimeCapsule') as Map? ?? {}).cast<String, dynamic>();
  List<Json> get letters => (capsule['letters'] as List? ?? [])
      .whereType<Map>()
      .map((l) => l.cast<String, dynamic>())
      .toList();
  List<int> dueLetters(int year) => [
    for (var i = 0; i < letters.length; i++)
      if (letters[i]['opened'] != true && number(letters[i]['opens']) <= year)
        i,
  ];
  Json? get pending => letters
      .where(
        (l) => l['opened'] != true && number(l['opens']) == nextBirthday(now()),
      )
      .firstOrNull;
  Future<void> _save(Json value) async {
    if (!closed) await store.set('TimeCapsule', value);
  }

  Future<void> writeLetter(String text, {WatchStats? snapshot}) async {
    text = clipRunes(text.trim(), 500);
    if (text.isEmpty || closed) return;
    final opens = nextBirthday(now());
    final rows = letters;
    final row = <String, dynamic>{
      'text': text,
      'written': now().toUtc().toIso8601String(),
      'opens': opens,
      if (snapshot != null) 'stats': snapshot.toJson(),
    };
    final index = rows.indexWhere(
      (l) => l['opened'] != true && number(l['opens']) == opens,
    );
    if (index >= 0) {
      rows[index] = row;
    } else {
      rows.add(row);
    }
    await _save({...capsule, 'letters': rows});
  }

  Future<void> markOpened(int index) async {
    final rows = letters;
    if (index < 0 || index >= rows.length) return;
    rows[index] = {...rows[index], 'opened': true};
    await _save({...capsule, 'letters': rows});
  }

  Future<void> markOffered(int year) => _save({...capsule, 'offered': year});
  Future<Json?> birthdayCommunity(int year) async {
    if (number(capsule['community_shown']) >= year) return null;
    Json? note;
    try {
      note = await discord.request('GET', '/v1/capsule', authenticated: false);
    } on BotFailure catch (e) {
      if (e.status != 404) rethrow;
    }
    await _save({...capsule, 'community_shown': year});
    return note;
  }

  Future<Json?> communityMine() async {
    try {
      return await discord.request('GET', '/v1/capsule/mine');
    } on BotFailure catch (e) {
      if (e.status == 404) return null;
      rethrow;
    }
  }

  Future<Json> writeCommunity(String text) => discord.request(
    'POST',
    '/v1/capsule',
    body: {'text': clipRunes(text.trim(), 300)},
  );
  Future<void> removeCommunity() async {
    await discord.request('DELETE', '/v1/capsule');
  }

  Future<void> reportCommunity(String id) async {
    await discord.request('POST', '/v1/capsule/report', body: {'id': id});
  }

  Future<int> milestoneOnExit() async {
    if (!progressWritten || !stats.auth.hasAccounts || closed) return 0;
    try {
      final total = await stats.totalEpisodes().timeout(
        const Duration(seconds: 4),
      );
      if (closed) return 0;
      final old = store.value('Milestone');
      final decision = milestoneToCelebrate(total, number(old), old is int);
      if (old == null || decision.record != old) {
        await store.set('Milestone', decision.record);
      }
      return decision.play;
    } catch (_) {
      return 0;
    }
  }
}
