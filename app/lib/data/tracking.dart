import 'dart:async';
import 'dart:math';

import 'package:flutter/foundation.dart';

import 'auth.dart';
import 'catalog.dart';
import 'models.dart';
import 'synced_resume.dart';

const anilistStatuses = {
  'ALL': 'All titles',
  'CURRENT': 'Watching',
  'PLANNING': 'Planning',
  'COMPLETED': 'Completed',
  'PAUSED': 'Paused',
  'DROPPED': 'Dropped',
  'REPEATING': 'Repeating',
};
const malStatuses = {
  'ALL': 'All titles',
  'watching': 'Watching',
  'plan_to_watch': 'Planning',
  'completed': 'Completed',
  'on_hold': 'On hold',
  'dropped': 'Dropped',
};
String malStatus(String status) =>
    const {
      'CURRENT': 'watching',
      'PLANNING': 'plan_to_watch',
      'COMPLETED': 'completed',
      'PAUSED': 'on_hold',
      'DROPPED': 'dropped',
      'REPEATING': 'watching',
    }[status] ??
    status;

String anilistStatus(String status) =>
    const {
      'watching': 'CURRENT',
      'plan_to_watch': 'PLANNING',
      'completed': 'COMPLETED',
      'on_hold': 'PAUSED',
      'dropped': 'DROPPED',
    }[status] ??
    status;

List<ListEntry> mergeTrackerLists(
  List<ListEntry> anilist,
  List<ListEntry> mal,
) {
  final remaining = {for (final e in mal) e.anime.malId: e};
  final merged = <ListEntry>[];
  ListEntry normalized(ListEntry e) => ListEntry(
    e.anime,
    e.isRepeating ? 'REPEATING' : anilistStatus(e.status),
    e.progress,
    notes: e.notes,
    score: e.score,
    isRepeating: e.isRepeating,
  );
  for (final ani in anilist) {
    final other = ani.anime.malId > 0
        ? remaining.remove(ani.anime.malId)
        : null;
    if (other == null) {
      merged.add(ani);
      continue;
    }
    final winner = other.progress > ani.progress ? normalized(other) : ani;
    merged.add(
      ListEntry(
        ani.anime,
        winner.status,
        winner.progress,
        notes: ani.notes,
        score: ani.score > 0 ? ani.score : other.score,
        isRepeating: winner.status == 'REPEATING',
      ),
    );
  }
  merged.addAll(remaining.values.map(normalized));
  return merged;
}

class CombinedAccountPage {
  const CombinedAccountPage(this.entries, {this.notLoaded = const []});
  final List<ListEntry> entries;
  final List<String> notLoaded;
}

class TrackingService extends ChangeNotifier {
  TrackingService(this.auth);
  final AuthService auth;
  String? lastError;
  int revision = 0;
  final Map<String, Future<void>> _writes = {};
  final Map<String, Anime> _shows = {};
  final Map<String, ListEntry?> _entries = {};
  String _entryKey(String service, Anime anime) =>
      service == 'MAL' ? 'MAL:mal:${anime.malId}' : 'AniList:${anime.key}';
  ListEntry? cached(String service, Anime anime) =>
      _entries[_entryKey(service, anime)];

  Future<CombinedAccountPage> combinedList({required bool adult}) async {
    final pages = <String, AccountPage>{};
    await Future.wait(
      ['AniList', 'MAL'].map((service) async {
        try {
          pages[service] = await list(service, adult: adult);
        } catch (_) {
          // Each tracker can still supply the list when the other is down.
        }
      }),
    );
    return CombinedAccountPage(
      mergeTrackerLists(
        pages['AniList']?.entries ?? [],
        pages['MAL']?.entries ?? [],
      ),
      notLoaded: [
        for (final service in ['AniList', 'MAL'])
          if (!pages.containsKey(service)) service,
      ],
    );
  }

  Future<AccountPage> list(String service, {required bool adult}) async {
    final rows = <String, ListEntry>{};
    if (service == 'AniList') {
      final account = auth.account(service)!;
      final d = await auth.graph(
        '''query(\$id:Int){MediaListCollection(userId:\$id,type:ANIME){lists{entries{status progress notes score(format:POINT_100) media{$mediaFields}}}}}''',
        {'id': account.userId},
      );
      for (final group
          in (d['MediaListCollection'] as Map?)?['lists'] as List? ?? []) {
        for (final e in (group as Map)['entries'] as List? ?? []) {
          final a = Anime.fromJson((e['media'] as Map).cast<String, dynamic>());
          if (!adult && a.isAdult) continue;
          final entry = ListEntry(
            a,
            e['status'] as String,
            number(e['progress']),
            notes: e['notes'] as String? ?? '',
            score: (e['score'] as num? ?? 0).toDouble(),
          );
          rows[a.key] = entry;
          _entries[_entryKey(service, a)] = entry;
        }
      }
    } else {
      for (var offset = 0; ; offset += 100) {
        final d = await auth.mal('GET', '/users/@me/animelist', {
          'limit': '100',
          'offset': '$offset',
          'sort': 'list_updated_at',
          'nsfw': '$adult',
          'fields': 'list_status{comments,score,is_rewatching},num_episodes,media_type,mean,average_episode_duration,start_season,nsfw,rating,alternative_titles,synopsis,status,genres',
        });
        for (final e in d['data'] as List? ?? []) {
          final a = Anime.fromMal((e['node'] as Map).cast<String, dynamic>());
          if (!adult && a.isAdult) continue;
          final s = (e['list_status'] as Map?) ?? {};
          final entry = ListEntry(
            a,
            s['status'] as String? ?? 'watching',
            number(s['num_episodes_watched']),
            notes: s['comments'] as String? ?? '',
            score: number(s['score']) * 10.0,
            isRepeating: s['is_rewatching'] == true,
          );
          rows[a.key] = entry;
          _entries[_entryKey(service, a)] = entry;
        }
        if ((d['paging'] as Map?)?['next'] == null) break;
      }
    }
    return AccountPage(
      rows.values.toList(),
      avatar: auth.account(service)!.avatar,
    );
  }

  Future<ListEntry?> entry(String service, Anime anime) async {
    ListEntry? result;
    if (service == 'AniList' && anime.id > 0) {
      final d = await auth.graph(
        '''query(\$id:Int){Media(id:\$id,type:ANIME){mediaListEntry{status progress notes score(format:POINT_100)}}}''',
        {'id': anime.id},
      );
      final e = (d['Media'] as Map?)?['mediaListEntry'] as Map?;
      if (e != null) {
        result = ListEntry(
          anime,
          e['status'] as String,
          number(e['progress']),
          notes: e['notes'] as String? ?? '',
          score: (e['score'] as num? ?? 0).toDouble(),
        );
      }
    } else if (service == 'MAL' && anime.malId > 0) {
      final d = await auth.mal('GET', '/anime/${anime.malId}', {
        'fields': 'my_list_status{comments,score,is_rewatching}',
      });
      final e = d['my_list_status'] as Map?;
      if (e != null) {
        result = ListEntry(
          anime,
          e['status'] as String? ?? 'watching',
          number(e['num_episodes_watched']),
          notes: e['comments'] as String? ?? '',
          score: number(e['score']) * 10.0,
          isRepeating: e['is_rewatching'] == true,
        );
      }
    }
    _entries[_entryKey(service, anime)] = result;
    return result;
  }

  Iterable<String> services(Anime anime) => auth.accounts.keys
      .where((s) => s == 'AniList' ? anime.id > 0 : anime.malId > 0)
      .toList();
  Future<void> _each(Anime anime, Future<void> Function(String) action) async {
    if (!auth.hasAccounts) return;
    final supported = services(anime).toList();
    final failures = auth.accounts.keys
        .where((s) => !supported.contains(s))
        .toList();
    await Future.wait(
      services(anime).map((service) async {
        try {
          await action(service);
        } catch (_) {
          failures.add(service);
        }
      }),
    );
    revision++;
    lastError = failures.isEmpty
        ? null
        : '${failures.join(' and ')} sync failed. Check Accounts and retry.';
    notifyListeners();
    if (lastError != null) throw LoginFailure(lastError!);
  }

  Future<void> _serial(Anime anime, Future<void> Function() action) {
    final previous = _writes[anime.key] ?? Future.value();
    final next = previous.catchError((Object _) {}).then((_) => action());
    _writes[anime.key] = next;
    return next;
  }

  Future<void> _save(
    String service,
    Anime anime, {
    String? status,
    int? progress,
    double? score,
    String? notes,
  }) async {
    if (service == 'AniList') {
      await auth.graph(
        '''mutation(\$id:Int,\$status:MediaListStatus,\$progress:Int,\$score:Int,\$notes:String){SaveMediaListEntry(mediaId:\$id,status:\$status,progress:\$progress,scoreRaw:\$score,notes:\$notes){id}}''',
        {
          'id': anime.id,
          'status': ?status,
          'progress': ?progress,
          if (score != null) 'score': score.round(),
          'notes': ?notes,
        },
      );
    } else {
      await auth.mal('PUT', '/anime/${anime.malId}/my_list_status', {
        if (status != null) 'status': malStatus(status),
        if (status == 'REPEATING') 'is_rewatching': 'true',
        if (status != null && status != 'REPEATING') 'is_rewatching': 'false',
        if (progress != null) 'num_watched_episodes': '$progress',
        if (score != null) 'score': '${(score / 10).round().clamp(0, 10)}',
        'comments': ?notes,
      });
    }
    final previous = cached(service, anime);
    _entries[_entryKey(service, anime)] = ListEntry(
      anime,
      status == null
          ? previous?.status ?? (service == 'MAL' ? 'watching' : 'CURRENT')
          : service == 'MAL'
          ? malStatus(status)
          : status,
      progress ?? previous?.progress ?? 0,
      notes: notes ?? previous?.notes ?? '',
      score: score ?? previous?.score ?? 0,
      isRepeating: status == null
          ? previous?.isRepeating ?? false
          : status == 'REPEATING',
    );
  }

  Future<void> update(
    Anime anime, {
    String? status,
    int? progress,
    double? score,
  }) => _serial(
    anime,
    () => _each(
      anime,
      (s) => _save(s, anime, status: status, progress: progress, score: score),
    ),
  );

  Future<SyncedResume> resume(Anime anime) async {
    var best = noResume;
    await Future.wait(
      services(anime).map((s) async {
        try {
          final e = await entry(s, anime).timeout(const Duration(seconds: 4));
          final p = parseSyncedResumeTag(e?.notes ?? '');
          if (p != null) best = laterSyncedResume(best, p);
        } catch (_) {
          /* A tracker lookup never blocks playback. */
        }
      }),
    );
    return best;
  }

  int progress(Anime anime) =>
      services(anime)
          .fold(0, (p, s) => max(p, cached(s, anime)?.progress ?? 0));

  Future<void> pushResume(Anime anime, SyncedResume p) => _serial(
    anime,
    () => _each(anime, (s) async {
      final e = await entry(s, anime);
      if (e == null) return;
      final notes = withSyncedResumeTag(e.notes, p);
      if (notes != e.notes) await _save(s, anime, notes: notes);
    }),
  );
  Future<void> finish(Anime anime, int episode, int total) => _serial(
    anime,
    () => _each(anime, (s) async {
      final e = await entry(s, anime);
      final progress = max(episode, e?.progress ?? 0);
      final complete = total > 0 && progress >= total;
      await _save(
        s,
        anime,
        progress: progress,
        status: complete
            ? 'COMPLETED'
            : (s == 'MAL'
                  ? const {
                          'plan_to_watch': 'CURRENT',
                          'watching': 'CURRENT',
                          'on_hold': 'CURRENT',
                          'dropped': 'CURRENT',
                          'completed': 'COMPLETED',
                        }[e?.status] ??
                        'CURRENT'
                  : e?.status == 'REPEATING'
                  ? 'REPEATING'
                  : e?.status == 'COMPLETED'
                  ? 'COMPLETED'
                  : 'CURRENT'),
        notes: syncedResumeAfterEpisode(e?.notes ?? '', episode, total),
      );
    }),
  );
  ResumePusher pusher() => ResumePusher(
    push: (key, p) => pushResume(_shows[key]!, p),
    finish: (key, ep, total) => finish(_shows[key]!, ep, total),
    onError: (e) {
      lastError = e.toString();
      notifyListeners();
    },
  );
  void remember(Anime anime) => _shows[anime.key] = anime;
}
